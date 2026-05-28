// Package client 实现 Apipost 开放接口 V2 的 HTTP 客户端。
// 关键职责：
//  - 注入两个全局 Header：api-token、project_name
//  - 响应体大小上限、超时控制
//  - 统一的错误翻译（ctx 超时 / HTTP 非 2xx / Apipost code != 0 / JSON 解析失败）
//  - Token 脱敏：任何对外输出路径 MUST 经 sanitize 去掉 token 明文
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"example.com/mcp-server/internal/config"
)

// Client 是 Apipost 开放接口的同步 HTTP 客户端。
// 线程安全：底层 http.Client 可并发使用。
type Client struct {
	baseURL          *url.URL
	apiToken         string
	projectName      string
	defaultProjectID string
	defaultTeamID    string
	httpClient       *http.Client
	maxResponseBytes int64
}

// ResponseMeta 描述一次请求的状态附加信息；成功与失败路径都返回。
type ResponseMeta struct {
	StatusCode int
	Truncated  bool
	BodyBytes  int64
}

// New 根据 ApipostConfig 构造客户端；必填字段已由 config.Validate 把关。
func New(cfg config.ApipostConfig) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("apipost: parse base_url: %w", err)
	}
	return &Client{
		baseURL:          u,
		apiToken:         cfg.APIToken,
		projectName:      cfg.ProjectName,
		defaultProjectID: cfg.ProjectID,
		defaultTeamID:    cfg.TeamID,
		httpClient:       &http.Client{Timeout: cfg.RequestTimeout},
		maxResponseBytes: cfg.MaxResponseBytes,
	}, nil
}

// DefaultProjectID 返回配置中的默认项目 ID（工具层回落使用）。
func (c *Client) DefaultProjectID() string { return c.defaultProjectID }

// DefaultTeamID 返回配置中的默认团队 ID。
func (c *Client) DefaultTeamID() string { return c.defaultTeamID }

// Do 向 baseURL+path 发起一次请求，按需带 query 和 JSON body。
// 响应体按 Apipost 统一结构 {code, msg, data} 解析到 out（out 必须是指针）。
// 返回的 error 已经过 Token 脱敏；外层可直接透传给 LLM。
//
// 成功路径（HTTP 2xx 且 JSON 可解析）：err == nil，meta.StatusCode 填充。
// 失败路径：err != nil，meta 仍会填充已知字段（StatusCode 可能为 0 / 非 2xx / 2xx 但 code != 0）。
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	out any,
) (*ResponseMeta, error) {
	reqURL := c.baseURL.JoinPath(path)
	if len(query) > 0 {
		reqURL.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return &ResponseMeta{}, fmt.Errorf("apipost: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return &ResponseMeta{}, c.sanitize(fmt.Errorf("apipost: build request: %w", err))
	}
	req.Header.Set("api-token", c.apiToken)
	req.Header.Set("project_name", c.projectName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return &ResponseMeta{}, errors.New("apipost: 请求超时")
		}
		return &ResponseMeta{}, c.sanitize(fmt.Errorf("apipost: request failed: %w", err))
	}
	defer resp.Body.Close()

	meta := &ResponseMeta{StatusCode: resp.StatusCode}
	limited := io.LimitReader(resp.Body, c.maxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return meta, c.sanitize(fmt.Errorf("apipost: read body: %w", err))
	}
	meta.BodyBytes = int64(len(raw))
	if int64(len(raw)) > c.maxResponseBytes {
		meta.Truncated = true
		raw = raw[:c.maxResponseBytes]
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(raw)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return meta, c.sanitize(fmt.Errorf("apipost: http %d: %s", resp.StatusCode, snippet))
	}

	// 先解析成 {code, msg, data} 通用结构；失败则报错
	var shell struct {
		Code json.Number `json:"code"`
		Msg  string      `json:"msg"`
	}
	if err := json.Unmarshal(raw, &shell); err != nil {
		snippet := string(raw)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return meta, c.sanitize(fmt.Errorf("apipost: 响应 JSON 解析失败: %v: %s", err, snippet))
	}
	if shell.Code.String() != "" && shell.Code.String() != "0" {
		return meta, c.sanitize(fmt.Errorf("apipost: code=%s msg=%s", shell.Code.String(), shell.Msg))
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return meta, c.sanitize(fmt.Errorf("apipost: decode into out: %w", err))
		}
	}
	return meta, nil
}

// sanitize 把 err.Error() 中的 token 明文替换为 ***；返回同语义的新 error。
// 输入 err == nil 时返回 nil。
func (c *Client) sanitize(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if c.apiToken != "" && strings.Contains(msg, c.apiToken) {
		msg = strings.ReplaceAll(msg, c.apiToken, "***")
		return errors.New(msg)
	}
	return err
}

// ForDisplayBaseURL 返回脱去路径片段的 base URL 字符串，供启动日志使用（不含 token）。
func (c *Client) ForDisplayBaseURL() string { return c.baseURL.String() }
