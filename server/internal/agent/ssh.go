package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// EnvironmentTarget 描述一次远程执行的目标环境（Linux 主机）。
type EnvironmentTarget struct {
	Kind     string // linux
	Host     string
	Port     int
	Username string
	Password string
}

const sshDialTimeout = 15 * time.Second

// runRemote 通过 SSH 登录目标环境，在被测目录内执行 CLI 智能体：
// 提示词经标准输入传入，返回智能体的 stdout。
func runRemote(ctx context.Context, cfg Config, prompt string, cliName string, args []string) (string, error) {
	env := cfg.Environment
	if env == nil || env.Host == "" {
		return "", errors.New("远程环境缺少主机地址")
	}
	if cfg.WorkDir == "" {
		return "", errors.New("缺少项目在环境上的路径")
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", env.Host, env.Port), &ssh.ClientConfig{
		User:            env.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(env.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 内网测试环境，首版不做主机指纹校验
		Timeout:         sshDialTimeout,
		// 连接超时由 ssh.ClientConfig.Timeout 控制
	})
	if err != nil {
		return "", fmt.Errorf("SSH 连接 %s 失败: %w", env.Host, err)
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建 SSH 会话失败: %w", err)
	}
	defer func() { _ = session.Close() }()

	remoteCommand := "cd " + shellQuote(cfg.WorkDir) + " && " + cliName + " " + joinQuoted(args)
	session.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(remoteCommand) }()

	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("远程执行失败：%w：%s", err, truncate(stderr.String(), 300))
		}
	case <-ctx.Done():
		_ = session.Close()
		return "", fmt.Errorf("远程执行超时或被取消")
	}
	return stdout.String(), nil
}

// cliRemoteArgs 返回远程执行 CLI 的参数（提示词经 stdin 传入，不在参数中）。
func cliRemoteArgs(kind, model, extra string) []string {
	var args []string
	switch kind {
	case TypeClaude:
		args = []string{"--print"}
	case TypeCodex:
		args = []string{"exec", "-"} // `-` 表示从 stdin 读取提示词
	case TypeZCode:
		args = []string{"--print"}
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, extraArgs(extra)...)
	return args
}

// shellQuote 做 POSIX 单引号转义。
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func joinQuoted(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}
