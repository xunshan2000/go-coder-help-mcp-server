package redis

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type respError string

func (e respError) Error() string {
	return string(e)
}

func (s *Source) PingContext(ctx context.Context) error {
	reply, err := s.Do(ctx, "PING")
	if err != nil {
		return err
	}
	pong, err := asString(reply)
	if err != nil {
		return err
	}
	if pong != "PONG" {
		return fmt.Errorf("unexpected PING reply %q", pong)
	}
	return nil
}

func (s *Source) Do(ctx context.Context, args ...string) (reply any, err error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("redis command cannot be empty")
	}
	if s.ssh != nil {
		defer func() {
			if err != nil {
				s.resetTunnel()
			}
		}()
	}

	addr, err := s.connectionAddr(ctx)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: s.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(commandDeadline(ctx, s.ReadTimeout)); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	if s.Auth != "" {
		if _, err := roundTrip(conn, reader, "AUTH", s.Auth); err != nil {
			return nil, fmt.Errorf("AUTH failed: %w", err)
		}
	}
	if s.Index != 0 {
		if _, err := roundTrip(conn, reader, "SELECT", strconv.Itoa(s.Index)); err != nil {
			return nil, fmt.Errorf("SELECT %d failed: %w", s.Index, err)
		}
	}

	return roundTrip(conn, reader, args...)
}

func commandDeadline(ctx context.Context, fallback time.Duration) time.Time {
	deadline := time.Now().Add(fallback)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		return dl
	}
	return deadline
}

func roundTrip(conn net.Conn, reader *bufio.Reader, args ...string) (any, error) {
	if _, err := conn.Write(encodeCommand(args)); err != nil {
		return nil, err
	}
	reply, err := readReply(reader)
	if err != nil {
		return nil, err
	}
	if redisErr, ok := reply.(respError); ok {
		return nil, redisErr
	}
	return reply, nil
}

func encodeCommand(args []string) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&buf, "$%d\r\n%s\r\n", len(arg), arg)
	}
	return buf.Bytes()
}

func readReply(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '+':
		return readLine(reader)
	case '-':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		return respError(line), nil
	case ':':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer reply %q: %w", line, err)
		}
		return n, nil
	case '$':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("invalid bulk size %q: %w", line, err)
		}
		if size == -1 {
			return nil, nil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("invalid array size %q: %w", line, err)
		}
		if size == -1 {
			return nil, nil
		}
		out := make([]any, 0, size)
		for i := 0; i < size; i++ {
			item, err := readReply(reader)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported redis reply prefix %q", string(prefix))
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
