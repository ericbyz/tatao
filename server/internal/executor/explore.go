package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
)

// exploreOutcome 是智能体探索项目后产出的知识草稿。
type exploreOutcome struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

// ExploreProject 让智能体以只读方式探索被测项目目录，把发现整理为
// 知识库条目（功能入口、启动/测试命令、数据与文件约定、接口、风险等）。
func ExploreProject(ctx context.Context, cfg agent.Config, workspaceDir, projectContext string) ([]domain.KnowledgeEntry, error) {
	if workspaceDir == "" {
		return nil, errors.New("缺少被测项目工作目录")
	}
	runCfg := cfg
	runCfg.WorkDir = workspaceDir
	runCfg.ExtraArgs = withReadOnlyPermissions(runCfg)

	prompt := buildExplorePrompt(workspaceDir, projectContext)
	start := time.Now()
	output, err := agent.RunPrompt(ctx, runCfg, prompt)
	duration := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("智能体探索失败：%w", err)
	}

	var outcomes []exploreOutcome
	if err := json.Unmarshal([]byte(ExtractJSON(output)), &outcomes); err != nil {
		return nil, fmt.Errorf("解析探索结果失败: %w", err)
	}

	entries := make([]domain.KnowledgeEntry, 0, len(outcomes))
	for _, item := range outcomes {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.Content)
		if title == "" || content == "" {
			continue
		}
		if len([]rune(content)) > 800 {
			content = string([]rune(content)[:800]) + "…"
		}
		entries = append(entries, domain.KnowledgeEntry{
			Category: normalizeExploreCategory(item.Category),
			Title:    truncate(title, 80),
			Content:  content,
			Source:   "ai",
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("智能体探索 %s 后没有产出有效知识", duration.Round(time.Second))
	}
	return entries, nil
}

// withReadOnlyPermissions 探索只需读取：CLI 智能体补充只读工具权限。
func withReadOnlyPermissions(cfg agent.Config) string {
	extra := strings.TrimSpace(cfg.ExtraArgs)
	switch cfg.Type {
	case agent.TypeClaude:
		if !strings.Contains(extra, "allowedTools") && !strings.Contains(extra, "permission-mode") {
			extra = joinArgs(extra, "--allowedTools Read --allowedTools Grep --allowedTools Glob")
		}
	case agent.TypeCodex:
		// codex exec 默认 read-only 沙箱，无需额外参数
	}
	return extra
}

func buildExplorePrompt(workspaceDir, projectContext string) string {
	var builder strings.Builder
	builder.WriteString("你是测试基础设施工程师。被测项目位于目录：")
	builder.WriteString(workspaceDir)
	builder.WriteString("。\n请实际浏览该目录（源码结构、构建/启动脚本、配置文件、文档），收集后续自动化测试设计与执行需要的项目知识。\n")
	if strings.TrimSpace(projectContext) != "" {
		builder.WriteString("\n【已知背景】\n")
		builder.WriteString(strings.TrimSpace(projectContext))
		builder.WriteString("\n")
	}
	builder.WriteString(`
收集范围：
1. 功能导航：主要功能模块、入口、页面或命令。
2. 启动与验证：构建、启动、运行测试的具体命令。
3. 数据与文件约定：配置文件位置、数据目录、日志目录。
4. 接口与服务：服务端口、API 前缀、认证方式。
5. 术语解释：领域缩写与内部术语。
6. 风险与禁忌：不应操作的真实资源、敏感数据、危险命令。

要求：
- 内容必须具体可执行（含真实路径、命令、端口），不要泛泛而谈。
- 只依据目录内真实存在的文件与内容，不要臆造。
- 不要修改任何文件，不要执行会改变系统状态的命令。

只输出 JSON 数组（6-12 条），格式：
[{"category":"功能导航","title":"...","content":"..."}]
category 只能取：功能导航 / 启动与验证 / 数据与文件约定 / 接口与服务 / 术语解释 / 风险与禁忌。
`)
	return builder.String()
}

// normalizeCategory 把智能体输出的类别收敛到预置集合。
func normalizeExploreCategory(value string) string {
	value = strings.TrimSpace(value)
	for _, known := range []string{"功能导航", "启动与验证", "数据与文件约定", "接口与服务", "账号与凭据", "术语解释", "风险与禁忌"} {
		if value == known {
			return known
		}
	}
	switch {
	case strings.Contains(value, "启动") || strings.Contains(value, "构建") || strings.Contains(value, "测试"):
		return "启动与验证"
	case strings.Contains(value, "接口") || strings.Contains(value, "服务"):
		return "接口与服务"
	case strings.Contains(value, "账号") || strings.Contains(value, "凭据"):
		return "账号与凭据"
	case strings.Contains(value, "风险") || strings.Contains(value, "禁忌"):
		return "风险与禁忌"
	case strings.Contains(value, "术语"):
		return "术语解释"
	default:
		return "功能导航"
	}
}
