package executor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/executor"
)

// fakeRunner 是一个读 stdin、按输入翻转结论的假 CLI 脚本（node 跨平台）。
func fakeRunner(t *testing.T, decisionJS string) agent.Config {
	t.Helper()
	script := "let raw='';process.stdin.on('data',c=>raw+=c).on('end',()=>{" +
		"const status=(" + decisionJS + ")?'passed':'failed';" +
		"process.stdout.write(JSON.stringify({status:status,summary:'core path verified',evidence:raw.slice(0,400)}))})"
	path := filepath.Join(t.TempDir(), "runner.js")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return agent.Config{Name: "fake", Type: agent.TypeCustom, Command: "node " + path}
}

func TestExecuteCaseParsesOutcome(t *testing.T) {
	// given: 输入包含标记时判定 passed 的假智能体（evidence 回显收到的提示词）
	cfg := fakeRunner(t, `raw.includes('MARKER')`)
	testCase := domain.TestCase{Title: "登录功能", Steps: []string{"输入正确密码", "点击登录"}, Expected: "进入首页"}

	// when: 携带项目上下文执行
	outcome, ms, err := executor.ExecuteCase(context.Background(), cfg, t.TempDir(), testCase, nil, "NAS storage tool. go test ./... MARKER-CONTEXT")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	// then: 结论与耗时被收集，且项目上下文与用例都进入了提示词
	if (outcome.Status != "passed" && outcome.Status != "failed") || outcome.Summary == "" || ms < 0 {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	for _, fragment := range []string{"MARKER-CONTEXT", "【项目背景】", "登录功能", "【预期结果】"} {
		if !strings.Contains(outcome.Evidence, fragment) {
			t.Fatalf("prompt missing %q, evidence: %.300s", fragment, outcome.Evidence)
		}
	}
}

func TestExecuteCaseExtractsJSONFromFencedOutput(t *testing.T) {
	// given: 输出带说明文字与 Markdown 代码块的假智能体
	script := "process.stdout.write(" +
		"'conclusion: ' + String.fromCharCode(96,96,96) + 'json' + String.fromCharCode(10) + " +
		"JSON.stringify({status:'passed',summary:'s',evidence:'e'}) + String.fromCharCode(10,96,96,96))"
	path := filepath.Join(t.TempDir(), "fenced.js")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := agent.Config{Name: "fake", Type: agent.TypeCustom, Command: "node " + path}

	// when: 执行
	outcome, _, err := executor.ExecuteCase(context.Background(), cfg, t.TempDir(), domain.TestCase{Title: "t", Steps: []string{"s"}}, nil, "")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	// then: 从围栏输出中提取结论
	if outcome.Status != "passed" {
		t.Fatalf("unexpected status: %+v", outcome)
	}
}

func TestExecuteCaseRequiresWorkspace(t *testing.T) {
	cfg := agent.Config{Name: "fake", Type: agent.TypeCustom, Command: "node -e 1"}
	if _, _, err := executor.ExecuteCase(context.Background(), cfg, "", domain.TestCase{}, nil, ""); err == nil {
		t.Fatal("expected error for empty workspace")
	}
}
