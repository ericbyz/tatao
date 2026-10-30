// Package mcp implements a minimal Model Context Protocol client kernel.
// It speaks JSON-RPC 2.0 over streamable HTTP or a stdio subprocess and
// covers the handshake plus tool surface that connectors rely on.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// protocolVersion is the MCP revision this client negotiates.
const protocolVersion = "2025-03-26"

// Tool describes one tool advertised by an MCP server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ToolContent is one content block inside a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is the outcome of a tools/call round trip.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ServerInfo identifies the remote MCP implementation.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type transport interface {
	request(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// Client is a live MCP session. It is safe for one caller at a time; the
// connectors build short-lived sessions per operation.
type Client struct {
	transport  transport
	serverInfo ServerInfo
}

// Dial opens a session: http(s) endpoints use the streamable HTTP transport
// with automatic fallback to the legacy HTTP+SSE flavor, anything else is
// treated as a stdio command line. The handshake (initialize plus the
// initialized notification) completes before Dial returns.
func Dial(ctx context.Context, endpoint string, header http.Header, env []string) (*Client, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return nil, errors.New("mcp: endpoint is empty")
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		tr, err := newStdioTransport(trimmed, env)
		if err != nil {
			return nil, err
		}
		client := &Client{transport: tr}
		if err := client.initialize(ctx); err != nil {
			_ = tr.close()
			return nil, err
		}
		return client, nil
	}
	return dialHTTP(ctx, trimmed, header)
}

// dialHTTP 先按 streamable HTTP 握手，服务端不支持时（4xx/5xx）降级到
// 旧版 HTTP+SSE 传输。
func dialHTTP(ctx context.Context, endpoint string, header http.Header) (*Client, error) {
	httpTransport, err := newHTTPTransport(endpoint, header)
	if err != nil {
		return nil, err
	}
	client := &Client{transport: httpTransport}
	if err := client.initialize(ctx); err == nil {
		return client, nil
	} else if !isStatusError(err) {
		_ = httpTransport.close()
		return nil, err
	}
	_ = httpTransport.close()

	sse, err := newSSETransport(ctx, endpoint, header)
	if err != nil {
		return nil, fmt.Errorf("mcp: initialize failed (%v) and SSE fallback failed: %w", "streamable http unsupported", err)
	}
	client = &Client{transport: sse}
	if err := client.initialize(ctx); err != nil {
		_ = sse.close()
		return nil, err
	}
	return client, nil
}

// statusError 标记 HTTP 层失败（非 2xx），用于判断是否可以降级传输方式。
type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("http %d: %s", e.code, truncate(e.body, 120))
	}
	return fmt.Sprintf("http %d", e.code)
}

func isStatusError(err error) bool {
	var status *statusError
	return errors.As(err, &status)
}

// ServerInfo returns the identity reported during the handshake.
func (c *Client) ServerInfo() ServerInfo { return c.serverInfo }

// ListTools returns the tools advertised by the server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	raw, err := c.transport.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp: list tools: %w", err)
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("mcp: decode tool list: %w", err)
	}
	return result.Tools, nil
}

// CallTool invokes one tool and returns its result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	raw, err := c.transport.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return ToolResult{}, fmt.Errorf("mcp: call tool %s: %w", name, err)
	}
	var result ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return ToolResult{}, fmt.Errorf("mcp: decode tool result: %w", err)
	}
	return result, nil
}

// Close shuts the transport down. For stdio sessions the subprocess is killed.
func (c *Client) Close() error { return c.transport.close() }

