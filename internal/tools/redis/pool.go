package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
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

	ssh    *config.SSHConfig
	tunnel *sshtunnel.Tunnel
	mu     sync.Mutex
}

type Pool struct {
	sources map[string]map[string]*Source
	ordered []*Source
}

func NewPool(_ context.Context, cfg *config.Config) (*Pool, error) {
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
			src := &Source{
				Environment: environment,
				Key:         key,
				Addr:        targetAddr,
				Auth:        rc.Auth,
				Index:       rc.Index,
				Write:       rc.Write,
				DialTimeout: rc.DialTimeout,
				ReadTimeout: rc.ReadTimeout,
				ssh:         rc.SSH,
			}

			if p.sources[environment] == nil {
				p.sources[environment] = map[string]*Source{}
			}
			p.sources[environment][key] = src
			p.ordered = append(p.ordered, src)
			fmt.Fprintf(os.Stderr, "[mcp-server] redis source configured: environment=%s key=%s addr=%s db=%d write=%t via_ssh=%t\n",
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
	for _, src := range p.ordered {
		if err := src.close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Source) connectionAddr(ctx context.Context) (string, error) {
	if s.ssh == nil {
		return s.Addr, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tunnel == nil {
		tunnel, err := sshtunnel.Open(ctx, *s.ssh, s.Addr)
		if err != nil {
			return "", fmt.Errorf("SSH tunnel failed: %w", err)
		}
		s.tunnel = tunnel
	}
	return s.tunnel.LocalAddr(), nil
}

func (s *Source) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tunnel == nil {
		return nil
	}
	err := s.tunnel.Close()
	s.tunnel = nil
	return err
}

func (s *Source) resetTunnel() {
	_ = s.close()
}
