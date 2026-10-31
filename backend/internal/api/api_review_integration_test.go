package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

func aiTaskFixture(t *testing.T) (*Server, store.TaskTemplate, store.Kid, store.DailyTask) {
	t.Helper()
	s, tpl, kid, dt := taskFixture(t)
	_, snap := readDailyTask(t, s.DB, dt.ID)
	snap.ConfirmMode, snap.AnalysisMode = "bc", "score"
	b, _ := json.Marshal(snap)
	dt.Snapshot = string(b)
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", dt.Snapshot).Error; err != nil {
		t.Fatal(err)
	}
	return s, tpl, kid, dt
}

func submitAI(t *testing.T, s *Server, tpl store.TaskTemplate, kid store.Kid, dt store.DailyTask, evidence any) store.AIReviewJob {
	t.Helper()
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	w := taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
		map[string]any{"duration_sec": 1800, "evidence": evidence})
	requireTaskStatus(t, w, http.StatusOK)
	var response struct {
		Submitted    bool  `json:"submitted"`
		Queued       bool  `json:"ai_queued"`
		CompletionID int64 `json:"completion_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !response.Submitted || !response.Queued || response.CompletionID == 0 {
		t.Fatalf("异步提交契约错误: %s %v", w.Body, err)
	}
	var job store.AIReviewJob
	if err := s.DB.Where("completion_id=?", response.CompletionID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func photoEvidence() []map[string]string {
	return []map[string]string{{"type": "photo", "path": "/uploads/one.jpg"}}
}

func claimedAI(t *testing.T, s *Server) store.AIReviewJob {
	t.Helper()
	job, err := s.claimReviewJob(time.Minute)
	if err != nil || job == nil {
		t.Fatalf("领取 job 失败: %v", err)
	}
	return *job
}

func readAI(t *testing.T, s *Server, id int64) store.AIReviewJob {
	t.Helper()
	var job store.AIReviewJob
	if err := s.DB.First(&job, id).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func awaitAI(t *testing.T, s *Server, id int64, status string) store.AIReviewJob {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		job := readAI(t, s, id)
		if job.Status == status {
			return job
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("job 未进入 %s: %+v", status, job)
		}
	}
}

func respondVerdict(w http.ResponseWriter, pass bool) {
	v, _ := json.Marshal(reviewVerdict{Pass: pass, Score: 4, Reason: "已完整验收", KidFeedback: "完成得很好"})
	json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(v)}}}})
}

func TestAsyncSubmissionSlowAIReleasesTransactionAndExposesViews(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		respondVerdict(w, true)
	}))
	defer mock.Close()
	defer unblock()
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	ctx, cancel := context.WithCancel(context.Background())
	done := s.StartReviewWorker(ctx)
	defer func() { unblock(); cancel(); waitTaskSignal(t, done) }()
	before := time.Now()
	job := submitAI(t, s, tpl, kid, dt, photoEvidence())
	if time.Since(before) > time.Second {
		t.Fatal("提交仍然等待慢 AI")
	}
	waitTaskSignal(t, started)
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), taskEditBody("duration", 30))
	}()
	select {
	case w := <-result:
		requireTaskStatus(t, w, http.StatusOK)
	case <-time.After(time.Second):
		t.Fatal("慢 AI 持有了任务事务锁")
	}
	w := taskTestRequest(r, http.MethodGet, "/api/admin/confirm/list", nil)
	requireTaskStatus(t, w, http.StatusOK)
	var list struct {
		Items []confirmItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("确认列表错误: %s %v", w.Body, err)
	}
	item := list.Items[0]
	if item.Duration != 1800 || item.CompletionID != job.CompletionID || item.ID != dt.ID || item.ConfirmStatus != "pending" ||
		item.AIReview == nil || item.AIReview.Status != "running" || item.AIReview.Stage != "reviewing" {
		t.Fatalf("确认 API 字段错误: %+v", item)
	}
	w = taskTestRequest(r, http.MethodGet, "/api/tasks/today", nil)
	var today struct {
		Tasks []todayTaskView `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &today); err != nil || len(today.Tasks) != 1 || today.Tasks[0].LatestSubmission == nil ||
		today.Tasks[0].LatestSubmission.CompletionID != job.CompletionID || today.Tasks[0].AIReview.Status != "running" {
		t.Fatalf("孩子轮询契约错误: %s %v", w.Body, err)
	}
	unblock()
	awaitAI(t, s, job.ID, "succeeded")
	w = taskTestRequest(r, http.MethodGet, "/api/tasks/today", nil)
	json.Unmarshal(w.Body.Bytes(), &today)
	if today.Tasks[0].AIReview.Result == nil || !today.Tasks[0].AIReview.Result.Pass || today.Tasks[0].LatestSubmission.ReviewResult == "" {
		t.Fatalf("轮询没有完整 AI 结果: %s", w.Body)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", job.CompletionID), nil), http.StatusConflict)
}

