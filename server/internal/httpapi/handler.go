package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/executor"
	"ai-test-tool/server/internal/store"
	"ai-test-tool/server/internal/tapd"
)

type Store interface {
	Projects() []domain.Project
	Project(id string) (domain.Project, bool)
	AddProject(domain.Project) domain.Project
	Environments() []domain.Environment
	Environment(id string) (domain.Environment, bool)
	AddEnvironment(domain.Environment, string) (domain.Environment, error)
	AssignEnvironment(string, []string) (domain.Environment, error)
	TestCases() []domain.TestCase
	AddTestCase(domain.TestCase) (domain.TestCase, error)
	UpsertExternalItems(projectID string, items []domain.ExternalItem)
	ExternalItems(projectID string) []domain.ExternalItem
	AllExternalItems() []domain.ExternalItem
	TAPDSettings() domain.TAPDSettings
	SetTAPDSettings(domain.TAPDSettings)
	Agents() []domain.AgentConfig
	Agent(id string) (domain.AgentConfig, bool)
	PutAgent(agent domain.AgentConfig, secret string) domain.AgentConfig
	DeleteAgent(id string) bool
	CreateRun(run domain.TestRun) domain.TestRun
	SaveRun(run domain.TestRun)
	Run(id string) (domain.TestRun, bool)
	Runs() []domain.TestRun
	PutSecret(id, value string)
	Secret(id string) (string, bool)
}

// TAPDGateway 是本服务对 TAPD 能力的依赖面（消费方接口）。
type TAPDGateway interface {
	Settings() tapd.Settings
	Apply(settings tapd.Settings, secret string)
	Status(ctx context.Context) tapd.Status
	Workspaces(ctx context.Context) ([]tapd.Workspace, error)
	WorkspaceByID(ctx context.Context, workspaceID string) (tapd.Workspace, bool)
	Sync(ctx context.Context, workspaceID string, kinds []string) ([]domain.ExternalItem, error)
}

type Handler struct {
	store Store
	tapd  TAPDGateway
}

func New(store Store, gateway TAPDGateway) http.Handler {
	handler := &Handler{store: store, tapd: gateway}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handler.health)
	mux.HandleFunc("GET /api/projects", handler.listProjects)
	mux.HandleFunc("POST /api/projects", handler.createProject)
	mux.HandleFunc("GET /api/environments", handler.listEnvironments)
	mux.HandleFunc("POST /api/environments", handler.createEnvironment)
	mux.HandleFunc("PATCH /api/environments/{id}/projects", handler.assignEnvironment)
	mux.HandleFunc("GET /api/test-cases", handler.listTestCases)
	mux.HandleFunc("POST /api/test-cases", handler.createTestCase)
	mux.HandleFunc("POST /api/test-cases/generate", handler.generateTestCases)
	mux.HandleFunc("GET /api/external-items", handler.listExternalItems)
	mux.HandleFunc("GET /api/tapd/status", handler.tapdStatus)
	mux.HandleFunc("GET /api/tapd/settings", handler.getTAPDSettings)
	mux.HandleFunc("PUT /api/tapd/settings", handler.putTAPDSettings)
	mux.HandleFunc("POST /api/tapd/test", handler.testTAPD)
	mux.HandleFunc("GET /api/tapd/workspaces", handler.tapdWorkspaces)
	mux.HandleFunc("GET /api/tapd/local-mcp", handler.localMCPProviders)
	mux.HandleFunc("GET /api/agents", handler.listAgents)
	mux.HandleFunc("GET /api/agents/local", handler.listLocalAgents)
	mux.HandleFunc("GET /api/agents/model", handler.agentModel)
	mux.HandleFunc("POST /api/agents", handler.saveAgent)
	mux.HandleFunc("POST /api/agents/test", handler.testAgent)
	mux.HandleFunc("DELETE /api/agents/{id}", handler.deleteAgent)
	mux.HandleFunc("POST /api/tapd/sync", handler.syncTAPD)
	mux.HandleFunc("GET /api/runs", handler.listRuns)
	mux.HandleFunc("GET /api/runs/{id}", handler.getRun)
	mux.HandleFunc("POST /api/runs", handler.createRun)
	return withMiddleware(mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) listProjects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Projects())
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var input domain.Project
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "项目名称不能为空")
		return
	}
	if input.Source != domain.ProjectSourceLocal && input.Source != domain.ProjectSourceTAPD {
		writeError(w, http.StatusBadRequest, "项目来源无效")
		return
	}
	if input.Source == domain.ProjectSourceTAPD && strings.TrimSpace(input.TAPDWorkspaceID) == "" {
		writeError(w, http.StatusBadRequest, "请选择 TAPD 工作区")
		return
	}
	writeJSON(w, http.StatusCreated, h.store.AddProject(input))
}

