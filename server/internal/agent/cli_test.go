package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
)

// fakeDesigner 是读 stdin、输出用例 JSON 的假 CLI 智能体脚本。
func fakeDesigner(t *testing.T) string {
	t.Helper()
	script := `let raw='';process.stdin.on('data',c=>raw+=c).on('end',()=>{const m=raw.match(/\[素材\d+\]/g)||[];const arr=[];for(let i=0;i<m.length;i++){arr.push({title:'CLI 设计用例 '+(i+1),preconditions:'环境就绪',steps:['打开目标页面','执行核心操作'],expected:'表现符合预期',priority:'P1'})}process.stdout.write(JSON.stringify(arr))})`
	path := filepath.Join(t.TempDir(), "fake-designer.js")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDesignViaCLICustomCommand(t *testing.T) {
	// given: 一个读 stdin 的自定义命令智能体（node 可跨平台）
	script := fakeDesigner(t)
	cfg := agent.Config{
		Name:    "fake-cli",
		Type:    agent.TypeCustom,
		Command: "node " + script,
	}

	// when: 设计用例
	drafts, err := agent.DesignTestCases(context.Background(), cfg, []domain.ExternalItem{
		{ID: "b1", Kind: "bug", Title: "登录超时"},
		{ID: "b2", Kind: "bug", Title: "页面空白"},
	}, "")
	if err != nil {
		t.Fatalf("design failed: %v", err)
	}

	// then: 两条草稿均带前置条件与步骤
	if len(drafts) != 2 || drafts[0].Preconditions == "" || len(drafts[0].Steps) != 2 {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}
}

func TestParseDraftsToleratesCodeFence(t *testing.T) {
	content := "```json\n" + mustJSON(t, []map[string]any{
		{"title": "带代码块的输出", "steps": []string{"步骤一", "步骤二"}, "expected": "通过", "priority": "P2"},
	}) + "\n```"
	drafts, err := agent.ParseDrafts(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Priority != "P2" {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
