package main

import (
	"path/filepath"
	"testing"
)

func TestSQLLogDirUsesConfigDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	want := filepath.Join(root, "logs")

	if got := sqlLogDir(configPath); got != want {
		t.Fatalf("sqlLogDir mismatch\nwant: %s\ngot:  %s", want, got)
	}
}
