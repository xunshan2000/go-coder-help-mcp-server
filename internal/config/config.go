package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Defaults  Defaults                `yaml:"defaults"`
	Databases map[string]SourceConfig `yaml:"databases"`
	Redis     map[string]RedisConfig  `yaml:"redis"`
	Apipost   *ApipostConfig          `yaml:"apipost"`
}

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
	Mode      string            `yaml:"mode"`
	Params    map[string]string `yaml:"params"`
	Pool      PoolConfig        `yaml:"pool"`
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
	DialTimeout time.Duration `yaml:"dial_timeout"`
	ReadTimeout time.Duration `yaml:"read_timeout"`
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

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Defaults.MaxRows == 0 {
		c.Defaults.MaxRows = 100
	}
	if c.Defaults.QueryTimeout == 0 {
		c.Defaults.QueryTimeout = 30 * time.Second
	}
	for key, src := range c.Databases {
		if src.Pool.MaxConnections == 0 {
			src.Pool.MaxConnections = 10
		}
		if src.Pool.ConnectTimeout == 0 {
			src.Pool.ConnectTimeout = 10 * time.Second
		}
		c.Databases[key] = src
	}
	for key, src := range c.Redis {
		if src.Port == 0 {
			src.Port = 6379
		}
		if src.DialTimeout == 0 {
			src.DialTimeout = 5 * time.Second
		}
		if src.ReadTimeout == 0 {
			src.ReadTimeout = 5 * time.Second
		}
		c.Redis[key] = src
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

func (c *Config) Validate() error {
	if len(c.Databases) == 0 && len(c.Redis) == 0 && c.Apipost == nil {
		return fmt.Errorf("config: at least one of databases, redis, or apipost must be configured")
	}
	if c.Defaults.MaxRows <= 0 {
		return fmt.Errorf("config.defaults.max_rows must be positive, got %d", c.Defaults.MaxRows)
	}

	for key, src := range c.Databases {
		if key == "" {
			return fmt.Errorf("config.databases: source key cannot be empty")
		}
		if err := validateSource(key, src); err != nil {
			return err
		}
	}

	for key, src := range c.Redis {
		if key == "" {
			return fmt.Errorf("config.redis: source key cannot be empty")
		}
		if err := validateRedisSource(key, src); err != nil {
			return err
		}
	}

	if c.Apipost != nil {
		if err := validateApipost(c.Apipost); err != nil {
			return err
		}
	}
	return nil
}

func validateSource(key string, src SourceConfig) error {
	if src.Driver == "" {
		return fmt.Errorf("databases.%s: driver cannot be empty", key)
	}
	if src.Host == "" {
		return fmt.Errorf("databases.%s: host cannot be empty", key)
	}
	if src.Database == "" {
		return fmt.Errorf("databases.%s: database cannot be empty", key)
	}
	if src.Username == "" {
		return fmt.Errorf("databases.%s: username cannot be empty", key)
	}
	if src.Mode == "" {
		return fmt.Errorf("databases.%s: mode cannot be empty", key)
	}
	if src.Mode != "r" && src.Mode != "rw" {
		return fmt.Errorf("databases.%s: invalid mode %q, allowed values are {r, rw}", key, src.Mode)
	}
	for k, v := range src.Params {
		if strings.EqualFold(k, "multiStatements") && strings.EqualFold(v, "true") {
			return fmt.Errorf("databases.%s.params: multiStatements=true is not allowed", key)
		}
	}
	return nil
}

func validateRedisSource(key string, src RedisConfig) error {
	if src.Host == "" {
		return fmt.Errorf("redis.%s: host cannot be empty", key)
	}
	if src.Port <= 0 {
		return fmt.Errorf("redis.%s: port must be > 0, got %d", key, src.Port)
	}
	if src.Index < 0 {
		return fmt.Errorf("redis.%s: index must be >= 0, got %d", key, src.Index)
	}
	if src.DialTimeout <= 0 {
		return fmt.Errorf("redis.%s: dial_timeout must be > 0, got %v", key, src.DialTimeout)
	}
	if src.ReadTimeout <= 0 {
		return fmt.Errorf("redis.%s: read_timeout must be > 0, got %v", key, src.ReadTimeout)
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