func TestAIFailureConcurrentRetryIsIdempotentAndOldAttemptCannotWrite(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	job := submitAI(t, s, tpl, kid, dt, photoEvidence())
	old := claimedAI(t, s)
	// 未配置 AI 必须是明确 failed，不默默返回通过或当成已完成。
	if err := s.executeReviewJob(context.Background(), old, time.Minute); err != nil {
		t.Fatal(err)
	}
	failed := readAI(t, s, job.ID)
	if failed.Status != "failed" || failed.Stage != "preparing" || failed.Error == "" || failed.Attempt != 1 || failed.FinishedAt == nil {
		t.Fatalf("失败状态错误: %+v", failed)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	path := fmt.Sprintf("/api/admin/confirm/%d/ai-retry", job.CompletionID)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- taskTestRequest(r, http.MethodPost, path, nil) }()
	}
	close(start)
	for i := 0; i < 8; i++ {
		select {
		case w := <-results:
			requireTaskStatus(t, w, http.StatusOK)
		case <-time.After(5 * time.Second):
			t.Fatal("重试事务阻塞")
		}
	}
	var count int64
	s.DB.Model(&store.AIReviewJob{}).Where("completion_id=?", job.CompletionID).Count(&count)
	queued := readAI(t, s, job.ID)
	if count != 1 || queued.Status != "queued" || queued.Attempt != 1 {
		t.Fatalf("并发重试创建重复 job 或提前增加 attempt: %d %+v", count, queued)
	}
	current := claimedAI(t, s)
	if current.Attempt != 2 || current.LeaseToken == old.LeaseToken {
		t.Fatal("重试未更换 attempt/租约")
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, nil), http.StatusOK)
	if err := s.reportReview(old, "reviewing", `{"stale":{}}`); !errors.Is(err, errReviewLeaseLost) {
		t.Fatalf("旧 attempt 可写缓存: %v", err)
	}
	if err := s.finishReviewJob(old, reviewVerdict{Pass: false}, nil); !errors.Is(err, errReviewLeaseLost) {
		t.Fatalf("旧 attempt 可写结果: %v", err)
	}
	if err := s.finishReviewJob(current, reviewVerdict{Pass: true, Reason: "本次结果"}, nil); err != nil {
		t.Fatal(err)
	}
	var comp store.TaskCompletion
	s.DB.First(&comp, job.CompletionID)
	if comp.ConfirmStatus != "pending" || !strings.Contains(comp.ReviewResult, "本次结果") {
		t.Fatalf("当前结果被覆盖: %+v", comp)
	}
	foreign := taskTestRouter(s, tpl.FamilyID+1, kid.ID)
	requireTaskStatus(t, taskTestRequest(foreign, http.MethodPost, path, nil), http.StatusNotFound)
}

