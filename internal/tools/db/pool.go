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
	"sync"
	"time"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/driver"
	"example.com/mcp-server/internal/tools/db/sshtunnel"
)

type Source struct {
	Environment  string
	Key          string
	Write        bool
	Driver       driver.Driver
	QueryTimeout time.Duration

	config config.SourceConfig
	db     *sql.DB
	tunnel *sshtunnel.Tunnel
	mu     sync.Mutex
}

type Pool struct {
	sources map[string]map[string]*Source
	ordered []*Source
}

type configuredSource struct {
	environment string
	key         string
	config      config.SourceConfig
	driver      driver.Driver
}

func NewPool(_ context.Context, cfg *config.Config) (*Pool, error) {
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

		source := &Source{
			Environment:  environment,
			Key:          key,
			Write:        src.Write,
			Driver:       drv,
			QueryTimeout: cfg.Defaults.QueryTimeout,
			config:       src,
		}
		db, err := openDatabase(drv, src)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("%s: %w", configPath, err)
		}
		if src.SSH == nil {
			source.db = db
		} else {
			_ = db.Close()
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
		fmt.Fprintf(os.Stderr, "[mcp-server] source configured: environment=%s key=%s driver=%s write=%t host=%s:%d database=%s via_ssh=%t\n",
			environment, key, src.Driver, src.Write, src.Host, port, src.Database, src.SSH != nil)
	}

	return p, nil
}

func (s *Source) database(ctx context.Context) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db, nil
	}

	dsnSource := s.config
	if dsnSource.SSH != nil {
		targetPort := dsnSource.Port
		if targetPort == 0 {
			targetPort = 3306
		}
		tunnel, err := sshtunnel.Open(ctx, *dsnSource.SSH, net.JoinHostPort(dsnSource.Host, strconv.Itoa(targetPort)))
		if err != nil {
			return nil, fmt.Errorf("SSH tunnel failed: %w",
				scrubError(err, dsnSource.SSH.Password, dsnSource.SSH.PrivateKeyPassphrase))
		}

		localHost, localPort, err := net.SplitHostPort(tunnel.LocalAddr())
		if err != nil {
			_ = tunnel.Close()
			return nil, fmt.Errorf("invalid SSH tunnel address: %w", err)
		}
		dsnSource.Host = localHost
		dsnSource.Port, _ = strconv.Atoi(localPort)
		s.tunnel = tunnel
	}

	db, err := openDatabase(s.Driver, dsnSource)
	if err != nil {
		if s.tunnel != nil {
			_ = s.tunnel.Close()
			s.tunnel = nil
		}
		return nil, err
	}
	s.db = db
	return db, nil
}

func openDatabase(drv driver.Driver, src config.SourceConfig) (*sql.DB, error) {
	dsn, err := drv.BuildDSN(src)
	if err != nil {
		return nil, fmt.Errorf("组装 DSN 失败: %w", err)
	}
	db, err := sql.Open(drv.SQLDriverName(), dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open 失败: %w", err)
	}
	db.SetMaxOpenConns(src.Pool.MaxConnections)
	db.SetMaxIdleConns(src.Pool.MinConnections)
	db.SetConnMaxIdleTime(src.Pool.MaxIdleTime)
	return db, nil
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
		if err := src.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Pool) closeAll() {
	for _, src := range p.ordered {
		_ = src.close()
	}
}

func (s *Source) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	if s.db != nil {
		errs = append(errs, s.db.Close())
		s.db = nil
	}
	if s.tunnel != nil {
		errs = append(errs, s.tunnel.Close())
		s.tunnel = nil
	}
	return errors.Join(errs...)
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
