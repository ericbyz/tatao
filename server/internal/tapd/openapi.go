package tapd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ai-test-tool/server/internal/domain"
)

// openAPIConnection 直连 TAPD OpenAPI（https://api.tapd.cn），支持 Basic
// 认证（API 账号）与 Bearer 认证（个人访问令牌），不依赖任何外部进程。
type openAPIConnection struct {
	baseURL      string
	authType     string
	apiUser      string
	apiSecret    string
	companyID    string
	workspaceIDs []string
	httpClient   *http.Client
}

func newOpenAPIConnection(settings Settings, secret string) *openAPIConnection {
	baseURL := strings.TrimRight(settings.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	authType := settings.AuthType
	if authType == "" {
		authType = AuthBasic
	}
	return &openAPIConnection{
		baseURL:      baseURL,
		authType:     authType,
		apiUser:      settings.APIUser,
		apiSecret:    secret,
		companyID:    strings.TrimSpace(settings.CompanyID),
		workspaceIDs: settings.WorkspaceIds,
		httpClient:   &http.Client{},
	}
}

func (c *openAPIConnection) Workspaces(ctx context.Context) ([]Workspace, error) {
	if c.companyID != "" {
		return c.workspacesByCompany(ctx)
	}
	if len(c.workspaceIDs) > 0 {
		return c.workspacesByIDs(ctx, c.workspaceIDs)
	}
	if _, err := c.workspacesByCompany(ctx); errors.Is(err, ErrCompanyIDRequired) {
		return nil, ErrCompanyIDRequired
	}
	// API 账号等无需公司 ID 的凭据：直接按全量查询。
	return c.workspacesByCompany(ctx)
}

func (c *openAPIConnection) workspacesByCompany(ctx context.Context) ([]Workspace, error) {
	query := url.Values{"type": {"project"}}
	if c.companyID != "" {
		query.Set("company_id", c.companyID)
	}
	body, err := c.get(ctx, "workspaces/projects", query)
	if err != nil {
		if strings.Contains(err.Error(), "company_id is required") {
			return nil, ErrCompanyIDRequired
		}
		return nil, err
	}
	entities, err := decodeList(body, "Workspace")
	if err != nil {
		return nil, err
	}
	workspaces := make([]Workspace, 0, len(entities))
	for _, entity := range entities {
		if entity.ID != "" {
			workspaces = append(workspaces, Workspace{ID: entity.ID, Name: entity.Name})
		}
	}
	return workspaces, nil
}

// workspacesByIDs 逐个向远程验证工作区清单；这是个人令牌缺少公司 ID 时
// 获取真实项目列表的方式。
func (c *openAPIConnection) workspacesByIDs(ctx context.Context, ids []string) ([]Workspace, error) {
	workspaces := make([]Workspace, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if workspace, ok := c.WorkspaceByID(ctx, id); ok {
			workspaces = append(workspaces, workspace)
		}
	}
	if len(workspaces) == 0 {
		return nil, fmt.Errorf("工作区清单均无法访问，请检查工作区 ID 或令牌权限")
	}
	return workspaces, nil
}

// WorkspaceByID 按工作区 ID 查询单个空间；不依赖公司 ID，个人令牌可用。
func (c *openAPIConnection) WorkspaceByID(ctx context.Context, workspaceID string) (Workspace, bool) {
	body, err := c.get(ctx, "workspaces", url.Values{"workspace_id": {workspaceID}})
	if err != nil {
		return Workspace{}, false
	}
	entities, err := decodeList(body, "Workspace")
	if err != nil {
		return Workspace{}, false
	}
	for _, entity := range entities {
		if entity.ID == workspaceID {
			return Workspace{ID: entity.ID, Name: entity.Name}, true
		}
	}
	return Workspace{}, false
}

func (c *openAPIConnection) List(ctx context.Context, kind string, workspaceID string) ([]domain.ExternalItem, error) {
	switch kind {
	case KindBug:
		return c.collect(ctx, kind, workspaceID, "bugs", "Bug", bugItem)
	case KindRequirement:
		return c.collect(ctx, kind, workspaceID, "stories", "Story", storyItem)
	default:
		return nil, fmt.Errorf("不支持的素材类型 %q", kind)
	}
}

func (c *openAPIConnection) Close() error { return nil }

func bugItem(entity entityFields) (domain.ExternalItem, bool) {
	if entity.ID == "" {
		return domain.ExternalItem{}, false
	}
	return domain.ExternalItem{
		ID:          entity.ID,
		Kind:        KindBug,
		Title:       entity.Title,
		Description: stripHTML(entity.Description),
		Status:      entity.Status,
		Priority:    normalizePriority(entity.Priority),
	}, true
}

func storyItem(entity entityFields) (domain.ExternalItem, bool) {
	if entity.ID == "" {
		return domain.ExternalItem{}, false
	}
	title := entity.Title
	if title == "" {
		title = entity.Name
	}
	return domain.ExternalItem{
		ID:          entity.ID,
		Kind:        KindRequirement,
		Title:       title,
		Description: stripHTML(entity.Description),
		Status:      entity.Status,
		Priority:    normalizePriority(entity.Priority),
	}, true
}

// collect 分页拉取一类实体并逐条映射。每类最多 openAPIMaxPerKind 条，
// 保证首版同步耗时可控。
func (c *openAPIConnection) collect(ctx context.Context, kind, workspaceID, path, wrapper string, mapper func(entityFields) (domain.ExternalItem, bool)) ([]domain.ExternalItem, error) {
	items := make([]domain.ExternalItem, 0, 64)
	for page := 1; len(items) < openAPIMaxPerKind; page++ {
		query := url.Values{
			"workspace_id": {workspaceID},
			"limit":        {fmt.Sprint(openAPIPageLimit)},
			"page":         {fmt.Sprint(page)},
		}
		body, err := c.get(ctx, path, query)
		if err != nil {
			return nil, err
		}
		entities, err := decodeList(body, wrapper)
		if err != nil {
			return nil, err
		}
		for _, entity := range entities {
			if item, ok := mapper(entity); ok {
				items = append(items, item)
			}
		}
		if len(entities) < openAPIPageLimit {
			break
		}
	}
	return items, nil
}

func (c *openAPIConnection) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("构建 TAPD 请求失败: %w", err)
	}
	if c.authType == AuthBearer {
		request.Header.Set("Authorization", "Bearer "+c.apiSecret)
	} else {
		request.SetBasicAuth(c.apiUser, c.apiSecret)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求 TAPD 失败: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 TAPD 响应失败: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Info    string `json:"info"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &failure) == nil {
			if failure.Info != "" {
				return nil, fmt.Errorf("TAPD 接口返回 %d：%s", response.StatusCode, failure.Info)
			}
			if failure.Message != "" {
				return nil, fmt.Errorf("TAPD 接口返回 %d：%s", response.StatusCode, failure.Message)
			}
		}
		return nil, fmt.Errorf("TAPD 接口返回 %d", response.StatusCode)
	}
	return body, nil
}

// entityFields 覆盖缺陷与需求共有的字段；需求标题字段为 name。
type entityFields struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
}

// decodeList 解析 `{"data":[...]}` 载荷；元素可能是 {"Bug":{...}} 包裹
// 形式，也可能是裸字段形式，两种都接受。
func decodeList(body []byte, wrapper string) ([]entityFields, error) {
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析 TAPD 响应失败: %w", err)
	}
	entities := make([]entityFields, 0, len(envelope.Data))
	for _, element := range envelope.Data {
		var entity entityFields
		if err := json.Unmarshal(element, &entity); err != nil {
			return nil, fmt.Errorf("解析 TAPD 实体失败: %w", err)
		}
		if entity.ID == "" {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(element, &nested); err != nil {
				return nil, fmt.Errorf("解析 TAPD 实体失败: %w", err)
			}
			if payload, ok := nested[wrapper]; ok {
				if err := json.Unmarshal(payload, &entity); err != nil {
					return nil, fmt.Errorf("解析 TAPD 实体失败: %w", err)
				}
			}
		}
		entities = append(entities, entity)
	}
	return entities, nil
}

var (
	htmlTagPattern    = regexp.MustCompile(`<[^>]*>`)
	whitespacePattern = regexp.MustCompile(`\s+`)
)

func stripHTML(value string) string {
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(value)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "urgent":
		return "P0"
	case "2", "high":
		return "P1"
	default:
		return "P2"
	}
}
