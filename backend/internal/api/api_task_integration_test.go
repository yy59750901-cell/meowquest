package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

// 仅使用明确提供的测试 DSN；所有表位于新建的隔离 schema，不调用生产迁移。
func taskIntegrationDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("MEOWQUEST_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("未设置 MEOWQUEST_TEST_POSTGRES_DSN，跳过 PostgreSQL 集成测试")
	}
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true}
	admin, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminSQL.Close() })
	schema := fmt.Sprintf("meowquest_test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	gdb, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := gdb.AutoMigrate(&store.Family{}, &store.Kid{}, &store.TaskTemplate{}, &store.TaskSchedule{},
		&store.TaskAssign{}, &store.DailyTask{}, &store.TaskCompletion{}, &store.LeaveRecord{},
		&store.RewardPolicy{}, &store.Holiday{}, &store.Pet{}, &store.PetEvent{}, &store.SettlementLedger{}); err != nil {
		t.Fatal(err)
	}
	return &store.DB{DB: gdb}
}

func taskFixture(t *testing.T) (*Server, store.TaskTemplate, store.Kid, store.DailyTask) {
	t.Helper()
	db := taskIntegrationDB(t)
	fam := store.Family{Code: "123456", Name: "回归测试"}
	mustTaskCreate(t, db, &fam)
	kid := store.Kid{FamilyID: fam.ID, Name: "测试孩子"}
	mustTaskCreate(t, db, &kid)
	tpl := store.TaskTemplate{FamilyID: fam.ID, Name: "原任务", GoalType: "duration", GoalValue: 20,
		ConfirmMode: "c", EvidenceType: "photo", EvidenceSteps: `[{"kind":"photo","label":"原照片","required":true}]`,
		Enabled: 1, BaseScore: 20, AnalysisMode: "judge", ReviewPrompt: "原验收"}
	mustTaskCreate(t, db, &tpl)
	sch := store.TaskSchedule{TemplateID: tpl.ID, DateMode: "weekday_fixed", Weekdays: "1,2,3,4,5,6,7"}
	mustTaskCreate(t, db, &sch)
	tpl.Schedule = &sch
	if err := db.AssignTask(tpl.ID, kid.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(engine.TemplateSnapshot(tpl, sch))
	dt := store.DailyTask{Date: time.Now().Format("2006-01-02"), TemplateID: tpl.ID, KidID: kid.ID, Snapshot: string(b), Status: "pending"}
	mustTaskCreate(t, db, &dt)
	return &Server{DB: db}, tpl, kid, dt
}

func mustTaskCreate(t *testing.T, db *store.DB, row any) {
	t.Helper()
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func taskEditBody(goalType string, goal int) map[string]any {
	return map[string]any{"name": "新任务", "goal_type": goalType, "goal_value": goal, "confirm_mode": "c",
		"evidence_type": "photo,audio", "evidence_steps": `[{"kind":"audio","label":"","required":true},{"kind":"photo","required":false}]`,
		"review_prompt": "新验收", "analysis_mode": "score", "date_mode": "weekday_fixed", "weekdays": "1,2,3,4,5,6,7"}
}

func readDailyTask(t *testing.T, db *store.DB, id int64) (store.DailyTask, engine.Snapshot) {
	t.Helper()
	var dt store.DailyTask
	if err := db.First(&dt, id).Error; err != nil {
		t.Fatal(err)
	}
	var snap engine.Snapshot
	if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
		t.Fatal(err)
	}
	return dt, snap
}

func requireTaskStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("返回 %d，期望 %d：%s", w.Code, status, w.Body)
	}
}

