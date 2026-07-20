package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Defaults     Defaults                     `yaml:"defaults"`
	Features     FeatureConfig                `yaml:"features"`
	Environments map[string]EnvironmentConfig `yaml:"environments"`
	Apipost      *ApipostConfig               `yaml:"apipost"`
}

type EnvironmentConfig struct {
	Databases map[string]SourceConfig `yaml:"databases"`
	Redis     map[string]RedisConfig  `yaml:"redis"`
}

type FeatureConfig struct {
	Database *bool `yaml:"database"`
	Redis    *bool `yaml:"redis"`
	Apipost  *bool `yaml:"apipost"`
	SQLAudit *bool `yaml:"sql_audit"`
}

func enabled(value *bool) bool {
	return value == nil || *value
}

func (f FeatureConfig) DatabaseEnabled() bool { return enabled(f.Database) }
func (f FeatureConfig) RedisEnabled() bool    { return enabled(f.Redis) }
func (f FeatureConfig) ApipostEnabled() bool  { return enabled(f.Apipost) }
func (f FeatureConfig) SQLAuditEnabled() bool { return enabled(f.SQLAudit) }

type Defaults struct {
	MaxRows      int           `yaml:"max_rows"`
	QueryTimeout time.Duration `yaml:"query_timeout"`
}

type SourceConfig struct {
	Driver    string            `yaml:"driver"`
	Host      string            `yaml:"host"`
	Port      int               `yaml:"port"`
	Database  string            `yaml:"database"`
	Username  string            `yaml:"username"`
	Password  string            `yaml:"password"`
	Charset   string            `yaml:"charset"`
	Collation string            `yaml:"collation"`
	Timezone  string            `yaml:"timezone"`
	Write     bool              `yaml:"write"`
	Params    map[string]string `yaml:"params"`
	Pool      PoolConfig        `yaml:"pool"`
	SSH       *SSHConfig        `yaml:"ssh"`
}

type SSHConfig struct {
	Host                 string        `yaml:"host"`
	Port                 int           `yaml:"port"`
	Username             string        `yaml:"username"`
	Password             string        `yaml:"password"`
	PrivateKey           string        `yaml:"private_key"`
	PrivateKeyPassphrase string        `yaml:"private_key_passphrase"`
	KnownHosts           string        `yaml:"known_hosts"`
	InsecureSkipHostKey  bool          `yaml:"insecure_skip_host_key"`
	ConnectTimeout       time.Duration `yaml:"connect_timeout"`
}

type PoolConfig struct {
	MaxConnections int           `yaml:"max_connections"`
	MinConnections int           `yaml:"min_connections"`
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	MaxIdleTime    time.Duration `yaml:"max_idle_time"`
}

type RedisConfig struct {
	Host        string        `yaml:"host"`
	Port        int           `yaml:"port"`
	Auth        string        `yaml:"auth"`
	Index       int           `yaml:"index"`
	Write       bool          `yaml:"write"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	ReadTimeout time.Duration `yaml:"read_timeout"`
	SSH         *SSHConfig    `yaml:"ssh"`
}

type ApipostConfig struct {
	BaseURL          string        `yaml:"base_url"`
	APIToken         string        `yaml:"api_token"`
	ProjectName      string        `yaml:"project_name"`
	ProjectID        string        `yaml:"project_id"`
	TeamID           string        `yaml:"team_id"`
	RequestTimeout   time.Duration `yaml:"request_timeout"`
	MaxResponseBytes int64         `yaml:"max_response_bytes"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.resolvePaths(path)
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) resolvePaths(configPath string) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return
	}
	baseDir := filepath.Dir(absPath)
	for environment, env := range c.Environments {
		for key, src := range env.Databases {
			resolveSSHPaths(baseDir, src.SSH)
			env.Databases[key] = src
		}
		for key, src := range env.Redis {
			resolveSSHPaths(baseDir, src.SSH)
			env.Redis[key] = src
		}
		c.Environments[environment] = env
	}
}

func resolveSSHPaths(baseDir string, ssh *SSHConfig) {
	if ssh == nil {
		return
	}
	if ssh.PrivateKey != "" && !filepath.IsAbs(ssh.PrivateKey) {
		ssh.PrivateKey = filepath.Join(baseDir, ssh.PrivateKey)
	}
	if ssh.KnownHosts != "" && !filepath.IsAbs(ssh.KnownHosts) {
		ssh.KnownHosts = filepath.Join(baseDir, ssh.KnownHosts)
	}
}

