package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

func TestKidApprovedContentPrefersAIFeedback(t *testing.T) {
	if got := kidApprovedContent(reviewVerdict{Pass: true, KidFeedback: "主人讲得很棒"}); got != "主人讲得很棒" {
		t.Fatalf("应优先用 AI 给孩子的话: %q", got)
	}
	// 没有 AI 评价（纯家长审核或解析失败）时也要有中性文案，不能发空留言。
	fallback := kidApprovedContent(reviewVerdict{Pass: true})
	if fallback == "" || !strings.Contains(fallback, "猫看完啦") {
		t.Fatalf("缺少中性兜底文案: %q", fallback)
	}
	notPass := kidApprovedContent(reviewVerdict{Pass: false})
	if notPass == fallback {
		t.Fatal("AI 未通过时的兜底文案应与通过时不同")
	}
}

func TestKidRejectedContentUsesTemplateNotLLM(t *testing.T) {
	withReason := kidRejectedContent("第二段的节奏")
	for _, want := range []string{"第二段的节奏", "不用重新计时"} {
		if !strings.Contains(withReason, want) {
			t.Fatalf("驳回话术缺少 %q: %q", want, withReason)
		}
	}
	// 家长没填理由也要有一条能看的鼓励话术。
	empty := kidRejectedContent("  ")
	if !strings.Contains(empty, "不用重新计时") || strings.Contains(empty, "妈妈想再看看  ，") {
		t.Fatalf("空理由兜底话术错误: %q", empty)
	}
	long := kidRejectedContent(strings.Repeat("很", 80))
	if len([]rune(long)) > 60 {
		t.Fatalf("家长理由应被截断: %q", long)
	}
}

// AI 通过 + 家长确认后，猫的留言必须带上 AI 给孩子的话、评分和关键点。
func TestKidMessageOnApprovalCarriesAIResult(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := json.Marshal(map[string]any{
			"pass": true, "score": 4, "reason": "情节准确",
			"kid_feedback": "主人讲得很棒", "highlights": []string{"发现暗道", "遇到管理员"},
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
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	// 结算需要宠物；留言与结算同事务，缺宠物时两者一起回滚。
	mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost,
		fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true}), http.StatusOK)

	var msg store.KidMessage
	if err := s.DB.Where("kid_id=?", kid.ID).Order("id DESC").First(&msg).Error; err != nil {
		t.Fatalf("未写入猫的留言: %v", err)
	}
	if msg.Kind != kidMessageApproved || msg.Content != "主人讲得很棒" || msg.Score != 4 {
		t.Fatalf("留言内容错误: %+v", msg)
	}
	if len(msg.Highlights) != 2 || msg.Highlights[0] != "发现暗道" {
		t.Fatalf("highlights 未带上: %+v", msg)
	}
	if msg.TaskName == "" || msg.ReadAt != nil {
		t.Fatalf("任务名缺失或已读状态错误: %+v", msg)
	}
}

// 驳回走模板话术：不能出现"AI 夸得很好、家长却驳回"的自相矛盾文案。
func TestKidMessageOnRejectionUsesEncouragingTemplate(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
		map[string]any{"duration_sec": 600, "evidence": photoEvidence()}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID),
		map[string]any{"approve": false, "reason": "第二段的节奏"}), http.StatusOK)

	var msg store.KidMessage
	if err := s.DB.Where("kid_id=?", kid.ID).Order("id DESC").First(&msg).Error; err != nil {
		t.Fatalf("驳回也应给孩子留言: %v", err)
	}
	if msg.Kind != kidMessageRejected || !strings.Contains(msg.Content, "第二段的节奏") {
		t.Fatalf("驳回话术错误: %+v", msg)
	}
	if len(msg.Highlights) != 0 || msg.Score != 0 {
		t.Fatalf("驳回不应带评分和关键点: %+v", msg)
	}
}

// 评分契约是 1-5，模型偶尔给越界值；前端显示 "score / 5 ★"，越界会很难看。
func TestKidMessageScoreIsClamped(t *testing.T) {
	cases := []struct {
		kind  string
		score int
		want  int
	}{
		{kidMessageApproved, 0, 0},   // 没评分就别显示星星
		{kidMessageApproved, 3, 3},   // 正常值原样保留
		{kidMessageApproved, 9, 5},   // 上限夹到 5
		{kidMessageApproved, -1, 0},  // 负数兜底
		{kidMessageRejected, 4, 0},   // 驳回一律不给评分
	}
	for _, c := range cases {
		if got := kidMessageScore(c.kind, c.score); got != c.want {
			t.Fatalf("%s/%d 期望 %d，实际 %d", c.kind, c.score, c.want, got)
		}
	}
}

// 信箱必须严格按孩子隔离，同一家庭的兄弟姐妹也不能互相看到。
func TestKidMessagesAreScopedAndReadStateIsPrivate(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	other := store.Kid{FamilyID: tpl.FamilyID, Name: "另一个孩子"}
	mustTaskCreate(t, s.DB, &other)
	if err := s.DB.Create(&store.KidMessage{KidID: other.ID, CompletionID: 999, Kind: kidMessageApproved,
		TaskName: "别人的任务", Content: "不该被看到"}).Error; err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
		map[string]any{"duration_sec": 600, "evidence": photoEvidence()}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID),
		map[string]any{"approve": true}), http.StatusOK)

	w := taskTestRequest(r, http.MethodGet, "/api/kid/messages", nil)
	requireTaskStatus(t, w, http.StatusOK)
	var listed struct {
		Items  []store.KidMessage `json:"items"`
		Unread int64              `json:"unread"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Unread != 1 || len(listed.Items) != 1 {
		t.Fatalf("应只看到自己的 1 条未读: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "不该被看到") {
		t.Fatal("看到了别人的留言")
	}

	// 标记已读只影响自己。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/kid/messages/read", map[string]any{}), http.StatusOK)
	w = taskTestRequest(r, http.MethodGet, "/api/kid/messages", nil)
	requireTaskStatus(t, w, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Unread != 0 {
		t.Fatalf("已读后未读数应为 0: %s", w.Body)
	}
	var othersUnread int64
	s.DB.Model(&store.KidMessage{}).Where("kid_id=? AND read_at IS NULL", other.ID).Count(&othersUnread)
	if othersUnread != 1 {
		t.Fatalf("不应把别人的留言也标记已读，实际未读 %d", othersUnread)
	}
}
