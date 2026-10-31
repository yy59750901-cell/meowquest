package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"meowquest/backend/internal/llm"
)

func parseVerdict(t *testing.T, raw string) reviewVerdict {
	t.Helper()
	var v reviewVerdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("job.result 不是合法 JSON: %v (%s)", err, raw)
	}
	return v
}

// highlights 必须能从模型输出一路落到 job.result，并在落库前完成条数与字数收口，
// 否则孩子端展示位会被超长或空白条目占满。
func TestReviewResultCarriesClippedHighlights(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := json.Marshal(map[string]any{
			"pass": true, "score": 4, "reason": "主要情节准确", "kid_feedback": "主人讲得很棒",
			"highlights": []string{"  发现暗道和红线柱  ", "", "  ", strings.Repeat("漏", 60), "遇到管理员", "第五", "第六"},
		})
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(v)}}}})
	}))
	defer mock.Close()
	s.Review = llm.Provider{Kind: llm.KindOpenAI, BaseURL: mock.URL, APIKey: "k", Model: "qwen3.5-flash"}

	submitAI(t, s, tpl, kid, dt, photoEvidence())
	job := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), job, time.Minute); err != nil {
		t.Fatalf("执行审核失败: %v", err)
	}
	got := readAI(t, s, job.ID)
	if got.Status != "succeeded" {
		t.Fatalf("job 未成功: %+v", got)
	}
	result := parseVerdict(t, got.Result)
	// 空白条目丢弃，超长条目按字符截断，总数封顶 5 条。
	want := []string{"发现暗道和红线柱", strings.Repeat("漏", maxHighlightRunes), "遇到管理员", "第五", "第六"}
	if len(result.Highlights) != len(want) {
		t.Fatalf("highlights 应为 %d 条，实际 %v", len(want), result.Highlights)
	}
	for i := range want {
		if result.Highlights[i] != want[i] {
			t.Fatalf("highlights[%d] 应为 %q，实际 %q", i, want[i], result.Highlights[i])
		}
	}
}

// 模型不给 highlights 或给了空数组都不能算失败，旧接入方/旧提示词同样要能跑。
func TestReviewResultToleratesMissingHighlights(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"content": `{"pass": true, "reason": "已完整验收", "kid_feedback": "完成得很好", "highlights": []}`}},
		}})
	}))
	defer mock.Close()
	s.Review = llm.Provider{Kind: llm.KindOpenAI, BaseURL: mock.URL, APIKey: "k", Model: "qwen3.5-flash"}

	submitAI(t, s, tpl, kid, dt, photoEvidence())
	job := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), job, time.Minute); err != nil {
		t.Fatalf("空 highlights 不应导致失败: %v", err)
	}
	got := readAI(t, s, job.ID)
	if got.Status != "succeeded" {
		t.Fatalf("job 未成功: %+v", got)
	}
	result := parseVerdict(t, got.Result)
	if len(result.Highlights) != 0 {
		t.Fatalf("空 highlights 应落库为空: %v", result.Highlights)
	}
	if result.Reason != "已完整验收" {
		t.Fatalf("reason 丢失: %+v", result)
	}
}
