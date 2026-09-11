package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/server"
	"example.com/mcp-server/internal/tools"
	"example.com/mcp-server/internal/tools/apipost"
	apipostclient "example.com/mcp-server/internal/tools/apipost/client"
	"example.com/mcp-server/internal/tools/db"
	"example.com/mcp-server/internal/tools/db/sqllog"
	redistools "example.com/mcp-server/internal/tools/redis"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	moduleName := flag.String("module", "all", "tool module to serve: all, mysql, redis, or apipost")
	environment := flag.String("env", "", "only serve sources from this environment")
	flag.Parse()

	module, err := parseModule(*moduleName)
	if err != nil {
		log.Fatal(err)
	}

	if module == moduleApipost && *environment != "" {
		log.Fatal("--env cannot be used with --module apipost")
	}
	cfg, err := config.LoadScoped(*configPath, config.LoadOptions{
		Environment: *environment,
		Database:    module.includes(moduleMySQL),
		Redis:       module.includes(moduleRedis),
		Apipost:     module.includes(moduleApipost),
	})
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := validateSelection(cfg, module, *environment); err != nil {
		log.Fatal(err)
	}

	srv := server.New(serverName(module, *environment), "0.5.0")
	reg := tools.NewRegistry(srv)

	if module.includes(moduleMySQL) && cfg.Features.DatabaseEnabled() && cfg.HasDatabases() {
		pool, err := db.NewPool(context.Background(), cfg)
		if err != nil {
			log.Fatalf("init db pool: %v", err)
		}
		defer pool.Close()

		var logger *sqllog.Logger
		if cfg.Features.SQLAuditEnabled() {
			logger, err = sqllog.New(sqlLogDir(*configPath))
			if err != nil {
				log.Fatalf("init sql audit logger: %v", err)
			}
			defer logger.Close()
		} else {
			fmt.Fprintln(os.Stderr, "[mcp-server] sql audit disabled")
		}

		db.Register(reg, pool, cfg.Defaults, logger)
	} else if module.includes(moduleMySQL) && !cfg.Features.DatabaseEnabled() {
		fmt.Fprintln(os.Stderr, "[mcp-server] database disabled")
	} else if module.includes(moduleMySQL) {
		fmt.Fprintln(os.Stderr, "[mcp-server] databases not configured; skipping db tools")
	}

	if module.includes(moduleRedis) && cfg.Features.RedisEnabled() && cfg.HasRedis() {
		pool, err := redistools.NewPool(context.Background(), cfg)
		if err != nil {
			log.Fatalf("init redis pool: %v", err)
		}
		defer pool.Close()

		redistools.Register(reg, pool)
	} else if module.includes(moduleRedis) && !cfg.Features.RedisEnabled() {
		fmt.Fprintln(os.Stderr, "[mcp-server] redis disabled")
	} else if module.includes(moduleRedis) {
		fmt.Fprintln(os.Stderr, "[mcp-server] redis not configured; skipping redis tools")
	}

	if module.includes(moduleApipost) && cfg.Features.ApipostEnabled() && cfg.Apipost != nil {
		client, err := apipostclient.New(*cfg.Apipost)
		if err != nil {
			log.Fatalf("init apipost client: %v", err)
		}
		apipost.Register(reg, client)
		fmt.Fprintf(os.Stderr, "[mcp-server] apipost enabled: base_url=%s project_name=%s timeout=%s\n",
			client.ForDisplayBaseURL(), cfg.Apipost.ProjectName, cfg.Apipost.RequestTimeout)
	} else if module.includes(moduleApipost) && !cfg.Features.ApipostEnabled() {
		fmt.Fprintln(os.Stderr, "[mcp-server] apipost disabled")
	} else if module.includes(moduleApipost) {
		fmt.Fprintln(os.Stderr, "[mcp-server] apipost not configured; skipping apipost tools")
	}

	if err := server.Serve(srv); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

type toolModule string

const (
	moduleAll     toolModule = "all"
	moduleMySQL   toolModule = "mysql"
	moduleRedis   toolModule = "redis"
	moduleApipost toolModule = "apipost"
)

func parseModule(value string) (toolModule, error) {
	switch toolModule(value) {
	case moduleAll, moduleMySQL, moduleRedis, moduleApipost:
		return toolModule(value), nil
	case "database":
		return moduleMySQL, nil
	default:
		return "", fmt.Errorf("invalid --module %q: expected all, mysql, redis, or apipost", value)
	}
}

func (m toolModule) includes(target toolModule) bool {
	return m == moduleAll || m == target
}

func validateSelection(cfg *config.Config, module toolModule, environment string) error {
	switch module {
	case moduleMySQL:
		if !cfg.Features.DatabaseEnabled() {
			return fmt.Errorf("mysql module is disabled by features.database")
		}
		if !cfg.HasDatabases() {
			return fmt.Errorf("mysql module has no database sources%s", environmentSuffix(environment))
		}
	case moduleRedis:
		if !cfg.Features.RedisEnabled() {
			return fmt.Errorf("redis module is disabled by features.redis")
		}
		if !cfg.HasRedis() {
			return fmt.Errorf("redis module has no sources%s", environmentSuffix(environment))
		}
	case moduleApipost:
		if !cfg.Features.ApipostEnabled() {
			return fmt.Errorf("apipost module is disabled by features.apipost")
		}
		if cfg.Apipost == nil {
			return fmt.Errorf("apipost module is not configured")
		}
	}
	return nil
}

func environmentSuffix(environment string) string {
	if environment == "" {
		return ""
	}
	return fmt.Sprintf(" in environment %q", environment)
}

func serverName(module toolModule, environment string) string {
	if module == moduleAll && environment == "" {
		return "mcp-server"
	}
	name := "mcp-server-" + string(module)
	if environment != "" {
		name += "-" + environment
	}
	return name
}

func sqlLogDir(configPath string) string {
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return filepath.Join(".", "logs")
	}
	return filepath.Join(filepath.Dir(absConfig), "logs")
}