func (c *Config) applyDefaults() {
	if c.Defaults.MaxRows == 0 {
		c.Defaults.MaxRows = 100
	}
	if c.Defaults.QueryTimeout == 0 {
		c.Defaults.QueryTimeout = 30 * time.Second
	}
	for environment, env := range c.Environments {
		for key, src := range env.Databases {
			if src.Pool.MaxConnections == 0 {
				src.Pool.MaxConnections = 10
			}
			if src.Pool.ConnectTimeout == 0 {
				src.Pool.ConnectTimeout = 10 * time.Second
			}
			applySSHDefaults(src.SSH)
			env.Databases[key] = src
		}
		for key, src := range env.Redis {
			if src.Port == 0 {
				src.Port = 6379
			}
			if src.DialTimeout == 0 {
				src.DialTimeout = 5 * time.Second
			}
			if src.ReadTimeout == 0 {
				src.ReadTimeout = 5 * time.Second
			}
			applySSHDefaults(src.SSH)
			env.Redis[key] = src
		}
		c.Environments[environment] = env
	}
	if c.Apipost != nil {
		if c.Apipost.RequestTimeout == 0 {
			c.Apipost.RequestTimeout = 30 * time.Second
		}
		if c.Apipost.MaxResponseBytes == 0 {
			c.Apipost.MaxResponseBytes = 10 * 1024 * 1024
		}
	}
}

func applySSHDefaults(ssh *SSHConfig) {
	if ssh == nil {
		return
	}
	if ssh.Port == 0 {
		ssh.Port = 22
	}
	if ssh.ConnectTimeout == 0 {
		ssh.ConnectTimeout = 10 * time.Second
	}
	if ssh.KnownHosts == "" && !ssh.InsecureSkipHostKey {
		if home, err := os.UserHomeDir(); err == nil {
			ssh.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
		}
	}
}