func (h *Handler) listEnvironments(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Environments())
}

func (h *Handler) createEnvironment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		domain.Environment
		Secret string `json:"secret"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Host) == "" || strings.TrimSpace(input.Username) == "" {
		writeError(w, http.StatusBadRequest, "名称、地址和账号不能为空")
		return
	}
	if input.Port == 0 {
		input.Port = 22
	}
	environment, err := h.store.AddEnvironment(input.Environment, input.Secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存环境失败")
		return
	}
	writeJSON(w, http.StatusCreated, environment)
}

func (h *Handler) assignEnvironment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectIDs []string `json:"projectIds"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	environment, err := h.store.AssignEnvironment(r.PathValue("id"), input.ProjectIDs)
	if errors.Is(err, store.ErrEnvironmentNotFound) {
		writeError(w, http.StatusNotFound, "环境不存在")
		return
	}
	if errors.Is(err, store.ErrProjectNotFound) {
		writeError(w, http.StatusBadRequest, "包含不存在的项目")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "分配项目失败")
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (h *Handler) listTestCases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.TestCases())
}

func (h *Handler) createTestCase(w http.ResponseWriter, r *http.Request) {
	var input domain.TestCase
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Title) == "" || input.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "项目和用例标题不能为空")
		return
	}
	testCase, err := h.store.AddTestCase(input)
	if errors.Is(err, store.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存用例失败")
		return
	}
	writeJSON(w, http.StatusCreated, testCase)
}

func (h *Handler) tapdStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.tapd.Status(r.Context()))
}

func (h *Handler) getTAPDSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.tapd.Settings())
}

type tapdSettingsInput struct {
	Transport    string   `json:"transport"`
	BaseURL      string   `json:"baseUrl"`
	AuthType     string   `json:"authType"`
	APIUser      string   `json:"apiUser"`
	CompanyID    string   `json:"companyId"`
	WorkspaceIds []string `json:"workspaceIds"`
	APIPassword  string   `json:"apiPassword"`
	Endpoint     string   `json:"endpoint"`
}

func (input tapdSettingsInput) toSettings(previousSecret bool) (tapd.Settings, string, error) {
	workspaceIds := make([]string, 0, len(input.WorkspaceIds))
	for _, id := range input.WorkspaceIds {
		if id = strings.TrimSpace(id); id != "" {
			workspaceIds = append(workspaceIds, id)
		}
	}
	settings := tapd.Settings{
		Transport:    strings.TrimSpace(input.Transport),
		BaseURL:      strings.TrimSpace(input.BaseURL),
		AuthType:     strings.TrimSpace(input.AuthType),
		APIUser:      strings.TrimSpace(input.APIUser),
		CompanyID:    strings.TrimSpace(input.CompanyID),
		WorkspaceIds: workspaceIds,
		Endpoint:     strings.TrimSpace(input.Endpoint),
		HasSecret:    previousSecret || strings.TrimSpace(input.APIPassword) != "",
	}
	if settings.AuthType == "" {
		settings.AuthType = tapd.AuthBasic
	}
	if settings.AuthType != tapd.AuthBasic && settings.AuthType != tapd.AuthBearer {
		return settings, "", errors.New("认证方式无效")
	}
	if settings.Transport != tapd.TransportOpenAPI && settings.Transport != tapd.TransportMCP {
		return settings, "", errors.New("传输方式无效")
	}
	if settings.Transport == tapd.TransportOpenAPI && settings.AuthType == tapd.AuthBasic && settings.APIUser == "" {
		return settings, "", errors.New("请填写 API 账号")
	}
	if settings.Transport == tapd.TransportMCP && settings.Endpoint == "" {
		return settings, "", errors.New("请填写 MCP 服务地址或启动命令")
	}
	return settings, strings.TrimSpace(input.APIPassword), nil
}

