package httpapi_test

import (
	"ai-test-tool/server/internal/httpapi"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-test-tool/server/internal/domain"
	"ai-test-tool/server/internal/store"
)

func TestDebugKnowledge(t *testing.T) {
	llm := fakeLLM(t)
	defer llm.Close()
	tapdServer := httptest.NewServer(&fakeTAPD{})
	defer tapdServer.Close()
	memory := store.NewMemory()
	handler := httpapi.New(memory, newOpenAPIGateway(t, tapdServer.URL))
	gateway := newOpenAPIGateway(t, tapdServer.URL)
	project := memory.AddProject(domain.Project{Name: "支付网关", Source: domain.ProjectSourceTAPD, TAPDWorkspaceID: "1001"})
	items, err := gateway.Sync(context.Background(), "1001", nil)
	if err != nil {
		t.Fatal(err)
	}
	memory.UpsertExternalItems(project.ID, items)
	knowledgeBody, _ := json.Marshal(map[string]string{"category": "功能导航", "title": "入口", "content": "内容"})
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/knowledge", bytes.NewReader(knowledgeBody)))
	fmt.Printf("POST knowledge: %d body=%s\n", resp.Code, resp.Body.String())
	resp2 := httptest.NewRecorder()
	handler.ServeHTTP(resp2, httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/knowledge", nil))
	fmt.Printf("GET knowledge: %d body=%s\n", resp2.Code, resp2.Body.String())
	fmt.Printf("store direct: %+v\n", memory.Knowledge(project.ID))
}