func (c *Config) Validate() error {
	if (!c.Features.DatabaseEnabled() || !c.HasDatabases()) &&
		(!c.Features.RedisEnabled() || !c.HasRedis()) &&
		(!c.Features.ApipostEnabled() || c.Apipost == nil) {
		return fmt.Errorf("config: at least one enabled feature must be available")
	}
	if c.Features.DatabaseEnabled() && c.Defaults.MaxRows <= 0 {
		return fmt.Errorf("config.defaults.max_rows must be positive, got %d", c.Defaults.MaxRows)
	}
	if c.Features.DatabaseEnabled() && c.Defaults.QueryTimeout <= 0 {
		return fmt.Errorf("config.defaults.query_timeout must be > 0, got %v", c.Defaults.QueryTimeout)
	}

	for environment, env := range c.Environments {
		if environment == "" {
			return fmt.Errorf("config.environments: environment key cannot be empty")
		}
		if c.Features.DatabaseEnabled() {
			for key, src := range env.Databases {
				if key == "" {
					return fmt.Errorf("config.environments.%s.databases: source key cannot be empty", environment)
				}
				if err := validateSource(fmt.Sprintf("environments.%s.databases.%s", environment, key), src); err != nil {
					return err
				}
			}
		}
		if c.Features.RedisEnabled() {
			for key, src := range env.Redis {
				if key == "" {
					return fmt.Errorf("config.environments.%s.redis: source key cannot be empty", environment)
				}
				if err := validateRedisSource(fmt.Sprintf("environments.%s.redis.%s", environment, key), src); err != nil {
					return err
				}
			}
		}
	}

	if c.Features.ApipostEnabled() && c.Apipost != nil {
		if err := validateApipost(c.Apipost); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) HasDatabases() bool {
	for _, env := range c.Environments {
		if len(env.Databases) > 0 {
			return true
		}
	}
	return false
}

func (c *Config) HasRedis() bool {
	for _, env := range c.Environments {
		if len(env.Redis) > 0 {
			return true
		}
	}
	return false
}

func validateSource(path string, src SourceConfig) error {
	if src.Driver == "" {
		return fmt.Errorf("%s: driver cannot be empty", path)
	}
	if src.Host == "" {
		return fmt.Errorf("%s: host cannot be empty", path)
	}
	if src.Port < 0 || src.Port > 65535 {
		return fmt.Errorf("%s: port must be between 1 and 65535 when set, got %d", path, src.Port)
	}
	if src.Database == "" {
		return fmt.Errorf("%s: database cannot be empty", path)
	}
	if src.Username == "" {
		return fmt.Errorf("%s: username cannot be empty", path)
	}
	for k, v := range src.Params {
		if strings.EqualFold(k, "multiStatements") && strings.EqualFold(v, "true") {
			return fmt.Errorf("%s.params: multiStatements=true is not allowed", path)
		}
	}
	if src.Pool.MaxConnections <= 0 {
		return fmt.Errorf("%s.pool.max_connections must be > 0, got %d", path, src.Pool.MaxConnections)
	}
	if src.Pool.MinConnections < 0 {
		return fmt.Errorf("%s.pool.min_connections must be >= 0, got %d", path, src.Pool.MinConnections)
	}
	if src.Pool.MinConnections > src.Pool.MaxConnections {
		return fmt.Errorf("%s.pool.min_connections must not exceed max_connections", path)
	}
	if src.Pool.ConnectTimeout <= 0 {
		return fmt.Errorf("%s.pool.connect_timeout must be > 0, got %v", path, src.Pool.ConnectTimeout)
	}
	if src.Pool.MaxIdleTime < 0 {
		return fmt.Errorf("%s.pool.max_idle_time must be >= 0, got %v", path, src.Pool.MaxIdleTime)
	}
	if src.SSH != nil {
		if src.Driver != "mysql" {
			return fmt.Errorf("%s.ssh: only driver mysql is supported", path)
		}
		if err := validateSSH(path, *src.SSH); err != nil {
			return err
		}
	}
	return nil
}

func validateSSH(path string, ssh SSHConfig) error {
	prefix := path + ".ssh"
	if ssh.Host == "" {
		return fmt.Errorf("%s.host is required", prefix)
	}
	if ssh.Port <= 0 || ssh.Port > 65535 {
		return fmt.Errorf("%s.port must be between 1 and 65535, got %d", prefix, ssh.Port)
	}
	if ssh.Username == "" {
		return fmt.Errorf("%s.username is required", prefix)
	}
	if ssh.Password == "" && ssh.PrivateKey == "" {
		return fmt.Errorf("%s: password or private_key is required", prefix)
	}
	if ssh.PrivateKeyPassphrase != "" && ssh.PrivateKey == "" {
		return fmt.Errorf("%s.private_key_passphrase requires private_key", prefix)
	}
	if !ssh.InsecureSkipHostKey && ssh.KnownHosts == "" {
		return fmt.Errorf("%s.known_hosts is required unless insecure_skip_host_key is true", prefix)
	}
	if ssh.ConnectTimeout <= 0 {
		return fmt.Errorf("%s.connect_timeout must be > 0, got %v", prefix, ssh.ConnectTimeout)
	}
	return nil
}

func validateRedisSource(path string, src RedisConfig) error {
	if src.Host == "" {
		return fmt.Errorf("%s: host cannot be empty", path)
	}
	if src.Port <= 0 || src.Port > 65535 {
		return fmt.Errorf("%s: port must be between 1 and 65535, got %d", path, src.Port)
	}
	if src.Index < 0 {
		return fmt.Errorf("%s: index must be >= 0, got %d", path, src.Index)
	}
	if src.DialTimeout <= 0 {
		return fmt.Errorf("%s: dial_timeout must be > 0, got %v", path, src.DialTimeout)
	}
	if src.ReadTimeout <= 0 {
		return fmt.Errorf("%s: read_timeout must be > 0, got %v", path, src.ReadTimeout)
	}
	if src.SSH != nil {
		if err := validateSSH(path, *src.SSH); err != nil {
			return err
		}
	}
	return nil
}

var apipostIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateApipost(a *ApipostConfig) error {
	if a.BaseURL == "" {
		return fmt.Errorf("config.apipost.base_url is required")
	}
	if !strings.HasPrefix(a.BaseURL, "http://") && !strings.HasPrefix(a.BaseURL, "https://") {
		return fmt.Errorf("config.apipost.base_url must start with http:// or https://, got %q", a.BaseURL)
	}
	if _, err := url.Parse(a.BaseURL); err != nil {
		return fmt.Errorf("config.apipost.base_url is not a valid URL: %w", err)
	}
	if a.APIToken == "" {
		return fmt.Errorf("config.apipost.api_token is required")
	}
	if a.ProjectName == "" {
		return fmt.Errorf("config.apipost.project_name is required")
	}
	if a.TeamID != "" && !apipostIDPattern.MatchString(a.TeamID) {
		return fmt.Errorf("config.apipost.team_id has invalid format; only letters, numbers, underscore, and hyphen are allowed")
	}
	if a.ProjectID != "" && !apipostIDPattern.MatchString(a.ProjectID) {
		return fmt.Errorf("config.apipost.project_id has invalid format; only letters, numbers, underscore, and hyphen are allowed")
	}
	if a.RequestTimeout <= 0 {
		return fmt.Errorf("config.apipost.request_timeout must be > 0, got %v", a.RequestTimeout)
	}
	if a.MaxResponseBytes <= 0 {
		return fmt.Errorf("config.apipost.max_response_bytes must be positive, got %d", a.MaxResponseBytes)
	}
	return nil
}
