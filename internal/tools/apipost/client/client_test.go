package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"example.com/mcp-server/internal/config"
)

func newClient(t *testing.T, baseURL string, maxBytes int64) *Client {
	t.Helper()
	c, err := New(config.ApipostConfig{
		BaseURL:          baseURL,
		APIToken:         "apt_secret_abcdef",
		ProjectName:      "my-project",
		RequestTimeout:   5 * time.Second,
		MaxResponseBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestClient_GlobalHeadersInjected(t *testing.T) {
	var (
		gotToken   string
		gotProject string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("api-token")
		gotProject = r.Header.Get("project_name")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024*1024)
	var out map[string]any
	if _, err := c.Do(context.Background(), http.MethodGet, "/open/project/list", nil, nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotToken != "apt_secret_abcdef" {
		t.Fatalf("api-token header mismatch: %q", gotToken)
	}
	if gotProject != "my-project" {
		t.Fatalf("project_name header mismatch: %q", gotProject)
	}
}

func TestClient_TruncateLargeBody(t *testing.T) {
	// 构造 2 KiB 响应；max 设 1 KiB
	payload := `{"code":0,"msg":"","data":"` + strings.Repeat("x", 2*1024) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024)
	var out map[string]any
	meta, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, &out)
	// 截断后 JSON 一般解析失败；接受任一路径，关键是 meta.Truncated=true
	if meta == nil || !meta.Truncated {
		t.Fatalf("expected Truncated=true, got meta=%+v err=%v", meta, err)
	}
}

func TestClient_BusinessCodeNonZeroBecomesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":10020,"msg":"target_id 不存在","data":null}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024*1024)
	_, err := c.Do(context.Background(), http.MethodPost, "/x", nil, map[string]any{"k": "v"}, nil)
	if err == nil {
		t.Fatalf("expected error for code!=0")
	}
	if !strings.Contains(err.Error(), "10020") || !strings.Contains(err.Error(), "target_id 不存在") {
		t.Fatalf("err missing code/msg: %v", err)
	}
}

func TestClient_HTTP4xxBecomesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Unauthorized")
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024*1024)
	meta, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for 401")
	}
	if meta.StatusCode != 401 {
		t.Fatalf("expected meta.StatusCode=401, got %d", meta.StatusCode)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("err missing 401: %v", err)
	}
}

func TestClient_ErrorDoesNotLeakToken(t *testing.T) {
	// 构造一个会让错误消息包含 token 的场景：直接 ReplaceAll 校验
	c := newClient(t, "https://example.com", 1024*1024)
	leakMsg := "some error containing apt_secret_abcdef in the middle"
	sanitized := c.sanitize(&stubErr{leakMsg}).Error()
	if strings.Contains(sanitized, "apt_secret_abcdef") {
		t.Fatalf("token leaked: %q", sanitized)
	}
	if !strings.Contains(sanitized, "***") {
		t.Fatalf("expected *** substitution, got %q", sanitized)
	}
}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }

func TestClient_ContextTimeoutTranslated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024*1024)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Do(ctx, http.MethodGet, "/x", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "请求超时") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout-like error, got %v", err)
	}
}

func TestClient_OutDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "ok",
			"data": map[string]any{"hello": "world", "n": 42},
		})
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, 1024*1024)
	var out struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	meta, err := c.Do(context.Background(), http.MethodGet, "/x", url.Values{"a": []string{"1"}}, nil, &out)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if meta.StatusCode != 200 {
		t.Fatalf("status: %d", meta.StatusCode)
	}
	if out.Data["hello"] != "world" {
		t.Fatalf("out.Data unexpected: %+v", out.Data)
	}
}
