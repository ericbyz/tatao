package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ModelInfo 是本机 CLI 智能体已配置的默认模型。
type ModelInfo struct {
	Model  string `json:"model"`
	Source string `json:"source"` // 配置来源描述
}

// knownModelCatalog 提供 CLI 常用模型候选，供界面下拉提示。
var knownModelCatalog = map[string][]string{
	TypeClaude: {"sonnet", "opus", "haiku"},
	TypeCodex:  {"gpt-5-codex", "gpt-5"},
	TypeZCode:  {},
}

// KnownModels 返回某类 CLI 的常见模型候选。
func KnownModels(agentType string) []string {
	return knownModelCatalog[agentType]
}

// DetectConfiguredModel 读取本机 CLI 自身的配置文件，返回其默认模型。
// 智能体执行时应当显式指定该模型，而不是依赖隐式默认。
func DetectConfiguredModel(agentType string) (ModelInfo, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ModelInfo{}, false
	}
	switch agentType {
	case TypeClaude:
		if model := jsonField(filepath.Join(home, ".claude", "settings.json"), "model"); model != "" {
			return ModelInfo{Model: model, Source: "~/.claude/settings.json"}, true
		}
		if model := jsonField(filepath.Join(home, ".claude.json"), "model"); model != "" {
			return ModelInfo{Model: model, Source: "~/.claude.json"}, true
		}
		return ModelInfo{Source: "~/.claude/settings.json"}, false
	case TypeCodex:
		if model := tomlField(filepath.Join(home, ".codex", "config.toml"), "model"); model != "" {
			return ModelInfo{Model: model, Source: "~/.codex/config.toml"}, true
		}
		return ModelInfo{Source: "~/.codex/config.toml"}, false
	case TypeZCode:
		if model := jsonField(filepath.Join(home, ".zcode", "settings.json"), "model"); model != "" {
			return ModelInfo{Model: model, Source: "~/.zcode/settings.json"}, true
		}
		return ModelInfo{Source: "~/.zcode/settings.json"}, false
	default:
		return ModelInfo{}, false
	}
}

// jsonField 读取 JSON 文件顶层的字符串字段。
func jsonField(path, field string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal(content, &payload) != nil {
		return ""
	}
	if value, ok := payload[field].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// tomlField 从 TOML 文件读取顶层 `field = "value"` 字符串字段。
func tomlField(path, field string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `\s*=\s*"([^"]+)"`)
	match := pattern.FindSubmatch(content)
	if match == nil {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}