func TestAIAllTwentyPhotosAndFullRecordingCacheSurvivesRetry(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	longText := strings.Repeat("完整中文录音内容。", 400) + "最后一句必须保留"
	var submits, polls, fetches, reviews atomic.Int32
	var mockURL string
	prompts := make(chan string, 2)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/services/audio/asr/transcription"):
			submits.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_id": "saved-task"}})
		case r.URL.Path == "/tasks/saved-task":
			polls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]any{"task_status": "SUCCEEDED", "results": []map[string]string{{"transcription_url": mockURL + "/transcript"}}}})
		case r.URL.Path == "/transcript":
			fetches.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"transcripts": []map[string]string{{"text": longText}, {"text": "第二声道也必须保留"}}})
		case r.URL.Path == "/chat/completions":
			var body struct {
				Messages []struct {
					Content []struct {
						Type     string `json:"type"`
						Text     string `json:"text"`
						ImageURL struct {
							URL string `json:"url"`
						} `json:"image_url"`
					} `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 1 || len(body.Messages[0].Content) != 21 {
				t.Errorf("未发送全部 20 图: %+v %v", body, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for i, content := range body.Messages[0].Content[1:] {
				// 图片改走 768px 中等尺寸：原图太大，接入方反向下载会超时。
				if content.ImageURL.URL != reviewImageURL(fmt.Sprintf("/uploads/%02d.jpg", i)) {
					t.Errorf("图片原顺序丢失: %d %+v", i, content)
				}
			}
			prompts <- body.Messages[0].Content[0].Text
			if reviews.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "模拟暂时不可用"}})
				return
			}
			respondVerdict(w, true)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mock.Close()
	mockURL = mock.URL
	t.Setenv("MEOW_ASR_BASE_URL", mock.URL)
	t.Setenv("MEOW_PUBLIC_URL", "")
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	ev := make([]map[string]string, 0, 21)
	for i := 0; i < 20; i++ {
		ev = append(ev, map[string]string{"type": "photo", "path": fmt.Sprintf("/uploads/%02d.jpg", i)})
	}
	ev = append(ev, map[string]string{"type": "audio", "path": "/uploads/full.wav"})
	job := submitAI(t, s, tpl, kid, dt, ev)
	first := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), first, time.Minute); err != nil {
		t.Fatal(err)
	}
	if state := readAI(t, s, job.ID); state.Status != "failed" || !strings.Contains(state.ASRCache, "saved-task") || !strings.Contains(state.ASRCache, "最后一句必须保留") {
		t.Fatalf("失败时未保存转写缓存: %+v", state)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", job.CompletionID), nil), http.StatusOK)
	second := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if readAI(t, s, job.ID).Status != "succeeded" || submits.Load() != 1 || polls.Load() != 1 || fetches.Load() != 1 || reviews.Load() != 2 {
		t.Fatalf("重试重复付转写费用: submit=%d poll=%d fetch=%d review=%d", submits.Load(), polls.Load(), fetches.Load(), reviews.Load())
	}
	for i := 0; i < 2; i++ {
		prompt := <-prompts
		if !strings.Contains(prompt, longText) || !strings.Contains(prompt, "第二声道也必须保留") {
			t.Fatal("完整录音被截成 900 字节或漏掉声道")
		}
	}
}

func TestRequiredRecordingFailureDoesNotSilentlyReviewPhotos(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	var visionCalls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/transcription") {
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_id": "failed-audio"}})
		} else if strings.Contains(r.URL.Path, "/tasks/") {
			json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_status": "FAILED"}, "message": "录音无法解析"})
		} else {
			visionCalls.Add(1)
			respondVerdict(w, true)
		}
	}))
	defer mock.Close()
	t.Setenv("MEOW_ASR_BASE_URL", mock.URL)
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	job := submitAI(t, s, tpl, kid, dt, []map[string]string{{"type": "photo", "path": "/uploads/one.jpg"}, {"type": "audio", "path": "/uploads/broken.wav"}})
	attempt := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), attempt, time.Minute); err != nil {
		t.Fatal(err)
	}
	state := readAI(t, s, job.ID)
	var comp store.TaskCompletion
	s.DB.First(&comp, job.CompletionID)
	if state.Status != "failed" || state.Stage != "transcribing" || !strings.Contains(state.Error, "录音转写失败") ||
		visionCalls.Load() != 0 || comp.ReviewResult != "" || comp.ConfirmStatus != "pending" {
		t.Fatalf("录音失败被静默跳过: %+v %+v vision=%d", state, comp, visionCalls.Load())
	}
}

func TestParentReviewCancelsLateAIAndProtectsNewSubmission(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(fmt.Sprint(approve), func(t *testing.T) {
			s, tpl, kid, dt := aiTaskFixture(t)
			mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
			job := submitAI(t, s, tpl, kid, dt, photoEvidence())
			attempt := claimedAI(t, s)
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; respondVerdict(w, false) }))
			defer mock.Close()
			defer unblock()
			s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
			result := make(chan error, 1)
			go func() { result <- s.executeReviewJob(context.Background(), attempt, time.Minute) }()
			waitTaskSignal(t, started)
			r := taskTestRouter(s, tpl.FamilyID, kid.ID)
			path := fmt.Sprintf("/api/admin/confirm/%d", dt.ID)
			requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"approve": approve, "reason": "家长意见", "completion_id": job.CompletionID}), http.StatusOK)
			if readAI(t, s, job.ID).Status != "canceled" {
				t.Fatal("人审未明确取消 AI")
			}
			var next store.AIReviewJob
			if !approve {
				next = submitAI(t, s, tpl, kid, dt, photoEvidence())
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"approve": true, "completion_id": job.CompletionID}), http.StatusConflict)
			}
			unblock()
			select {
			case err := <-result:
				if !errors.Is(err, errReviewLeaseLost) {
					t.Fatalf("晚到结果没被租约拒绝: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("晚到 AI 调用未完成")
			}
			var comp store.TaskCompletion
			s.DB.First(&comp, job.CompletionID)
			want := "rejected"
			if approve {
				want = "approved"
			}
			if comp.ConfirmStatus != want || comp.ReviewResult != "" {
				t.Fatalf("人审被晚到结果覆盖: %+v", comp)
			}
			current, _ := readDailyTask(t, s.DB, dt.ID)
			if approve {
				if current.Status != "approved" {
					t.Fatal("人审通过被 AI 驳回")
				}
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"approve": true}), http.StatusBadRequest)
				var n int64
				s.DB.Model(&store.SettlementLedger{}).Where("daily_task_id=?", dt.ID).Count(&n)
				if n != 1 {
					t.Fatalf("重复奖励: %d", n)
				}
			} else if current.Status != "submitted" || readAI(t, s, next.ID).Status != "queued" {
				t.Fatal("新 completion 被旧 AI 覆盖")
			}
			requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", job.CompletionID), nil), http.StatusConflict)
			w := taskTestRequest(r, http.MethodGet, "/api/admin/confirm/history", nil)
			var history struct {
				Items []confirmItem `json:"items"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.Items[0].AIReview == nil || history.Items[0].AIReview.Status != "canceled" {
				t.Fatalf("提前人审的历史未显式 canceled: %s %v", w.Body, err)
			}
		})
	}
}

