package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"meowquest/backend/internal/llm"
	"meowquest/backend/internal/store"
)

var errReviewLeaseLost = errors.New("AI 审核租约已失效")

func reviewTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("MEOW_AI_REVIEW_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return 5 * time.Minute
}

// StartReviewWorker 使用数据库队列；每个进程一个 worker，无需新 MQ 服务。
func (s *Server) StartReviewWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for ctx.Err() == nil {
			if err := s.recoverReviewLeases(); err != nil {
				log.Printf("AI 审核租约恢复失败: %v", err)
			} else {
				timeout := reviewTimeout()
				job, err := s.claimReviewJob(timeout)
				if err != nil {
					log.Printf("AI 审核领取失败: %v", err)
				} else if job != nil {
					if err := s.executeReviewJob(ctx, *job, timeout); err != nil && !errors.Is(err, errReviewLeaseLost) {
						log.Printf("AI 审核落库失败 job=%d: %v", job.ID, err)
					}
					continue
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func (s *Server) recoverReviewLeases() error {
	return s.DB.Model(&store.AIReviewJob{}).
		Where("status='running' AND (lease_expires_at IS NULL OR lease_expires_at<=?)", time.Now()).
		Updates(map[string]any{"status": "queued", "stage": "queued", "lease_token": "", "lease_expires_at": nil}).Error
}

func (s *Server) claimReviewJob(timeout time.Duration) (*store.AIReviewJob, error) {
	var job store.AIReviewJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status='queued'").Order("id").Limit(1).Find(&job)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		now := time.Now()
		expires := now.Add(timeout + 30*time.Second)
		job.Status, job.Stage, job.Error = "running", "preparing", ""
		job.Attempt++
		job.LeaseToken = store.NewToken()
		job.StartedAt, job.FinishedAt, job.LeaseExpiresAt = &now, nil, &expires
		return tx.Model(&store.AIReviewJob{}).Where("id=?", job.ID).Updates(map[string]any{
			"status": job.Status, "stage": job.Stage, "error": "", "attempt": job.Attempt,
			"lease_token": job.LeaseToken, "lease_expires_at": expires, "started_at": now, "finished_at": nil,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func reviewLease(db *gorm.DB, job store.AIReviewJob) *gorm.DB {
	return db.Model(&store.AIReviewJob{}).Where("id=? AND status='running' AND lease_token=? AND attempt=? AND lease_expires_at>?",
		job.ID, job.LeaseToken, job.Attempt, time.Now())
}

func (s *Server) reportReview(job store.AIReviewJob, stage, cache string) error {
	res := reviewLease(s.DB.DB, job).Updates(map[string]any{"stage": stage, "asr_cache": cache})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// MySQL 默认返回实际改变行数；同阶段/缓存在同毫秒上报可能是合法 no-op。
		var n int64
		if err := reviewLease(s.DB.DB, job).Count(&n).Error; err != nil {
			return err
		}
		if n != 1 {
			return errReviewLeaseLost
		}
	}
	return nil
}

func (s *Server) executeReviewJob(parent context.Context, job store.AIReviewJob, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var comp store.TaskCompletion
	if err := s.DB.First(&comp, job.CompletionID).Error; err != nil {
		return s.finishReviewJob(job, reviewVerdict{}, err)
	}
	var eligible int64
	if err := s.DB.Table("daily_task d").Joins("JOIN task_completion p ON p.daily_task_id=d.id").
		Where("d.id=? AND p.id=? AND d.status='submitted' AND p.confirm_status='pending'", job.DailyTaskID, job.CompletionID).
		Where("NOT EXISTS (SELECT 1 FROM task_completion newer WHERE newer.daily_task_id=d.id AND newer.id>p.id)").Count(&eligible).Error; err != nil {
		return s.finishReviewJob(job, reviewVerdict{}, err)
	}
	if eligible != 1 {
		return s.finishReviewJob(job, reviewVerdict{}, nil)
	}
	cache := map[string]*llm.TranscriptionState{}
	if job.ASRCache != "" {
		if err := json.Unmarshal([]byte(job.ASRCache), &cache); err != nil {
			return s.finishReviewJob(job, reviewVerdict{}, fmt.Errorf("ASR 缓存无效: %w", err))
		}
	}
	if cache == nil {
		cache = map[string]*llm.TranscriptionState{}
	}
	report := func(stage string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := json.Marshal(cache)
		if err != nil {
			return err
		}
		return s.reportReview(job, stage, string(b))
	}
	v, err := s.runReview(ctx, store.DailyTask{ID: job.DailyTaskID, KidID: job.KidID, Snapshot: job.Snapshot},
		EvidenceRow{Evidence: comp.Evidence, DurationSec: comp.DurationSec}, cache, report)
	if errors.Is(err, errReviewLeaseLost) {
		return err
	}
	if parent.Err() != nil {
		// 正常退出不消耗一次人工重试；缓存已保存，下次启动继续同一个上游 task。
		return reviewLease(s.DB.DB, job).Updates(map[string]any{"status": "queued", "stage": "queued", "lease_token": "", "lease_expires_at": nil}).Error
	}
	return s.finishReviewJob(job, v, err)
}

// finishReviewJob 锁顺序统一为 daily_task → job，CAS 拒绝旧 attempt 和人审后的晚到结果。
func (s *Server) finishReviewJob(job store.AIReviewJob, verdict reviewVerdict, reviewErr error) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var dt store.DailyTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&dt, job.DailyTaskID).Error; err != nil {
			return err
		}
		var current store.AIReviewJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, job.ID).Error; err != nil {
			return err
		}
		if current.Status != "running" || current.LeaseToken != job.LeaseToken || current.Attempt != job.Attempt ||
			current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(time.Now()) {
			return errReviewLeaseLost
		}
		var latest store.TaskCompletion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("daily_task_id=?", dt.ID).Order("id DESC").First(&latest).Error; err != nil {
			return err
		}
		now := time.Now()
		cols := map[string]any{"lease_token": "", "lease_expires_at": nil, "finished_at": now}
		if latest.ID != job.CompletionID || latest.ConfirmStatus != "pending" || dt.Status != "submitted" {
			cols["status"], cols["stage"] = "canceled", "canceled"
		} else if reviewErr != nil {
			cols["status"], cols["error"] = "failed", reviewErr.Error()
		} else {
			b, err := json.Marshal(verdict)
			if err != nil {
				return err
			}
			cols["status"], cols["stage"], cols["result"], cols["error"] = "succeeded", "finished", string(b), ""
			compCols := map[string]any{"review_result": string(b)}
			if !verdict.Pass {
				compCols["confirm_status"], compCols["confirmed_at"], compCols["reject_reason"] = "rejected", now, "AI："+verdict.Reason
				if err := tx.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "pending").Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&store.TaskCompletion{}).Where("id=? AND confirm_status='pending'", latest.ID).Updates(compCols).Error; err != nil {
				return err
			}
			var snap struct {
				Name string `json:"name"`
			}
			json.Unmarshal([]byte(job.Snapshot), &snap)
			payload, _ := json.Marshal(map[string]any{"task": snap.Name, "pass": verdict.Pass, "score": verdict.Score, "completion_id": latest.ID})
			if err := tx.Create(&store.PetEvent{KidID: dt.KidID, Type: "ai_review", Day: now.Format("2006-01-02"), Payload: string(payload)}).Error; err != nil {
				return err
			}
		}
		res := reviewLease(tx, job).Updates(cols)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errReviewLeaseLost
		}
		return nil
	})
}

