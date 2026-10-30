package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ai-test-tool/server/internal/agent"
	"ai-test-tool/server/internal/domain"
)

// TestDesignWithRealClaude 走真实本机 claude CLI；无 claude 时跳过。
func TestDesignWithRealClaude(t *testing.T) {
	found := false
	for _, local := range agent.DetectLocalAgents() {
		if local.Available && local.Type == agent.TypeClaude {
			found = true
		}
	}
	if !found {
		t.Skip("claude not installed")
	}

	cfg := agent.Config{Name: "Claude Code", Type: agent.TypeClaude}
	items := []domain.ExternalItem{{
		ID: "1164077031001024273", Kind: "bug", Title: "无法同步上传大文件",
		Description: "同步任务包含超过 2GB 的单文件时上传中断，任务显示失败，但重新运行后可以继续。小文件正常。",
		Status:      "closed", Priority: "P1",
	}}
	start := time.Now()
	drafts, err := agent.DesignTestCases(context.Background(), cfg, items)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("real claude design failed after %s: %v", elapsed.Round(time.Second), err)
	}
	raw, _ := json.MarshalIndent(drafts, "", "  ")
	t.Logf("elapsed %s\n%s", elapsed.Round(time.Second), raw)
	if len(drafts) == 0 || len(drafts[0].Steps) < 2 {
		t.Fatalf("thin drafts: %+v", drafts)
	}
}