func (h *Handler) putTAPDSettings(w http.ResponseWriter, r *http.Request) {
	var input tapdSettingsInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, hadSecret := h.store.Secret(store.SecretTAPD)
	settings, secret, err := input.toSettings(hadSecret)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if secret != "" {
		h.store.PutSecret(store.SecretTAPD, secret)
	}
	h.store.SetTAPDSettings(settings)
	h.tapd.Apply(settings, h.currentTAPDSecret())
	status := h.tapd.Status(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"settings": h.tapd.Settings(), "status": status})
}

func (h *Handler) testTAPD(w http.ResponseWriter, r *http.Request) {
	var input tapdSettingsInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, hadSecret := h.store.Secret(store.SecretTAPD)
	settings, secret, err := input.toSettings(hadSecret)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if secret == "" {
		secret, _ = h.store.Secret(store.SecretTAPD)
	}
	writeJSON(w, http.StatusOK, tapd.Probe(r.Context(), settings, secret))
}

func (h *Handler) tapdWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.tapd.Workspaces(r.Context())
	if errors.Is(err, tapd.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, tapd.ErrCompanyIDRequired) {
		// 个人令牌列全表需要公司 ID；降级为枚举项目里已绑定的工作区。
		if workspaces := h.knownWorkspaces(r.Context()); len(workspaces) > 0 {
			writeJSON(w, http.StatusOK, workspaces)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "获取工作区失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

// knownWorkspaces 收集项目已绑定的 TAPD 工作区并查详情，用于降级展示。
func (h *Handler) knownWorkspaces(ctx context.Context) []tapd.Workspace {
	seen := make(map[string]struct{})
	ids := make([]string, 0, 4)
	for _, project := range h.store.Projects() {
		id := strings.TrimSpace(project.TAPDWorkspaceID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	workspaces := make([]tapd.Workspace, 0, len(ids))
	for _, id := range ids {
		if workspace, ok := h.tapd.WorkspaceByID(ctx, id); ok {
			workspaces = append(workspaces, workspace)
		}
	}
	return workspaces
}

func (h *Handler) listAgents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Agents())
}

func (h *Handler) listLocalAgents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, agent.DetectLocalAgents())
}

// agentModel 返回本机 CLI 已配置的默认模型与候选列表。
func (h *Handler) agentModel(w http.ResponseWriter, r *http.Request) {
	agentType := r.URL.Query().Get("type")
	info, _ := agent.DetectConfiguredModel(agentType)
	writeJSON(w, http.StatusOK, map[string]any{
		"model":       info.Model,
		"source":      info.Source,
		"configured":  info.Model != "",
		"knownModels": agent.KnownModels(agentType),
	})
}

type agentInput struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	Command   string `json:"command"`
	ExtraArgs string `json:"extraArgs"`
	APIKey    string `json:"apiKey"`
	IsDefault bool   `json:"isDefault"`
	ID        string `json:"id"`
}

func (h *Handler) saveAgent(w http.ResponseWriter, r *http.Request) {
	var input agentInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeError(w, http.StatusBadRequest, "名称不能为空")
		return
	}
	switch strings.TrimSpace(input.Type) {
	case agent.TypeOpenAI:
		if strings.TrimSpace(input.BaseURL) == "" || strings.TrimSpace(input.Model) == "" {
			writeError(w, http.StatusBadRequest, "OpenAI 兼容接口需要填写服务地址和模型")
			return
		}
	case agent.TypeCustom:
		if strings.TrimSpace(input.Command) == "" {
			writeError(w, http.StatusBadRequest, "自定义智能体需要填写命令")
			return
		}
	case agent.TypeClaude, agent.TypeCodex, agent.TypeZCode, "":
		// 内置 CLI 类型无需额外配置
	default:
		writeError(w, http.StatusBadRequest, "未知智能体类型")
		return
	}
	agent := h.store.PutAgent(domain.AgentConfig{
		ID:        input.ID,
		Name:      input.Name,
		Type:      input.Type,
		BaseURL:   input.BaseURL,
		Model:     input.Model,
		Command:   input.Command,
		ExtraArgs: input.ExtraArgs,
		IsDefault: input.IsDefault,
	}, strings.TrimSpace(input.APIKey))
	writeJSON(w, http.StatusCreated, agent)
}