func TestExpiredLeaseRestartsAndConcurrentClaimsHaveOneOwner(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	job := submitAI(t, s, tpl, kid, dt, photoEvidence())
	start := make(chan struct{})
	claims := make(chan *store.AIReviewJob, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; j, err := s.claimReviewJob(time.Minute); claims <- j; errs <- err }()
	}
	close(start)
	var old store.AIReviewJob
	owners := 0
	for i := 0; i < 2; i++ {
		select {
		case j := <-claims:
			if j != nil {
				old = *j
				owners++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("并发领取未完成")
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if owners != 1 {
		t.Fatalf("同一个 queued job 被 %d 个 worker 领取", owners)
	}
	cache := `{"saved-audio":{"task_id":"preserved","done":true,"text":"已转写"}}`
	if err := s.reportReview(old, "reviewing", cache); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.AIReviewJob{}).Where("id=?", job.ID).Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { respondVerdict(w, true) }))
	defer mock.Close()
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	ctx, cancel := context.WithCancel(context.Background())
	done := s.StartReviewWorker(ctx)
	defer func() { cancel(); waitTaskSignal(t, done) }()
	recovered := awaitAI(t, s, job.ID, "succeeded")
	if recovered.Attempt != 2 || !strings.Contains(recovered.ASRCache, `"task_id":"preserved"`) || !strings.Contains(recovered.ASRCache, `"text":"已转写"`) || recovered.StartedAt == nil || recovered.FinishedAt == nil {
		t.Fatalf("重启恢复丢失 attempt 或缓存: %+v", recovered)
	}
	if err := s.finishReviewJob(old, reviewVerdict{Pass: false}, nil); !errors.Is(err, errReviewLeaseLost) {
		t.Fatalf("过期 attempt 覆盖恢复结果: %v", err)
	}
}

