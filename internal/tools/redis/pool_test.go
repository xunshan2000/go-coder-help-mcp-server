package redis

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"example.com/mcp-server/internal/config"
)

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

	direct := config.RedisConfig{Host: host, Port: port, DialTimeout: time.Second, ReadTimeout: time.Second}
	viaSSH := direct
	viaSSH.SSH = &config.SSHConfig{
		Host: host, Port: port, Username: "user", Password: "secret", InsecureSkipHostKey: true,
		ConnectTimeout: time.Second,
	}
	cfg := &config.Config{Environments: map[string]config.EnvironmentConfig{
		"test": {Redis: map[string]config.RedisConfig{
			"direct": direct,
			"ssh":    viaSSH,
		}},
	}}

	pool, err := NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPool() should not require reachable Redis sources: %v", err)
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
		t.Fatal("Redis network connection started while configuring pool")
	case <-time.After(100 * time.Millisecond):
	}
}
