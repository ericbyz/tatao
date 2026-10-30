package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// cliTimeout 覆盖 CLI 智能体完整的思考与执行时间。
const cliTimeout = 8 * time.Minute

// cliPromptSuffix 提醒 CLI 智能体不要执行工作区内操作，只输出设计结果。
const cliPromptSuffix = "\n\n注意：只输出用例设计结果（JSON 数组），不要读写工作区文件，不要执行命令。"

// chatCLI 调用本机 CLI 智能体（claude -p / codex exec / 自定义命令），
// 提示词经标准输入或参数传入，从 stdout 提取用例 JSON。
func chatCLI(ctx context.Context, cfg Config, prompt string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	runCtx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	if cfg.Environment != nil {
		var cliName string
		switch cfg.Type {
		case TypeClaude, TypeCustom:
			cliName = "claude"
		case TypeCodex:
			cliName = "codex"
		case TypeZCode:
			cliName = "zcode"
		}
		if cfg.Type == TypeCustom {
			fields := strings.Fields(strings.TrimSpace(cfg.Command))
			if len(fields) == 0 {
				return "", errors.New("自定义智能体缺少命令")
			}
			cliName = fields[0]
		}
		if cliName == "" {
			return "", fmt.Errorf("未知智能体类型 %q", cfg.Type)
		}
		return runRemote(ctx, cfg, prompt, cliName, cliRemoteArgs(cfg.Type, cfg.Model, cfg.ExtraArgs))
	}

	var command *exec.Cmd
	defer cancel()
	switch cfg.Type {
	case TypeClaude, TypeCodex, TypeZCode:
		name := cfg.Type
		args := cliArgs(cfg.Type, prompt, cfg.Model, cfg.ExtraArgs)
		command = resolveCommand(name, args)
	case TypeCustom:
		line := strings.TrimSpace(cfg.Command)
		if line == "" {
			return "", errors.New("自定义智能体缺少命令")
		}
		fields := strings.Fields(line)
		if strings.Contains(line, "{prompt}") {
			replaced := make([]string, 0, len(fields))
			for _, field := range fields {
				replaced = append(replaced, strings.ReplaceAll(field, "{prompt}", prompt))
			}
			command = resolveCommand(fields[0], append(replaced[1:], extraArgs(cfg.ExtraArgs)...))
		} else {
			command = resolveCommand(fields[0], append(fields[1:], extraArgs(cfg.ExtraArgs)...))
			command.Stdin = strings.NewReader(prompt)
		}
	default:
		return "", fmt.Errorf("未知智能体类型 %q", cfg.Type)
	}

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if command.Stdin == nil {
		command.Stdin = strings.NewReader(prompt)
	}
	command = commandWithRunCtx(command, runCtx)
	if cfg.WorkDir != "" {
		command.Dir = cfg.WorkDir
	}
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("智能体进程失败：%w：%s", err, truncate(stderr.String(), 200))
	}
	return unwrapCLIOutput(stdout.String()), nil
}

// cliArgs 组装内置 CLI 的参数；提示词经参数传入（Windows 参数上限 32K，足够）。
func cliArgs(kind, prompt, model, extra string) []string {
	var args []string
	switch kind {
	case TypeClaude:
		args = []string{"--print"}
	case TypeCodex:
		args = []string{"exec"}
	case TypeZCode:
		args = []string{"--print"}
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, extraArgs(extra)...)
	return append(args, prompt)
}

func extraArgs(extra string) []string {
	return strings.Fields(extra)
}

// resolveCommand 处理 Windows 下 .cmd/.bat shim（npm 全局包）需要经 cmd /c 运行的问题。
func resolveCommand(name string, args []string) *exec.Cmd {
	path, err := exec.LookPath(name)
	if err != nil {
		return exec.Command(name, args...)
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		return exec.Command("cmd", append([]string{"/c", path}, args...)...)
	}
	return exec.Command(path, args...)
}

// unwrapCLIOutput 兼容 claude --output-format json 的 {"result": "..."} 包裹。
func unwrapCLIOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	var envelope struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil && strings.TrimSpace(envelope.Result) != "" {
		return envelope.Result
	}
	return trimmed
}

// DetectLocalAgents 检测本机 PATH 中可用的 CLI 智能体。
func DetectLocalAgents() []LocalAgent {
	kinds := []struct {
		Type string
		Name string
	}{{TypeClaude, "claude"}, {TypeCodex, "codex"}, {TypeZCode, "zcode"}}
	agents := make([]LocalAgent, 0, len(kinds))
	for _, kind := range kinds {
		agent := LocalAgent{Type: kind.Type, Name: kind.Name}
		if path, err := exec.LookPath(kind.Name); err == nil {
			agent.Available = true
			agent.Path = path
		}
		agents = append(agents, agent)
	}
	return agents
}

// LocalAgent 描述一个本机 CLI 智能体的可用性。
type LocalAgent struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
}

var _ = os.Environ // 保留 os 导入，子进程继承当前环境

// commandWithRunCtx 把超时上下文绑定到命令（exec.CommandContext 需在构造时传入）。
func commandWithRunCtx(command *exec.Cmd, runCtx context.Context) *exec.Cmd {
	bound := exec.CommandContext(runCtx, command.Path, command.Args[1:]...)
	bound.Stdin = command.Stdin
	bound.Stdout = command.Stdout
	bound.Stderr = command.Stderr
	bound.Env = command.Env
	bound.Dir = command.Dir
	return bound
}
