// Package tapd connects the platform to TAPD. A direct OpenAPI client and an
// MCP-server connector sit behind one Manager, so callers never deal with
// transport details and can switch transports from the web UI.
package tapd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"ai-test-tool/server/internal/domain"
)

// 支持的传输方式、认证方式与素材类型。
const (
	TransportOpenAPI  = "openapi"
	TransportMCP      = "mcp"
	AuthBasic         = "basic"
	AuthBearer        = "bearer"
	KindBug           = "bug"
	KindRequirement   = "requirement"
	DefaultAPIBaseURL = "https://api.tapd.cn"
	operationTimeout  = 45 * time.Second
	openAPIPageLimit  = 200
	openAPIMaxPerKind = 500
)

var (
	ErrNotConfigured     = errors.New("尚未配置 TAPD 连接，请先在连接设置中填写账号")
	ErrCompanyIDRequired = errors.New("个人访问令牌需要公司 ID（TAPD 网页地址栏中的数字），请在连接设置中填写")
)

// Settings 复用领域层的连接设置结构，保持单一事实来源。
type Settings = domain.TAPDSettings

// Workspace 是 TAPD 项目空间的快照，供新建项目时选择。
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status 是一次连接探测的结果。
type Status struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Transport  string `json:"transport"`
	Message    string `json:"message"`
}

// connection 是一种传输方式在单次操作内的会话。
type connection interface {
	Workspaces(ctx context.Context) ([]Workspace, error)
	WorkspaceByID(ctx context.Context, workspaceID string) (Workspace, bool)
	List(ctx context.Context, kind string, workspaceID string) ([]domain.ExternalItem, error)
	Close() error
}

// Manager 保存当前生效的连接配置，并在每次操作时构建短生命周期会话，
// 让设置修改立即生效而无需重启。
type Manager struct {
	mu       sync.RWMutex
	settings Settings
	secret   string
}

func NewManager(settings Settings, secret string) *Manager {
	return &Manager{settings: settings, secret: secret}
}

// Settings 返回当前配置快照（不含密钥）。
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// Apply 更新运行时配置，通常在持久化之后调用。
func (m *Manager) Apply(settings Settings, secret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
	m.secret = secret
}

func (m *Manager) configured() bool {
	settings := m.Settings()
	switch settings.Transport {
	case TransportOpenAPI:
		if m.secret == "" {
			return false
		}
		return settings.AuthType == AuthBearer || settings.APIUser != ""
	case TransportMCP:
		return settings.Endpoint != ""
	default:
		return false
	}
}

// Status 用一次真实请求验证连接。个人令牌缺少公司 ID 时（此时认证已经
// 通过），标记为已连接并附带公司 ID 提示。
func (m *Manager) Status(ctx context.Context) Status {
	settings := m.Settings()
	status := Status{Configured: m.configured(), Transport: settings.Transport}
	if !status.Configured {
		status.Message = "未配置连接，请填写 API 账号或 MCP 服务地址"
		return status
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := m.connect(probeCtx)
	if err != nil {
		status.Message = "连接失败：" + err.Error()
		return status
	}
	defer func() { _ = conn.Close() }()

	workspaces, err := conn.Workspaces(probeCtx)
	switch {
	case err == nil:
		status.Connected = true
		status.Message = fmt.Sprintf("连接成功，可见 %d 个工作区", len(workspaces))
	case errors.Is(err, ErrCompanyIDRequired):
		status.Connected = true
		status.Message = "凭据有效。" + ErrCompanyIDRequired.Error()
	default:
		status.Message = "连接失败：" + err.Error()
	}
	return status
}

// Workspaces 列出账号可见的项目空间，用于新建项目时的选择器。
func (m *Manager) Workspaces(ctx context.Context) ([]Workspace, error) {
	conn, err := m.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	return conn.Workspaces(ctx)
}

// WorkspaceByID 按 ID 查询单个工作区详情。
func (m *Manager) WorkspaceByID(ctx context.Context, workspaceID string) (Workspace, bool) {
	conn, err := m.open(ctx)
	if err != nil {
		return Workspace{}, false
	}
	defer func() { _ = conn.Close() }()
	return conn.WorkspaceByID(ctx, workspaceID)
}

// Sync 拉取一个工作区的缺陷与需求，kinds 为空时默认全量两类。
func (m *Manager) Sync(ctx context.Context, workspaceID string, kinds []string) ([]domain.ExternalItem, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errors.New("缺少 TAPD 工作区 ID")
	}
	syncCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	conn, err := m.open(syncCtx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	if len(kinds) == 0 {
		kinds = []string{KindBug, KindRequirement}
	}
	items := make([]domain.ExternalItem, 0, 64)
	for _, kind := range kinds {
		batch, err := conn.List(syncCtx, kind, workspaceID)
		if err != nil {
			return nil, err
		}
		for index := range batch {
			batch[index].ProjectID = ""
		}
		items = append(items, batch...)
	}
	return items, nil
}

func (m *Manager) open(ctx context.Context) (connection, error) {
	if !m.configured() {
		return nil, ErrNotConfigured
	}
	return m.connect(ctx)
}

func (m *Manager) connect(ctx context.Context) (connection, error) {
	settings := m.Settings()
	if settings.Transport == "" || settings.Transport == TransportOpenAPI {
		return newOpenAPIConnection(settings, m.secret), nil
	}
	return newMCPConnection(ctx, settings, m.secret)
}

// Probe 用给定配置做一次连接测试，不改动运行时状态。
func Probe(ctx context.Context, settings Settings, secret string) Status {
	temporary := NewManager(settings, secret)
	return temporary.Status(ctx)
}
