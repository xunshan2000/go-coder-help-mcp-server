package main

import (
	"path/filepath"
	"strings"
	"testing"

	"example.com/mcp-server/internal/config"
)

func TestSQLLogDirUsesConfigDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	want := filepath.Join(root, "logs")

	if got := sqlLogDir(configPath); got != want {
		t.Fatalf("sqlLogDir mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestParseModule(t *testing.T) {
	for _, value := range []string{"all", "mysql", "redis", "apipost"} {
		got, err := parseModule(value)
		if err != nil || string(got) != value {
			t.Fatalf("parseModule(%q) = %q, %v", value, got, err)
		}
	}
	if got, err := parseModule("database"); err != nil || got != moduleMySQL {
		t.Fatalf("database alias = %q, %v", got, err)
	}
	if _, err := parseModule("unknown"); err == nil {
		t.Fatal("unknown module should fail")
	}
}

func TestScopeConfigFiltersEnvironment(t *testing.T) {
	cfg := &config.Config{Environments: map[string]config.EnvironmentConfig{
		"product": {Databases: map[string]config.SourceConfig{"platform": {}}},
	}}

	if err := validateSelection(cfg, moduleMySQL, "product"); err != nil {
		t.Fatal(err)
	}
	if got := serverName(moduleMySQL, "product"); got != "mcp-server-mysql-product" {
		t.Fatalf("serverName = %q", got)
	}
	if got := serverName(moduleAll, ""); got != "mcp-server" {
		t.Fatalf("default serverName = %q", got)
	}
}

func TestScopeConfigRejectsInvalidSelection(t *testing.T) {
	cfg := &config.Config{}
	if err := validateSelection(cfg, moduleMySQL, "product"); err == nil || !strings.Contains(err.Error(), "no database sources") {
		t.Fatalf("missing database error = %v", err)
	}
}
