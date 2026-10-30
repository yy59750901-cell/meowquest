package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMultiImageFailureReportsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"InvalidParameter","message":"media download failed test-secret"}}`))
	}))
	defer server.Close()
	_, err := LLMVisionMulti(context.Background(), server.URL, "test-secret", "mock", "review", []string{"https://example.com/photo.jpg"})
	if err == nil || !strings.Contains(err.Error(), "InvalidParameter") || !strings.Contains(err.Error(), "media download failed") || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("错误描述缺失或泄漏认证信息: %v", err)
	}
}
