package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"meowquest/backend/internal/llm"
)

// 切换接入方只应改配置：同一份材料、同一套业务，请求体按 provider 拼装。
func TestReviewUsesConfiguredProviderAndSendsAllPhotos(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	var body map[string]any
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		respondVerdict(w, true)
	}))
	defer mock.Close()
	s.Review = llm.Provider{Kind: llm.KindZhipu, BaseURL: mock.URL, APIKey: "k", Model: "glm-5.3-flash"}

	evidence := make([]map[string]string, 0, 20)
	for i := 0; i < 20; i++ {
		evidence = append(evidence, map[string]string{"type": "photo", "path": fmt.Sprintf("/uploads/p%02d.jpg", i+1)})
	}
	submitAI(t, s, tpl, kid, dt, evidence)
	job := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), job, time.Minute); err != nil {
		t.Fatalf("执行审核失败: %v", err)
	}
	if got := readAI(t, s, job.ID); got.Status != "succeeded" {
		t.Fatalf("job 未成功: %+v", got)
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("智谱接入方未开启思考: %v", body["thinking"])
	}
	if _, bad := body["enable_thinking"]; bad {
		t.Fatal("智谱接入方不应发送 enable_thinking")
	}
	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 21 {
		t.Fatalf("应发送 1 段文本 + 20 张图，实际 %d 块", len(content))
	}
}

// 未单独配置审核接入方时沿用对话侧配置，保持旧行为。
func TestReviewFallsBackToChatProvider(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	var body map[string]any
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		respondVerdict(w, true)
	}))
	defer mock.Close()
	s.Review = llm.Provider{}
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "qwen3.5-flash"}

	submitAI(t, s, tpl, kid, dt, photoEvidence())
	job := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), job, time.Minute); err != nil {
		t.Fatalf("执行审核失败: %v", err)
	}
	if got := readAI(t, s, job.ID); got.Status != "succeeded" {
		t.Fatalf("job 未成功: %+v", got)
	}
	if got := body["enable_thinking"]; got != false {
		t.Fatalf("回落兼容协议应关闭思考链: %v", got)
	}
	if got := body["model"]; got != "qwen3.5-flash" {
		t.Fatalf("回落应使用对话侧模型: %v", got)
	}
}
