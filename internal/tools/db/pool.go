package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/driver"
	"example.com/mcp-server/internal/tools/db/sshtunnel"
)

type Source struct {
	Environment  string
	Key          string
	Write        bool
	DB           *sql.DB
	Driver       driver.Driver
	QueryTimeout time.Duration
}

type Pool struct {
	sources map[string]map[string]*Source
	ordered []*Source
	tunnels []*sshtunnel.Tunnel
}

type configuredSource struct {
	environment string
	key         string
	config      config.SourceConfig
	driver      driver.Driver
}

func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	p := &Pool{sources: map[string]map[string]*Source{}}

	var configured []configuredSource
	environments := make([]string, 0, len(cfg.Environments))
	for environment := range cfg.Environments {
		environments = append(environments, environment)
	}
	sort.Strings(environments)
	for _, environment := range environments {
		env := cfg.Environments[environment]
		keys := make([]string, 0, len(env.Databases))
		for key := range env.Databases {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			src := env.Databases[key]
			drv, ok := driver.Get(src.Driver)
			if !ok {
				return nil, fmt.Errorf("environments.%s.databases.%s: driver %q 未注册, 已注册的驱动: %v", environment, key, src.Driver, driver.Names())
			}
			configured = append(configured, configuredSource{environment: environment, key: key, config: src, driver: drv})
		}
	}

	for _, item := range configured {
		environment, key, src, drv := item.environment, item.key, item.config, item.driver
		configPath := fmt.Sprintf("environments.%s.databases.%s", environment, key)

		dsnSource := src
		if src.SSH != nil {
			targetPort := src.Port
			if targetPort == 0 {
				targetPort = 3306
			}
			tunnel, tunnelErr := sshtunnel.Open(ctx, *src.SSH, net.JoinHostPort(src.Host, strconv.Itoa(targetPort)))
			if tunnelErr != nil {
				p.closeAll()
				return nil, fmt.Errorf("%s: SSH tunnel failed: %w", configPath,
					scrubError(tunnelErr, src.SSH.Password, src.SSH.PrivateKeyPassphrase))
			}
			p.tunnels = append(p.tunnels, tunnel)

			localHost, localPort, splitErr := net.SplitHostPort(tunnel.LocalAddr())
			if splitErr != nil {
				p.closeAll()
				return nil, fmt.Errorf("%s: invalid SSH tunnel address: %w", configPath, splitErr)
			}
			dsnSource.Host = localHost
			dsnSource.Port, _ = strconv.Atoi(localPort)
		}

		dsn, err := drv.BuildDSN(dsnSource)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("%s: 组装 DSN 失败: %w", configPath, err)
		}

		db, err := sql.Open(drv.SQLDriverName(), dsn)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("%s: sql.Open 失败: %w", configPath, err)
		}

		db.SetMaxOpenConns(src.Pool.MaxConnections)
		db.SetMaxIdleConns(src.Pool.MinConnections)
		db.SetConnMaxIdleTime(src.Pool.MaxIdleTime)

		pingCtx, cancel := context.WithTimeout(ctx, src.Pool.ConnectTimeout)
		pingErr := db.PingContext(pingCtx)
		cancel()
		if pingErr != nil {
			_ = db.Close()
			p.closeAll()
			return nil, fmt.Errorf("%s: Ping 失败: %w", configPath, scrubError(pingErr, src.Password))
		}

		source := &Source{
			Environment:  environment,
			Key:          key,
			Write:        src.Write,
			DB:           db,
			Driver:       drv,
			QueryTimeout: cfg.Defaults.QueryTimeout,
		}
		if p.sources[environment] == nil {
			p.sources[environment] = map[string]*Source{}
		}
		p.sources[environment][key] = source
		p.ordered = append(p.ordered, source)

		port := src.Port
		if port == 0 {
			port = 3306
		}
		fmt.Fprintf(os.Stderr, "[mcp-server] source loaded: environment=%s key=%s driver=%s write=%t host=%s:%d database=%s via_ssh=%t\n",
			environment, key, src.Driver, src.Write, src.Host, port, src.Database, src.SSH != nil)
	}

	return p, nil
}

func (p *Pool) Get(environment, key string) (*Source, error) {
	if sources, ok := p.sources[environment]; ok {
		if src, ok := sources[key]; ok {
			return src, nil
		}
	}
	return nil, fmt.Errorf("database source %q not found in environment %q, available: %v", key, environment, p.SourceNames())
}

func (p *Pool) SourceNames() []string {
	out := make([]string, 0, len(p.ordered))
	for _, src := range p.ordered {
		out = append(out, src.Environment+"/"+src.Key)
	}
	return out
}

func (p *Pool) HasWritableSources() bool {
	for _, src := range p.ordered {
		if src.Write {
			return true
		}
	}
	return false
}

func (s *Source) auditMode() string {
	if s.Write {
		return "rw"
	}
	return "r"
}

func (p *Pool) Close() error {
	var errs []error
	for _, src := range p.ordered {
		if err := src.DB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, tunnel := range p.tunnels {
		if err := tunnel.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Pool) closeAll() {
	for _, src := range p.ordered {
		_ = src.DB.Close()
	}
	for _, tunnel := range p.tunnels {
		_ = tunnel.Close()
	}
}

// scrubError 确保返回的错误信息不包含密码或口令明文。
// 即使底层驱动的错误偶尔回显 DSN，我们也做一次兜底替换。
func scrubError(err error, secrets ...string) error {
	if err == nil {
		return err
	}
	msg := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	return errors.New(msg)
}