func (h *Handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	if !h.store.DeleteAgent(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "智能体不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) testAgent(w http.ResponseWriter, r *http.Request) {
	var input agentInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := h.resolveAgentConfig(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	elapsed, err := agent.Verify(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("连接成功，耗时 %s", elapsed.Round(time.Millisecond))})
}

// resolveAgentConfig 把"保存的 ID"或"未保存的表单值"解析为完整调用配置。
func (h *Handler) resolveAgentConfig(input agentInput) (agent.Config, error) {
	if input.ID != "" && input.BaseURL == "" {
		saved, ok := h.store.Agent(input.ID)
		if !ok {
			return agent.Config{}, errors.New("智能体不存在")
		}
		secret, _ := h.store.Secret(store.SecretAgentPrefix + saved.ID)
		return agent.Config{Name: saved.Name, Type: saved.Type, BaseURL: saved.BaseURL, Model: saved.Model, Command: saved.Command, ExtraArgs: saved.ExtraArgs, APIKey: secret}, nil
	}
	return agent.Config{
		Name:      input.Name,
		Type:      input.Type,
		BaseURL:   input.BaseURL,
		Model:     input.Model,
		Command:   input.Command,
		ExtraArgs: input.ExtraArgs,
		APIKey:    strings.TrimSpace(input.APIKey),
	}, nil
}

func (h *Handler) localMCPProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, tapd.FindLocalProviders())
}

func (h *Handler) syncTAPD(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID string   `json:"projectId"`
		Kinds     []string `json:"kinds"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	project, ok := h.store.Project(input.ProjectID)
	if !ok {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	if strings.TrimSpace(project.TAPDWorkspaceID) == "" {
		writeError(w, http.StatusBadRequest, "项目未绑定 TAPD 工作区")
		return
	}
	items, err := h.tapd.Sync(r.Context(), project.TAPDWorkspaceID, input.Kinds)
	if errors.Is(err, tapd.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "TAPD 同步失败："+err.Error())
		return
	}
	h.store.UpsertExternalItems(project.ID, items)
	summary := domain.SyncSummary{Total: len(items)}
	for _, item := range items {
		if item.Kind == tapd.KindBug {
			summary.Bugs++
		} else {
			summary.Requirements++
		}
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) listExternalItems(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("projectId")
	if projectID == "" {
		writeJSON(w, http.StatusOK, h.store.AllExternalItems())
		return
	}
	writeJSON(w, http.StatusOK, h.store.ExternalItems(projectID))
}

func (h *Handler) generateTestCases(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID string   `json:"projectId"`
		ItemIDs   []string `json:"itemIds"`
		AgentID   string   `json:"agentId"`
		Model     string   `json:"model"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.ProjectID == "" || len(input.ItemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "请选择要生成用例的素材")
		return
	}
	if _, ok := h.store.Project(input.ProjectID); !ok {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	selected := make(map[string]struct{}, len(input.ItemIDs))
	for _, id := range input.ItemIDs {
		selected[id] = struct{}{}
	}
	items := make([]domain.ExternalItem, 0, len(selected))
	for _, item := range h.store.ExternalItems(input.ProjectID) {
		if _, ok := selected[item.ID]; ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "选中的素材不存在或已生成过用例")
		return
	}

	var cases []domain.TestCase
	agentName := ""
	if input.AgentID != "" {
		saved, ok := h.store.Agent(input.AgentID)
		if !ok {
			writeError(w, http.StatusNotFound, "智能体不存在")
			return
		}
		secret, _ := h.store.Secret(store.SecretAgentPrefix + saved.ID)
		cfg := agent.Config{Name: saved.Name, Type: saved.Type, BaseURL: saved.BaseURL, Model: saved.Model, Command: saved.Command, ExtraArgs: saved.ExtraArgs, APIKey: secret}
		if strings.TrimSpace(input.Model) != "" {
			cfg.Model = strings.TrimSpace(input.Model)
		}
		drafts, err := agent.DesignTestCases(r.Context(), cfg, items)
		if err != nil {
			writeError(w, http.StatusBadGateway, "智能体生成失败："+err.Error())
			return
		}
		agentName = saved.Name
		cases = draftsToCases(input.ProjectID, agentName, drafts)
	} else {
		cases = make([]domain.TestCase, 0, len(items))
		for _, item := range items {
			cases = append(cases, testCaseFromItem(input.ProjectID, item))
		}
	}

	created := make([]domain.TestCase, 0, len(cases))
	for _, testCase := range cases {
		testCase.Status = "draft"
		saved, err := h.store.AddTestCase(testCase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存用例失败")
			return
		}
		created = append(created, saved)
	}
	writeJSON(w, http.StatusCreated, created)
}

