package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureBody(t *testing.T, response string) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, &captured
}

func okResponse(content string) string {
	return `{"choices":[{"finish_reason":"stop","message":{"content":` + mustQuote(content) + `}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
}

func TestOpenAICompatibleBodyDisablesThinking(t *testing.T) {
	server, body := captureBody(t, okResponse("ok"))
	p := Provider{Kind: KindOpenAI, BaseURL: server.URL, APIKey: "k", Model: "qwen3.5-flash"}
	if _, _, err := p.Complete(context.Background(), []Part{TextPart("hi"), ImagePart("https://example.com/a.jpg")}, 512); err != nil {
		t.Fatal(err)
	}
	if got := (*body)["enable_thinking"]; got != false {
		t.Fatalf("兼容协议应关闭思考链: %v", got)
	}
	if _, ok := (*body)["thinking"]; ok {
		t.Fatal("兼容协议不应发送 thinking 字段")
	}
	if got := (*body)["max_tokens"]; got != float64(512) {
		t.Fatalf("max_tokens 未生效: %v", got)
	}
	parts := (*body)["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("内容块数量错误: %d", len(parts))
	}
}

func TestZhipuBodyEnablesThinkingAndReasoningEffort(t *testing.T) {
	server, body := captureBody(t, okResponse("ok"))
	p := Provider{Kind: KindZhipu, BaseURL: server.URL, APIKey: "k", Model: "glm-5.3-flash", ReasoningEffort: "low"}
	if _, _, err := p.Complete(context.Background(), []Part{TextPart("hi")}, 8192); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*body)["enable_thinking"]; ok {
		t.Fatal("智谱不应发送 enable_thinking")
	}
	thinking, ok := (*body)["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("智谱必须开启思考: %v", (*body)["thinking"])
	}
	if got := (*body)["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort 未生效: %v", got)
	}
}

func TestCompleteRejectsTruncatedOutput(t *testing.T) {
	server, _ := captureBody(t, `{"choices":[{"finish_reason":"length","message":{"content":""}}]}`)
	p := Provider{Kind: KindOpenAI, BaseURL: server.URL, APIKey: "k", Model: "m"}
	if _, _, err := p.Complete(context.Background(), []Part{TextPart("hi")}, 100); !errors.Is(err, ErrTruncated) {
		t.Fatalf("截断应返回 ErrTruncated: %v", err)
	}
}

func TestCompleteRedactsAPIKeyOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"InvalidParameter","message":"media download failed top-secret-key"}}`))
	}))
	defer server.Close()
	p := Provider{Kind: KindOpenAI, BaseURL: server.URL, APIKey: "top-secret-key", Model: "m"}
	if _, _, err := p.Complete(context.Background(), []Part{TextPart("hi")}, 100); err == nil {
		t.Fatal("失败应返回错误")
	} else if !strings.Contains(err.Error(), "InvalidParameter") || strings.Contains(err.Error(), "top-secret-key") {
		t.Fatalf("错误描述缺失或泄露密钥: %v", err)
	}
}

func TestProviderFromEnvFallsBackWhenUnset(t *testing.T) {
	fallback := Provider{Kind: KindOpenAI, BaseURL: "https://fallback.example/v1", APIKey: "fk", Model: "qwen3.5-flash"}
	got := ProviderFromEnv("REVIEW", fallback)
	if got != fallback {
		t.Fatalf("未配置时应原样回落: %+v", got)
	}
}

func TestProviderFromEnvOverridesReviewProvider(t *testing.T) {
	t.Setenv("MEOW_REVIEW_PROVIDER", "zhipu")
	t.Setenv("MEOW_REVIEW_MODEL", "glm-5.3-flash")
	t.Setenv("MEOW_REVIEW_API_KEY", "zk")
	t.Setenv("MEOW_REVIEW_MAX_TOKENS", "8192")
	fallback := Provider{Kind: KindOpenAI, BaseURL: "https://fallback.example/v1", APIKey: "fk", Model: "qwen3.5-flash"}
	got := ProviderFromEnv("REVIEW", fallback)
	if got.Kind != KindZhipu || got.Model != "glm-5.3-flash" || got.APIKey != "zk" {
		t.Fatalf("切换接入方失败: %+v", got)
	}
	if got.BaseURL != DefaultZhipuBaseURL {
		t.Fatalf("智谱默认地址错误: %s", got.BaseURL)
	}
	if got.MaxTokens != 8192 {
		t.Fatalf("max_tokens 未生效: %d", got.MaxTokens)
	}
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
