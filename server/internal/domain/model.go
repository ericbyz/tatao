package domain

import "time"

type ProjectSource string

const (
	ProjectSourceLocal ProjectSource = "local"
	ProjectSourceTAPD  ProjectSource = "tapd"
)

type Project struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Source           ProjectSource `json:"source"`
	LocalPath        string        `json:"localPath,omitempty"`
	TAPDWorkspaceID  string        `json:"tapdWorkspaceId,omitempty"`
	EnvironmentCount int           `json:"environmentCount"`
	CaseCount        int           `json:"caseCount"`
	Status           string        `json:"status"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type Environment struct {
	ID         string   `json:"id"`
	ProjectIDs []string `json:"projectIds"`
	Name       string   `json:"name"`
	OS         string   `json:"os"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Username   string   `json:"username"`
	AuthType   string   `json:"authType"`
	SecretSet  bool     `json:"secretSet"`
	Status     string   `json:"status"`
}

type TestCase struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"projectId"`
	Title         string   `json:"title"`
	Priority      string   `json:"priority"`
	Source        string   `json:"source"`
	ExternalID    string   `json:"externalId,omitempty"`
	Preconditions string   `json:"preconditions,omitempty"`
	Steps         []string `json:"steps"`
	Expected      string   `json:"expected"`
	Status        string   `json:"status"`
}

// AgentConfig 是一个可用于用例设计的 AI 智能体：本机 CLI（Claude Code /
// Codex 等）或 OpenAI 兼容 HTTP 接口。
type AgentConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // claude / codex / zcode / custom / openai
	BaseURL   string `json:"baseUrl,omitempty"`
	Model     string `json:"model,omitempty"`
	Command   string `json:"command,omitempty"`   // custom：含 {prompt} 占位符的完整命令
	ExtraArgs string `json:"extraArgs,omitempty"` // 附加命令行参数
	APIKeySet bool   `json:"apiKeySet"`
	IsDefault bool   `json:"isDefault"`
}

// TAPD 连接设置。密钥永不入库为明文，HasSecret 只用于界面展示。
type TAPDSettings struct {
	Transport    string   `json:"transport"` // openapi 直连 / mcp 外部 MCP 服务
	BaseURL      string   `json:"baseUrl"`
	AuthType     string   `json:"authType"` // basic（API 账号）/ bearer（个人访问令牌）
	APIUser      string   `json:"apiUser"`
	CompanyID    string   `json:"companyId"`    // 个人访问令牌列全部工作区时必填
	WorkspaceIds []string `json:"workspaceIds"` // 无公司 ID 时按此清单逐个远程验证工作区
	Endpoint     string   `json:"endpoint"`     // MCP：http(s) 地址或 stdio 启动命令
	HasSecret    bool     `json:"hasSecret"`
}

type ExternalItem struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority,omitempty"`
}

type SyncSummary struct {
	Total        int `json:"total"`
	Bugs         int `json:"bugs"`
	Requirements int `json:"requirements"`
}

// TestRun 是一次批量用例执行的批次记录。
type TestRun struct {
	ID              string          `json:"id"`
	ProjectID       string          `json:"projectId"`
	AgentID         string          `json:"agentId"`
	AgentName       string          `json:"agentName"`
	Model           string          `json:"model"`
	EnvironmentID   string          `json:"environmentId"`
	EnvironmentName string          `json:"environmentName"`
	WorkspaceDir    string          `json:"workspaceDir"`
	Status          string          `json:"status"` // running / completed / failed
	Total           int             `json:"total"`
	Passed          int             `json:"passed"`
	Failed          int             `json:"failed"`
	Errors          int             `json:"errors"`
	CurrentTitle    string          `json:"currentTitle,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	FinishedAt      time.Time       `json:"finishedAt"`
	Results         []RunCaseResult `json:"results"`
}

// RunCaseResult 是一条用例的执行结果。
type RunCaseResult struct {
	CaseID     string `json:"caseId"`
	Title      string `json:"title"`
	Source     string `json:"source"`
	Status     string `json:"status"` // passed / failed / error
	Summary    string `json:"summary"`
	Evidence   string `json:"evidence,omitempty"`
	DurationMs int64  `json:"durationMs"`
}
