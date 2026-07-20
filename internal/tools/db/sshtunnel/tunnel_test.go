package sshtunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"example.com/mcp-server/internal/config"
)

type directTCPIPRequest struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func TestTunnelForwardsTCP(t *testing.T) {
	target := startEchoServer(t)
	sshAddr, hostKey := startSSHServer(t)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	knownHostsLine := knownhosts.Line([]string{sshAddr}, hostKey) + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(knownHostsLine), 0o600); err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(sshAddr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	tunnel, err := Open(context.Background(), config.SSHConfig{
		Host: host, Port: port, Username: "test", Password: "secret",
		KnownHosts: knownHostsPath, ConnectTimeout: 5 * time.Second,
	}, target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer tunnel.Close()

	conn, err := net.DialTimeout("tcp", tunnel.LocalAddr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial local tunnel: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	want := []byte("through-ssh")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("echo mismatch: got %q want %q", got, want)
	}

	if err := tunnel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tunnel.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return listener.Addr().String()
}

func startSSHServer(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() != "test" || string(password) != "secret" {
				return nil, fmt.Errorf("authentication failed")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			rawConn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSSHConnection(rawConn, serverConfig)
		}
	}()
	return listener.Addr().String(), signer.PublicKey()
}

func serveSSHConnection(rawConn net.Conn, serverConfig *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(rawConn, serverConfig)
	if err != nil {
		_ = rawConn.Close()
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "direct-tcpip" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel")
			continue
		}
		var request directTCPIPRequest
		if err := ssh.Unmarshal(newChannel.ExtraData(), &request); err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, "invalid forwarding request")
			continue
		}
		target, err := net.Dial("tcp", net.JoinHostPort(request.DestAddr, strconv.Itoa(int(request.DestPort))))
		if err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			_ = target.Close()
			continue
		}
		go ssh.DiscardRequests(channelRequests)
		go bridge(channel, target)
	}
}

func bridge(left, right io.ReadWriteCloser) {
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, 2)
	copyConn := func(dst, src io.ReadWriteCloser) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
		done <- struct{}{}
	}
	go copyConn(left, right)
	go copyConn(right, left)
	<-done
	<-done
}
