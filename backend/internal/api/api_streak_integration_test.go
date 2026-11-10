package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

func streakFixture(t *testing.T, base int64, legacy bool) (*Server, store.Kid, time.Time) {
	t.Helper()
	db := taskIntegrationDB(t)
	fam := store.Family{Code: "112233", Name: "连击测试"}
	mustTaskCreate(t, db, &fam)
	kid := store.Kid{FamilyID: fam.ID, Name: "测试孩子"}
	mustTaskCreate(t, db, &kid)
	now, _ := time.ParseInLocation("2006-01-02 15:04", engine.BusinessDate(time.Now())+" 12:00", engine.BusinessLocation)
	pet := store.Pet{KidID: kid.ID, Name: "测试猫", Streak: base, StreakBaseValue: base, Balance: 100, EarnedTotal: 100}
	// 结算会在发奖前按结算时钟补齐衰减；基线对齐到 fixture 时钟，用例不受本机当前时刻影响。
	pet.LastTick = &now
	if !legacy {
		pet.StreakBaseDate = engine.BusinessDate(now)
	}
	mustTaskCreate(t, db, &pet)
	return &Server{DB: db, Now: func() time.Time { return now }}, kid, now
}

func streakTask(t *testing.T, s *Server, kid store.Kid, date time.Time, enabled int, mode, dateMode, status string) store.DailyTask {
	t.Helper()
	tpl := store.TaskTemplate{FamilyID: kid.FamilyID, Name: "日任务", BaseScore: 10, ConfirmMode: mode, EvidenceType: "none", EvidenceSteps: "[]", CreatedAt: date.AddDate(0, 0, -10)}
	mustTaskCreate(t, s.DB, &tpl)
	sch := store.TaskSchedule{TemplateID: tpl.ID, DateMode: dateMode, Weekdays: "1,2,3,4,5,6,7", WeeklyQuota: 7}
	mustTaskCreate(t, s.DB, &sch)
	snap := engine.TemplateSnapshot(tpl, sch)
	snap.StreakEnabled = &enabled
	snap.DateMultiplier = 1
	b, _ := json.Marshal(snap)
	dt := store.DailyTask{KidID: kid.ID, TemplateID: tpl.ID, Date: engine.BusinessDate(date), Snapshot: string(b), Status: status}
	mustTaskCreate(t, s.DB, &dt)
	if status == "submitted" {
		mustTaskCreate(t, s.DB, &store.TaskCompletion{KidID: kid.ID, DailyTaskID: dt.ID, Evidence: "[]", SubmittedAt: date})
	}
	return dt
}