func (c *Client) initialize(ctx context.Context) error {
	raw, err := c.transport.request(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "ai-test-tool", "version": "0.1.0"},
	})
	if err != nil {
		return fmt.Errorf("mcp: initialize: %w", err)
	}
	var result struct {
		ServerInfo ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("mcp: decode initialize result: %w", err)
	}
	c.serverInfo = result.ServerInfo
	if err := c.transport.notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("mcp: complete handshake: %w", err)
	}
	return nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func decodeResponse(raw []byte) (json.RawMessage, error) {
	var envelope rpcEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("mcp: decode response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}

// httpTransport speaks the streamable HTTP flavor: every request is a POST
// that answers with either a JSON document or a text/event-stream body.
type httpTransport struct {
	endpoint string
	header   http.Header
	client   *http.Client
	mu       sync.Mutex
	session  string
	nextID   atomic.Int64
}

func newHTTPTransport(endpoint string, header http.Header) (*httpTransport, error) {
	if header == nil {
		header = http.Header{}
	}
	return &httpTransport{endpoint: endpoint, header: header.Clone(), client: &http.Client{}}, nil
}

func (t *httpTransport) roundTrip(ctx context.Context, payload []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, values := range t.header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()
	if session != "" {
		request.Header.Set("Mcp-Session-Id", session)
	}

	response, err := t.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("mcp: request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if sid := response.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.session = sid
		t.mu.Unlock()
	}
	if response.StatusCode == http.StatusAccepted {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("mcp: read response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, &statusError{code: response.StatusCode, body: truncate(string(body), 200)}
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		return ssePayload(body)
	}
	return body, nil
}

func (t *httpTransport) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: t.nextID.Add(1), Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode request: %w", err)
	}
	raw, err := t.roundTrip(ctx, payload)
	if err != nil {
		return nil, err
	}
	return decodeResponse(raw)
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("mcp: encode notification: %w", err)
	}
	_, err = t.roundTrip(ctx, payload)
	return err
}

func (t *httpTransport) close() error { return nil }

// ssePayload extracts the first JSON data line from an SSE body.
func ssePayload(body []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if json.Valid([]byte(payload)) {
			return []byte(payload), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp: read event stream: %w", err)
	}
	return nil, errors.New("mcp: event stream contained no JSON data")
}

// stdioTransport speaks newline-delimited JSON-RPC with a subprocess.
type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
	mu     sync.Mutex
	nextID atomic.Int64
	closed bool
}

func newStdioTransport(command string, env []string) (*stdioTransport, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("mcp: stdio command is empty")
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Env = append(os.Environ(), env...)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %s: %w", fields[0], err)
	}
	return &stdioTransport{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64*1024), stderr: stderr}, nil
}

func (t *stdioTransport) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode request: %w", err)
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, errors.New("mcp: session closed")
	}
	if _, err := t.stdin.Write(append(payload, '\n')); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("mcp: write to %s: %w", t.cmd.Path, err)
	}
	t.mu.Unlock()

	for {
		line, err := t.readLine(ctx)
		if err != nil {
			return nil, err
		}
		var envelope rpcEnvelope
		if jsonErr := json.Unmarshal(line, &envelope); jsonErr != nil {
			continue // skip non JSON-RPC output such as startup banners
		}
		if len(envelope.ID) == 0 || string(envelope.ID) != fmt.Sprint(id) {
			continue // server notifications and unrelated requests
		}
		return decodeResponse(line)
	}
}

func (t *stdioTransport) readLine(ctx context.Context) ([]byte, error) {
	type readOutcome struct {
		line []byte
		err  error
	}
	outcome := make(chan readOutcome, 1)
	go func() {
		line, err := t.stdout.ReadBytes('\n')
		outcome <- readOutcome{line, err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("mcp: %s did not answer in time", t.cmd.Path)
	case result := <-outcome:
		if result.err != nil {
			return nil, fmt.Errorf("mcp: %s stopped: %w: %s", t.cmd.Path, result.err, truncate(t.stderr.String(), 200))
		}
		return result.line, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("mcp: encode notification: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("mcp: session closed")
	}
	if _, err := t.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("mcp: write notification: %w", err)
	}
	return nil
}

func (t *stdioTransport) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	_ = t.stdin.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return t.cmd.Wait()
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
