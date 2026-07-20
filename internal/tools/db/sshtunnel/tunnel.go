package sshtunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"example.com/mcp-server/internal/config"
)

type Tunnel struct {
	client     *ssh.Client
	listener   net.Listener
	targetAddr string
	acceptDone chan struct{}
	connWG     sync.WaitGroup
	closeOnce  sync.Once
}

func Open(ctx context.Context, cfg config.SSHConfig, targetAddr string) (*Tunnel, error) {
	auth, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}

	sshAddr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	dialCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	rawConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", sshAddr)
	if err != nil {
		return nil, fmt.Errorf("dial SSH server %s: %w", sshAddr, err)
	}

	deadline := time.Now().Add(cfg.ConnectTimeout)
	_ = rawConn.SetDeadline(deadline)
	conn, chans, reqs, err := ssh.NewClientConn(rawConn, sshAddr, &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.ConnectTimeout,
	})
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("SSH handshake with %s failed: %w", sshAddr, err)
	}
	_ = rawConn.SetDeadline(time.Time{})

	client := ssh.NewClient(conn, chans, reqs)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("open local SSH tunnel listener: %w", err)
	}

	t := &Tunnel{
		client:     client,
		listener:   listener,
		targetAddr: targetAddr,
		acceptDone: make(chan struct{}),
	}
	go t.acceptLoop()
	return t, nil
}

func authMethods(cfg config.SSHConfig) ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	if cfg.PrivateKey != "" {
		key, err := os.ReadFile(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("read SSH private key: %w", err)
		}
		var signer ssh.Signer
		if cfg.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(cfg.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}
	if len(methods) == 0 {
		return nil, errors.New("SSH password or private key is required")
	}
	return methods, nil
}

func hostKeyCallback(cfg config.SSHConfig) (ssh.HostKeyCallback, error) {
	if cfg.InsecureSkipHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	callback, err := knownhosts.New(cfg.KnownHosts)
	if err != nil {
		return nil, fmt.Errorf("load SSH known_hosts: %w", err)
	}
	return callback, nil
}

func (t *Tunnel) LocalAddr() string {
	return t.listener.Addr().String()
}

func (t *Tunnel) acceptLoop() {
	defer close(t.acceptDone)
	for {
		localConn, err := t.listener.Accept()
		if err != nil {
			return
		}
		t.connWG.Add(1)
		go t.forward(localConn)
	}
}

func (t *Tunnel) forward(localConn net.Conn) {
	defer t.connWG.Done()
	defer localConn.Close()

	remoteConn, err := t.client.Dial("tcp", t.targetAddr)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	var copies sync.WaitGroup
	copies.Add(2)
	copyConn := func(dst, src net.Conn) {
		defer copies.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
	}
	go copyConn(remoteConn, localConn)
	go copyConn(localConn, remoteConn)
	copies.Wait()
}

func (t *Tunnel) Close() error {
	var errs []error
	t.closeOnce.Do(func() {
		if err := t.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
		if err := t.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
		<-t.acceptDone
		t.connWG.Wait()
	})
	return errors.Join(errs...)
}
