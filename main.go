package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/server"
	"example.com/mcp-server/internal/tools"
	"example.com/mcp-server/internal/tools/add"
	"example.com/mcp-server/internal/tools/apipost"
	apipostclient "example.com/mcp-server/internal/tools/apipost/client"
	"example.com/mcp-server/internal/tools/db"
	"example.com/mcp-server/internal/tools/db/sqllog"
	redistools "example.com/mcp-server/internal/tools/redis"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv := server.New("mcp-server", "0.3.0")
	reg := tools.NewRegistry(srv)

	add.Register(reg)

	if len(cfg.Databases) > 0 {
		pool, err := db.NewPool(context.Background(), cfg)
		if err != nil {
			log.Fatalf("init db pool: %v", err)
		}
		defer pool.Close()

		logger, err := sqllog.New("./logs")
		if err != nil {
			log.Fatalf("init sql audit logger: %v", err)
		}
		defer logger.Close()

		db.Register(reg, pool, cfg.Defaults, logger)
	} else {
		fmt.Fprintln(os.Stderr, "[mcp-server] databases not configured; skipping db tools")
	}

	if len(cfg.Redis) > 0 {
		pool, err := redistools.NewPool(context.Background(), cfg)
		if err != nil {
			log.Fatalf("init redis pool: %v", err)
		}
		defer pool.Close()

		redistools.Register(reg, pool)
	} else {
		fmt.Fprintln(os.Stderr, "[mcp-server] redis not configured; skipping redis tools")
	}

	if cfg.Apipost != nil {
		client, err := apipostclient.New(*cfg.Apipost)
		if err != nil {
			log.Fatalf("init apipost client: %v", err)
		}
		apipost.Register(reg, client)
		fmt.Fprintf(os.Stderr, "[mcp-server] apipost enabled: base_url=%s project_name=%s timeout=%s\n",
			client.ForDisplayBaseURL(), cfg.Apipost.ProjectName, cfg.Apipost.RequestTimeout)
	} else {
		fmt.Fprintln(os.Stderr, "[mcp-server] apipost not configured; skipping apipost tools")
	}

	if err := server.Serve(srv); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
