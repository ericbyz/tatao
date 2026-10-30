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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// sseTransport 实现旧版 HTTP+SSE 传输：客户端 GET 建立事件流接收消息，
// 服务端通过首个 endpoint 事件告知 POST 地址，请求结果也经事件流返回。
type sseTransport struct {
	endpoint  string
	header    http.Header
	client    *http.Client
	postURL   atomic.Value // string
	messages  chan []byte
	endpointC chan string
	closed    chan struct{}
	closeOnce sync.Once
	body      io.ReadCloser
	nextID    atomic.Int64
	ready     chan error
}

const sseHandshakeTimeout = 10 * time.Second

func newSSETransport(ctx context.Context, endpoint string, header http.Header) (*sseTransport, error) {
	if header == nil {
		header = http.Header{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: build sse request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	for key, values := range header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("mcp: open sse stream: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		_ = response.Body.Close()
		return nil, &statusError{code: response.StatusCode, body: string(body)}
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		_ = response.Body.Close()
		return nil, fmt.Errorf("mcp: endpoint did not return an event stream: %s", truncate(string(body), 120))
	}

	transport := &sseTransport{
		endpoint:  endpoint,
		header:    header.Clone(),
		client:    client,
		messages:  make(chan []byte, 32),
		endpointC: make(chan string, 4),
		closed:    make(chan struct{}),
		body:      response.Body,
		ready:     make(chan error, 1),
	}
	go transport.readLoop(response.Body)

	// 等待服务端下发 endpoint 事件，否则无法发送请求。
	select {
	case postURL := <-transport.endpointC:
		resolved, err := resolveEndpoint(endpoint, postURL)
		if err != nil {
			transport.close()
			return nil, err
		}
		transport.postURL.Store(resolved)
	case err := <-transport.ready:
		transport.close()
		return nil, fmt.Errorf("mcp: sse stream ended before endpoint event: %v", err)
	case <-time.After(sseHandshakeTimeout):
		transport.close()
		return nil, errors.New("mcp: timed out waiting for endpoint event")
	}
	return transport, nil
}

func (t *sseTransport) readLoop(body io.ReadCloser) {
	defer close(t.ready)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var event string
	var data bytes.Buffer
	for scanner.Scan() {
		select {
		case <-t.closed:
			return
		default:
		}
		line := scanner.Text()
		switch {
		case line == "":
			if data.Len() == 0 {
				event = ""
				continue
			}
			payload := strings.TrimSpace(data.String())
			name := event
			event = ""
			data.Reset()
			if name == "endpoint" {
				select {
				case t.endpointC <- payload:
				case <-t.closed:
					return
				}
				continue
			}
			if json.Valid([]byte(payload)) {
				select {
				case t.messages <- []byte(payload):
				case <-t.closed:
					return
				default: // 队列满时丢弃最旧消息，保持会话存活
					select {
					case <-t.messages:
					default:
					}
					select {
					case t.messages <- []byte(payload):
					case <-t.closed:
						return
					}
				}
			}
		case strings.HasPrefix(line, ":"):
			// keep-alive 注释
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	t.ready <- scanner.Err()
}

func (t *sseTransport) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode request: %w", err)
	}
	if err := t.post(ctx, payload); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("mcp: %s did not answer in time", method)
		case raw := <-t.messages:
			var envelope rpcEnvelope
			if jsonErr := json.Unmarshal(raw, &envelope); jsonErr != nil {
				continue // 忽略无法解析的推送
			}
			if len(envelope.ID) == 0 || string(envelope.ID) != fmt.Sprint(id) {
				continue // 服务端通知或过期响应
			}
			return decodeResponse(raw)
		case <-t.closed:
			return nil, errors.New("mcp: sse session closed")
		}
	}
}

func (t *sseTransport) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("mcp: encode notification: %w", err)
	}
	return t.post(ctx, payload)
}

func (t *sseTransport) post(ctx context.Context, payload []byte) error {
	postURL, _ := t.postURL.Load().(string)
	if postURL == "" {
		return errors.New("mcp: sse endpoint not ready")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mcp: build post: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, values := range t.header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := t.client.Do(request)
	if err != nil {
		return fmt.Errorf("mcp: post failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusAccepted {
		return nil // legacy SSE 约定 202；部分实现返回 200，结果走事件流
	}
	return &statusError{code: response.StatusCode}
}

func (t *sseTransport) close() error {
	t.closeOnce.Do(func() {
		close(t.closed)
		_ = t.body.Close()
	})
	return nil
}

// resolveEndpoint 把服务端下发的相对地址解析为可 POST 的绝对地址。
func resolveEndpoint(base, endpoint string) (string, error) {
	if endpoint == "" {
		return "", errors.New("mcp: empty endpoint event")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("mcp: parse endpoint event: %w", err)
	}
	if parsed.IsAbs() {
		return endpoint, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("mcp: parse base url: %w", err)
	}
	return baseURL.ResolveReference(parsed).String(), nil
}