func TestHistoryOwnershipStablePaginationAndApprovedAIResult(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
	firstJob := submitAI(t, s, tpl, kid, dt, photoEvidence())
	first := claimedAI(t, s)
	if err := s.finishReviewJob(first, reviewVerdict{Pass: false, Reason: "首次 AI 退回"}, nil); err != nil {
		t.Fatal(err)
	}
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if current.Status != "pending" {
		t.Fatal("AI 拒绝没有让孩子重做")
	}
	secondJob := submitAI(t, s, tpl, kid, dt, photoEvidence())
	second := claimedAI(t, s)
	if err := s.finishReviewJob(second, reviewVerdict{Pass: true, Score: 5, Reason: "第二次 AI 评价"}, nil); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true, "completion_id": secondJob.CompletionID}), http.StatusOK)
	// 同时间戳必须靠 completion_id 稳定排序；编辑后的任务快照不能改历史 AI snapshot。
	sameTime := time.Now().Add(-time.Hour)
	if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Update("submitted_at", sameTime).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", `{"name":"已变更任务"}`).Error; err != nil {
		t.Fatal(err)
	}
	foreignKid := store.Kid{FamilyID: tpl.FamilyID + 1, Name: "其他家庭"}
	mustTaskCreate(t, s.DB, &foreignKid)
	foreignTask := store.DailyTask{KidID: foreignKid.ID, TemplateID: tpl.ID, Date: dt.Date, Snapshot: dt.Snapshot, Status: "approved"}
	mustTaskCreate(t, s.DB, &foreignTask)
	mustTaskCreate(t, s.DB, &store.TaskCompletion{DailyTaskID: foreignTask.ID, KidID: foreignKid.ID, ConfirmStatus: "approved", DurationSec: 9999})
	for page, expectedID := range []int64{secondJob.CompletionID, firstJob.CompletionID} {
		w := taskTestRequest(r, http.MethodGet, fmt.Sprintf("/api/admin/confirm/history?page=%d&page_size=1", page+1), nil)
		requireTaskStatus(t, w, http.StatusOK)
		var response struct {
			Items []confirmItem `json:"items"`
			Total int64         `json:"total"`
			Page  int           `json:"page"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Total != 2 || response.Page != page+1 || len(response.Items) != 1 {
			t.Fatalf("历史分页或权限错误: %s %v", w.Body, err)
		}
		item := response.Items[0]
		if item.CompletionID != expectedID || item.Duration != 1800 || item.AIReview == nil || item.AIReview.Status != "succeeded" || strings.Contains(item.Snapshot, "已变更任务") {
			t.Fatalf("历史丢失评价/30 分钟时长/快照: %+v", item)
		}
		if page == 0 && (item.ConfirmStatus != "approved" || item.AIReview.Result == nil || item.AIReview.Result.Reason != "第二次 AI 评价") {
			t.Fatalf("人工通过后丢失当时 AI 评价: %+v", item)
		}
	}
	for _, path := range []string{"/api/admin/confirm/history?page=0", "/api/admin/confirm/history?page_size=101", "/api/admin/confirm/history?page=bad"} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodGet, path, nil), http.StatusBadRequest)
	}
	foreign := taskTestRouter(s, tpl.FamilyID+100, kid.ID)
	w := taskTestRequest(foreign, http.MethodGet, "/api/admin/confirm/history", nil)
	if !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("跨家庭泄露历史: %s", w.Body)
	}
}

func TestLegacyAIEmptyResultRetryAndEarlyParentCancel(t *testing.T) {
	for _, action := range []string{"retry", "approve"} {
		t.Run(action, func(t *testing.T) {
			s, tpl, kid, dt := aiTaskFixture(t)
			if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "submitted").Error; err != nil {
				t.Fatal(err)
			}
			comp := store.TaskCompletion{DailyTaskID: dt.ID, KidID: kid.ID, Evidence: `[]`, DurationSec: 1800}
			mustTaskCreate(t, s.DB, &comp)
			r := taskTestRouter(s, tpl.FamilyID, kid.ID)
			if action == "retry" {
				path := fmt.Sprintf("/api/admin/confirm/%d/ai-retry", comp.ID)
				for i := 0; i < 2; i++ {
					requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, nil), http.StatusOK)
				}
			} else {
				mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true}), http.StatusOK)
			}
			var jobs []store.AIReviewJob
			if err := s.DB.Where("completion_id=?", comp.ID).Find(&jobs).Error; err != nil || len(jobs) != 1 {
				t.Fatalf("旧数据兼容创建重复 job: %+v %v", jobs, err)
			}
			want := "queued"
			if action == "approve" {
				want = "canceled"
			}
			if jobs[0].Status != want || jobs[0].Attempt != 0 {
				t.Fatalf("旧数据审核状态错误: %+v", jobs[0])
			}
		})
	}
}

func TestOldSubmissionGuardCancelsResultEvenIfLeaseStillValid(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	job := submitAI(t, s, tpl, kid, dt, photoEvidence())
	attempt := claimedAI(t, s)
	// 模拟旧数据迁移/另一提交入口未取消 job，最新 completion 校验仍必须生效。
	newer := store.TaskCompletion{DailyTaskID: dt.ID, KidID: kid.ID, DurationSec: 1801, Evidence: `[]`}
	mustTaskCreate(t, s.DB, &newer)
	if err := s.finishReviewJob(attempt, reviewVerdict{Pass: false, Reason: "旧提交 AI"}, nil); err != nil {
		t.Fatal(err)
	}
	var oldComp, newComp store.TaskCompletion
	s.DB.First(&oldComp, job.CompletionID)
	s.DB.First(&newComp, newer.ID)
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if readAI(t, s, job.ID).Status != "canceled" || oldComp.ReviewResult != "" || newComp.ReviewResult != "" ||
		newComp.ConfirmStatus != "pending" || current.Status != "submitted" {
		t.Fatalf("旧提交影响了新提交: %+v %+v %+v", oldComp, newComp, current)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", job.CompletionID), nil), http.StatusConflict)
}

func TestBackgroundReviewTimeoutIsFailedAndKeepsParentQueue(t *testing.T) {
	s, tpl, kid, dt := aiTaskFixture(t)
	release := make(chan struct{})
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer mock.Close()
	defer close(release)
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	job := submitAI(t, s, tpl, kid, dt, photoEvidence())
	attempt := claimedAI(t, s)
	if err := s.executeReviewJob(context.Background(), attempt, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	state := readAI(t, s, job.ID)
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if state.Status != "failed" || state.Stage != "reviewing" || !strings.Contains(state.Error, "context deadline exceeded") ||
		state.FinishedAt == nil || current.Status != "submitted" {
		t.Fatalf("后台超时丢状态或退出家长队列: %+v %+v", state, current)
	}
}

func TestLegacyHistoryAIViewsAndCancellation(t *testing.T) {
	now := time.Now()
	view := legacyReviewView(`{"confirm_mode":"bc"}`, `{"pass":true,"reason":"旧 AI 评价"}`, "approved", &now)
	if view == nil || view.Status != "succeeded" || view.Result == nil || view.Result.Reason != "旧 AI 评价" {
		t.Fatalf("旧 AI 评价丢失: %+v", view)
	}
	view = legacyReviewView(`{"confirm_mode":"bc"}`, "", "approved", &now)
	if view == nil || view.Status != "canceled" || view.Result != nil {
		t.Fatalf("旧提前人审没有 canceled: %+v", view)
	}
	view = legacyReviewView(`{"confirm_mode":"b"}`, "", "pending", nil)
	if view == nil || view.Status != "failed" || view.Stage != "legacy" || view.Error == "" {
		t.Fatalf("存量失败状态未展示: %+v", view)
	}
	if legacyReviewView(`{"confirm_mode":"c"}`, "", "approved", &now) != nil {
		t.Fatal("普通非 AI 任务被错误显示为 canceled")
	}
}

func TestAIReviewTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{"", 5 * time.Minute}, {"7m", 7 * time.Minute}, {"120ms", 120 * time.Millisecond}, {"-1s", 5 * time.Minute}, {"bad", 5 * time.Minute}} {
		t.Setenv("MEOW_AI_REVIEW_TIMEOUT", tc.value)
		if got := reviewTimeout(); got != tc.want {
			t.Fatalf("timeout %s: %v", tc.value, got)
		}
	}
}

// 保证测试路由返回的 ai_review 为对象/null，内部租约和 ASR URL 不进入响应。
func TestAIReviewViewDoesNotExposeLeaseOrASRCache(t *testing.T) {
	b, err := json.Marshal(gin.H{"ai_review": reviewView(store.AIReviewJob{Status: "running", LeaseToken: "private-lease", ASRCache: "private-url"})})
	if err != nil || strings.Contains(string(b), "private") || !strings.Contains(string(b), `"result":null`) {
		t.Fatalf("内部字段泄露: %s %v", b, err)
	}
}
