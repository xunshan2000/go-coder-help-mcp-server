package redis

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"example.com/mcp-server/internal/config"
)

type Source struct {
	Key         string
	Addr        string
	Auth        string
	Index       int
	DialTimeout time.Duration
	ReadTimeout time.Duration
}

type Pool struct {
	sources map[string]*Source
	keys    []string
}

func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	p := &Pool{sources: map[string]*Source{}}

	keys := make([]string, 0, len(cfg.Redis))
	for k := range cfg.Redis {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		rc := cfg.Redis[key]
		src := &Source{
			Key:         key,
			Addr:        fmt.Sprintf("%s:%d", rc.Host, rc.Port),
			Auth:        rc.Auth,
			Index:       rc.Index,
			DialTimeout: rc.DialTimeout,
			ReadTimeout: rc.ReadTimeout,
		}
		if err := src.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("redis.%s: ping failed: %w", key, err)
		}

		p.sources[key] = src
		p.keys = append(p.keys, key)
		fmt.Fprintf(os.Stderr, "[mcp-server] redis source loaded: key=%s addr=%s db=%d\n", key, src.Addr, src.Index)
	}

	return p, nil
}

func (p *Pool) Get(key string) (*Source, error) {
	if src, ok := p.sources[key]; ok {
		return src, nil
	}
	return nil, fmt.Errorf("redis source %q not found, available: %v", key, p.keys)
}

func (p *Pool) Keys() []string {
	out := make([]string, len(p.keys))
	copy(out, p.keys)
	return out
}

func (p *Pool) Close() error {
	return nil
}
