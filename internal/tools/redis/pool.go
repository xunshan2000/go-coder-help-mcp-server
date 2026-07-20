package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/sshtunnel"
)

type Source struct {
	Environment string
	Key         string
	Addr        string
	Auth        string
	Index       int
	Write       bool
	DialTimeout time.Duration
	ReadTimeout time.Duration
}

type Pool struct {
	sources map[string]map[string]*Source
	ordered []*Source
	tunnels []*sshtunnel.Tunnel
}

func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	p := &Pool{sources: map[string]map[string]*Source{}}

	environments := make([]string, 0, len(cfg.Environments))
	for environment := range cfg.Environments {
		environments = append(environments, environment)
	}
	sort.Strings(environments)
	for _, environment := range environments {
		env := cfg.Environments[environment]
		keys := make([]string, 0, len(env.Redis))
		for key := range env.Redis {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			rc := env.Redis[key]
			targetAddr := net.JoinHostPort(rc.Host, strconv.Itoa(rc.Port))
			addr := targetAddr
			if rc.SSH != nil {
				tunnel, err := sshtunnel.Open(ctx, *rc.SSH, targetAddr)
				if err != nil {
					p.closeAll()
					return nil, fmt.Errorf("environments.%s.redis.%s: SSH tunnel failed: %w", environment, key, err)
				}
				p.tunnels = append(p.tunnels, tunnel)
				addr = tunnel.LocalAddr()
			}
			src := &Source{
				Environment: environment,
				Key:         key,
				Addr:        addr,
				Auth:        rc.Auth,
				Index:       rc.Index,
				Write:       rc.Write,
				DialTimeout: rc.DialTimeout,
				ReadTimeout: rc.ReadTimeout,
			}
			if err := src.PingContext(ctx); err != nil {
				p.closeAll()
				return nil, fmt.Errorf("environments.%s.redis.%s: ping failed: %w", environment, key, err)
			}

			if p.sources[environment] == nil {
				p.sources[environment] = map[string]*Source{}
			}
			p.sources[environment][key] = src
			p.ordered = append(p.ordered, src)
			fmt.Fprintf(os.Stderr, "[mcp-server] redis source loaded: environment=%s key=%s addr=%s db=%d write=%t via_ssh=%t\n",
				environment, key, targetAddr, src.Index, src.Write, rc.SSH != nil)
		}
	}

	return p, nil
}

func (p *Pool) Get(environment, key string) (*Source, error) {
	if sources, ok := p.sources[environment]; ok {
		if src, ok := sources[key]; ok {
			return src, nil
		}
	}
	return nil, fmt.Errorf("redis source %q not found in environment %q, available: %v", key, environment, p.SourceNames())
}

func (p *Pool) GetWritable(environment, key string) (*Source, error) {
	src, err := p.Get(environment, key)
	if err != nil {
		return nil, err
	}
	if !src.Write {
		return nil, fmt.Errorf("redis source %q in environment %q is read-only; set environments.%s.redis.%s.write: true to enable write tools", key, environment, environment, key)
	}
	return src, nil
}

func (p *Pool) HasWritableSources() bool {
	for _, src := range p.ordered {
		if src.Write {
			return true
		}
	}
	return false
}

func (p *Pool) SourceNames() []string {
	out := make([]string, 0, len(p.ordered))
	for _, src := range p.ordered {
		out = append(out, src.Environment+"/"+src.Key)
	}
	return out
}

func (p *Pool) Close() error {
	var errs []error
	for _, tunnel := range p.tunnels {
		if err := tunnel.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Pool) closeAll() {
	for _, tunnel := range p.tunnels {
		_ = tunnel.Close()
	}
}
