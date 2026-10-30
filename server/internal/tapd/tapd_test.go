package tapd_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/tapd"
)

func TestOpenAPIWorkspacesAndSync(t *testing.T) {
	// given: 模拟 TAPD OpenAPI（包裹形式 + 裸字段形式混合返回）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, password, ok := r.BasicAuth(); !ok || user != "qa" || password != "pw" {
			t.Fatalf("missing basic auth: %q %q", user, password)
		}
		switch r.URL.Path {
		case "/workspaces/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"Workspace": map[string]any{"id": "42", "name": "核心交易"}},
			}})
		case "/bugs":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"Bug": map[string]any{
					"id": "b1", "title": "登录超时", "description": "<p>第一步&nbsp;登录</p><p>第二步 打开首页</p>",
					"status": "new", "priority": "2",
				}},
			}})
		case "/stories":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"id": "s1", "name": "支持多端登录", "status": "develop", "priority": "9"},
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	manager := tapd.NewManager(tapd.Settings{
		Transport: tapd.TransportOpenAPI,
		BaseURL:   server.URL,
		APIUser:   "qa",
		HasSecret: true,
	}, "pw")

	// when: 探测连接并列出工作区
	status := manager.Status(context.Background())
	workspaces, err := manager.Workspaces(context.Background())
	if err != nil {
		t.Fatalf("workspaces failed: %v", err)
	}

	// then: 连接成功且工作区映射正确
	if !status.Connected || len(workspaces) != 1 || workspaces[0].ID != "42" {
		t.Fatalf("unexpected status/workspaces: %+v %+v", status, workspaces)
	}

	// when: 同步缺陷与需求
	items, err := manager.Sync(context.Background(), "42", nil)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// then: 字段映射与 HTML 清洗正确
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %+v", items)
	}
	if items[0].Kind != tapd.KindBug || items[0].Title != "登录超时" || items[0].Priority != "P1" {
		t.Fatalf("unexpected bug item: %+v", items[0])
	}
	if items[0].Description != "第一步 登录 第二步 打开首页" {
		t.Fatalf("description not cleaned: %q", items[0].Description)
	}
	if items[1].Kind != tapd.KindRequirement || items[1].Title != "支持多端登录" || items[1].Priority != "P2" {
		t.Fatalf("unexpected story item: %+v", items[1])
	}
}

func TestOpenAPIAuthFailureSurfacesMessage(t *testing.T) {
	// given: 返回 401 的模拟服务
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "authorization failed"})
	}))
	defer server.Close()
	manager := tapd.NewManager(tapd.Settings{
		Transport: tapd.TransportOpenAPI,
		BaseURL:   server.URL,
		APIUser:   "wrong",
	}, "wrong")

	// when: 探测连接
	status := manager.Status(context.Background())

	// then: 状态标记为未连通且消息可读
	if status.Connected || status.Message == "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestUnconfiguredManagerReportsNotConfigured(t *testing.T) {
	manager := tapd.NewManager(tapd.Settings{}, "")

	if _, err := manager.Sync(context.Background(), "42", nil); err != tapd.ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	status := manager.Status(context.Background())
	if status.Configured || status.Connected {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestMCPSyncViaFakeServer(t *testing.T) {
	// given: 一个以 data 数组返回缺陷的 MCP 服务
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"serverInfo": map[string]string{"name": "tapd"}},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"tools": []map[string]string{{"name": "get_bugs"}}},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{
					"content": []map[string]string{{"type": "text", "text": `{"data":[{"id":"b9","title":"页面空白","status":"new","priority":"1"}]}`}},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	manager := tapd.NewManager(tapd.Settings{
		Transport: tapd.TransportMCP,
		Endpoint:  server.URL,
	}, "")

	// when: 同步缺陷
	items, err := manager.Sync(context.Background(), "42", []string{tapd.KindBug})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// then: 结果与直连方式一致
	if len(items) != 1 || items[0] != (domain.ExternalItem{
		ID: "b9", Kind: tapd.KindBug, Title: "页面空白", Status: "new", Priority: "P0",
	}) {
		t.Fatalf("unexpected items: %+v", items)
	}
}