func settleStreak(t *testing.T, s *Server, dt store.DailyTask, now time.Time) bool {
	t.Helper()
	settled := false
	err := s.DB.TaskTransaction(func(tx *gorm.DB) error {
		db := &store.DB{DB: tx}
		if err := engine.LockKid(db, dt.KidID); err != nil {
			return err
		}
		var err error
		settled, err = engine.SettleAt(db, dt.ID, 0, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

func streakPet(t *testing.T, s *Server, kid store.Kid) store.Pet {
	t.Helper()
	var p store.Pet
	if err := s.DB.Where("kid_id=?", kid.ID).First(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

func streakDay(t *testing.T, s *Server, kid store.Kid, date time.Time, state string, after *int64) {
	t.Helper()
	var day store.KidStreakDay
	if err := s.DB.Where("kid_id=? AND date=?", kid.ID, engine.BusinessDate(date)).First(&day).Error; err != nil {
		t.Fatal(err)
	}
	if day.State != state || (day.StreakAfter == nil) != (after == nil) || (after != nil && *after != *day.StreakAfter) {
		t.Fatalf("日状态错误: %+v，期望 %s/%v", day, state, after)
	}
}

func streakLedger(t *testing.T, s *Server, id int64) store.SettlementLedger {
	t.Helper()
	var led store.SettlementLedger
	if err := s.DB.Where("daily_task_id=? AND source='task'", id).First(&led).Error; err != nil {
		t.Fatal(err)
	}
	return led
}

func TestStreakSameDayThreeTasksNonStreakDoesNotClaimSlot(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
	tasks := []store.DailyTask{}
	for i := 0; i < 3; i++ {
		tasks = append(tasks, streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending"))
	}
	if settleStreak(t, s, non, now) {
		t.Fatal("非连击任务必须等待本任务日结果")
	}
	current, _ := readDailyTask(t, s.DB, non.ID)
	if current.Status != "approved" || current.SettlementStatus != "pending" || current.SettlementWaitDate != non.Date {
		t.Fatalf("本日等待字段错误: %+v", current)
	}
	if p := streakPet(t, s, kid); p.Streak != 2 {
		t.Fatal("非连击抢了日名额")
	}
	if p := streakPet(t, s, kid); p.Balance != 100 {
		t.Fatal("本日未确定却提前发奖")
	}
	for _, dt := range tasks {
		if !settleStreak(t, s, dt, now) {
			t.Fatal("同日成功不应等待剩余任务审核")
		}
	}
	want := int64(3)
	streakDay(t, s, kid, now, "success", &want)
	if p := streakPet(t, s, kid); p.Streak != 3 || p.Balance != 152 {
		t.Fatalf("连击或奖励不正确: %+v", p)
	}
	for _, dt := range append(tasks, non) {
		if led := streakLedger(t, s, dt.ID); led.StreakMultiplier != 1.3 {
			t.Fatalf("同日倍率错误: %+v", led)
		}
	}
	for _, dt := range append(tasks, non) {
		settleStreak(t, s, dt, now)
	}
	if p := streakPet(t, s, kid); p.Balance != 152 || p.Streak != 3 {
		t.Fatal("重复结算改变了余额/连击")
	}
	var n int64
	s.DB.Model(&store.SettlementLedger{}).Count(&n)
	if n != 4 {
		t.Fatalf("重复流水 %d", n)
	}
}

func TestStreakTodayApprovedWaitsForYesterdayAndViewsMatchContract(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	yesterday := now.AddDate(0, 0, -1)
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"streak_base_date": engine.BusinessDate(yesterday)}); err != nil {
		t.Fatal(err)
	}
	old := streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted")
	today := streakTask(t, s, kid, now, 1, "c", "weekday_fixed", "submitted")
	non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	w := taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", today.ID), map[string]any{"approve": true})
	requireTaskStatus(t, w, http.StatusOK)
	var response struct {
		Settled bool `json:"settled"`
		Pending bool `json:"settlement_pending"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Settled || !response.Pending {
		t.Fatalf("确认响应错误: %s", w.Body)
	}
	w = taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", non.ID), map[string]any{"evidence": []any{}})
	requireTaskStatus(t, w, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Settled || !response.Pending {
		t.Fatalf("提交响应错误: %s", w.Body)
	}
	current, _ := readDailyTask(t, s.DB, today.ID)
	if current.Status != "approved" || current.SettlementStatus != "pending" || current.SettlementWaitDate != old.Date {
		t.Fatalf("审批与到账未分离: %+v", current)
	}
	if p := streakPet(t, s, kid); p.Balance != 100 || p.Streak != 2 {
		t.Fatal("前置未确定时已发奖")
	}
	var messages int64
	s.DB.Model(&store.KidMessage{}).Count(&messages)
	if messages != 1 {
		t.Fatal("家长确认时没有写猫留言")
	}
	var completion store.TaskCompletion
	s.DB.Where("daily_task_id=?", today.ID).First(&completion)
	if completion.ConfirmStatus != "approved" || completion.ConfirmedAt == nil {
		t.Fatal("待结算影响了审批历史")
	}
	w = taskTestRequest(r, http.MethodGet, "/api/tasks/today", nil)
	if !containsJSONField(w, "settlement_status", "pending") || !containsJSONField(w, "settlement_wait_date", old.Date) {
		t.Fatalf("今日任务未暴露等待字段: %s", w.Body)
	}
	w = taskTestRequest(r, http.MethodGet, "/api/admin/confirm/history", nil)
	if !containsJSONField(w, "settlement_status", "pending") {
		t.Fatalf("历史未暴露等待字段: %s", w.Body)
	}
	w = taskTestRequest(r, http.MethodGet, "/api/pet", nil)
	requireTaskStatus(t, w, http.StatusOK)
	var petResponse struct {
		Pending bool `json:"streak_pending"`
		Count   int  `json:"pending_reward_count"`
	}
	json.Unmarshal(w.Body.Bytes(), &petResponse)
	if !petResponse.Pending || petResponse.Count != 2 {
		t.Fatalf("宠物待结算计数错误: %s", w.Body)
	}
	w = taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", old.ID), map[string]any{"approve": true})
	requireTaskStatus(t, w, http.StatusOK)
	for _, dt := range []store.DailyTask{old, today, non} {
		current, _ := readDailyTask(t, s.DB, dt.ID)
		if current.SettlementStatus != "settled" || current.SettlementWaitDate != "" {
			t.Fatalf("等待没有解锁: %+v", current)
		}
	}
	if streakLedger(t, s, old.ID).StreakMultiplier != 1.3 || streakLedger(t, s, today.ID).StreakMultiplier != 1.4 || streakLedger(t, s, non.ID).StreakMultiplier != 1.4 {
		t.Fatal("跨日最终倍率错误")
	}
	want := int64(4)
	streakDay(t, s, kid, now, "success", &want)
	if err := engine.Reconcile(s.DB, now); err != nil {
		t.Fatal(err)
	}
	if p := streakPet(t, s, kid); p.Balance != 141 || p.Streak != 4 {
		t.Fatalf("解锁重复支付: %+v", p)
	}
}

func containsJSONField(w *httptest.ResponseRecorder, key, value string) bool {
	var body any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		return false
	}
	var has func(any) bool
	has = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if x[key] == value {
				return true
			}
			for _, v := range x {
				if has(v) {
					return true
				}
			}
		case []any:
			for _, v := range x {
				if has(v) {
					return true
				}
			}
		}
		return false
	}
	return has(body)
}

func TestStreakRejectedOrExpiredYesterdayUnlocksAndResetsMultiplier(t *testing.T) {
	for _, action := range []string{"reject", "timeout", "ai_reject"} {
		t.Run(action, func(t *testing.T) {
			s, kid, now := streakFixture(t, 4, false)
			yesterday := now.AddDate(0, 0, -1)
			if err := s.DB.UpdatePet(kid.ID, map[string]any{"streak_base_date": engine.BusinessDate(yesterday)}); err != nil {
				t.Fatal(err)
			}
			old := streakTask(t, s, kid, yesterday, 1, "bc", "weekday_fixed", "submitted")
			today := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
			if settleStreak(t, s, today, now) {
				t.Fatal("昨天待审未阻塞今天")
			}
			if action == "timeout" {
				// 审核截止按任务日+2零点，而不是 submitted_at+48h。
				s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", old.ID).Update("submitted_at", now.Add(-time.Minute))
				var comp store.TaskCompletion
				s.DB.Where("daily_task_id=?", old.ID).First(&comp)
				mustTaskCreate(t, s.DB, &store.AIReviewJob{KidID: kid.ID, DailyTaskID: old.ID, CompletionID: comp.ID, Snapshot: old.Snapshot, Status: "running", Stage: "reviewing"})
				day, _ := time.ParseInLocation("2006-01-02", today.Date, engine.BusinessLocation)
				if err := engine.Reconcile(s.DB, day.AddDate(0, 0, 1).Add(-time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
				current, _ := readDailyTask(t, s.DB, today.ID)
				if current.SettlementStatus != "pending" {
					t.Fatal("宽限结束前提早结算")
				}
				if err := engine.Reconcile(s.DB, day.AddDate(0, 0, 1)); err != nil {
					t.Fatal(err)
				}
				s.DB.First(&comp, comp.ID)
				if comp.ConfirmStatus != "expired" {
					t.Fatal("超时completion历史未终结")
				}
				var job store.AIReviewJob
				s.DB.Where("daily_task_id=?", old.ID).First(&job)
				if job.Status != "canceled" || job.Stage != "expired" {
					t.Fatalf("超时job仍可运行: %+v", job)
				}
			} else if action == "reject" {
				r := taskTestRouter(s, kid.FamilyID, kid.ID)
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", old.ID), map[string]any{"approve": false, "reason": "未完成"}), http.StatusOK)
			} else {
				var comp store.TaskCompletion
				s.DB.Where("daily_task_id=?", old.ID).First(&comp)
				job := store.AIReviewJob{KidID: kid.ID, DailyTaskID: old.ID, CompletionID: comp.ID, Snapshot: old.Snapshot, Status: "queued", Stage: "queued"}
				mustTaskCreate(t, s.DB, &job)
				claimed := claimedAI(t, s)
				if err := s.finishReviewJob(claimed, reviewVerdict{Pass: false, Reason: "未完成"}, nil); err != nil {
					t.Fatal(err)
				}
			}
			current, _ := readDailyTask(t, s.DB, old.ID)
			if current.Status == "pending" || current.Status == "submitted" {
				t.Fatalf("截止后拒绝/超时仍可重交: %+v", current)
			}
			want := int64(0)
			streakDay(t, s, kid, yesterday, "missed", &want)
			current, _ = readDailyTask(t, s.DB, today.ID)
			if current.Status != "approved" || current.SettlementStatus != "settled" {
				t.Fatalf("等待没有解锁: %+v", current)
			}
			if led := streakLedger(t, s, today.ID); led.StreakMultiplier != 1.1 {
				t.Fatalf("漏做后倍率未重置: %+v", led)
			}
			if p := streakPet(t, s, kid); p.Streak != 1 {
				t.Fatalf("缓存错误: %+v", p)
			}
		})
	}
}

func TestStreakRestLeaveAndWeeklyQuotaNeutral(t *testing.T) {
	for _, kind := range []string{"rest", "leave", "weekly", "weekly_success", "leave_with_approved"} {
		t.Run(kind, func(t *testing.T) {
			s, kid, now := streakFixture(t, 3, false)
			yesterday := now.AddDate(0, 0, -1)
			if err := s.DB.UpdatePet(kid.ID, map[string]any{"streak_base_date": engine.BusinessDate(yesterday)}); err != nil {
				t.Fatal(err)
			}
			want := int64(3)
			switch kind {
			case "leave":
				old := streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted")
				today := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
				if settleStreak(t, s, today, now) {
					t.Fatal("昨日待审应阻塞")
				}
				r := taskTestRouter(s, kid.FamilyID, kid.ID)
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/leave", map[string]any{"date": old.Date, "kid_id": kid.ID, "reason": "整日休息"}), http.StatusOK)
				current, _ := readDailyTask(t, s.DB, old.ID)
				if current.Status != "skipped" {
					t.Fatal("请假仍有未终结提交")
				}
				var comp store.TaskCompletion
				s.DB.Where("daily_task_id=?", old.ID).First(&comp)
				if comp.ConfirmStatus != "skipped" {
					t.Fatal("请假未同步提交历史")
				}
				if streakLedger(t, s, today.ID).StreakMultiplier != 1.4 {
					t.Fatal("请假未自动解除等待")
				}
			case "weekly":
				streakTask(t, s, kid, yesterday, 1, "a", "weekly_quota", "pending")
			case "weekly_success":
				dt := streakTask(t, s, kid, now, 1, "a", "weekly_quota", "pending")
				if !settleStreak(t, s, dt, now) {
					t.Fatal("周次数完成未成功")
				}
			case "leave_with_approved":
				approved := streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted")
				settleStreak(t, s, approved, now)
				mustTaskCreate(t, s.DB, &store.LeaveRecord{KidID: kid.ID, Date: approved.Date, Reason: "已有完成"})
				want = 4
			}
			if err := engine.Reconcile(s.DB, now); err != nil {
				t.Fatal(err)
			}
			state := "neutral"
			if kind == "leave_with_approved" {
				state = "success"
			}
			streakDay(t, s, kid, yesterday, state, &want)
			if kind == "weekly_success" {
				want = 4
				streakDay(t, s, kid, now, "success", &want)
			}
		})
	}
}

func TestStreakConcurrentTasksAndReconcileAreIdempotent(t *testing.T) {
	s, kid, now := streakFixture(t, 1, false)
	tasks := []store.DailyTask{}
	for i := 0; i < 8; i++ {
		tasks = append(tasks, streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending"))
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 16)
	var wg sync.WaitGroup
	for _, dt := range tasks {
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(dt store.DailyTask) {
				defer wg.Done()
				<-start
				results <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}})
			}(dt)
		}
	}
	reconciled := make(chan error, 1)
	go func() { <-start; reconciled <- engine.Reconcile(s.DB, now) }()
	close(start)
	wg.Wait()
	close(results)
	codes := map[int]int{}
	for w := range results {
		codes[w.Code]++
		if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
			t.Fatalf("并发失败: %s", w.Body)
		}
	}
	if err := <-reconciled; err != nil {
		t.Fatal(err)
	}
	if codes[http.StatusOK] != 8 || codes[http.StatusBadRequest] != 8 {
		t.Fatalf("并发重复提交错误: %v", codes)
	}
	var count int64
	s.DB.Model(&store.SettlementLedger{}).Count(&count)
	if count != 8 {
		t.Fatalf("并发重复支付: %d", count)
	}
	if p := streakPet(t, s, kid); p.Streak != 2 || p.Balance != 196 {
		t.Fatalf("并发连击/余额错误: %+v", p)
	}
}

func TestStreakLegacyBaselineKeepsHistoryAndCountsOpenYesterdayOnce(t *testing.T) {
	for _, paidToday := range []bool{false, true} {
		t.Run(fmt.Sprint(paidToday), func(t *testing.T) {
			s, kid, now := streakFixture(t, 4, true)
			ancient := streakTask(t, s, kid, now.AddDate(0, 0, -20), 1, "c", "weekday_fixed", "submitted")
			oldApproved := streakTask(t, s, kid, now.AddDate(0, 0, -3), 1, "c", "weekday_fixed", "approved")
			id := oldApproved.ID
			historical := store.SettlementLedger{KidID: kid.ID, Date: oldApproved.Date, DailyTaskID: &id, Source: "task", Base: 987, Final: 777, DeltaBalance: 777, DeltaEarned: 777, DateMultiplier: 3, StreakMultiplier: 1.5}
			mustTaskCreate(t, s.DB, &historical)
			// NULL 和同task的非task流水均保持原样且不阻止 task 正常结算。
			mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, Source: "wish_refund", Final: 5})
			olds := []store.DailyTask{streakTask(t, s, kid, now.AddDate(0, 0, -1), 1, "c", "weekday_fixed", "submitted"), streakTask(t, s, kid, now.AddDate(0, 0, -1), 1, "c", "weekday_fixed", "submitted")}
			if paidToday {
				dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "approved")
				id := dt.ID
				mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, DailyTaskID: &id, Date: dt.Date, Source: "task", Final: 123})
			}
			if err := engine.Reconcile(s.DB, now); err != nil {
				t.Fatal(err)
			}
			p := streakPet(t, s, kid)
			if p.Streak != 4 || p.Balance != 100 || p.StreakBaseDate != olds[0].Date {
				t.Fatalf("初始化重置/重付: %+v", p)
			}
			for _, dt := range olds {
				settleStreak(t, s, dt, now)
			}
			want := int64(5)
			balance, oldAfter, final := int64(130), int64(5), 15
			if paidToday {
				balance, oldAfter, final = 128, 4, 14
			}
			p = streakPet(t, s, kid)
			if p.Streak != want || p.Balance != balance {
				t.Fatalf("旧日审批信用日期/付款错误: %+v", p)
			}
			streakDay(t, s, kid, now.AddDate(0, 0, -1), "success", &oldAfter)
			for _, dt := range olds {
				led := streakLedger(t, s, dt.ID)
				if led.Final != final || led.StreakMultiplier != 1+0.1*float64(oldAfter) {
					t.Fatalf("昨日吃了今日信用: %+v", led)
				}
			}
			if paidToday {
				streakDay(t, s, kid, now, "success", &want)
				if p.StreakBaseValue != 3 {
					t.Fatalf("before没有扣除今日信用: %+v", p)
				}
			}
			current, _ := readDailyTask(t, s.DB, ancient.ID)
			if current.Status != "expired" {
				t.Fatal("legacy前置待审永久阻塞")
			}
			var days int64
			s.DB.Model(&store.KidStreakDay{}).Where("date<?", olds[0].Date).Count(&days)
			if days != 0 {
				t.Fatal("无限回放旧日")
			}
			preserved := streakLedger(t, s, oldApproved.ID)
			if preserved.ID != historical.ID || preserved.Base != 987 || preserved.Final != 777 || preserved.StreakMultiplier != 1.5 {
				t.Fatalf("历史流水被重算: %+v", preserved)
			}
			current, _ = readDailyTask(t, s.DB, oldApproved.ID)
			if current.Status != "approved" || current.SettlementStatus != "" {
				t.Fatal("历史通过任务被撤销或补付")
			}
		})
	}
}

func TestStreakLegacyPaidSwitchDayDoesNotIncrementAgain(t *testing.T) {
	for _, hasLedger := range []bool{false, true} {
		t.Run(fmt.Sprint(hasLedger), func(t *testing.T) {
			s, kid, now := streakFixture(t, 3, true)
			old := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "approved")
			if hasLedger {
				id := old.ID
				mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, DailyTaskID: &id, Date: old.Date, Source: "task", Final: 999})
			}
			current := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
			settleStreak(t, s, current, now)
			if p := streakPet(t, s, kid); p.Streak != 3 || p.Balance != 113 {
				t.Fatalf("切换日再+1或补发历史: %+v", p)
			}
			next := now.AddDate(0, 0, 1)
			dt := streakTask(t, s, kid, next, 1, "a", "weekday_fixed", "pending")
			settleStreak(t, s, dt, next)
			if p := streakPet(t, s, kid); p.Streak != 4 {
				t.Fatal("基线抑制了后续正常连击")
			}
		})
	}
}

func TestStreakStartupCatchupMissingDaysAndMidnightDeadlines(t *testing.T) {
	s, kid, now := streakFixture(t, 3, false)
	start := now.AddDate(0, 0, -4)
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"streak_base_date": engine.BusinessDate(start)}); err != nil {
		t.Fatal(err)
	}
	tpl := store.TaskTemplate{FamilyID: kid.FamilyID, Name: "补跑固定任务", BaseScore: 10, CreatedAt: start.AddDate(0, 0, -1)}
	mustTaskCreate(t, s.DB, &tpl)
	sch := store.TaskSchedule{TemplateID: tpl.ID, DateMode: "weekday_fixed", Weekdays: "1,2,3,4,5,6,7"}
	mustTaskCreate(t, s.DB, &sch)
	if err := s.DB.AssignTask(tpl.ID, kid.ID); err != nil {
		t.Fatal(err)
	}
	old := streakTask(t, s, kid, start, 1, "c", "weekday_fixed", "submitted")
	if err := engine.Reconcile(s.DB, now); err != nil {
		t.Fatal(err)
	}
	var count int64
	s.DB.Model(&store.KidStreakDay{}).Where("kid_id=?", kid.ID).Count(&count)
	if count != 5 {
		t.Fatalf("遗漏缺日: %d", count)
	}
	want := int64(0)
	for d := start; engine.BusinessDate(d) < engine.BusinessDate(now); d = d.AddDate(0, 0, 1) {
		streakDay(t, s, kid, d, "missed", &want)
	}
	current, _ := readDailyTask(t, s.DB, old.ID)
	if current.Status != "expired" {
		t.Fatal("只扫了昨日，遗漏旧待审")
	}
	if err := engine.Reconcile(s.DB, now); err != nil {
		t.Fatal(err)
	}
	s.DB.Model(&store.KidStreakDay{}).Count(&count)
	if count != 5 {
		t.Fatal("补跑重复日记录")
	}
	day, _ := time.ParseInLocation("2006-01-02", engine.BusinessDate(now), engine.BusinessLocation)
	if !engine.SubmissionOpen(engine.BusinessDate(now), day.AddDate(0, 0, 1).Add(-time.Nanosecond)) || engine.SubmissionOpen(engine.BusinessDate(now), day.AddDate(0, 0, 1)) {
		t.Fatal("提交午夜边界错误")
	}
	if !engine.ReviewOpen(engine.BusinessDate(now), day.AddDate(0, 0, 2).Add(-time.Nanosecond)) || engine.ReviewOpen(engine.BusinessDate(now), day.AddDate(0, 0, 2)) {
		t.Fatal("审核午夜边界错误")
	}
	utc := day.AddDate(0, 0, 1).UTC()
	if engine.BusinessDate(utc) != engine.BusinessDate(day.AddDate(0, 0, 1)) {
		t.Fatal("北京时间日期依赖服务器时区")
	}
}

func TestStreakSnapshotMultiplierEligibilityAndExtraRewardShared(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	var submitted []store.DailyTask
	for _, mode := range []string{"a", "c"} {
		dt := streakTask(t, s, kid, now, 0, mode, "weekday_fixed", "pending")
		_, snap := readDailyTask(t, s.DB, dt.ID)
		snap.ExtraMinutes = 5
		snap.ExtraStars = 3
		snap.ExtraCap = 5
		snap.DateMultiplier = 2
		b, _ := json.Marshal(snap)
		s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", string(b))
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"extra_sec": 601, "evidence": []any{}}), http.StatusOK)
		// 提交后更改策略和模板，审批必须使用已提交的日期倍数及资格。
		s.DB.Model(&store.TaskTemplate{}).Where("id=?", dt.TemplateID).Update("streak_enabled", 1)
		mustTaskCreate(t, s.DB, &store.RewardPolicy{MatchType: "date", MatchValue: dt.Date, Multiplier: 8})
		if mode == "c" {
			requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true}), http.StatusOK)
		}
		submitted = append(submitted, dt)
		current, _ := readDailyTask(t, s.DB, dt.ID)
		if current.Status != "approved" || current.SettlementStatus != "pending" || current.SettlementWaitDate != dt.Date {
			t.Fatalf("仅非连击任务的日未结束却已发奖: %+v", current)
		}
		if current.Snapshot != string(b) {
			t.Fatal("已提交快照被重写")
		}
	}
	if p := streakPet(t, s, kid); p.Balance != 100 {
		t.Fatal("自然中性在午夜前提前付款")
	}
	midnight, _ := time.ParseInLocation("2006-01-02", engine.BusinessDate(now), engine.BusinessLocation)
	if err := engine.Reconcile(s.DB, midnight.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	for _, dt := range submitted {
		led := streakLedger(t, s, dt.ID)
		if led.Base != 15 || led.DateMultiplier != 2 || led.StreakMultiplier != 1.2 || led.Final != 36 {
			t.Fatalf("人审/自动额外奖励或快照不一致: %+v", led)
		}
	}
	if p := streakPet(t, s, kid); p.Streak != 2 || p.Balance != 172 {
		t.Fatal("非连击资格从模板追读或奖励错误")
	}
}

func TestStreakExplicitZeroCreateAndOmittedEditKeepValue(t *testing.T) {
	s, kid, _ := streakFixture(t, 0, false)
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	for _, enabled := range []int{0, 1} {
		name := fmt.Sprintf("开关%d", enabled)
		body := map[string]any{"name": name, "base_score": 3, "streak_enabled": enabled, "date_mode": "weekday_fixed", "weekdays": "1,2,3,4,5,6,7"}
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/task", body), http.StatusOK)
		var tpl store.TaskTemplate
		s.DB.Where("name=?", name).First(&tpl)
		if tpl.StreakEnabled != enabled {
			t.Fatalf("GORM吞掉显式0: %+v", tpl)
		}
		delete(body, "streak_enabled")
		requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", tpl.ID), body), http.StatusOK)
		s.DB.First(&tpl, tpl.ID)
		if tpl.StreakEnabled != enabled {
			t.Fatal("省略字段重写已保存开关")
		}
		var dt store.DailyTask
		s.DB.Where("template_id=?", tpl.ID).First(&dt)
		_, snap := readDailyTask(t, s.DB, dt.ID)
		if snap.StreakEnabled == nil || *snap.StreakEnabled != enabled {
			t.Fatal("连击资格未入快照")
		}
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/task", map[string]any{"name": "默认开启"}), http.StatusOK)
	var tpl store.TaskTemplate
	s.DB.Where("name=?", "默认开启").First(&tpl)
	if tpl.StreakEnabled != 1 {
		t.Fatal("默认未开启")
	}
	foreign := store.Kid{FamilyID: kid.FamilyID + 1, Name: "其他家庭"}
	mustTaskCreate(t, s.DB, &foreign)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/leave", map[string]any{"date": engine.BusinessDate(s.now()), "kid_id": foreign.ID}), http.StatusNotFound)
	var count int64
	s.DB.Model(&store.LeaveRecord{}).Where("kid_id=?", foreign.ID).Count(&count)
	if count != 0 {
		t.Fatal("跨家庭写请假")
	}
}

func TestStreakSettlementErrorsRollbackWholeApprovalAndRejectBareDB(t *testing.T) {
	s, kid, now := streakFixture(t, 1, false)
	dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if _, err := engine.SettleAt(s.DB, dt.ID, 0, now); err == nil {
		t.Fatal("裸DB被当成事务")
	}
	if err := s.DB.Callback().Create().Before("gorm:create").Register("fail_task_ledger", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*store.SettlementLedger); ok {
			tx.AddError(errors.New("模拟流水写入失败"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}}), http.StatusInternalServerError)
	current, _ := readDailyTask(t, s.DB, dt.ID)
	if current.Status != "pending" || current.SettlementStatus != "" {
		t.Fatalf("错误没回滚审批: %+v", current)
	}
	if p := streakPet(t, s, kid); p.Streak != 1 || p.Balance != 100 {
		t.Fatal("错误后缓存/余额残留")
	}
	var count int64
	s.DB.Model(&store.TaskCompletion{}).Count(&count)
	if count != 0 {
		t.Fatal("错误后提交流水残留")
	}
}

func TestStreakLegacyPaidTodaySurvivesPreviousDayResetWithoutRepayment(t *testing.T) {
	s, kid, now := streakFixture(t, 4, true)
	old := streakTask(t, s, kid, now.AddDate(0, 0, -1), 1, "c", "weekday_fixed", "submitted")
	paid := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "approved")
	id := paid.ID
	mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, DailyTaskID: &id, Date: paid.Date, Source: "task", Final: 999})
	// 第一个新引擎入口直接是昨日拒绝，也必须先初始化宽限内日基线。
	if p := streakPet(t, s, kid); p.StreakBaseDate != "" || p.Streak != 4 {
		t.Fatal("测试不是未切换状态")
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", old.ID), map[string]any{"approve": false}), http.StatusOK)
	if p := streakPet(t, s, kid); p.Streak != 1 || p.Balance != 100 {
		t.Fatalf("重置后丢失本日成功或重付: %+v", p)
	}
	if led := streakLedger(t, s, paid.ID); led.Final != 999 {
		t.Fatal("已发流水被重算")
	}
	if err := engine.Reconcile(s.DB, now); err != nil {
		t.Fatal(err)
	}
	if p := streakPet(t, s, kid); p.Streak != 1 || p.Balance != 100 {
		t.Fatal("补扫不幂等")
	}
}

func TestStreakLegacyNonStreakApprovalDoesNotWaitForFutureDay(t *testing.T) {
	s, kid, now := streakFixture(t, 2, true)
	old := streakTask(t, s, kid, now.AddDate(0, 0, -1), 0, "c", "weekday_fixed", "submitted")
	streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if !settleStreak(t, s, old, now) {
		t.Fatal("旧非连击任务等待了未来未提交日")
	}
	if p := streakPet(t, s, kid); p.Streak != 2 || p.Balance != 112 {
		t.Fatalf("legacy非连击推进连击或倍率错误: %+v", p)
	}
}

func TestStreakLegacyMissingRewardSnapshotFreezesOnlyBeforeFirstSubmit(t *testing.T) {
	for _, submitted := range []bool{false, true} {
		t.Run(fmt.Sprint(submitted), func(t *testing.T) {
			s, kid, now := streakFixture(t, 2, false)
			status := "pending"
			if submitted {
				status = "submitted"
			}
			dt := streakTask(t, s, kid, now, 1, "c", "weekday_fixed", status)
			raw := `{"name":"旧快照","confirm_mode":"c","base_score":10,"date_mode":"weekday_fixed","evidence_steps":"[]"}`
			if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("snapshot", raw).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Model(&store.TaskTemplate{}).Where("id=?", dt.TemplateID).Update("streak_enabled", 0).Error; err != nil {
				t.Fatal(err)
			}
			mustTaskCreate(t, s.DB, &store.RewardPolicy{MatchType: "date", MatchValue: dt.Date, Multiplier: 2})
			r := taskTestRouter(s, kid.FamilyID, kid.ID)
			if !submitted {
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}}), http.StatusOK)
			}
			current, snap := readDailyTask(t, s.DB, dt.ID)
			if submitted && current.Snapshot != raw {
				t.Fatal("迁移改写已提交快照")
			}
			if !submitted && (snap.StreakEnabled == nil || *snap.StreakEnabled != 0 || snap.DateMultiplier != 2) {
				t.Fatalf("首次提交未冻结奖励字段: %+v", snap)
			}
			requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true}), http.StatusOK)
			if !submitted {
				current, _ := readDailyTask(t, s.DB, dt.ID)
				if current.SettlementStatus != "pending" {
					t.Fatal("自然中性未等待本日结束")
				}
				midnight, _ := time.ParseInLocation("2006-01-02", dt.Date, engine.BusinessLocation)
				if err := engine.Reconcile(s.DB, midnight.AddDate(0, 0, 1)); err != nil {
					t.Fatal(err)
				}
			}
			led := streakLedger(t, s, dt.ID)
			if submitted {
				if led.DateMultiplier != 1 || led.StreakMultiplier != 1.3 {
					t.Fatalf("legacy明确默认值错误: %+v", led)
				}
			} else if led.DateMultiplier != 2 || led.StreakMultiplier != 1.2 {
				t.Fatalf("首次冻结字段无效: %+v", led)
			}
		})
	}
}

func TestStreakReconcileReportsFailureButDoesNotSkipOtherKids(t *testing.T) {
	s, kid, now := streakFixture(t, 1, false)
	broken := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	s.DB.Model(&store.DailyTask{}).Where("id=?", broken.ID).Update("snapshot", "{")
	other := store.Kid{FamilyID: kid.FamilyID, Name: "另一孩子"}
	mustTaskCreate(t, s.DB, &other)
	mustTaskCreate(t, s.DB, &store.Pet{KidID: other.ID, Name: "猫", StreakBaseDate: engine.BusinessDate(now.AddDate(0, 0, -1)), StreakBaseValue: 2, Streak: 2})
	old := streakTask(t, s, other, now.AddDate(0, 0, -1), 1, "a", "weekday_fixed", "pending")
	if err := engine.Reconcile(s.DB, now); err == nil {
		t.Fatal("补扫吞掉了错误")
	}
	current, _ := readDailyTask(t, s.DB, old.ID)
	if current.Status != "expired" {
		t.Fatal("一个孩子失败导致其他孩子永不补扫")
	}
	if p := streakPet(t, s, other); p.Streak != 0 {
		t.Fatal("另一孩子没有收尾")
	}
}

func TestStreakNonStreakRewardsUseFinalDayMultiplierRegardlessOfOrder(t *testing.T) {
	for _, eligibleFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(eligibleFirst), func(t *testing.T) {
			s, kid, now := streakFixture(t, 2, false)
			non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
			eligible := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
			if eligibleFirst {
				settleStreak(t, s, eligible, now)
				settleStreak(t, s, non, now)
			} else {
				if settleStreak(t, s, non, now) {
					t.Fatal("非连击奖励抢跑")
				}
				settleStreak(t, s, eligible, now)
			}
			for _, dt := range []store.DailyTask{non, eligible} {
				led := streakLedger(t, s, dt.ID)
				if led.Final != 13 || led.StreakMultiplier != 1.3 {
					t.Fatalf("同日倍率依审批顺序变化: %+v", led)
				}
			}
			if p := streakPet(t, s, kid); p.Streak != 3 || p.Balance != 126 {
				t.Fatalf("同日信用/余额错误: %+v", p)
			}
		})
	}
	s, kid, now := streakFixture(t, 4, false)
	non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
	streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if settleStreak(t, s, non, now) {
		t.Fatal("固定任务尚未结束时不能判定最终倍率")
	}
	midnight, _ := time.ParseInLocation("2006-01-02", non.Date, engine.BusinessLocation)
	if err := engine.Reconcile(s.DB, midnight.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	led := streakLedger(t, s, non.ID)
	if led.Final != 10 || led.StreakMultiplier != 1 {
		t.Fatalf("本日漏做后非连击未用重置倍率: %+v", led)
	}
}

func TestStreakTaskTransactionSourceGuard(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	files := []string{"api_confirm.go", "api_task.go", "api_review_job.go", "api_review_history.go", "../engine/materialize.go", "../engine/settle.go"}
	calls := 0
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(current), name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "Transaction" {
				t.Errorf("%s 绕过了统一ReadCommitted事务", name)
			}
			if selector.Sel.Name == "TaskTransaction" {
				calls++
			}
			return true
		})
	}
	if calls != 11 {
		t.Fatalf("领域事务入口覆盖发生变化，需要复核: %d", calls)
	}
}

func TestStreakTaskTransactionsActuallyUseReadCommittedAtEveryEntry(t *testing.T) {
	s, kid, _ := streakFixture(t, 2, false)
	auto := streakTask(t, s, kid, s.now(), 1, "a", "weekday_fixed", "pending")
	human := streakTask(t, s, kid, s.now(), 1, "c", "weekday_fixed", "submitted")
	ai := streakTask(t, s, kid, s.now(), 1, "bc", "weekday_fixed", "submitted")
	var comp store.TaskCompletion
	if err := s.DB.Where("daily_task_id=?", ai.ID).First(&comp).Error; err != nil {
		t.Fatal(err)
	}
	job := store.AIReviewJob{KidID: kid.ID, DailyTaskID: ai.ID, CompletionID: comp.ID, Snapshot: ai.Snapshot, Status: "queued", Stage: "queued"}
	mustTaskCreate(t, s.DB, &job)
	var checked atomic.Int32
	if err := s.DB.Callback().Query().Before("gorm:query").Register("assert_task_isolation", func(tx *gorm.DB) {
		if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
			return
		}
		if err := (&store.DB{DB: tx}).RequireTaskTransaction(); err != nil {
			t.Error(err)
		}
		var isolation string
		if err := tx.Statement.ConnPool.QueryRowContext(context.Background(), "SHOW transaction_isolation").Scan(&isolation); err != nil {
			t.Error(err)
		} else if isolation != "read committed" {
			t.Errorf("实际隔离级别不是RC: %s", isolation)
		}
		checked.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	entry := func(name string, run func()) {
		t.Helper()
		before := checked.Load()
		run()
		if checked.Load() == before {
			t.Fatalf("%s未验证到领域事务", name)
		}
	}
	entry("submit", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", auto.ID), map[string]any{"evidence": []any{}}), http.StatusOK)
	})
	entry("confirm", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", human.ID), map[string]any{"approve": true}), http.StatusOK)
	})
	var claimed store.AIReviewJob
	entry("AI claim", func() { claimed = claimedAI(t, s) })
	entry("AI finish", func() {
		if err := s.finishReviewJob(claimed, reviewVerdict{}, errors.New("本地模拟失败")); err != nil {
			t.Fatal(err)
		}
	})
	entry("AI retry", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", comp.ID), nil), http.StatusOK)
	})
	entry("update", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", auto.TemplateID), taskEditBody("free", 0)), http.StatusOK)
	})
	entry("create", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/task", map[string]any{"name": "隔离级别任务"}), http.StatusOK)
	})
	entry("materialize", func() {
		if err := engine.MaterializeCurrent(s.DB, s.now); err != nil {
			t.Fatal(err)
		}
	})
	entry("reconcile", func() {
		if err := engine.ReconcileWithClock(s.DB, s.now); err != nil {
			t.Fatal(err)
		}
	})
	entry("leave", func() {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/leave", map[string]any{"date": engine.BusinessDate(s.now()), "kid_id": kid.ID}), http.StatusOK)
	})
}

func TestStreakConcurrentYesterdayApprovalsCannotRepayWaitingToday(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	yesterday := now.AddDate(0, 0, -1)
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"streak_base_date": engine.BusinessDate(yesterday)}); err != nil {
		t.Fatal(err)
	}
	olds := []store.DailyTask{streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted"), streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted")}
	today := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if settleStreak(t, s, today, now) {
		t.Fatal("前置待审未挡住今日奖励")
	}
	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	blockDone := make(chan error, 1)
	go func() {
		blockDone <- s.DB.TaskTransaction(func(tx *gorm.DB) error {
			if err := engine.LockKid(&store.DB{DB: tx}, kid.ID); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	waitTaskSignal(t, locked)
	identities := make(chan struct{})
	var reads atomic.Int32
	if err := s.DB.Callback().Query().After("gorm:query").Register("both_old_identities", func(tx *gorm.DB) {
		dt, ok := tx.Statement.Dest.(*store.DailyTask)
		if !ok {
			return
		}
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			return
		}
		if dt.ID == olds[0].ID || dt.ID == olds[1].ID {
			if reads.Add(1) == 2 {
				close(identities)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, dt := range olds {
		go func(dt store.DailyTask) {
			results <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true})
		}(dt)
	}
	waitTaskSignal(t, identities)
	unblock()
	if err := <-blockDone; err != nil {
		t.Fatal(err)
	}
	for range olds {
		select {
		case result := <-results:
			requireTaskStatus(t, result, http.StatusOK)
		case <-time.After(5 * time.Second):
			t.Fatal("昨日并发审批没有结束")
		}
	}
	for _, dt := range append(olds, today) {
		var n int64
		if err := s.DB.Model(&store.SettlementLedger{}).Where("daily_task_id=? AND source='task'", dt.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("task %d发了%d次", dt.ID, n)
		}
	}
	if p := streakPet(t, s, kid); p.Balance != 140 || p.Streak != 4 {
		t.Fatalf("重复发今日等待奖励: %+v", p)
	}
}

func TestStreakClockSampledAfterKidLockAcrossMidnight(t *testing.T) {
	for _, operation := range []string{"reconcile", "submit", "confirm", "retry", "ai", "materialize", "update"} {
		t.Run(operation, func(t *testing.T) {
			s, kid, noon := streakFixture(t, 3, false)
			day, _ := time.ParseInLocation("2006-01-02", engine.BusinessDate(noon), engine.BusinessLocation)
			before := day.AddDate(0, 0, 1).Add(-time.Millisecond)
			after := day.AddDate(0, 0, 1)
			date, status, mode := day, "pending", "a"
			if operation == "confirm" || operation == "retry" || operation == "ai" {
				date, status, mode = day.AddDate(0, 0, -1), "submitted", "bc"
			}
			dt := streakTask(t, s, kid, date, 1, mode, "weekday_fixed", status)
			var non, future store.DailyTask
			var completion store.TaskCompletion
			var claimed store.AIReviewJob
			if operation == "reconcile" {
				non = streakTask(t, s, kid, day, 0, "a", "weekday_fixed", "pending")
				if settleStreak(t, s, non, noon) {
					t.Fatal("本日待定却发奖")
				}
			}
			if operation == "retry" || operation == "ai" {
				if err := s.DB.Where("daily_task_id=?", dt.ID).First(&completion).Error; err != nil {
					t.Fatal(err)
				}
				jobStatus := "failed"
				if operation == "ai" {
					jobStatus = "queued"
				}
				job := store.AIReviewJob{KidID: kid.ID, DailyTaskID: dt.ID, CompletionID: completion.ID, Snapshot: dt.Snapshot, Status: jobStatus, Stage: jobStatus}
				mustTaskCreate(t, s.DB, &job)
				if operation == "ai" {
					claimed = claimedAI(t, s)
				}
			}
			if operation == "materialize" || operation == "update" {
				if err := s.DB.AssignTask(dt.TemplateID, kid.ID); err != nil {
					t.Fatal(err)
				}
				if operation == "update" {
					if err := engine.Materialize(s.DB, after); err != nil {
						t.Fatal(err)
					}
					if err := s.DB.Where("template_id=? AND kid_id=? AND date=?", dt.TemplateID, kid.ID, engine.BusinessDate(after)).First(&future).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			var clockValue atomic.Int64
			clockValue.Store(before.UnixNano())
			var clockCalls atomic.Int32
			s.Now = func() time.Time { clockCalls.Add(1); return time.Unix(0, clockValue.Load()) }
			locked, release, waiting := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce, waitingOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			blockDone := make(chan error, 1)
			go func() {
				blockDone <- s.DB.TaskTransaction(func(tx *gorm.DB) error {
					if err := engine.LockKid(&store.DB{DB: tx}, kid.ID); err != nil {
						return err
					}
					close(locked)
					<-release
					return nil
				})
			}()
			waitTaskSignal(t, locked)
			if err := s.DB.Callback().Query().Before("gorm:query").Register("waiting_for_kid", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*store.Kid); ok {
					if _, locking := tx.Statement.Clauses["FOR"]; locking {
						waitingOnce.Do(func() { close(waiting) })
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			r := taskTestRouter(s, kid.FamilyID, kid.ID)
			done := make(chan error, 1)
			response := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				switch operation {
				case "reconcile":
					done <- engine.ReconcileWithClock(s.DB, s.now)
				case "materialize":
					done <- engine.MaterializeCurrent(s.DB, s.now)
				case "ai":
					done <- s.finishReviewJob(claimed, reviewVerdict{Pass: true}, nil)
				case "submit":
					response <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}})
				case "confirm":
					response <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", dt.ID), map[string]any{"approve": true})
				case "retry":
					response <- taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d/ai-retry", completion.ID), nil)
				case "update":
					response <- taskTestRequest(r, http.MethodPut, fmt.Sprintf("/api/admin/task/%d", dt.TemplateID), taskEditBody("free", 0))
				}
			}()
			waitTaskSignal(t, waiting)
			if clockCalls.Load() != 0 {
				t.Fatal("在孩子锁前就读取了业务时钟")
			}
			clockValue.Store(after.UnixNano())
			unblock()
			if err := <-blockDone; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case result := <-response:
				expected := http.StatusBadRequest
				if operation == "retry" {
					expected = http.StatusConflict
				}
				if operation == "update" {
					expected = http.StatusOK
				}
				requireTaskStatus(t, result, expected)
			case <-time.After(5 * time.Second):
				t.Fatal("跨午夜操作未完成")
			}
			switch operation {
			case "reconcile":
				want := int64(0)
				streakDay(t, s, kid, day, "missed", &want)
				if led := streakLedger(t, s, non.ID); led.Final != 10 {
					t.Fatal("锁后仍按过期ticker时间发奖")
				}
				if err := engine.Reconcile(s.DB, before); err != nil {
					t.Fatal(err)
				}
				if p := streakPet(t, s, kid); p.StreakEvaluatedNS == nil || *p.StreakEvaluatedNS < after.UnixNano() || p.Streak != 0 {
					t.Fatalf("晚到时钟使watermark回退: %+v", p)
				}
				precise := after.Add(123 * time.Nanosecond)
				if err := engine.Reconcile(s.DB, precise); err != nil {
					t.Fatal(err)
				}
				if err := engine.Reconcile(s.DB, precise.Add(-time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
				if p := streakPet(t, s, kid); p.StreakEvaluatedNS == nil || *p.StreakEvaluatedNS != precise.UnixNano() {
					t.Fatal("数据库时间精度丢掉了watermark纳秒")
				}
			case "ai":
				s.DB.First(&completion, completion.ID)
				if completion.ConfirmStatus != "expired" {
					t.Fatal("锁后AI仍批准过审核截止的任务")
				}
			case "materialize":
				var n int64
				s.DB.Model(&store.DailyTask{}).Where("kid_id=? AND template_id=? AND date=?", kid.ID, dt.TemplateID, engine.BusinessDate(after)).Count(&n)
				if n != 1 {
					t.Fatal("按锁前旧日期物化")
				}
			case "update":
				old, _ := readDailyTask(t, s.DB, dt.ID)
				next, snap := readDailyTask(t, s.DB, future.ID)
				if old.Snapshot != dt.Snapshot || snap.Name != "新任务" || next.Date != engine.BusinessDate(after) {
					t.Fatal("跨午夜刷新了昨日或漏掉新业务日")
				}
			}
		})
	}
}

func TestStreakBaselineCreditsBelongToEachReplayDay(t *testing.T) {
	s, kid, now := streakFixture(t, 4, true)
	yesterday := now.AddDate(0, 0, -1)
	for _, date := range []time.Time{yesterday, now} {
		paid := streakTask(t, s, kid, date, 1, "a", "weekday_fixed", "approved")
		id := paid.ID
		mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, DailyTaskID: &id, Date: paid.Date, Source: "task", Final: 999})
	}
	old := streakTask(t, s, kid, yesterday, 1, "c", "weekday_fixed", "submitted")
	if !settleStreak(t, s, old, now) {
		t.Fatal("同日已有通过还等待其他任务")
	}
	p := streakPet(t, s, kid)
	if p.StreakBaseValue != 2 || p.Streak != 4 || p.Balance != 113 {
		t.Fatalf("两日信用没有逐日投放: %+v", p)
	}
	oldAfter, todayAfter := int64(3), int64(4)
	streakDay(t, s, kid, yesterday, "success", &oldAfter)
	streakDay(t, s, kid, now, "success", &todayAfter)
	if led := streakLedger(t, s, old.ID); led.Final != 13 || led.StreakMultiplier != 1.3 {
		t.Fatalf("跨日提前使用信用: %+v", led)
	}
}

func TestStreakNoCandidatesWaitsForLateCreatedEligibleTask(t *testing.T) {
	for _, outcome := range []string{"success", "missed", "neutral"} {
		t.Run(outcome, func(t *testing.T) {
			s, kid, now := streakFixture(t, 2, false)
			non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
			r := taskTestRouter(s, kid.FamilyID, kid.ID)
			w := taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", non.ID), map[string]any{"evidence": []any{}})
			requireTaskStatus(t, w, http.StatusOK)
			var submitted struct {
				Settled bool `json:"settled"`
				Pending bool `json:"settlement_pending"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &submitted); err != nil || submitted.Settled || !submitted.Pending {
				t.Fatalf("无候选日提前发奖: %s", w.Body)
			}
			current, _ := readDailyTask(t, s.DB, non.ID)
			if current.Status != "approved" || current.SettlementStatus != "pending" || current.SettlementWaitDate != non.Date {
				t.Fatalf("缺少本日待定状态: %+v", current)
			}
			streakDay(t, s, kid, now, "pending", nil)
			var petResponse struct {
				Pending bool `json:"streak_pending"`
				Count   int  `json:"pending_reward_count"`
			}
			w = taskTestRequest(r, http.MethodGet, "/api/pet", nil)
			requireTaskStatus(t, w, http.StatusOK)
			if err := json.Unmarshal(w.Body.Bytes(), &petResponse); err != nil || !petResponse.Pending || petResponse.Count != 1 {
				t.Fatalf("无候选日pet待定合同错误: %s", w.Body)
			}
			if outcome != "neutral" {
				body := map[string]any{"name": "晚创建连击任务", "base_score": 10, "confirm_mode": "a", "evidence_steps": "[]", "date_mode": "weekday_fixed", "weekdays": "1,2,3,4,5,6,7", "streak_enabled": 1}
				w = taskTestRequest(r, http.MethodPost, "/api/admin/task", body)
				requireTaskStatus(t, w, http.StatusOK)
				var tpl store.TaskTemplate
				if err := s.DB.Where("name=?", body["name"]).First(&tpl).Error; err != nil {
					t.Fatal(err)
				}
				var eligible store.DailyTask
				if err := s.DB.Where("kid_id=? AND template_id=? AND date=?", kid.ID, tpl.ID, non.Date).First(&eligible).Error; err != nil {
					t.Fatal(err)
				}
				if outcome == "success" {
					// 同日另一个候选尚未完成，不应该再阻塞日成功。
					streakTask(t, s, kid, now, 1, "c", "weekday_fixed", "pending")
					w = taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", eligible.ID), map[string]any{"evidence": []any{}})
					requireTaskStatus(t, w, http.StatusOK)
					for _, dt := range []store.DailyTask{non, eligible} {
						led := streakLedger(t, s, dt.ID)
						if led.Final != 13 || led.StreakMultiplier != 1.3 {
							t.Fatalf("晚增资格未统一倍率: %+v", led)
						}
					}
				}
			}
			if outcome != "success" {
				midnight, _ := time.ParseInLocation("2006-01-02", non.Date, engine.BusinessLocation)
				if err := engine.Reconcile(s.DB, midnight.AddDate(0, 0, 1).Add(-time.Nanosecond)); err != nil {
					t.Fatal(err)
				}
				if p := streakPet(t, s, kid); p.Balance != 100 {
					t.Fatal("自然中性在提交截止前付款")
				}
				end := midnight.AddDate(0, 0, 1)
				if err := engine.Reconcile(s.DB, end); err != nil {
					t.Fatal(err)
				}
				s.Now = func() time.Time { return end }
				led := streakLedger(t, s, non.ID)
				want := 12
				mul := 1.2
				state := "neutral"
				streak := int64(2)
				if outcome == "missed" {
					want, mul, state, streak = 10, 1, "missed", 0
				}
				if led.Final != want || led.StreakMultiplier != mul {
					t.Fatalf("午夜最终倍率错误: %+v", led)
				}
				streakDay(t, s, kid, now, state, &streak)
			}
			w = taskTestRequest(r, http.MethodGet, "/api/pet", nil)
			requireTaskStatus(t, w, http.StatusOK)
			if err := json.Unmarshal(w.Body.Bytes(), &petResponse); err != nil || petResponse.Count != 0 {
				t.Fatalf("完成后仍有待发奖励: %s", w.Body)
			}
			if outcome == "success" && petResponse.Pending {
				t.Fatalf("同日success未清除streak_pending: %s", w.Body)
			}
			// 新业务日未结束显示pending是预期；不能据此认为旧日奖励仍在等待。
			if err := engine.ReconcileWithClock(s.DB, s.now); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := s.DB.Model(&store.SettlementLedger{}).Where("daily_task_id=? AND source='task'", non.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("重复补扫发奖")
			}
		})
	}
}

func TestStreakWholeDayLeaveFreezesLaterSubmissions(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	non := streakTask(t, s, kid, now, 0, "a", "weekday_fixed", "pending")
	if settleStreak(t, s, non, now) {
		t.Fatal("本日尚未最终确定")
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/leave", map[string]any{"kid_id": kid.ID, "date": non.Date}), http.StatusOK)
	want := int64(2)
	streakDay(t, s, kid, now, "neutral", &want)
	if led := streakLedger(t, s, non.ID); led.Final != 12 || led.StreakMultiplier != 1.2 {
		t.Fatalf("有效整日请假没有即时解锁奖励: %+v", led)
	}
	for _, mode := range []string{"a", "c"} {
		name := "请假后新增" + mode
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/admin/task", map[string]any{"name": name, "confirm_mode": mode, "base_score": 10, "date_mode": "weekday_fixed", "weekdays": "1,2,3,4,5,6,7", "evidence_steps": "[]"}), http.StatusOK)
		var tpl store.TaskTemplate
		if err := s.DB.Where("name=?", name).First(&tpl).Error; err != nil {
			t.Fatal(err)
		}
		var dt store.DailyTask
		if err := s.DB.Where("template_id=? AND kid_id=? AND date=?", tpl.ID, kid.ID, non.Date).First(&dt).Error; err != nil {
			t.Fatal(err)
		}
		if dt.Status != "skipped" {
			t.Fatal("请假日新增任务未冻结")
		}
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}}), http.StatusBadRequest)
		// 兼容尚未物化/未正确标为skipped的旧实例也必须由leave事实拦截。
		if err := s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "pending").Error; err != nil {
			t.Fatal(err)
		}
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/tasks/%d/submit", dt.ID), map[string]any{"evidence": []any{}}), http.StatusBadRequest)
		var count int64
		if err := s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=?", dt.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("请假冻结后仍创建了submission")
		}
	}
	if p := streakPet(t, s, kid); p.Balance != 112 || p.Streak != 2 {
		t.Fatalf("请假后提交改变了奖励/信用: %+v", p)
	}
	var response struct {
		Pending bool `json:"streak_pending"`
		Count   int  `json:"pending_reward_count"`
	}
	w := taskTestRequest(r, http.MethodGet, "/api/pet", nil)
	requireTaskStatus(t, w, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Pending || response.Count != 0 {
		t.Fatalf("有效请假日仍待定: %s", w.Body)
	}
}

