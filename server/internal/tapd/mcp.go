package tapd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/mcp"
)

// mcpConnection 通过外部 MCP 服务访问 TAPD（官方 mcp-server-tapd 或兼容实现），
// 底层复用内置 MCP 内核。工具按名称启发式匹配，参数与返回值做了宽容解析。
type mcpConnection struct {
	session *mcp.Client
	tools   []mcp.Tool
}

func newMCPConnection(ctx context.Context, settings Settings, secret string) (*mcpConnection, error) {
	session, err := mcp.Dial(ctx, settings.Endpoint, basicAuthHeader(settings.APIUser, secret), mcpEnv(settings, secret))
	if err != nil {
		return nil, err
	}
	tools, err := session.ListTools(ctx)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	return &mcpConnection{session: session, tools: tools}, nil
}

func (c *mcpConnection) Workspaces(ctx context.Context) ([]Workspace, error) {
	tool, ok := findTool(c.tools, []string{"workspace", "get_workspaces", "list_workspaces", "projects"})
	if !ok {
		return nil, fmt.Errorf("MCP 服务未提供工作区工具，可用工具：%s", toolNames(c.tools))
	}
	result, err := c.session.CallTool(ctx, tool.Name, map[string]any{})
	if err != nil {
		return nil, err
	}
	rows, err := resultRows(result)
	if err != nil {
		return nil, err
	}
	workspaces := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		id := rowString(row, "workspace_id", "id", "workspaceId")
		name := rowString(row, "name", "title", "workspace_name")
		if id != "" {
			workspaces = append(workspaces, Workspace{ID: id, Name: name})
		}
	}
	return workspaces, nil
}

func (c *mcpConnection) List(ctx context.Context, kind string, workspaceID string) ([]domain.ExternalItem, error) {
	var candidates []string
	switch kind {
	case KindBug:
		candidates = []string{"bug"}
	case KindRequirement:
		candidates = []string{"story", "requirement"}
	default:
		return nil, fmt.Errorf("不支持的素材类型 %q", kind)
	}
	tool, ok := findTool(c.tools, candidates)
	if !ok {
		return nil, fmt.Errorf("MCP 服务未提供 %s 相关工具，可用工具：%s", kind, toolNames(c.tools))
	}
	result, err := c.session.CallTool(ctx, tool.Name, map[string]any{"workspace_id": workspaceID, "limit": openAPIPageLimit})
	if err != nil {
		return nil, err
	}
	rows, err := resultRows(result)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ExternalItem, 0, len(rows))
	for _, row := range rows {
		item := rowItem(row, kind)
		if item.ID != "" {
			items = append(items, item)
		}
	}
	return items, nil
}

// WorkspaceByID 在 MCP 传输下不可用，工作区发现依赖服务端工具。
func (c *mcpConnection) WorkspaceByID(_ context.Context, _ string) (Workspace, bool) {
	return Workspace{}, false
}

func (c *mcpConnection) Close() error { return c.session.Close() }

// findTool 按候选关键字匹配工具名，优先精确包含，其次宽松包含。
func findTool(tools []mcp.Tool, keywords []string) (mcp.Tool, bool) {
	for _, keyword := range keywords {
		for _, tool := range tools {
			if strings.Contains(strings.ToLower(tool.Name), keyword) {
				return tool, true
			}
		}
	}
	return mcp.Tool{}, false
}

func toolNames(tools []mcp.Tool) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// resultRows 从工具结果里提取对象数组：优先 content 里的 JSON 文本，
// 支持顶层数组、{"data": [...]} 或单一数组字段三种形态。
func resultRows(result mcp.ToolResult) ([]map[string]any, error) {
	var text string
	for _, block := range result.Content {
		if block.Text != "" {
			text = block.Text
			break
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("MCP 工具返回了无法解析的内容: %w", err)
	}
	if array, ok := parsed.([]any); ok {
		return toRows(array), nil
	}
	if object, ok := parsed.(map[string]any); ok {
		for _, key := range []string{"data", "items", "list", "bugs", "stories"} {
			if array, ok := object[key].([]any); ok {
				return toRows(array), nil
			}
		}
		for _, value := range object {
			if array, ok := value.([]any); ok {
				return toRows(array), nil
			}
		}
	}
	return nil, errors.New("MCP 工具返回内容中没有数据数组")
}

func toRows(array []any) []map[string]any {
	rows := make([]map[string]any, 0, len(array))
	for _, element := range array {
		if row, ok := element.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func rowString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			if text, ok := value.(string); ok && text != "" {
				return text
			}
			if number, ok := value.(float64); ok {
				return fmt.Sprint(int64(number))
			}
		}
	}
	return ""
}

func rowItem(row map[string]any, kind string) domain.ExternalItem {
	title := rowString(row, "title", "name")
	return domain.ExternalItem{
		ID:          rowString(row, "id", "bug_id", "story_id"),
		Kind:        kind,
		Title:       title,
		Description: stripHTML(rowString(row, "description", "desc")),
		Status:      rowString(row, "status"),
		Priority:    normalizePriority(rowString(row, "priority")),
	}
}

func basicAuthHeader(user, secret string) http.Header {
	if user == "" && secret == "" {
		return nil
	}
	header := http.Header{}
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
	return header
}

// mcpEnv 为 stdio 方式的 MCP 子进程注入 TAPD 凭据；HTTP 方式无需环境变量。
func mcpEnv(settings Settings, secret string) []string {
	if strings.HasPrefix(settings.Endpoint, "http://") || strings.HasPrefix(settings.Endpoint, "https://") {
		return nil
	}
	env := []string{}
	if settings.BaseURL != "" {
		env = append(env, "TAPD_BASE_URL="+settings.BaseURL, "TAPD_API_BASE_URL="+settings.BaseURL)
	}
	if settings.APIUser != "" {
		env = append(env, "TAPD_API_USER="+settings.APIUser)
	}
	if secret != "" {
		env = append(env, "TAPD_API_PASSWORD="+secret, "TAPD_ACCESS_TOKEN="+secret)
	}
	return env
}
