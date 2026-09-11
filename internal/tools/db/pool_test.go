package db

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/mcp-server/internal/config"
)

func TestNewPoolValidatesAllDriversBeforeNetwork(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan struct{}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- struct{}{}
			_ = conn.Close()
		}
	}()

	cfg := &config.Config{
		Defaults: config.Defaults{MaxRows: 100, QueryTimeout: time.Second},
		Environments: map[string]config.EnvironmentConfig{
			"test1": {Databases: map[string]config.SourceConfig{
				"a_mysql": {
					Driver: "mysql", Host: host, Port: port, Database: "app", Username: "user",
					Pool: config.PoolConfig{MaxConnections: 1, ConnectTimeout: time.Second},
				},
				"z_unknown": {Driver: "missing-driver"},
			}},
		},
	}

	_, err = NewPool(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "z_unknown") || !strings.Contains(err.Error(), "missing-driver") {
		t.Fatalf("NewPool() error = %v, want unknown driver error", err)
	}
	select {
	case <-accepted:
		t.Fatal("database network connection started before all drivers were validated")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNewPoolDoesNotConnectAtStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan struct{}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- struct{}{}
			_ = conn.Close()
		}
	}()

	source := config.SourceConfig{
		Driver: "mysql", Host: host, Port: port, Database: "app", Username: "user",
		Pool: config.PoolConfig{MaxConnections: 1, ConnectTimeout: time.Second},
	}
	sshSource := source
	sshSource.SSH = &config.SSHConfig{
		Host: host, Port: port, Username: "user", Password: "secret", InsecureSkipHostKey: true,
		ConnectTimeout: time.Second,
	}
	cfg := &config.Config{
		Defaults: config.Defaults{MaxRows: 100, QueryTimeout: time.Second},
		Environments: map[string]config.EnvironmentConfig{
			"test": {Databases: map[string]config.SourceConfig{
				"direct": source,
				"ssh":    sshSource,
			}},
		},
	}

	pool, err := NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPool() should not require reachable databases: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Get("test", "direct"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Get("test", "ssh"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-accepted:
		t.Fatal("database network connection started while configuring pool")
	case <-time.After(100 * time.Millisecond):
	}
}