// draftsToCases 把智能体产出的草稿映射为用例，数量不足时以规则生成补齐。
func draftsToCases(projectID, agentName string, drafts []agent.TestCaseDraft) []domain.TestCase {
	source := "AI 智能体"
	if agentName != "" {
		source = "AI · " + agentName
	}
	cases := make([]domain.TestCase, 0, len(drafts))
	for _, draft := range drafts {
		steps := draft.Steps
		if len(steps) == 0 {
			steps = []string{"按用例标题执行对应操作"}
		}
		expected := draft.Expected
		if expected == "" {
			expected = "实际表现与预期结果一致"
		}
		cases = append(cases, domain.TestCase{
			ProjectID:     projectID,
			Title:         draft.Title,
			Priority:      draft.Priority,
			Source:        source,
			Preconditions: draft.Preconditions,
			Steps:         steps,
			Expected:      expected,
		})
	}
	return cases
}

// testCaseFromItem 把一条 TAPD 素材映射为草稿用例，生成结果仍可人工编辑。
func testCaseFromItem(projectID string, item domain.ExternalItem) domain.TestCase {
	source := "TAPD 需求"
	expected := "功能表现与需求描述一致，验收标准全部通过"
	steps := draftSteps(item, []string{
		"按需求「" + truncateRunes(item.Title, 40) + "」执行主流程",
		"对照验收标准逐项检查输出与预期一致",
	})
	if item.Kind == tapd.KindBug {
		source = "TAPD 缺陷"
		expected = "缺陷场景不再复现，相关功能表现正常"
		steps = draftSteps(item, []string{
			"复现「" + truncateRunes(item.Title, 40) + "」描述的场景，确认当前异常表现",
			"按修复后的版本重新执行相同操作",
			"核对相关功能无回归",
		})
	}
	return domain.TestCase{
		ProjectID:  projectID,
		Title:      item.Title,
		Priority:   item.Priority,
		Source:     source,
		ExternalID: item.ID,
		Steps:      steps,
		Expected:   expected,
		Status:     "draft",
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func draftSteps(item domain.ExternalItem, fallback []string) []string {
	steps := make([]string, 0, 6)
	for _, line := range strings.Split(item.Description, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "0123456789.-、）)")
		if len([]rune(line)) < 4 {
			continue
		}
		if len([]rune(line)) > 120 {
			line = string([]rune(line)[:120]) + "…"
		}
		steps = append(steps, line)
		if len(steps) == 6 {
			break
		}
	}
	if len(steps) < 2 {
		return fallback
	}
	return steps
}

func (h *Handler) currentTAPDSecret() string {
	secret, _ := h.store.Secret(store.SecretTAPD)
	return secret
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4173")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	runs := h.store.Runs()
	if projectID := r.URL.Query().Get("projectId"); projectID != "" {
		filtered := make([]domain.TestRun, 0, len(runs))
		for _, run := range runs {
			if run.ProjectID == projectID {
				filtered = append(filtered, run)
			}
		}
		runs = filtered
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok := h.store.Run(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "执行记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID     string   `json:"projectId"`
		CaseIDs       []string `json:"caseIds"`
		AgentID       string   `json:"agentId"`
		Model         string   `json:"model"`
		EnvironmentID string   `json:"environmentId"`
		WorkspaceDir  string   `json:"workspaceDir"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.ProjectID == "" || len(input.CaseIDs) == 0 || strings.TrimSpace(input.WorkspaceDir) == "" {
		writeError(w, http.StatusBadRequest, "项目、用例和工作目录不能为空")
		return
	}
	if info, err := os.Stat(strings.TrimSpace(input.WorkspaceDir)); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "工作目录不存在或不是目录")
		return
	}
	project, ok := h.store.Project(input.ProjectID)
	if !ok {
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	}
	savedAgent, ok := h.store.Agent(input.AgentID)
	if !ok {
		writeError(w, http.StatusNotFound, "智能体不存在")
		return
	}
	secret, _ := h.store.Secret(store.SecretAgentPrefix + savedAgent.ID)
	cfg := agent.Config{Name: savedAgent.Name, Type: savedAgent.Type, BaseURL: savedAgent.BaseURL, Model: savedAgent.Model, Command: savedAgent.Command, ExtraArgs: savedAgent.ExtraArgs, APIKey: secret}
	if strings.TrimSpace(input.Model) != "" {
		cfg.Model = strings.TrimSpace(input.Model)
	}

	selected := make(map[string]struct{}, len(input.CaseIDs))
	for _, id := range input.CaseIDs {
		selected[id] = struct{}{}
	}
	cases := make([]domain.TestCase, 0, len(selected))
	for _, testCase := range h.store.TestCases() {
		if testCase.ProjectID == project.ID {
			if _, ok := selected[testCase.ID]; ok {
				cases = append(cases, testCase)
			}
		}
	}
	if len(cases) == 0 {
		writeError(w, http.StatusBadRequest, "选中的用例不存在")
		return
	}

	environmentName := "本机"
	if input.EnvironmentID != "" {
		environment, ok := h.store.Environment(input.EnvironmentID)
		if !ok {
			writeError(w, http.StatusNotFound, "测试环境不存在")
			return
		}
		password, _ := h.store.Secret(environment.ID)
		cfg.Environment = &agent.EnvironmentTarget{
			Kind:     "linux",
			Host:     environment.Host,
			Port:     environment.Port,
			Username: environment.Username,
			Password: password,
		}
		environmentName = environment.Name
	}
	run := h.store.CreateRun(domain.TestRun{
		ProjectID:       project.ID,
		AgentID:         savedAgent.ID,
		AgentName:       savedAgent.Name,
		EnvironmentID:   input.EnvironmentID,
		EnvironmentName: environmentName,
		Model:           cfg.Model,
		WorkspaceDir:    strings.TrimSpace(input.WorkspaceDir),
		Status:          "running",
		Results:         []domain.RunCaseResult{},
		Total:           len(cases),
	})

	go h.executeRun(run, cfg, cases)
	writeJSON(w, http.StatusCreated, run)
}

// executeRun 串行执行批次内全部用例，实时回写结果。
func (h *Handler) executeRun(run domain.TestRun, cfg agent.Config, cases []domain.TestCase) {
	ctx := context.Background()
	for _, testCase := range cases {
		run.CurrentTitle = testCase.Title
		h.store.SaveRun(run)

		outcome, durationMs, err := executor.ExecuteCase(ctx, cfg, run.WorkspaceDir, testCase)
		result := domain.RunCaseResult{
			CaseID:     testCase.ID,
			Title:      testCase.Title,
			Source:     testCase.Source,
			DurationMs: durationMs,
		}
		if err != nil && outcome.Status == "" {
			outcome.Status = "error"
			outcome.Summary = "执行失败：" + err.Error()
		}
		result.Status = outcome.Status
		result.Summary = outcome.Summary
		result.Evidence = outcome.Evidence
		run.Results = append(run.Results, result)
		switch result.Status {
		case "passed":
			run.Passed++
		case "failed":
			run.Failed++
		default:
			run.Errors++
		}
		run.CurrentTitle = ""
		h.store.SaveRun(run)
	}
	run.Status = "completed"
	run.FinishedAt = time.Now()
	h.store.SaveRun(run)
	slog.Info("test run finished", "run", run.ID, "passed", run.Passed, "failed", run.Failed, "errors", run.Errors)
}
