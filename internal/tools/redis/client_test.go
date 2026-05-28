package redis

import (
	"bufio"
	"strings"
	"testing"
)

func TestEncodeCommand(t *testing.T) {
	got := string(encodeCommand([]string{"GET", "demo"}))
	want := "*2\r\n$3\r\nGET\r\n$4\r\ndemo\r\n"
	if got != want {
		t.Fatalf("encodeCommand mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReadReply_Array(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("*2\r\n$1\r\n0\r\n*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"))
	reply, err := readReply(reader)
	if err != nil {
		t.Fatalf("readReply error: %v", err)
	}

	arr, ok := reply.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", reply)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
	if arr[0] != "0" {
		t.Fatalf("expected cursor 0, got %#v", arr[0])
	}

	values, ok := arr[1].([]any)
	if !ok {
		t.Fatalf("expected nested []any, got %T", arr[1])
	}
	if len(values) != 2 || values[0] != "foo" || values[1] != "bar" {
		t.Fatalf("unexpected nested values: %#v", values)
	}
}

func TestReadReply_Error(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("-ERR nope\r\n"))
	reply, err := readReply(reader)
	if err != nil {
		t.Fatalf("readReply error: %v", err)
	}
	if _, ok := reply.(respError); !ok {
		t.Fatalf("expected respError, got %T", reply)
	}
}
