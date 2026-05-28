package config

import (
	"strings"
	"testing"
	"time"
)

func base() *Config {
	return &Config{
		Defaults: Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Databases: map[string]SourceConfig{
			"default": {
				Driver: "mysql", Host: "h", Database: "d", Username: "u", Mode: "r",
				Pool: PoolConfig{MaxConnections: 10, ConnectTimeout: 10 * time.Second},
			},
		},
	}
}

func TestValidateRedis_WithoutDatabasesIsFine(t *testing.T) {
	c := &Config{
		Defaults:  Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Databases: nil,
		Redis: map[string]RedisConfig{
			"cache": {
				Host:        "127.0.0.1",
				Port:        6379,
				Index:       6,
				DialTimeout: 5 * time.Second,
				ReadTimeout: 5 * time.Second,
			},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("redis-only config should be allowed, got %v", err)
	}
}

func TestApplyDefaults_RedisSection(t *testing.T) {
	c := &Config{
		Defaults: Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Redis: map[string]RedisConfig{
			"cache": {Host: "127.0.0.1"},
		},
	}
	c.applyDefaults()

	got := c.Redis["cache"]
	if got.Port != 6379 {
		t.Fatalf("expected default redis port 6379, got %d", got.Port)
	}
	if got.DialTimeout != 5*time.Second {
		t.Fatalf("expected default DialTimeout 5s, got %v", got.DialTimeout)
	}
	if got.ReadTimeout != 5*time.Second {
		t.Fatalf("expected default ReadTimeout 5s, got %v", got.ReadTimeout)
	}
}

func TestValidateRedis_Invalid(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "empty host",
			mut: func(c *Config) {
				c.Databases = nil
				c.Redis = map[string]RedisConfig{
					"cache": {Port: 6379, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
				}
			},
			want: "host",
		},
		{
			name: "negative index",
			mut: func(c *Config) {
				c.Databases = nil
				c.Redis = map[string]RedisConfig{
					"cache": {Host: "127.0.0.1", Port: 6379, Index: -1, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
				}
			},
			want: "index",
		},
		{
			name: "zero read timeout",
			mut: func(c *Config) {
				c.Databases = nil
				c.Redis = map[string]RedisConfig{
					"cache": {Host: "127.0.0.1", Port: 6379, DialTimeout: 5 * time.Second},
				}
			},
			want: "read_timeout",
		},
		{
			name: "nothing configured",
			mut: func(c *Config) {
				c.Databases = nil
				c.Redis = nil
				c.Apipost = nil
			},
			want: "at least one",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %q missing substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateApipost_MissingSectionIsFine(t *testing.T) {
	c := base()
	c.Apipost = nil
	if err := c.Validate(); err != nil {
		t.Fatalf("nil Apipost should be allowed, got %v", err)
	}
}

func TestValidateApipost_Valid(t *testing.T) {
	c := base()
	c.Apipost = &ApipostConfig{
		BaseURL:          "https://v2-openapi.apipost.net",
		APIToken:         "apt_xxx",
		ProjectName:      "my-project",
		ProjectID:        "abc_123",
		TeamID:           "tm-xyz",
		RequestTimeout:   30 * time.Second,
		MaxResponseBytes: 1024,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid Apipost rejected: %v", err)
	}
}

func TestApplyDefaults_ApipostSection(t *testing.T) {
	c := base()
	c.Apipost = &ApipostConfig{
		BaseURL: "https://x", APIToken: "t", ProjectName: "p",
	}
	c.applyDefaults()
	if c.Apipost.RequestTimeout != 30*time.Second {
		t.Fatalf("expected default RequestTimeout 30s, got %v", c.Apipost.RequestTimeout)
	}
	if c.Apipost.MaxResponseBytes != 10*1024*1024 {
		t.Fatalf("expected default MaxResponseBytes 10MiB, got %d", c.Apipost.MaxResponseBytes)
	}
}

func TestValidateApipost_Invalid(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*ApipostConfig)
		want string
	}{
		{"base_url empty", func(a *ApipostConfig) { a.BaseURL = "" }, "base_url"},
		{"base_url no scheme", func(a *ApipostConfig) { a.BaseURL = "v2.apipost.net" }, "http://"},
		{"api_token empty", func(a *ApipostConfig) { a.APIToken = "" }, "api_token"},
		{"project_name empty", func(a *ApipostConfig) { a.ProjectName = "" }, "project_name"},
		{"team_id bad char", func(a *ApipostConfig) { a.TeamID = "../evil" }, "team_id"},
		{"project_id space", func(a *ApipostConfig) { a.ProjectID = "has space" }, "project_id"},
		{"timeout zero", func(a *ApipostConfig) { a.RequestTimeout = 0 }, "request_timeout"},
		{"timeout negative", func(a *ApipostConfig) { a.RequestTimeout = -1 * time.Second }, "request_timeout"},
		{"max_bytes zero", func(a *ApipostConfig) { a.MaxResponseBytes = 0 }, "max_response_bytes"},
		{"max_bytes negative", func(a *ApipostConfig) { a.MaxResponseBytes = -1 }, "max_response_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			c.Apipost = &ApipostConfig{
				BaseURL:          "https://x",
				APIToken:         "t",
				ProjectName:      "p",
				RequestTimeout:   30 * time.Second,
				MaxResponseBytes: 1024,
			}
			tc.mut(c.Apipost)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %q missing substring %q", err.Error(), tc.want)
			}
		})
	}
}
