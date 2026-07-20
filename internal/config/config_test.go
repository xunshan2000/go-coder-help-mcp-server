package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func base() *Config {
	return &Config{
		Defaults: Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Environments: map[string]EnvironmentConfig{
			"test": {Databases: map[string]SourceConfig{
				"default": {
					Driver: "mysql", Host: "h", Database: "d", Username: "u",
					Pool: PoolConfig{MaxConnections: 10, ConnectTimeout: 10 * time.Second},
				},
			}},
		},
	}
}

func setDatabases(c *Config, databases map[string]SourceConfig) {
	env := c.Environments["test"]
	env.Databases = databases
	c.Environments["test"] = env
}

func setRedis(c *Config, redis map[string]RedisConfig) {
	if c.Environments == nil {
		c.Environments = map[string]EnvironmentConfig{}
	}
	env := c.Environments["test"]
	env.Redis = redis
	c.Environments["test"] = env
}

func updateDatabase(c *Config, key string, update func(*SourceConfig)) {
	env := c.Environments["test"]
	src := env.Databases[key]
	update(&src)
	env.Databases[key] = src
	c.Environments["test"] = env
}

func testEnvironment(c *Config) EnvironmentConfig {
	return c.Environments["test"]
}

func TestValidateRedis_WithoutDatabasesIsFine(t *testing.T) {
	c := &Config{
		Defaults: Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Environments: map[string]EnvironmentConfig{"test": {Redis: map[string]RedisConfig{
			"cache": {Host: "127.0.0.1", Port: 6379, Index: 6, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
		}}},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("redis-only config should be allowed, got %v", err)
	}
}

func TestApplyDefaults_RedisSection(t *testing.T) {
	c := &Config{
		Defaults: Defaults{MaxRows: 100, QueryTimeout: 30 * time.Second},
		Environments: map[string]EnvironmentConfig{"test": {Redis: map[string]RedisConfig{
			"cache": {Host: "127.0.0.1", SSH: &SSHConfig{
				Host: "bastion.example.com", Username: "deploy", Password: "secret", InsecureSkipHostKey: true,
			}},
		}}},
	}
	c.applyDefaults()

	got := testEnvironment(c).Redis["cache"]
	if got.Port != 6379 {
		t.Fatalf("expected default redis port 6379, got %d", got.Port)
	}
	if got.DialTimeout != 5*time.Second {
		t.Fatalf("expected default DialTimeout 5s, got %v", got.DialTimeout)
	}
	if got.ReadTimeout != 5*time.Second {
		t.Fatalf("expected default ReadTimeout 5s, got %v", got.ReadTimeout)
	}
	if got.SSH.Port != 22 || got.SSH.ConnectTimeout != 10*time.Second {
		t.Fatalf("SSH defaults mismatch: %+v", *got.SSH)
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
				setDatabases(c, nil)
				setRedis(c, map[string]RedisConfig{
					"cache": {Port: 6379, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
				})
			},
			want: "host",
		},
		{
			name: "port too large",
			mut: func(c *Config) {
				setDatabases(c, nil)
				setRedis(c, map[string]RedisConfig{
					"cache": {Host: "127.0.0.1", Port: 70000, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
				})
			},
			want: "port",
		},
		{
			name: "negative index",
			mut: func(c *Config) {
				setDatabases(c, nil)
				setRedis(c, map[string]RedisConfig{
					"cache": {Host: "127.0.0.1", Port: 6379, Index: -1, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second},
				})
			},
			want: "index",
		},
		{
			name: "zero read timeout",
			mut: func(c *Config) {
				setDatabases(c, nil)
				setRedis(c, map[string]RedisConfig{
					"cache": {Host: "127.0.0.1", Port: 6379, DialTimeout: 5 * time.Second},
				})
			},
			want: "read_timeout",
		},
		{
			name: "invalid SSH",
			mut: func(c *Config) {
				setDatabases(c, nil)
				setRedis(c, map[string]RedisConfig{
					"cache": {
						Host: "redis.internal", Port: 6379, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
						SSH: &SSHConfig{
							Host: "bastion.example.com", Port: 22, Username: "deploy",
							KnownHosts: "known_hosts", ConnectTimeout: 10 * time.Second,
						},
					},
				})
			},
			want: "password or private_key",
		},
		{
			name: "all features unavailable",
			mut: func(c *Config) {
				c.Environments = nil
				c.Apipost = nil
				c.Features = FeatureConfig{
					Database: boolPtr(false), Redis: boolPtr(false), Apipost: boolPtr(false),
				}
			},
			want: "at least one enabled feature",
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

func TestFeatureDefaultsAndExplicitSwitches(t *testing.T) {
	var defaults FeatureConfig
	if !defaults.DatabaseEnabled() || !defaults.RedisEnabled() ||
		!defaults.ApipostEnabled() || !defaults.SQLAuditEnabled() {
		t.Fatal("omitted feature switches must default to enabled")
	}

	disabled := FeatureConfig{
		Database: boolPtr(false), Redis: boolPtr(false),
		Apipost: boolPtr(false), SQLAudit: boolPtr(false),
	}
	if disabled.DatabaseEnabled() || disabled.RedisEnabled() ||
		disabled.ApipostEnabled() || disabled.SQLAuditEnabled() {
		t.Fatal("explicit false feature switches must stay disabled")
	}
}

func TestDisabledIntegrationSkipsItsValidation(t *testing.T) {
	t.Run("redis and apipost disabled", func(t *testing.T) {
		c := base()
		c.Features = FeatureConfig{Redis: boolPtr(false), Apipost: boolPtr(false)}
		setRedis(c, map[string]RedisConfig{"broken": {}})
		c.Apipost = &ApipostConfig{}
		if err := c.Validate(); err != nil {
			t.Fatalf("disabled integrations should not validate inactive config: %v", err)
		}
	})

	t.Run("database disabled", func(t *testing.T) {
		c := &Config{
			Defaults: Defaults{MaxRows: -1, QueryTimeout: -time.Second},
			Features: FeatureConfig{Database: boolPtr(false), Apipost: boolPtr(false)},
			Environments: map[string]EnvironmentConfig{"test": {
				Databases: map[string]SourceConfig{"broken": {}},
				Redis: map[string]RedisConfig{"cache": {
					Host: "127.0.0.1", Port: 6379, DialTimeout: time.Second, ReadTimeout: time.Second,
				}},
			}},
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("disabled database should not validate inactive config: %v", err)
		}
	})
}

func TestValidateDatabaseRuntimeSettings(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"query timeout", func(c *Config) { c.Defaults.QueryTimeout = -time.Second }, "query_timeout"},
		{"database port", func(c *Config) { updateDatabase(c, "default", func(s *SourceConfig) { s.Port = 70000 }) }, "port"},
		{"max connections", func(c *Config) { updateDatabase(c, "default", func(s *SourceConfig) { s.Pool.MaxConnections = -1 }) }, "max_connections"},
		{"min connections", func(c *Config) { updateDatabase(c, "default", func(s *SourceConfig) { s.Pool.MinConnections = -1 }) }, "min_connections"},
		{"min exceeds max", func(c *Config) { updateDatabase(c, "default", func(s *SourceConfig) { s.Pool.MinConnections = 11 }) }, "must not exceed"},
		{"connect timeout", func(c *Config) {
			updateDatabase(c, "default", func(s *SourceConfig) { s.Pool.ConnectTimeout = -time.Second })
		}, "connect_timeout"},
		{"max idle time", func(c *Config) {
			updateDatabase(c, "default", func(s *SourceConfig) { s.Pool.MaxIdleTime = -time.Second })
		}, "max_idle_time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mut(c)
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateSSH(t *testing.T) {
	valid := SSHConfig{
		Host: "bastion.example.com", Port: 22, Username: "deploy", Password: "secret",
		KnownHosts: "known_hosts", ConnectTimeout: 10 * time.Second,
	}
	cases := []struct {
		name string
		mut  func(*SSHConfig)
		want string
	}{
		{"missing host", func(s *SSHConfig) { s.Host = "" }, "host"},
		{"invalid port", func(s *SSHConfig) { s.Port = 70000 }, "port"},
		{"missing user", func(s *SSHConfig) { s.Username = "" }, "username"},
		{"missing auth", func(s *SSHConfig) { s.Password = "" }, "password or private_key"},
		{"passphrase without key", func(s *SSHConfig) { s.PrivateKeyPassphrase = "p" }, "requires private_key"},
		{"missing known hosts", func(s *SSHConfig) { s.KnownHosts = "" }, "known_hosts"},
		{"invalid timeout", func(s *SSHConfig) { s.ConnectTimeout = -time.Second }, "connect_timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ssh := valid
			tc.mut(&ssh)
			if err := validateSSH("primary", ssh); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateSSH() error = %v, want substring %q", err, tc.want)
			}
		})
	}
	valid.KnownHosts = ""
	valid.InsecureSkipHostKey = true
	if err := validateSSH("primary", valid); err != nil {
		t.Fatalf("explicit insecure host key mode should be accepted: %v", err)
	}
}

func TestLoadSSHDefaultsAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	raw := `
features:
  redis: false
  apipost: false
environments:
  pro:
    databases:
      primary:
        driver: mysql
        host: mysql.internal
        database: app
        username: app
        write: true
        ssh:
          host: bastion.example.com
          username: deploy
          private_key: keys/id_ed25519
          known_hosts: keys/known_hosts
    redis:
      cache:
        host: redis.internal
        ssh:
          host: bastion.example.com
          username: deploy
          private_key: redis/id_ed25519
          known_hosts: redis/known_hosts
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	source := cfg.Environments["pro"].Databases["primary"]
	ssh := source.SSH
	if ssh == nil {
		t.Fatal("SSH config missing")
	}
	if ssh.Port != 22 || ssh.ConnectTimeout != 10*time.Second {
		t.Fatalf("SSH defaults mismatch: %+v", *ssh)
	}
	if ssh.PrivateKey != filepath.Join(dir, "keys", "id_ed25519") {
		t.Fatalf("private key path not resolved: %s", ssh.PrivateKey)
	}
	if ssh.KnownHosts != filepath.Join(dir, "keys", "known_hosts") {
		t.Fatalf("known_hosts path not resolved: %s", ssh.KnownHosts)
	}
	if !source.Write {
		t.Fatal("write flag was not loaded")
	}
	redisSSH := cfg.Environments["pro"].Redis["cache"].SSH
	if redisSSH == nil {
		t.Fatal("Redis SSH config missing")
	}
	if redisSSH.Port != 22 || redisSSH.ConnectTimeout != 10*time.Second {
		t.Fatalf("Redis SSH defaults mismatch: %+v", *redisSSH)
	}
	if redisSSH.PrivateKey != filepath.Join(dir, "redis", "id_ed25519") {
		t.Fatalf("Redis private key path not resolved: %s", redisSSH.PrivateKey)
	}
	if redisSSH.KnownHosts != filepath.Join(dir, "redis", "known_hosts") {
		t.Fatalf("Redis known_hosts path not resolved: %s", redisSSH.KnownHosts)
	}
}

func TestWriteFlagsDefaultToReadOnly(t *testing.T) {
	if (SourceConfig{}).Write {
		t.Fatal("database write must default to false")
	}
	if (RedisConfig{}).Write {
		t.Fatal("redis write must default to false")
	}
}

func TestLoadRejectsLegacyModeField(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := `
environments:
  pro:
    databases:
      primary:
        driver: mysql
        host: localhost
        database: app
        username: app
        mode: rw
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(configPath)
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("Load() error = %v, want unknown legacy mode field", err)
	}
}

func TestLoadSupportsCustomEnvironmentsWithSameSourceName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := `
features:
  redis: false
  apipost: false
environments:
  test1:
    databases:
      platform:
        driver: mysql
        host: test1.mysql
        database: platform
        username: app
  test2:
    databases:
      platform:
        driver: mysql
        host: test2.mysql
        database: platform
        username: app
        write: true
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Environments["test1"].Databases["platform"].Host != "test1.mysql" {
		t.Fatal("test1/platform was not loaded")
	}
	if !cfg.Environments["test2"].Databases["platform"].Write {
		t.Fatal("test2/platform write flag was not loaded")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("load config.example.yaml: %v", err)
	}
	if _, ok := cfg.Environments["pro"].Databases["platform"]; !ok {
		t.Fatal("config example missing pro/platform database")
	}
	if _, ok := cfg.Environments["local"].Databases["platform"]; !ok {
		t.Fatal("config example missing local/platform database")
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
