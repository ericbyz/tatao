package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-test-tool/server/internal/mcp"
)

// fakeMCPServer 实现最小 streamable HTTP MCP 服务：initialize →
// tools/list → tools/call。
func fakeMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if user, password, ok := r.BasicAuth(); !ok || user != "qa" || password != "token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"serverInfo":      map[string]string{"name": "fake-tapd", "version": "1.0"},
				},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"tools": []map[string]any{
					{"name": "get_bugs", "description": "查询缺陷"},
				}},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{
					"content": []map[string]string{{"type": "text", "text": `[{"id":"1","title":"样例缺陷"}]`}},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			})
		}
	})
	return httptest.NewServer(mux)
}

func TestDialListAndCallOverHTTP(t *testing.T) {
	// given: 一个要求 Basic 认证的 MCP 服务
	server := fakeMCPServer(t)
	defer server.Close()
	header := http.Header{}
	header.Set("Authorization", "Basic cWE6dG9rZW4=") // qa:token

	// when: 握手、发现工具并调用
	session, err := mcp.Dial(context.Background(), server.URL+"/mcp", header, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = session.Close() }()
	if session.ServerInfo().Name != "fake-tapd" {
		t.Fatalf("unexpected server info: %+v", session.ServerInfo())
	}
	tools, err := session.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "get_bugs" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	result, err := session.CallTool(context.Background(), "get_bugs", map[string]any{"workspace_id": "1001"})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}

	// then: 工具结果文本可读
	if len(result.Content) != 1 || result.Content[0].Text == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDialRejectsBadCredentials(t *testing.T) {
	server := fakeMCPServer(t)
	defer server.Close()

	if _, err := mcp.Dial(context.Background(), server.URL+"/mcp", nil, nil); err == nil {
		t.Fatal("expected handshake to fail without credentials")
	}
}

func TestDialRejectsEmptyEndpoint(t *testing.T) {
	if _, err := mcp.Dial(context.Background(), "  ", nil, nil); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}
