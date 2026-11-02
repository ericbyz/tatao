// Package executor 通过 AI 智能体在被测项目工作目录中实际执行测试用例，
// 收集每条用例的通过状态、结论与证据。
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

// Outcome 是一条用例的执行结论。
type Outcome struct {
	Status   string // passed / failed / error
	Summary  string
	Evidence string
}

type caseOutcome struct {
	Status   string `json:"status"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
}

// ExecuteCase 在 workspaceDir 中调用智能体实际执行一条测试用例。
// source 是用例关联的 TAPD 素材原文（可空），extraContext 是被测项目背景说明。
func ExecuteCase(ctx context.Context, cfg agent.Config, workspaceDir string, testCase domain.TestCase, source *domain.ExternalItem, extraContext string) (Outcome, int64, error) {
	if workspaceDir == "" {
		return Outcome{}, 0, errors.New("缺少被测项目工作目录")
	}
	runCfg := cfg
	runCfg.WorkDir = workspaceDir
	runCfg.ExtraArgs = withExecutionPermissions(runCfg)

	prompt := buildExecutionPrompt(workspaceDir, testCase, source, extraContext)
	start := time.Now()
	output, err := agent.RunPrompt(ctx, runCfg, prompt)
	duration := time.Since(start)
	if err != nil {
		return Outcome{Status: "error", Summary: "智能体执行失败：" + err.Error()}, duration.Milliseconds(), err
	}

	var outcome caseOutcome
	if err := json.Unmarshal([]byte(ExtractJSON(output)), &outcome); err != nil {
		return Outcome{
			Status:   "error",
			Summary:  "无法解析智能体输出",
			Evidence: truncate(output, 4000),
		}, duration.Milliseconds(), fmt.Errorf("解析执行结果失败: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(outcome.Status)) {
	case "passed":
		outcome.Status = "passed"
	case "failed":
		outcome.Status = "failed"
	default:
		outcome.Status = "error"
	}
	outcome.Summary = strings.TrimSpace(outcome.Summary)
	outcome.Evidence = strings.TrimSpace(outcome.Evidence)
	if outcome.Summary == "" {
		outcome.Summary = "智能体未给出结论"
	}
	return Outcome(outcome), duration.Milliseconds(), nil
}

// withExecutionPermissions 为 CLI 智能体补上执行测试所需的权限参数；
// 用户已配置同类参数时不重复添加。
func withExecutionPermissions(cfg agent.Config) string {
	extra := strings.TrimSpace(cfg.ExtraArgs)
	switch cfg.Type {
	case agent.TypeClaude:
		if !strings.Contains(extra, "permission-mode") && !strings.Contains(extra, "dangerously") {
			extra = joinArgs(extra, "--permission-mode bypassPermissions")
		}
	case agent.TypeCodex:
		if !strings.Contains(extra, "sandbox") && !strings.Contains(extra, "full-auto") {
			extra = joinArgs(extra, "--sandbox danger-full-access")
		}
	}
	return extra
}

func joinArgs(base, addition string) string {
	if base == "" {
		return addition
	}
	return base + " " + addition
}

func buildExecutionPrompt(workspaceDir string, testCase domain.TestCase, source *domain.ExternalItem, extraContext string) string {
	var builder strings.Builder
	builder.WriteString("你是测试执行工程师。被测项目位于目录：")
	builder.WriteString(workspaceDir)
	builder.WriteString("。\n")
	if strings.TrimSpace(extraContext) != "" {
		builder.WriteString("\n【项目背景】\n")
		builder.WriteString(strings.TrimSpace(extraContext))
		builder.WriteString("\n")
	}
	if source != nil {
		builder.WriteString("\n【用例来源素材（")
		builder.WriteString(source.Kind)
		builder.WriteString(" ")
		builder.WriteString(source.ID)
		builder.WriteString("）原文】\n")
		builder.WriteString(source.Title)
		if strings.TrimSpace(source.Description) != "" {
			builder.WriteString("\n")
			builder.WriteString(source.Description)
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\n请实际执行下面这条测试用例并给出结论。\n\n")
	builder.WriteString("【用例标题】")
	builder.WriteString(testCase.Title)
	builder.WriteString("\n")
	if testCase.Preconditions != "" {
		builder.WriteString("【前置条件】")
		builder.WriteString(testCase.Preconditions)
		builder.WriteString("\n")
	}
	builder.WriteString("【执行步骤】\n")
	for index, step := range testCase.Steps {
		builder.WriteString(fmt.Sprintf("%d. %s\n", index+1, step))
	}
	builder.WriteString("【预期结果】")
	builder.WriteString(testCase.Expected)
	builder.WriteString("\n\n执行要求：\n")
	builder.WriteString("1. 在被测项目目录内按步骤逐项验证：可阅读源码与配置、运行项目已有的测试/构建/脚本命令来收集证据。\n")
	builder.WriteString("2. 无法在当前环境验证的步骤，给出基于代码与配置的静态分析结论，并说明原因。\n")
	builder.WriteString("3. 只输出一个 JSON 对象，不要输出解释、Markdown 标记或其他文本，格式：\n")
	builder.WriteString(`{"status":"passed|failed|error","summary":"一句话结论","evidence":"执行过程与关键输出摘录"}` + "\n")
	builder.WriteString("status 判定：所有步骤通过且预期满足为 passed；预期未满足为 failed；无法执行或环境受限为 error。\n")
	return builder.String()
}

// ExtractJSON 从模型输出中提取第一个 JSON 值（对象或数组），容忍代码块与
// 前后缀文本；按先出现的结构定界。
func ExtractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	objectStart := strings.Index(trimmed, "{")
	arrayStart := strings.Index(trimmed, "[")
	switch {
	case arrayStart >= 0 && (objectStart < 0 || arrayStart < objectStart):
		end := strings.LastIndex(trimmed, "]")
		if end > arrayStart {
			return trimmed[arrayStart : end+1]
		}
	case objectStart >= 0:
		end := strings.LastIndex(trimmed, "}")
		if end > objectStart {
			return trimmed[objectStart : end+1]
		}
	}
	return trimmed
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