func TestShopBuyOnlyDeductsCurrentKidAcrossFamilies(t *testing.T) {
	s, kid, _ := streakFixture(t, 0, false)
	fam := store.Family{Code: "991122", Name: "另一个家庭"}
	mustTaskCreate(t, s.DB, &fam)
	other := store.Kid{FamilyID: fam.ID, Name: "其他孩子"}
	mustTaskCreate(t, s.DB, &other)
	mustTaskCreate(t, s.DB, &store.Pet{KidID: other.ID, Name: "另一猫", Balance: 200, EarnedTotal: 200})
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 5}); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "nest"}), http.StatusBadRequest)
	if p := streakPet(t, s, other); p.Balance != 200 {
		t.Fatal("本孩子钱不足却扣了其他家庭")
	}
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 100}); err != nil {
		t.Fatal(err)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "nest"}), http.StatusOK)
	if p := streakPet(t, s, kid); p.Balance != 70 {
		t.Fatalf("本人扣款错误: %+v", p)
	}
	if p := streakPet(t, s, other); p.Balance != 200 {
		t.Fatal("购买跨家庭扣款")
	}
	otherRouter := taskTestRouter(s, other.FamilyID, other.ID)
	requireTaskStatus(t, taskTestRequest(otherRouter, http.MethodPost, "/api/shop/buy", map[string]any{"key": "yarn"}), http.StatusOK)
	if p := streakPet(t, s, other); p.Balance != 185 {
		t.Fatal("其他家庭购买扣款错误")
	}
	if p := streakPet(t, s, kid); p.Balance != 70 {
		t.Fatal("其他家庭购买扣到本孩子")
	}
	for _, k := range []store.Kid{kid, other} {
		var owned []store.OwnedItem
		if err := s.DB.Where("kid_id=?", k.ID).Find(&owned).Error; err != nil {
			t.Fatal(err)
		}
		if len(owned) != 1 {
			t.Fatalf("货品归属/数量错误: %+v", owned)
		}
	}
}

