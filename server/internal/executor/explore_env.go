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

// EnvironmentAccess 描述一个测试目标环境的访问方式（由平台注入提示词，
// 本机智能体自行通过 SSH 等工具访问该环境）。
type EnvironmentAccess struct {
	Name          string
	Host          string
	Port          int
	Username      string
	Password      string
	Notes         string
	ExplorePrompt string
}

// ExploreEnvironment 让本机智能体通过 SSH 只读探索目标环境，把发现的
// 部署服务、端口、配置、风险整理为环境知识条目。
func ExploreEnvironment(ctx context.Context, cfg agent.Config, env agent.EnvironmentAccess) ([]domain.KnowledgeEntry, error) {
	prompt := buildEnvironmentExplorePrompt(env)
	start := time.Now()
	output, err := agent.RunPrompt(ctx, cfg, prompt)
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
		return nil, fmt.Errorf("智能体探索 %s 后没有产出有效知识", duration.Round(time.Second))
	}
	return entries, nil
}

func buildEnvironmentExplorePrompt(env agent.EnvironmentAccess) string {
	var builder strings.Builder
	builder.WriteString("你是测试基础设施工程师，正在用户的本机工作。测试目标环境是一台远程 Linux 机器：\n")
	builder.WriteString(fmt.Sprintf("- 主机：%s\n- 端口：%d\n- 用户名：%s\n- 密码：%s\n",
		env.Host, env.Port, env.Username, env.Password))
	builder.WriteString("\n请通过 SSH 连接该机器（非交互密码认证可用 sshpass，或 SSH_ASKPASS 机制：写临时脚本 echo 密码并设置 SSH_ASKPASS 与 SSH_ASKPASS_REQUIRE=force），只读探索这台机器，收集后续自动化测试设计与执行需要的环境知识。\n")
	if strings.TrimSpace(env.Notes) != "" {
		builder.WriteString("\n【环境已知信息】\n")
		builder.WriteString(strings.TrimSpace(env.Notes))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(env.ExplorePrompt) != "" {
		builder.WriteString("\n【探索重点要求】\n")
		builder.WriteString(strings.TrimSpace(env.ExplorePrompt))
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
- 只读探索：通过 SSH 执行的命令只能查看，不得修改、删除、重启远程机器上的任何服务或文件。
- 每条知识内容必须具体（含真实路径、命令、端口）。
- 所有探索通过 ssh 远程执行只读命令完成；不要修改本机与远程的任何文件。

只输出 JSON 数组（6-12 条），格式：
[{"category":"功能导航","title":"...","content":"..."}]
category 只能取：功能导航 / 启动与验证 / 数据与文件约定 / 接口与服务 / 术语解释 / 风险与禁忌。
`)
	return builder.String()
}
