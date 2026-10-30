package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-test-tool/server/internal/mcp"
)

// fakeSSEServer 模拟旧版 HTTP+SSE MCP 服务：GET 返回 endpoint 事件与响应流，
// POST 接收 JSON-RPC 并把结果写回事件流。
type fakeSSEServer struct {
	mu       sync.Mutex
	clients  map[string]chan string // POST 地址（含 session）→ 事件通道
	endpoint string
}

func (s *fakeSSEServer) handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		session := fmt.Sprintf("sess-%d", time.Now().UnixNano())
		postPath := "/messages?session_id=" + session
		events := make(chan string, 16)
		s.mu.Lock()
		s.clients[postPath] = events
		s.endpoint = postPath
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postPath)
		flusher.Flush()
		for {
			select {
			case event := <-events:
				fmt.Fprint(w, event)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	case http.MethodPost:
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		s.mu.Lock()
		events := s.clients[r.URL.Path+"?"+r.URL.RawQuery]
		s.mu.Unlock()
		if events == nil {
			http.NotFound(w, r)
			return
		}
		var result string
		switch request.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-03-26","serverInfo":{"name":"fake-sse","version":"1.0"}}`
		case "tools/call":
			result = `{"content":[{"type":"text","text":"{\"data\":[{\"id\":\"b1\",\"title\":\"SSE缺陷\"}]}"}]}`
		default:
			result = `{}`
		}
		payload, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(result),
		})
		events <- "event: message\ndata: " + string(payload) + "\n\n"
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestDialOverLegacySSE(t *testing.T) {
	// given: 一个旧版 HTTP+SSE MCP 服务
	server := httptest.NewServer(http.HandlerFunc((&fakeSSEServer{clients: map[string]chan string{}}).handler))
	defer server.Close()

	// when: 握手并调用工具
	session, err := mcp.Dial(context.Background(), server.URL, nil, nil)
	if err != nil {
		t.Fatalf("dial over sse failed: %v", err)
	}
	defer func() { _ = session.Close() }()
	if session.ServerInfo().Name != "fake-sse" {
		t.Fatalf("unexpected server info: %+v", session.ServerInfo())
	}
	result, err := session.CallTool(context.Background(), "get_bugs", map[string]any{"workspace_id": "44165834"})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}

	// then: 通过事件流拿到工具结果
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "SSE缺陷") {
		t.Fatalf("unexpected result: %+v", result)
	}
}
