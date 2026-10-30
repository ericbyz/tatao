package tapd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LocalProvider 是从本机常见 MCP 客户端配置中发现的 TAPD 服务条目。
type LocalProvider struct {
	Source   string `json:"source"`   // 配置文件路径
	Kind     string `json:"kind"`     // 客户端类型，如 claude / cursor
	Endpoint string `json:"endpoint"` // http(s) MCP 地址
}

type localMCPEntry struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

// FindLocalProviders 扫描常见 MCP 客户端配置文件里名字带 tapd 的远程服务，
// 便于在连接设置中一键导入；文件不存在时返回空列表。
func FindLocalProviders() []LocalProvider {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []struct {
		path string
		kind string
		keys []string
	}{
		{filepath.Join(home, ".claude.json"), "claude", []string{"mcpServers"}},
		{filepath.Join(home, ".cursor", "mcp.json"), "cursor", []string{"mcpServers"}},
		{filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json"), "claude-desktop", []string{"mcpServers"}},
	}

	providers := make([]LocalProvider, 0, 2)
	for _, candidate := range candidates {
		if candidate.path == "" {
			continue
		}
		content, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		var root map[string]json.RawMessage
		if json.Unmarshal(content, &root) != nil {
			continue
		}
		for _, key := range candidate.keys {
			var servers map[string]localMCPEntry
			if json.Unmarshal(root[key], &servers) != nil {
				continue
			}
			for name, entry := range servers {
				if !strings.Contains(strings.ToLower(name), "tapd") {
					continue
				}
				if strings.HasPrefix(entry.URL, "http://") || strings.HasPrefix(entry.URL, "https://") {
					providers = append(providers, LocalProvider{
						Source:   candidate.path,
						Kind:     candidate.kind,
						Endpoint: entry.URL,
					})
				}
			}
		}
	}
	return providers
}
