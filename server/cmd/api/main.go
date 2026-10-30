package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/httpapi"
	"ai-test-tool/server/internal/store"
	"ai-test-tool/server/internal/tapd"
)

func main() {
	address := envOr("AI_TEST_TOOL_ADDR", ":8080")
	memoryStore := store.NewMemory()
	if err := memoryStore.EnablePersistence(envOr("AI_TEST_TOOL_DATA", "data/state.json")); err != nil {
		slog.Warn("无法启用持久化，将以内存模式运行", "error", err)
	}
	gateway := buildTAPDGateway(memoryStore)
	handler := httpapi.New(memoryStore, gateway)

	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	slog.Info("AI Test Tool API listening", "address", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// buildTAPDGateway 优先读取界面保存的连接设置，其次回退到环境变量。
func buildTAPDGateway(memoryStore *store.Memory) *tapd.Manager {
	settings := memoryStore.TAPDSettings()
	secret, _ := memoryStore.Secret(store.SecretTAPD)
	if settings.Transport == "" {
		settings = settingsFromEnv()
		if settings.Transport == tapd.TransportMCP && secret == "" {
			secret = os.Getenv("TAPD_API_PASSWORD")
		}
	}
	return tapd.NewManager(settings, secret)
}

func settingsFromEnv() domain.TAPDSettings {
	if endpoint := os.Getenv("TAPD_MCP_ENDPOINT"); endpoint != "" {
		return domain.TAPDSettings{Transport: tapd.TransportMCP, Endpoint: endpoint}
	}
	return domain.TAPDSettings{
		Transport: tapd.TransportOpenAPI,
		BaseURL:   envOr("TAPD_BASE_URL", tapd.DefaultAPIBaseURL),
		APIUser:   os.Getenv("TAPD_API_USER"),
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