func TestLedgerKidRejectsForeignFamilyAndMissingKid(t *testing.T) {
	s, kid, _ := streakFixture(t, 0, false)
	fam := store.Family{Code: "778899", Name: "其他家庭"}
	mustTaskCreate(t, s.DB, &fam)
	other := store.Kid{FamilyID: fam.ID, Name: "他家孩子"}
	mustTaskCreate(t, s.DB, &other)
	for _, k := range []store.Kid{kid, other} {
		mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: k.ID, Source: "task", Final: 99})
	}
	for _, owner := range []store.Kid{kid, other} {
		r := taskTestRouter(s, owner.FamilyID, owner.ID)
		w := taskTestRequest(r, http.MethodGet, fmt.Sprintf("/api/admin/ledger/%d", owner.ID), nil)
		requireTaskStatus(t, w, http.StatusOK)
		var response struct {
			Items []store.SettlementLedger `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Items) != 1 || response.Items[0].KidID != owner.ID {
			t.Fatalf("授权流水错误: %s", w.Body)
		}
		foreign := other
		if owner.ID == other.ID {
			foreign = kid
		}
		requireTaskStatus(t, taskTestRequest(r, http.MethodGet, fmt.Sprintf("/api/admin/ledger/%d", foreign.ID), nil), http.StatusNotFound)
		requireTaskStatus(t, taskTestRequest(r, http.MethodGet, "/api/admin/ledger/999999", nil), http.StatusNotFound)
	}
}

func TestShopBuyAndFamilyLeaveReversePetOrderHaveNoDeadlock(t *testing.T) {
	s := &Server{DB: taskIntegrationDB(t)}
	fam := store.Family{Code: "665544", Name: "并发家庭"}
	mustTaskCreate(t, s.DB, &fam)
	first := store.Kid{FamilyID: fam.ID, Name: "先创建孩子"}
	second := store.Kid{FamilyID: fam.ID, Name: "后创建孩子"}
	mustTaskCreate(t, s.DB, &first)
	mustTaskCreate(t, s.DB, &second)
	// 领养顺序与孩子ID相反。旧全表扣款按pet主键先锁second再锁first，
	// 请假则按孩子ID先first再second，会形成锁反向依赖。
	mustTaskCreate(t, s.DB, &store.Pet{KidID: second.ID, Name: "先领养猫", Balance: 100})
	mustTaskCreate(t, s.DB, &store.Pet{KidID: first.ID, Name: "后领养猫", Balance: 100})
	at := time.Now()
	s.Now = func() time.Time { return at }
	firstPetHeld, release := make(chan struct{}), make(chan struct{})
	var heldOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	if err := s.DB.Callback().Update().After("gorm:update").Register("hold_leave_first_pet", func(tx *gorm.DB) {
		if tx.Statement.Table != "pet" || tx.Error != nil {
			return
		}
		fields, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, ok := fields["streak_evaluated_ns"]; !ok {
			return
		}
		heldOnce.Do(func() { close(firstPetHeld); <-release })
	}); err != nil {
		t.Fatal(err)
	}
	r := taskTestRouter(s, fam.ID, second.ID)
	leaveDone, buyDone := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go func() {
		leaveDone <- taskTestRequest(r, http.MethodPost, "/api/admin/leave", map[string]any{"date": engine.BusinessDate(at)})
	}()
	waitTaskSignal(t, firstPetHeld)
	go func() { buyDone <- taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "nest"}) }()
	var buy *httptest.ResponseRecorder
	select {
	case buy = <-buyDone:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("本孩子扣款仍依赖其他孩子的pet锁")
	}
	unblock()
	requireTaskStatus(t, buy, http.StatusOK)
	select {
	case w := <-leaveDone:
		requireTaskStatus(t, w, http.StatusOK)
	case <-time.After(5 * time.Second):
		t.Fatal("反向领养顺序下请假/购买死锁")
	}
	if p := streakPet(t, s, first); p.Balance != 100 {
		t.Fatal("购买误扣先创建孩子")
	}
	if p := streakPet(t, s, second); p.Balance != 70 {
		t.Fatal("购买扣款不正确")
	}
	for _, k := range []store.Kid{first, second} {
		var n int64
		if err := s.DB.Model(&store.LeaveRecord{}).Where("kid_id=? AND date=?", k.ID, engine.BusinessDate(at)).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatal("并发请假丢失")
		}
	}
}

func TestStreakLegacyZeroCachePaidTodayStillEndsAtOne(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			s, kid, now := streakFixture(t, 0, true)
			old := streakTask(t, s, kid, now.AddDate(0, 0, -1), 1, "c", "weekday_fixed", "submitted")
			paid := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "approved")
			id := paid.ID
			mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, DailyTaskID: &id, Date: paid.Date, Source: "task", Final: 999})
			if reject {
				r := taskTestRouter(s, kid.FamilyID, kid.ID)
				requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/admin/confirm/%d", old.ID), map[string]any{"approve": false}), http.StatusOK)
			} else {
				if err := engine.Reconcile(s.DB, now); err != nil {
					t.Fatal(err)
				}
				day, _ := time.ParseInLocation("2006-01-02", paid.Date, engine.BusinessLocation)
				if err := engine.Reconcile(s.DB, day.AddDate(0, 0, 1)); err != nil {
					t.Fatal(err)
				}
			}
			if p := streakPet(t, s, kid); p.Streak != 1 || p.StreakBaseValue < 0 || p.Balance != 100 {
				t.Fatalf("旧缓存0/错过收尾时丢了所属日信用: %+v", p)
			}
			if led := streakLedger(t, s, paid.ID); led.Final != 999 {
				t.Fatal("重算历史流水")
			}
		})
	}
}

// 心情与星星同在唯一发奖点：每个任务 +10，重复结算不重加，封顶 100。
func TestTaskSettlementAddsMoodOncePerTaskAndCapsAt100(t *testing.T) {
	s, kid, now := streakFixture(t, 0, false)
	setMood := func(v float64) {
		t.Helper()
		if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Update("mood", v).Error; err != nil {
			t.Fatal(err)
		}
	}
	setMood(50)
	dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if !settleStreak(t, s, dt, now) {
		t.Fatal("任务应完成结算")
	}
	if p := streakPet(t, s, kid); p.Mood != 60 {
		t.Fatalf("完成一个任务应 +10 心情，实际 %.1f", p.Mood)
	}
	// 补审/重放不能重复加心情
	settleStreak(t, s, dt, now)
	if p := streakPet(t, s, kid); p.Mood != 60 {
		t.Fatalf("重复结算又加了心情：%.1f", p.Mood)
	}
	// 第二个任务各加一次，并保持 100 上限
	setMood(95)
	dt2 := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	settleStreak(t, s, dt2, now)
	if p := streakPet(t, s, kid); p.Mood != 100 {
		t.Fatalf("心情应封顶 100，实际 %.1f", p.Mood)
	}
	setMood(50)
	dt3 := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	settleStreak(t, s, dt3, now)
	if p := streakPet(t, s, kid); p.Mood != 60 {
		t.Fatalf("新任务应再 +10 心情，实际 %.1f", p.Mood)
	}
}

// 长期不上线：发奖前先按结算时钟衰减，+10 不会被 100 上限吃掉；饱腹同时被提前结算。
func TestTaskSettlementTicksBeforeMoodReward(t *testing.T) {
	s, kid, now := streakFixture(t, 0, false)
	last := now.Add(-40 * time.Hour)
	if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Updates(map[string]any{
		"mood": 100, "hunger": 100, "hunger_decay": 2, "last_tick": last,
	}).Error; err != nil {
		t.Fatal(err)
	}
	dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if !settleStreak(t, s, dt, now) {
		t.Fatal("任务应完成结算")
	}
	p := streakPet(t, s, kid)
	if p.Mood != 90 {
		t.Fatalf("应先衰减 20 再 +10，期望 90，实际 %.1f", p.Mood)
	}
	if p.Hunger != 20 {
		t.Fatalf("发奖时应同时结算饱腹衰减，期望 20，实际 %.1f", p.Hunger)
	}
	if p.LastTick == nil || p.LastTick.Before(now.Add(-time.Minute)) {
		t.Fatalf("last_tick 应推进到结算时刻，实际 %v", p.LastTick)
	}
}

// 断签：连击从有到无当天 -20，重复对账/连续漏做不重扣，下限 0。
func TestStreakBreakDeductsMoodTwentyOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mood, want float64
	}{
		{"正常扣 20", 80, 60},
		{"下限 0", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, kid, now := streakFixture(t, 2, false)
			if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Update("mood", tc.mood).Error; err != nil {
				t.Fatal(err)
			}
			// 当天有参与连击的固定日任务，孩子没做；过了提交窗口即断签。
			streakTask(t, s, kid, now, 1, "c", "weekday_fixed", "pending")
			midnight, _ := time.ParseInLocation("2006-01-02", engine.BusinessDate(now), engine.BusinessLocation)
			end := midnight.AddDate(0, 0, 1)
			if err := engine.Reconcile(s.DB, end); err != nil {
				t.Fatal(err)
			}
			if p := streakPet(t, s, kid); p.Streak != 0 || p.Mood != tc.want {
				t.Fatalf("断签应清零连击且心情 %.0f，实际 %+v", tc.want, p)
			}
			if err := engine.Reconcile(s.DB, end); err != nil {
				t.Fatal(err)
			}
			if p := streakPet(t, s, kid); p.Mood != tc.want {
				t.Fatalf("重复对账又扣了心情：%.1f", p.Mood)
			}
			if err := engine.Reconcile(s.DB, end.AddDate(0, 0, 1)); err != nil {
				t.Fatal(err)
			}
			if p := streakPet(t, s, kid); p.Mood != tc.want {
				t.Fatalf("连击为 0 后的漏做不应再扣：%.1f", p.Mood)
			}
		})
	}
}

// 请假/休息日是 neutral，不算断签：不扣心情，也不清连击。
func TestLeaveDayDoesNotDeductMood(t *testing.T) {
	s, kid, now := streakFixture(t, 2, false)
	if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Update("mood", 80).Error; err != nil {
		t.Fatal(err)
	}
	streakTask(t, s, kid, now, 1, "c", "weekday_fixed", "pending")
	mustTaskCreate(t, s.DB, &store.LeaveRecord{KidID: kid.ID, Date: engine.BusinessDate(now), Reason: "请假"})
	midnight, _ := time.ParseInLocation("2006-01-02", engine.BusinessDate(now), engine.BusinessLocation)
	if err := engine.Reconcile(s.DB, midnight.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if p := streakPet(t, s, kid); p.Mood != 80 || p.Streak != 2 {
		t.Fatalf("请假日不应扣心情或清连击：%+v", p)
	}
}