// reviewSnapshot 只在入队事务内补旧快照缺失字段，显式空值保持不变。
func reviewSnapshot(tx *gorm.DB, dt store.DailyTask) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dt.Snapshot), &fields); err != nil {
		return "", err
	}
	if fields == nil {
		return "", fmt.Errorf("任务快照无效")
	}
	if _, ok := fields["review_prompt"]; !ok {
		var tpl store.TaskTemplate
		if err := tx.First(&tpl, dt.TemplateID).Error; err != nil {
			return "", err
		}
		fields["review_prompt"], _ = json.Marshal(tpl.ReviewPrompt)
	}
	b, err := json.Marshal(fields)
	return string(b), err
}

func newReviewJob(tx *gorm.DB, dt store.DailyTask, comp store.TaskCompletion) (store.AIReviewJob, error) {
	snapshot, err := reviewSnapshot(tx, dt)
	if err != nil {
		return store.AIReviewJob{}, err
	}
	job := store.AIReviewJob{CompletionID: comp.ID, DailyTaskID: dt.ID, KidID: dt.KidID,
		Snapshot: snapshot, Status: "queued", Stage: "queued", ASRCache: "{}"}
	return job, nil
}

func cancelReviewJob(tx *gorm.DB, completionID int64) error {
	return tx.Model(&store.AIReviewJob{}).Where("completion_id=? AND status IN ('queued','running','failed')", completionID).
		Updates(map[string]any{"status": "canceled", "stage": "canceled", "finished_at": time.Now(), "lease_token": "", "lease_expires_at": nil}).Error
}

type aiReviewView struct {
	Status     string         `json:"status"`
	Stage      string         `json:"stage"`
	Attempt    int            `json:"attempt"`
	Error      string         `json:"error"`
	StartedAt  *time.Time     `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at"`
	Result     *reviewVerdict `json:"result"`
}

func reviewView(job store.AIReviewJob) *aiReviewView {
	v := &aiReviewView{Status: job.Status, Stage: job.Stage, Attempt: job.Attempt, Error: job.Error,
		StartedAt: job.StartedAt, FinishedAt: job.FinishedAt}
	if job.Result != "" {
		var result reviewVerdict
		if json.Unmarshal([]byte(job.Result), &result) == nil {
			v.Result = &result
		}
	}
	return v
}

