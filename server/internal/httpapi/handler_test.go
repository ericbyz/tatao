package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/httpapi"
	"ai-test-tool/server/internal/store"
	"ai-test-tool/server/internal/tapd"
)

// gatewayStub 覆盖 TAPDGateway 的最小实现，用于与 TAPD 无关的用例。
type gatewayStub struct {
	status tapd.Status
}

func (g gatewayStub) Settings() tapd.Settings { return tapd.Settings{} }

func (g gatewayStub) Apply(tapd.Settings, string) {}

func (g gatewayStub) Status(context.Context) tapd.Status { return g.status }

func (g gatewayStub) Workspaces(context.Context) ([]tapd.Workspace, error) {
	return nil, tapd.ErrNotConfigured
}

func (g gatewayStub) WorkspaceByID(context.Context, string) (tapd.Workspace, bool) {
	return tapd.Workspace{}, false
}

func (g gatewayStub) Sync(context.Context, string, []string) ([]domain.ExternalItem, error) {
	return nil, tapd.ErrNotConfigured
}

func TestCreateProject(t *testing.T) {
	handler := httpapi.New(store.NewMemory(), gatewayStub{})
	body, _ := json.Marshal(map[string]string{"name": "结算服务", "source": "local", "localPath": `D:\work\settlement`})
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	// then: 项目创建成功且不回显敏感信息
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	var project domain.Project
	if err := json.NewDecoder(response.Body).Decode(&project); err != nil {
		t.Fatal(err)
	}
	if project.Name != "结算服务" || project.ID == "" {
		t.Fatalf("unexpected project: %+v", project)
	}
}