func TestRejectedEditKeepsIDGoalAndRealDuration(t *testing.T) {
	s, tpl, kid, original := taskFixture(t)
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	path := fmt.Sprintf("/api/tasks/%d/submit", original.ID)
	confirm := fmt.Sprintf("/api/admin/confirm/%d", original.ID)
	edit := fmt.Sprintf("/api/admin/task/%d", tpl.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"duration_sec": 1377,
		"evidence": []map[string]string{{"type": "photo", "path": "/uploads/old.jpg"}}}), http.StatusOK)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, confirm, map[string]any{"approve": false, "reason": "补充材料"}), http.StatusOK)
	var first store.TaskCompletion
	if err := s.DB.Where("daily_task_id=?", original.ID).First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if first.ConfirmStatus != "rejected" {
		t.Fatalf("历史状态 %s", first.ConfirmStatus)
	}

	// 同模板的未达标、已提交、已通过和昨日实例分别验证刷新范围。
	statuses := []string{"pending", "submitted", "approved", "pending"}
	var others []store.DailyTask
	for i, status := range statuses {
		k := store.Kid{FamilyID: tpl.FamilyID, Name: fmt.Sprintf("孩子%d", i)}
		mustTaskCreate(t, s.DB, &k)
		if err := s.DB.AssignTask(tpl.ID, k.ID); err != nil {
			t.Fatal(err)
		}
		dt := store.DailyTask{Date: original.Date, KidID: k.ID, TemplateID: tpl.ID, Snapshot: original.Snapshot, Status: status}
		if i == 3 {
			dt.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		}
		mustTaskCreate(t, s.DB, &dt)
		mustTaskCreate(t, s.DB, &store.TaskCompletion{DailyTaskID: dt.ID, KidID: k.ID, DurationSec: 899, ConfirmStatus: "rejected"})
		others = append(others, dt)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPut, edit, taskEditBody("duration", 30)), http.StatusOK)
	dt, snap := readDailyTask(t, s.DB, original.ID)
	if dt.ID != original.ID || dt.Status != "pending" || snap.GoalType != "duration" || snap.GoalValue != 20 {
		t.Fatalf("原实例目标或 ID 被改：%+v %+v", dt, snap)
	}
	if snap.ReviewPrompt != "新验收" || snap.AnalysisMode != "score" {
		t.Fatalf("验收快照未刷新：%+v", snap)
	}
	steps, err := engine.ParseSteps(snap.EvidenceSteps)
	if err != nil || len(steps) != 2 || steps[0].Label != "说说完成情况" || steps[1].Required {
		t.Fatalf("材料快照错误：%+v %v", steps, err)
	}
	for i, old := range others {
		current, currentSnap := readDailyTask(t, s.DB, old.ID)
		if i == 0 {
			if currentSnap.GoalValue != 30 {
				t.Fatal("未达标目标没有更新")
			}
		} else if current.Snapshot != old.Snapshot || current.Status != old.Status {
			t.Fatalf("历史或已提交实例被修改：%+v", current)
		}
	}
	var history store.TaskCompletion
	if err := s.DB.First(&history, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if history.DailyTaskID != original.ID || history.DurationSec != 1377 || history.ConfirmStatus != "rejected" {
		t.Fatalf("历史流水变化：%+v", history)
	}
	if err := engine.Materialize(s.DB, time.Now().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var future store.DailyTask
	if err := s.DB.Where("template_id=? AND kid_id=? AND date=?", tpl.ID, kid.ID, time.Now().AddDate(0, 0, 1).Format("2006-01-02")).First(&future).Error; err != nil {
		t.Fatal(err)
	}
	_, futureSnap := readDailyTask(t, s.DB, future.ID)
	if futureSnap.GoalValue != 30 {
		t.Fatal("未来实例未使用新目标")
	}

	// 补交沿用真实历史最大值，新提交更长时更新，绝不以目标秒数兜底。
	for _, attempt := range []struct{ submitted, want int }{{0, 1377}, {1, 1377}, {1511, 1511}, {0, 1511}} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"duration_sec": attempt.submitted,
			"evidence": []map[string]string{{"type": "audio", "path": "/uploads/new.wav"}}}), http.StatusOK)
		var latest store.TaskCompletion
		if err := s.DB.Where("daily_task_id=?", original.ID).Order("id DESC").First(&latest).Error; err != nil {
			t.Fatal(err)
		}
		if latest.DurationSec != attempt.want {
			t.Fatalf("补交时长被伪造或丢失：%d", latest.DurationSec)
		}
		for _, state := range []string{"submitted", "pending"} {
			w := taskTestRequest(r, http.MethodGet, "/api/tasks/today", nil)
			requireTaskStatus(t, w, http.StatusOK)
			var response struct {
				Had      map[string]bool `json:"had_duration"`
				Duration map[string]int  `json:"achieved_duration"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprint(original.ID)
			if !response.Had[key] || response.Duration[key] != attempt.want {
				t.Fatalf("%s 达标字段丢失：%s", state, w.Body)
			}
			if otherKey := fmt.Sprint(others[0].ID); response.Had[otherKey] || response.Duration[otherKey] != 0 {
				t.Fatal("其他实例未达标历史被当成当前实例的达标时长")
			}
			if state == "submitted" {
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, confirm, map[string]any{"approve": false}), http.StatusOK)
			}
		}
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPut, edit, taskEditBody("free", 0)), http.StatusOK)
	_, snap = readDailyTask(t, s.DB, original.ID)
	if snap.GoalType != "duration" || snap.GoalValue != 20 {
		t.Fatal("历史达标实例的目标类型未冻结")
	}
}

func TestTaskEditRollbackAndOwnership(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	edit := fmt.Sprintf("/api/admin/task/%d", tpl.ID)
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", "{").Error; err != nil {
		t.Fatal(err)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPut, edit, taskEditBody("duration", 30)), http.StatusInternalServerError)
	var current store.TaskTemplate
	if err := s.DB.First(&current, tpl.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.GoalValue != 20 || current.Name != tpl.Name {
		t.Fatal("刷新失败没有回滚模板")
	}
	r = taskTestRouter(s, tpl.FamilyID+1, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPut, edit, taskEditBody("duration", 30)), http.StatusNotFound)
	r = taskTestRouter(s, tpl.FamilyID, kid.ID+1)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}}), http.StatusNotFound)
}

func TestConcurrentTaskEditAndSubmit(t *testing.T) {
	for _, first := range []string{"submit", "edit"} {
		t.Run(first, func(t *testing.T) {
			s, tpl, kid, original := taskFixture(t)
			r := taskTestRouter(s, tpl.FamilyID, kid.ID)
			locked, release, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var lockOnce, startOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			isSubmit := func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*store.DailyTask); return ok }
			isEdit := func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*[]store.DailyTask); return ok }
			if err := s.DB.Callback().Query().After("gorm:query").Register("test_hold_task_lock", func(tx *gorm.DB) {
				_, hasLock := tx.Statement.Clauses["FOR"]
				if hasLock && ((first == "submit" && isSubmit(tx)) || (first == "edit" && isEdit(tx))) {
					lockOnce.Do(func() { close(locked); <-release })
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Callback().Query().Before("gorm:query").Register("test_second_started", func(tx *gorm.DB) {
				if (first == "submit" && isEdit(tx)) || (first == "edit" && isSubmit(tx)) {
					startOnce.Do(func() { close(secondStarted) })
				}
			}); err != nil {
				t.Fatal(err)
			}
			editResult, submitResult := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			edit := func() {
				editResult <- taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), taskEditBody("duration", 30))
			}
			submit := func() {
				submitResult <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", original.ID), map[string]any{"duration_sec": 1377,
					"evidence": []map[string]string{{"type": "photo", "path": "/uploads/a.jpg"}}})
			}
			if first == "submit" {
				go submit()
			} else {
				go edit()
			}
			waitTaskSignal(t, locked)
			if first == "submit" {
				go edit()
			} else {
				go submit()
			}
			waitTaskSignal(t, secondStarted)
			unblock()
			var editW, submitW *httptest.ResponseRecorder
			select {
			case editW = <-editResult:
			case <-time.After(5 * time.Second):
				t.Fatal("编辑事务未结束")
			}
			select {
			case submitW = <-submitResult:
			case <-time.After(5 * time.Second):
				t.Fatal("提交事务未结束")
			}
			requireTaskStatus(t, editW, http.StatusOK)
			dt, snap := readDailyTask(t, s.DB, original.ID)
			if first == "submit" {
				requireTaskStatus(t, submitW, http.StatusOK)
				if dt.Status != "submitted" || dt.Snapshot != original.Snapshot {
					t.Fatal("提交先取得锁时快照被编辑改写")
				}
			} else {
				requireTaskStatus(t, submitW, http.StatusBadRequest)
				if dt.Status != "pending" || snap.GoalValue != 30 {
					t.Fatal("编辑先取得锁时提交未读取新快照")
				}
				var n int64
				if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatal("材料校验失败却创建了流水")
				}
			}
		})
	}
}

func TestAIRejectedEditKeepsDurationAndExplicitEmptyPrompt(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	_, snap := readDailyTask(t, s.DB, dt.ID)
	snap.ConfirmMode, snap.ReviewPrompt = "b", ""
	b, _ := json.Marshal(snap)
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", string(b)).Error; err != nil {
		t.Fatal(err)
	}
	prompts := make(chan string, 1)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		prompts <- string(body)
		w.Header().Set("Content-Type", "application/json")
		verdict, _ := json.Marshal(map[string]any{"pass": false, "reason": "补充材料"})
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(verdict)}}}})
	}))
	defer mock.Close()
	s.LLM = &llm.Client{BaseURL: mock.URL, APIKey: "test", Model: "mock"}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	w := taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
		map[string]any{"duration_sec": 1377, "evidence": []map[string]string{{"type": "photo", "path": "/uploads/a.jpg"}}})
	requireTaskStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `"redo":true`) {
		t.Fatalf("AI 未按快照进入驳回分支：%s", w.Body)
	}
	select {
	case prompt := <-prompts:
		if !strings.Contains(prompt, "提交的证据是否体现孩子认真完成了任务") || strings.Contains(prompt, "原验收") {
			t.Fatalf("显式空验收指令被模板偷偷恢复：%s", prompt)
		}
	default:
		t.Fatal("未调用本地模拟 AI")
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), taskEditBody("duration", 30)), http.StatusOK)
	current, currentSnap := readDailyTask(t, s.DB, dt.ID)
	if current.Status != "pending" || currentSnap.GoalValue != 20 || currentSnap.ReviewPrompt != "新验收" {
		t.Fatalf("AI 驳回后的快照不正确：%+v %+v", current, currentSnap)
	}
	var comp store.TaskCompletion
	if err := s.DB.Where("daily_task_id=?", dt.ID).First(&comp).Error; err != nil {
		t.Fatal(err)
	}
	if comp.DurationSec != 1377 || comp.ConfirmStatus != "rejected" {
		t.Fatalf("AI 驳回历史丢失：%+v", comp)
	}
}

func TestAutomaticSubmissionIsAtomicAndUsesSnapshotMode(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	_, snap := readDailyTask(t, s.DB, dt.ID)
	snap.ConfirmMode = "a"
	b, _ := json.Marshal(snap)
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", string(b)).Error; err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	path := fmt.Sprintf("/api/tasks/%d/submit", dt.ID)
	body := map[string]any{"duration_sec": 1377, "evidence": []map[string]string{{"type": "photo", "path": "/uploads/a.jpg"}}}
	// 无宠物导致结算失败，提交流水和状态必须一起回滚。
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, body), http.StatusInternalServerError)
	var n int64
	if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("结算失败仍保留提交流水")
	}
	mustTaskCreate(t, s.DB, &store.Pet{KidID: kid.ID, Name: "测试宠物"})
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, body), http.StatusOK)
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if current.Status != "approved" {
		t.Fatal("提交使用了模板而非快照的确认方式")
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, body), http.StatusBadRequest)
	if err := s.DB.Model(&store.SettlementLedger{}).Where("daily_task_id=?", dt.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("重复结算：%d", n)
	}
}

func TestConcurrentDuplicateSubmissionCreatesOneCompletion(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
				map[string]any{"duration_sec": 1377, "evidence": []map[string]string{{"type": "photo", "path": "/uploads/a.jpg"}}})
		}()
	}
	close(start)
	codes := map[int]int{}
	for i := 0; i < 2; i++ {
		select {
		case w := <-results:
			codes[w.Code]++
		case <-time.After(5 * time.Second):
			t.Fatal("重复提交事务未结束")
		}
	}
	if codes[http.StatusOK] != 1 || codes[http.StatusBadRequest] != 1 {
		t.Fatalf("重复提交响应：%v", codes)
	}
	var n int64
	if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("重复提交流水：%d", n)
	}
}

func TestEditOmittedStepsKeepsExplicitConfiguration(t *testing.T) {
	for _, raw := range []string{"[]", `[{"kind":"audio","label":"","required":false}]`, ""} {
		t.Run(raw, func(t *testing.T) {
			s, tpl, kid, dt := taskFixture(t)
			if err := s.DB.Model(&store.TaskTemplate{}).Where("id=?", tpl.ID).
				Updates(map[string]any{"evidence_steps": raw, "evidence_type": "photo,video"}).Error; err != nil {
				t.Fatal(err)
			}
			r := taskTestRouter(s, tpl.FamilyID, kid.ID)
			body := taskEditBody("duration", 25)
			delete(body, "evidence_steps")
			body["evidence_type"] = "photo,video"
			want := engine.NormalizeSteps(raw, "photo,video")
			for i := 0; i < 2; i++ {
				if i == 1 {
					delete(body, "evidence_type")
				}
				requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), body), http.StatusOK)
				var current store.TaskTemplate
				if err := s.DB.First(&current, tpl.ID).Error; err != nil {
					t.Fatal(err)
				}
				_, snap := readDailyTask(t, s.DB, dt.ID)
				if current.EvidenceSteps != want || snap.EvidenceSteps != want || current.EvidenceType != "photo,video" {
					t.Fatalf("遗漏字段覆盖了显式材料配置：%+v %+v", current, snap)
				}
			}
			if raw != "" {
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
					map[string]any{"duration_sec": 1500, "evidence": []any{}}), http.StatusOK)
			}
			// 显式清空始终优先于旧媒体字段，且不会因后续遗漏步骤字段而复活。
			body["evidence_steps"] = "[]"
			requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), body), http.StatusOK)
			delete(body, "evidence_steps")
			requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), body), http.StatusOK)
			var cleared store.TaskTemplate
			if err := s.DB.First(&cleared, tpl.ID).Error; err != nil || cleared.EvidenceSteps != "[]" {
				t.Fatalf("清空后恢复了旧媒体：%+v %v", cleared, err)
			}
		})
	}
}

func TestUnachievedHistoryDoesNotInventResubmissionDuration(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	mustTaskCreate(t, s.DB, &store.TaskCompletion{DailyTaskID: dt.ID, KidID: kid.ID, DurationSec: 1199, ConfirmStatus: "rejected"})
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	path := fmt.Sprintf("/api/tasks/%d/submit", dt.ID)
	for i := 0; i < 2; i++ {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path,
			map[string]any{"evidence": []map[string]string{{"type": "photo", "path": "/uploads/a.jpg"}}}), http.StatusOK)
		var latest store.TaskCompletion
		if err := s.DB.Where("daily_task_id=?", dt.ID).Order("id DESC").First(&latest).Error; err != nil || latest.DurationSec != 0 {
			t.Fatalf("未达标历史被目标兜底：%+v %v", latest, err)
		}
		w := taskTestRequest(r, http.MethodGet, "/api/tasks/today", nil)
		requireTaskStatus(t, w, http.StatusOK)
		var response struct {
			Had      map[string]bool `json:"had_duration"`
			Duration map[string]int  `json:"achieved_duration"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if key := fmt.Sprint(dt.ID); response.Had[key] || response.Duration[key] != 0 {
			t.Fatal("未达标历史返回了达标映射")
		}
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": false}), http.StatusOK)
	}
}

func TestInvalidSubmissionEvidenceCreatesNoCompletion(t *testing.T) {
	s, tpl, kid, dt := taskFixture(t)
	_, snap := readDailyTask(t, s.DB, dt.ID)
	snap.EvidenceSteps = `[{"kind":"photo","label":"","required":true}]`
	b, _ := json.Marshal(snap)
	if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", string(b)).Error; err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, tpl.FamilyID, kid.ID)
	for _, evidence := range []any{nil, map[string]any{}, []any{nil}, []any{1}, []any{},
		[]map[string]string{{"type": "photo", "path": " "}}} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID),
			map[string]any{"duration_sec": 1377, "evidence": evidence}), http.StatusBadRequest)
	}
	// 外层请求 JSON 损坏也不能绕过必填材料。
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), strings.NewReader(`{"evidence":[`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireTaskStatus(t, w, http.StatusBadRequest)
	var n int64
	if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("非法材料创建了流水：%d %v", n, err)
	}
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if current.Status != "pending" {
		t.Fatal("失败的提交改变了状态")
	}
}

func waitTaskSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("未观察到预期事务行锁")
	}
}
