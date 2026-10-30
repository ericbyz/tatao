// Package agent 调用 AI 智能体（OpenAI 兼容 chat/completions 接口）为
// TAPD 素材设计测试用例：前置条件、具体步骤与预期结果。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-test-tool/server/internal/domain"
)

const requestTimeout = 90 * time.Second

// 支持的智能体类型。
const (
	TypeOpenAI = "openai"
	TypeClaude = "claude"
	TypeCodex  = "codex"
	TypeZCode  = "zcode"
	TypeCustom = "custom"
)

// Config 是一次调用的完整配置（密钥由调用方注入）。
type Config struct {
	Name        string
	Type        string
	BaseURL     string
	Model       string
	Command     string
	ExtraArgs   string
	APIKey      string
	WorkDir     string             // CLI 智能体的工作目录（被测项目路径）
	Environment *EnvironmentTarget // 非空时经 SSH 在远程环境执行
}

func (c Config) isCLI() bool {
	switch c.Type {
	case TypeClaude, TypeCodex, TypeZCode, TypeCustom:
		return true
	default:
		return false
	}
}

// TestCaseDraft 是智能体产出的用例草稿。
type TestCaseDraft struct {
	Title         string   `json:"title"`
	Preconditions string   `json:"preconditions"`
	Steps         []string `json:"steps"`
	Expected      string   `json:"expected"`
	Priority      string   `json:"priority"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// RunPrompt 向智能体发送一段任务提示词，返回其文本输出（CLI 与 HTTP 通用）。
func RunPrompt(ctx context.Context, cfg Config, prompt string) (string, error) {
	if cfg.isCLI() {
		return chatCLI(ctx, cfg, prompt)
	}
	verifyCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return chat(verifyCtx, cfg, prompt)
}

// DesignTestCases 为一批素材设计用例，返回与输入顺序一致的草稿。
func DesignTestCases(ctx context.Context, cfg Config, items []domain.ExternalItem) ([]TestCaseDraft, error) {
	if len(items) == 0 {
		return nil, nil
	}

	prompt := buildSystemPrompt() + "\n\n" + buildPrompt(items)
	if cfg.isCLI() {
		content, err := chatCLI(ctx, cfg, prompt)
		if err != nil {
			return nil, err
		}
		drafts, err := ParseDrafts(content)
		if err != nil {
			return nil, err
		}
		if len(drafts) < len(items) {
			return drafts, fmt.Errorf("智能体只返回了 %d/%d 条用例，可重试或改用规则生成", len(drafts), len(items))
		}
		return drafts, nil
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	content, err := chat(ctx, cfg, prompt)
	if err != nil {
		return nil, err
	}
	drafts, err := ParseDrafts(content)
	if err != nil {
		return nil, err
	}
	if len(drafts) < len(items) {
		return drafts, fmt.Errorf("智能体只返回了 %d/%d 条用例，可重试或改用规则生成", len(drafts), len(items))
	}
	return drafts, nil
}

// Verify 发送一次最小请求验证配置可用。
func Verify(ctx context.Context, cfg Config) (time.Duration, error) {
	start := time.Now()
	if cfg.isCLI() {
		verifyCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		if _, err := chatCLI(verifyCtx, cfg, "只回复两个字：正常"); err != nil {
			return 0, err
		}
		return time.Since(start), nil
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := chat(verifyCtx, cfg, "只回复两个字：正常"); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func chat(ctx context.Context, cfg Config, userContent string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:       cfg.Model,
		Temperature: 0.3,
		Messages: []chatMessage{
			{Role: "system", Content: buildSystemPrompt()},
			{Role: "user", Content: userContent},
		},
	})
	if err != nil {
		return "", fmt.Errorf("编码请求失败: %w", err)
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构建请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return "", fmt.Errorf("请求智能体失败: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("读取智能体响应失败: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("智能体返回 %d：%s", response.StatusCode, truncate(string(raw), 200))
	}
	var payload chatResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("解析智能体响应失败: %w", err)
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return "", fmt.Errorf("智能体错误：%s", payload.Error.Message)
	}
	if len(payload.Choices) == 0 || strings.TrimSpace(payload.Choices[0].Message.Content) == "" {
		return "", errors.New("智能体返回了空内容")
	}
	return payload.Choices[0].Message.Content, nil
}

func buildSystemPrompt() string { return baseSystemPrompt + cliPromptSuffix }

const baseSystemPrompt = `你是资深测试设计工程师。根据给定的 TAPD 缺陷或需求素材，为每条素材设计一条可直接执行的测试用例。
要求：
1. 用例必须覆盖素材描述的核心场景，并补充边界与异常场景。
2. 前置条件说明执行前需要满足的环境、数据和状态。
3. 步骤 3-6 条，每条是一个具体的可执行操作，包含操作对象和操作方式，不要笼统表述。
4. 预期结果具体可判定，包含可观察的现象或数据。
5. 优先级取 P0/P1/P2：核心功能与主流程用 P0/P1，边缘场景用 P2。
只输出 JSON 数组，不要输出任何解释、Markdown 代码块标记或多余文本。数组元素格式：
{"title":"用例标题","preconditions":"前置条件","steps":["步骤1","步骤2"],"expected":"预期结果","priority":"P1"}
输出数组的顺序、条数必须与输入素材一一对应。`

func buildPrompt(items []domain.ExternalItem) string {
	var builder strings.Builder
	builder.WriteString("请为以下 ")
	builder.WriteString(fmt.Sprint(len(items)))
	builder.WriteString(" 条素材设计测试用例：\n")
	for i, item := range items {
		kind := "需求"
		if item.Kind == "bug" {
			kind = "缺陷"
		}
		builder.WriteString(fmt.Sprintf("\n%d. [素材%d] 类型：%s；ID：%s；标题：%s；状态：%s；优先级：%s\n描述：%s\n",
			i+1, i+1, kind, item.ID, item.Title, item.Status, item.Priority, item.Description))
	}
	return builder.String()
}

// parseDrafts 从模型输出中提取 JSON 数组，容忍代码块包裹等常见格式。
func ParseDrafts(content string) ([]TestCaseDraft, error) {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start < 0 || end <= start {
		return nil, errors.New("智能体输出中没有 JSON 数组")
	}
	var drafts []TestCaseDraft
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &drafts); err != nil {
		return nil, fmt.Errorf("解析用例 JSON 失败: %w", err)
	}
	cleaned := drafts[:0]
	for _, draft := range drafts {
		draft.Title = strings.TrimSpace(draft.Title)
		if draft.Title == "" {
			continue
		}
		steps := make([]string, 0, len(draft.Steps))
		for _, step := range draft.Steps {
			if step = strings.TrimSpace(step); step != "" {
				steps = append(steps, step)
			}
		}
		if len(steps) == 0 {
			continue
		}
		draft.Steps = steps
		draft.Expected = strings.TrimSpace(draft.Expected)
		draft.Preconditions = strings.TrimSpace(draft.Preconditions)
		draft.Priority = normalizePriority(draft.Priority)
		cleaned = append(cleaned, draft)
	}
	if len(cleaned) == 0 {
		return nil, errors.New("智能体输出中没有有效用例")
	}
	return cleaned, nil
}

func normalizePriority(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "P0":
		return "P0"
	case "P1":
		return "P1"
	default:
		return "P2"
	}
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
