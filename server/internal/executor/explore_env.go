package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
)

// ExploreEnvironment 让智能体经 SSH 登录目标环境，探索机器上与测试相关的
// 信息（部署的服务、端口、配置、目录、风险），整理为环境知识条目。
func ExploreEnvironment(ctx context.Context, cfg agent.Config, env agent.EnvironmentTarget, environmentNotes, environmentExplorePrompt string) ([]domain.KnowledgeEntry, error) {
	runCfg := cfg
	runCfg.Environment = &env
	runCfg.ExtraArgs = withExecutionPermissions(runCfg)

	prompt := buildEnvironmentExplorePrompt(environmentNotes, environmentExplorePrompt)
	start := time.Now()
	output, err := agent.RunPrompt(ctx, runCfg, prompt)
	duration := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("智能体探索环境失败：%w", err)
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
		return nil, fmt.Errorf("智能体探索环境 %s 后没有产出有效知识", duration.Round(time.Second))
	}
	return entries, nil
}

func buildEnvironmentExplorePrompt(environmentNotes, environmentExplorePrompt string) string {
	var builder strings.Builder
	builder.WriteString("你是测试基础设施工程师。当前通过 SSH 位于一台测试目标环境（Linux）上。\n")
	builder.WriteString("请只读探索这台机器，收集后续自动化测试设计与执行需要的环境知识。\n")
	if strings.TrimSpace(environmentNotes) != "" {
		builder.WriteString("\n【环境已知信息】\n")
		builder.WriteString(strings.TrimSpace(environmentNotes))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(environmentExplorePrompt) != "" {
		builder.WriteString("\n【探索重点要求】\n")
		builder.WriteString(strings.TrimSpace(environmentExplorePrompt))
		builder.WriteString("\n")
	}
	builder.WriteString(`
探索范围（以真实命令输出为准，不要臆造）：
1. 部署的服务与应用：运行中的服务、监听端口、部署路径。
2. 启动与验证：服务的启动/停止/状态查看命令。
3. 数据与文件约定：配置文件位置、数据与日志目录。
4. 接口与服务：API 前缀、管理端入口。
5. 术语解释：机器上出现的领域术语。
6. 风险与禁忌：不应操作的真实资源、危险路径。

要求：
- 只读探索：不要修改、删除、重启任何服务或文件。
- 每条知识内容必须具体（含真实路径、命令、端口）。
- 只输出 JSON 数组（6-12 条），格式：
[{"category":"功能导航","title":"...","content":"..."}]
category 只能取：功能导航 / 启动与验证 / 数据与文件约定 / 接口与服务 / 术语解释 / 风险与禁忌。
`)
	return builder.String()
}