func TestCreateTAPDProjectRequiresWorkspace(t *testing.T) {
	handler := httpapi.New(store.NewMemory(), gatewayStub{})
	body, _ := json.Marshal(map[string]string{"name": "缺工作区", "source": "tapd"})
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	// then: TAPD 项目必须绑定工作区
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

func TestCreateEnvironmentDoesNotExposeSecret(t *testing.T) {
	handler := httpapi.New(store.NewMemory(), gatewayStub{})
	body := []byte(`{"name":"回归环境","host":"10.0.0.8","port":22,"username":"qa","authType":"password","secret":"do-not-return"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/environments", bytes.NewReader(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("do-not-return")) {
		t.Fatal("response leaked environment secret")
	}
}

func TestAssignEnvironmentToMultipleProjects(t *testing.T) {
	memory := store.NewMemory()
	projectA := memory.AddProject(domain.Project{Name: "项目 A", Source: domain.ProjectSourceLocal})
	projectB := memory.AddProject(domain.Project{Name: "项目 B", Source: domain.ProjectSourceTAPD})
	environment, err := memory.AddEnvironment(domain.Environment{Name: "共享 Linux", Host: "127.0.0.1", Port: 22, Username: "qa"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(memory, gatewayStub{})
	body, _ := json.Marshal(map[string][]string{"projectIds": {projectA.ID, projectB.ID}})
	request := httptest.NewRequest(http.MethodPatch, "/api/environments/"+environment.ID+"/projects", bytes.NewReader(body))
	request.SetPathValue("id", environment.ID)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var assignedEnvironment domain.Environment
	if err := json.NewDecoder(response.Body).Decode(&assignedEnvironment); err != nil {
		t.Fatal(err)
	}
	if len(assignedEnvironment.ProjectIDs) != 2 {
		t.Fatalf("expected two project assignments, got %+v", assignedEnvironment.ProjectIDs)
	}
}

// fakeTAPD 模拟 TAPD OpenAPI，覆盖工作区、缺陷与需求查询。
type fakeTAPD struct{ requests int }

func (f *fakeTAPD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests++
	if user, password, ok := r.BasicAuth(); !ok || user != "qa-user" || password != "qa-secret" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "401", "message": "authorization failed"})
		return
	}
	switch r.URL.Path {
	case "/workspaces/projects":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"Workspace": map[string]any{"id": "1001", "name": "支付网关"}},
		}})
	case "/bugs":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"Bug": map[string]any{
				"id": "1100000001", "title": "下单后金额显示为 0", "description": "<p>1. 下单<br>2. 查看订单详情</p>",
				"status": "new", "priority": "1",
			}},
		}})
	case "/stories":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"Story": map[string]any{
				"id": "2000000001", "name": "支持优惠券抵扣", "description": "结算时可选优惠券",
				"status": "develop", "priority": "2",
			}},
		}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// openAPIGateway 用真实 Manager 指向 fakeTAPD。
type openAPIGateway struct {
	manager *tapd.Manager
}

func newOpenAPIGateway(t *testing.T, baseURL string) openAPIGateway {
	t.Helper()
	return openAPIGateway{manager: tapd.NewManager(tapd.Settings{
		Transport: tapd.TransportOpenAPI,
		BaseURL:   baseURL,
		APIUser:   "qa-user",
		HasSecret: true,
	}, "qa-secret")}
}

func (g openAPIGateway) Settings() tapd.Settings { return g.manager.Settings() }
func (g openAPIGateway) Apply(settings tapd.Settings, secret string) {
	g.manager.Apply(settings, secret)
}
func (g openAPIGateway) Status(ctx context.Context) tapd.Status { return g.manager.Status(ctx) }
func (g openAPIGateway) Workspaces(ctx context.Context) ([]tapd.Workspace, error) {
	return g.manager.Workspaces(ctx)
}
func (g openAPIGateway) WorkspaceByID(ctx context.Context, workspaceID string) (tapd.Workspace, bool) {
	return g.manager.WorkspaceByID(ctx, workspaceID)
}
func (g openAPIGateway) Sync(ctx context.Context, workspaceID string, kinds []string) ([]domain.ExternalItem, error) {
	return g.manager.Sync(ctx, workspaceID, kinds)
}

func TestTAPDSyncAndGenerateFlow(t *testing.T) {
	// given: 指向模拟 TAPD 的后端与一个已绑定工作区的 TAPD 项目
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	handler := httpapi.New(memory, newOpenAPIGateway(t, tapdServer.URL))
	project := memory.AddProject(domain.Project{Name: "支付网关", Source: domain.ProjectSourceTAPD, TAPDWorkspaceID: "1001"})

	// when: 同步缺陷与需求
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tapd/sync",
		bytes.NewReader([]byte(fmt.Sprintf(`{"projectId":%q,"kinds":["bug","requirement"]}`, project.ID)))))

	// then: 返回两类素材的统计
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var summary domain.SyncSummary
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.Bugs != 1 || summary.Requirements != 1 || summary.Total != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	// when: 再次同步（去重）后读取素材
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/tapd/sync",
		bytes.NewReader([]byte(fmt.Sprintf(`{"projectId":%q}`, project.ID)))))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/external-items?projectId="+project.ID, nil))

	// then: 素材只保留一份，且携带项目归属
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var items []domain.ExternalItem
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ProjectID != project.ID {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].Description == "" || bytes.ContainsAny(response.Body.Bytes(), "<>") {
		t.Fatalf("description should be plain text: %+v", items[0])
	}

	// when: 选中缺陷生成测试用例
	body, _ := json.Marshal(map[string]any{"projectId": project.ID, "itemIds": []string{"1100000001"}})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/test-cases/generate", bytes.NewReader(body)))

	// then: 生成草稿用例并计入项目
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	var cases []domain.TestCase
	if err := json.NewDecoder(response.Body).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Status != "draft" || cases[0].ExternalID != "1100000001" || cases[0].Priority != "P0" {
		t.Fatalf("unexpected cases: %+v", cases)
	}
	updatedProject, _ := memory.Project(project.ID)
	if updatedProject.CaseCount != 1 {
		t.Fatalf("expected case count 1, got %d", updatedProject.CaseCount)
	}
}

func TestTAPDSyncWithoutWorkspaceFails(t *testing.T) {
	// given: 未绑定工作区的项目
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	handler := httpapi.New(memory, newOpenAPIGateway(t, tapdServer.URL))
	project := memory.AddProject(domain.Project{Name: "本地项目", Source: domain.ProjectSourceLocal})

	// when: 直接同步
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tapd/sync",
		bytes.NewReader([]byte(fmt.Sprintf(`{"projectId":%q}`, project.ID)))))

	// then: 返回明确错误
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

func TestPutTAPDSettingsPersistsAndMasksSecret(t *testing.T) {
	// given: 指向模拟 TAPD 的后端
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	gateway := newOpenAPIGateway(t, tapdServer.URL)
	handler := httpapi.New(memory, gateway)

	// when: 保存直连配置
	body := []byte(`{"transport":"openapi","baseUrl":"` + tapdServer.URL + `","apiUser":"qa-user","apiPassword":"qa-secret"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/tapd/settings", bytes.NewReader(body)))

	// then: 探测成功且响应不泄漏密钥，设置与密钥已入库
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("qa-secret")) {
		t.Fatal("response leaked TAPD secret")
	}
	var payload struct {
		Settings domain.TAPDSettings `json:"settings"`
		Status   tapd.Status         `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Status.Connected || !payload.Settings.HasSecret {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if _, ok := memory.Secret(store.SecretTAPD); !ok {
		t.Fatal("secret was not persisted")
	}
	if memory.TAPDSettings().Transport != tapd.TransportOpenAPI {
		t.Fatalf("settings were not persisted: %+v", memory.TAPDSettings())
	}
}

func TestPutTAPDSettingsRequiresUserForOpenAPI(t *testing.T) {
	memory := store.NewMemory()
	handler := httpapi.New(memory, gatewayStub{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/tapd/settings",
		bytes.NewReader([]byte(`{"transport":"openapi","apiUser":""}`))))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, response.Code, response.Body.String())
	}
}

// llmPrompts 记录 fakeLLM 收到的提示词，供断言上下文注入。
var llmPrompts []string

// fakeLLM 模拟 OpenAI 兼容 chat/completions，返回结构化用例 JSON。
func fakeLLM(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		for _, message := range request.Messages {
			llmPrompts = append(llmPrompts, message.Content)
		}
		userContent := request.Messages[len(request.Messages)-1].Content
		count := strings.Count(userContent, "[素材")
		var drafts []map[string]any
		for i := 0; i < count; i++ {
			drafts = append(drafts, map[string]any{
				"title":         "AI 设计的用例 " + fmt.Sprint(i+1),
				"preconditions": "环境已就绪",
				"steps":         []string{"执行主流程操作", "验证输出结果"},
				"expected":      "表现符合预期",
				"priority":      "P1",
			})
		}
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"role": "assistant", "content": string(payload2(drafts)),
			}}},
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(json.RawMessage(payload))
	}))
}

func payload2(v any) []byte { b, _ := json.Marshal(v); return b }

func TestGenerateWithAgent(t *testing.T) {
	// given: 指向模拟 LLM 的智能体与已同步素材的项目
	llm := fakeLLM(t)
	defer llm.Close()
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	handler := httpapi.New(memory, newOpenAPIGateway(t, tapdServer.URL))
	gateway := newOpenAPIGateway(t, tapdServer.URL)
	project := memory.AddProject(domain.Project{Name: "支付网关", Source: domain.ProjectSourceTAPD, TAPDWorkspaceID: "1001"})
	items, err := gateway.Sync(context.Background(), "1001", nil)
	if err != nil {
		t.Fatal(err)
	}
	memory.UpsertExternalItems(project.ID, items)
	savedAgent := memory.PutAgent(domain.AgentConfig{Name: "设计智能体", BaseURL: llm.URL, Model: "fake-model"}, "sk-secret")

	// when: 指定智能体生成用例
	body, _ := json.Marshal(map[string]any{"projectId": project.ID, "itemIds": []string{"1100000001"}, "agentId": savedAgent.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/test-cases/generate", bytes.NewReader(body)))

	// then: 生成 AI 设计的用例，含前置条件与步骤，密钥不泄漏
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("sk-secret")) {
		t.Fatal("response leaked agent API key")
	}
	var cases []domain.TestCase
	if err := json.NewDecoder(response.Body).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Source != "AI · 设计智能体" || cases[0].Preconditions == "" || len(cases[0].Steps) < 2 {
		t.Fatalf("unexpected AI cases: %+v", cases)
	}
}

func TestAgentsCRUDMasksKey(t *testing.T) {
	memory := store.NewMemory()
	handler := httpapi.New(memory, gatewayStub{})

	body := []byte(`{"name":"主力智能体","baseUrl":"https://llm.example.com","model":"gpt-x","apiKey":"sk-live-key","isDefault":true}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("sk-live-key")) {
		t.Fatal("response leaked agent key")
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var agents []domain.AgentConfig
	if err := json.NewDecoder(response.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || !agents[0].APIKeySet || !agents[0].IsDefault {
		t.Fatalf("unexpected agents: %+v", agents)
	}

	agentID := agents[0].ID
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/agents/"+agentID, nil)
	request.SetPathValue("id", agentID)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, response.Code)
	}
}

func TestKnowledgeInjectedIntoDesignPrompt(t *testing.T) {
	// given: 项目 + 素材 + 一条知识条目
	llm := fakeLLM(t)
	defer llm.Close()
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	handler := httpapi.New(memory, newOpenAPIGateway(t, tapdServer.URL))
	gateway := newOpenAPIGateway(t, tapdServer.URL)
	project := memory.AddProject(domain.Project{Name: "支付网关", Source: domain.ProjectSourceTAPD, TAPDWorkspaceID: "1001"})
	items, err := gateway.Sync(context.Background(), "1001", nil)
	if err != nil {
		t.Fatal(err)
	}
	memory.UpsertExternalItems(project.ID, items)
	savedAgent := memory.PutAgent(domain.AgentConfig{Name: "设计智能体", BaseURL: llm.URL, Model: "fake-model"}, "sk-secret")
	knowledgeBody, _ := json.Marshal(map[string]string{
		"category": "功能导航",
		"title":    "创建同步任务的入口",
		"content":  "任务管理 → 新建任务，必选源目录与目标目录",
	})
	knowledgeResp := httptest.NewRecorder()
	handler.ServeHTTP(knowledgeResp, httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/knowledge", bytes.NewReader(knowledgeBody)))
	if knowledgeResp.Code != http.StatusCreated {
		t.Fatalf("knowledge create failed: %d", knowledgeResp.Code)
	}
	llmPrompts = nil

	// when: 指定智能体生成用例
	body, _ := json.Marshal(map[string]any{"projectId": project.ID, "itemIds": []string{"1100000001"}, "agentId": savedAgent.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/test-cases/generate", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("expected %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}

	// then: 设计提示词包含知识条目
	combined := strings.Join(llmPrompts, "\n")
	if !strings.Contains(combined, "创建同步任务的入口") || !strings.Contains(combined, "【项目知识库】") {
		t.Fatalf("knowledge missing. prompts=%d len=%d content=%.2400s", len(llmPrompts), len(combined), combined)
	}
}
