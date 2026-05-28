package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/driver"
)

type Mode string

const (
	ModeR  Mode = "r"
	ModeRW Mode = "rw"
)

type Source struct {
	Key          string
	Mode         Mode
	DB           *sql.DB
	Driver       driver.Driver
	QueryTimeout time.Duration
}

type Pool struct {
	sources map[string]*Source
	keys    []string
}

func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	p := &Pool{sources: map[string]*Source{}}

	keys := make([]string, 0, len(cfg.Databases))
	for k := range cfg.Databases {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		src := cfg.Databases[key]

		drv, ok := driver.Get(src.Driver)
		if !ok {
			p.closeAll()
			return nil, fmt.Errorf("databases.%s: driver %q 未注册, 已注册的驱动: %v", key, src.Driver, driver.Names())
		}

		dsn, err := drv.BuildDSN(src)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("databases.%s: 组装 DSN 失败: %w", key, err)
		}

		db, err := sql.Open(drv.SQLDriverName(), dsn)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("databases.%s: sql.Open 失败: %w", key, err)
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
			return nil, fmt.Errorf("databases.%s: Ping 失败: %w", key, scrubError(pingErr, src.Password))
		}

		source := &Source{
			Key:          key,
			Mode:         Mode(src.Mode),
			DB:           db,
			Driver:       drv,
			QueryTimeout: cfg.Defaults.QueryTimeout,
		}
		p.sources[key] = source
		p.keys = append(p.keys, key)

		port := src.Port
		if port == 0 {
			port = 3306
		}
		fmt.Fprintf(os.Stderr, "[mcp-server] source loaded: key=%s driver=%s mode=%s host=%s:%d database=%s\n",
			key, src.Driver, src.Mode, src.Host, port, src.Database)
	}

	return p, nil
}

func (p *Pool) Get(key string) (*Source, error) {
	if src, ok := p.sources[key]; ok {
		return src, nil
	}
	return nil, fmt.Errorf("source %q not found, available: %v", key, p.keys)
}

func (p *Pool) Keys() []string {
	out := make([]string, len(p.keys))
	copy(out, p.keys)
	return out
}

func (p *Pool) Close() error {
	var errs []error
	for _, src := range p.sources {
		if err := src.DB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Pool) closeAll() {
	for _, src := range p.sources {
		_ = src.DB.Close()
	}
}

// scrubError 确保返回的错误信息不包含密码明文。
// 即使底层驱动的错误偶尔回显 DSN，我们也做一次兜底替换。
func scrubError(err error, password string) error {
	if err == nil || password == "" {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, password) {
		return fmt.Errorf("%s", strings.ReplaceAll(msg, password, "***"))
	}
	return err
}