// 存量 completion 尚无 job：已有评价照常显示，已人审但 AI 未返回则显式 canceled。
func legacyReviewView(snapshot, result, confirmStatus string, confirmedAt *time.Time) *aiReviewView {
	if result != "" {
		var verdict reviewVerdict
		if json.Unmarshal([]byte(result), &verdict) == nil {
			return &aiReviewView{Status: "succeeded", Stage: "finished", Result: &verdict}
		}
	}
	if confirmStatus == "pending" {
		var snap struct {
			ConfirmMode string `json:"confirm_mode"`
		}
		if json.Unmarshal([]byte(snapshot), &snap) == nil && confirmModeContains(snap.ConfirmMode, 'b') && !confirmModeContains(snap.ConfirmMode, 'a') {
			return &aiReviewView{Status: "failed", Stage: "legacy", Error: "旧提交未保存 AI 分析结果，可重试分析或直接由家长审核。"}
		}
	}
	if confirmStatus == "approved" || confirmStatus == "rejected" {
		var snap struct {
			ConfirmMode string `json:"confirm_mode"`
		}
		if json.Unmarshal([]byte(snapshot), &snap) == nil && confirmModeContains(snap.ConfirmMode, 'b') && !confirmModeContains(snap.ConfirmMode, 'a') {
			return &aiReviewView{Status: "canceled", Stage: "canceled", FinishedAt: confirmedAt}
		}
	}
	return nil
}

type submissionView struct {
	CompletionID  int64      `json:"completion_id"`
	ConfirmStatus string     `json:"confirm_status"`
	DurationSec   int        `json:"duration_sec"`
	SubmittedAt   time.Time  `json:"submitted_at"`
	ConfirmedAt   *time.Time `json:"confirmed_at"`
	RejectReason  string     `json:"reject_reason"`
	ReviewResult  string     `json:"review_result"`
}

type todayTaskView struct {
	store.DailyTask
	AIReview         *aiReviewView   `json:"ai_review"`
	LatestSubmission *submissionView `json:"latest_submission"`
}

func (s *Server) todaySubmissionViews(tasks []store.DailyTask) ([]todayTaskView, error) {
	out := make([]todayTaskView, 0, len(tasks))
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	var completions []store.TaskCompletion
	if err := s.DB.Where("daily_task_id IN ?", ids).
		Where("NOT EXISTS (SELECT 1 FROM task_completion newer WHERE newer.daily_task_id=task_completion.daily_task_id AND newer.id>task_completion.id)").Find(&completions).Error; err != nil {
		return nil, err
	}
	latest := map[int64]*submissionView{}
	completionIDs := make([]int64, 0, len(completions))
	for _, comp := range completions {
		latest[comp.DailyTaskID] = &submissionView{CompletionID: comp.ID, ConfirmStatus: comp.ConfirmStatus,
			DurationSec: comp.DurationSec, SubmittedAt: comp.SubmittedAt, ConfirmedAt: comp.ConfirmedAt,
			RejectReason: comp.RejectReason, ReviewResult: comp.ReviewResult}
		completionIDs = append(completionIDs, comp.ID)
	}
	views := map[int64]*aiReviewView{}
	if len(completionIDs) > 0 {
		var jobs []store.AIReviewJob
		if err := s.DB.Where("completion_id IN ?", completionIDs).Find(&jobs).Error; err != nil {
			return nil, err
		}
		for _, job := range jobs {
			views[job.CompletionID] = reviewView(job)
		}
	}
	for _, task := range tasks {
		view := todayTaskView{DailyTask: task, LatestSubmission: latest[task.ID]}
		if sub := view.LatestSubmission; sub != nil {
			view.AIReview = views[sub.CompletionID]
			if view.AIReview == nil {
				view.AIReview = legacyReviewView(task.Snapshot, sub.ReviewResult, sub.ConfirmStatus, sub.ConfirmedAt)
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func attachReviewViews(db *gorm.DB, items []confirmItem) error {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CompletionID)
	}
	if len(ids) == 0 {
		return nil
	}
	var jobs []store.AIReviewJob
	if err := db.Where("completion_id IN ?", ids).Find(&jobs).Error; err != nil {
		return err
	}
	views := map[int64]*aiReviewView{}
	for _, job := range jobs {
		views[job.CompletionID] = reviewView(job)
	}
	for i := range items {
		items[i].AIReview = views[items[i].CompletionID]
		if items[i].AIReview == nil {
			items[i].AIReview = legacyReviewView(items[i].Snapshot, items[i].ReviewResult, items[i].ConfirmStatus, items[i].ConfirmedAt)
		}
	}
	return nil
}
