package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

// ===== 孩子端：提交任务成果 =====

// POST /api/tasks/:id/submit
// 流水线：confirm_mode 含 a → 自动通过结算；含 c → submitted 等家长；b → P2 AI 验收
func (s *Server) submitTask(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		DurationSec int             `json:"duration_sec"`
		ExtraSec    int             `json:"extra_sec"`
		Evidence    json.RawMessage `json:"evidence"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.DurationSec < 0 || req.ExtraSec < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "计时时长不能为负数"})
		return
	}
	ev := "[]"
	if len(req.Evidence) > 0 {
		ev = string(req.Evidence)
	}
	status := http.StatusInternalServerError
	var result gin.H
	// 行锁覆盖校验、流水和状态转换，编辑不能刷新正在提交的快照。
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		db := &store.DB{DB: tx}
		var dt store.DailyTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND kid_id=?", id, kidOf(c)).First(&dt).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				status = http.StatusNotFound
			}
			return err
		}
		if dt.Status != "pending" {
			status = http.StatusBadRequest
			return fmt.Errorf("task status %s", dt.Status)
		}
		var snap engine.Snapshot
		if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
			return err
		}
		missing, err := missingRequiredSteps(dt.Snapshot, ev)
		if err != nil {
			status = http.StatusBadRequest
			return err
		}
		if missing != "" {
			status = http.StatusBadRequest
			return fmt.Errorf("还差：%s", missing)
		}
		duration, err := engine.AchievedDuration(db, dt.ID, snap)
		if err != nil {
			return err
		}
		if req.DurationSec > duration {
			duration = req.DurationSec
		}
		comp := store.TaskCompletion{DailyTaskID: dt.ID, KidID: dt.KidID, Evidence: ev, DurationSec: duration}
		if err := tx.Create(&comp).Error; err != nil {
			return err
		}
		mode := snap.ConfirmMode
		if mode == "" { // 仅缺失字段的旧快照从模板兼容。
			var tpl store.TaskTemplate
			if err := tx.First(&tpl, dt.TemplateID).Error; err != nil {
				return err
			}
			mode = tpl.ConfirmMode
		}
		// 无论新提交的确认模式是否仍为 AI，都取消旧提交尚未完成的审核。
		if err := tx.Model(&store.AIReviewJob{}).Where("daily_task_id=? AND status IN ('queued','running')", dt.ID).
			Updates(map[string]any{"status": "canceled", "stage": "canceled", "finished_at": time.Now(), "lease_token": "", "lease_expires_at": nil}).Error; err != nil {
			return err
		}
		if confirmModeContains(mode, 'a') {
			extraBase := 0
			if snap.ExtraMinutes > 0 && req.ExtraSec >= snap.ExtraMinutes*60 {
				extraBase = req.ExtraSec / (snap.ExtraMinutes * 60) * snap.ExtraStars
				if snap.ExtraCap > 0 && extraBase > snap.ExtraCap {
					extraBase = snap.ExtraCap
				}
			}
			now := time.Now()
			if err := tx.Model(&store.TaskCompletion{}).Where("id=?", comp.ID).
				Updates(map[string]any{"confirm_status": "auto", "confirmed_at": &now}).Error; err != nil {
				return err
			}
			if err := engine.Settle(db, dt.ID, extraBase); err != nil {
				return err
			}
			result = gin.H{"settled": true}
			return nil
		}
		if confirmModeContains(mode, 'b') {
			// 只入队，所有 ASR/LLM 网络调用由 worker 在事务外执行。
			job, err := newReviewJob(tx, dt, comp)
			if err != nil {
				return err
			}
			if err := tx.Create(&job).Error; err != nil {
				return err
			}
			result = gin.H{"submitted": true, "ai_queued": true, "completion_id": comp.ID, "waiting": "parent_confirm"}
		} else {
			result = gin.H{"waiting": "parent_confirm"}
		}
		return tx.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "submitted").Error
	})
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func confirmModeContains(mode string, ch byte) bool {
	for i := 0; i < len(mode); i++ {
		if mode[i] == ch {
			return true
		}
	}
	return false
}

func atoi64(s string) int64 {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int64(c-'0')
	}
	return v
}

// ===== 家长端：确认队列 =====

type confirmItem struct {
	ID            int64         `gorm:"column:id" json:"id"` // 兼容旧前端：daily_task_id
	DailyTaskID   int64         `gorm:"column:daily_task_id" json:"daily_task_id"`
	CompletionID  int64         `gorm:"column:completion_id" json:"completion_id"`
	KidName       string        `gorm:"column:kid_name" json:"kid_name"`
	Date          string        `gorm:"column:date" json:"date"`
	Snapshot      string        `gorm:"column:snapshot" json:"snapshot"`
	Duration      int           `gorm:"column:duration_sec" json:"duration_sec"`
	ReviewResult  string        `gorm:"column:review_result" json:"review_result"`
	Evidence      string        `gorm:"column:evidence" json:"evidence"`
	SubmittedAt   time.Time     `gorm:"column:submitted_at" json:"submitted_at"`
	ConfirmStatus string        `gorm:"column:confirm_status" json:"confirm_status"`
	ConfirmedAt   *time.Time    `gorm:"column:confirmed_at" json:"confirmed_at"`
	RejectReason  string        `gorm:"column:reject_reason" json:"reject_reason"`
	AIReview      *aiReviewView `gorm:"-" json:"ai_review"`
}

const confirmSelect = `SELECT d.id, d.id AS daily_task_id, p.id AS completion_id,
	COALESCE(k.name,'') AS kid_name, d.date, COALESCE(j.snapshot,d.snapshot) AS snapshot,
	p.duration_sec, COALESCE(p.review_result,'') AS review_result, p.evidence, p.submitted_at,
	p.confirm_status, p.confirmed_at, COALESCE(p.reject_reason,'') AS reject_reason
	FROM task_completion p JOIN daily_task d ON d.id=p.daily_task_id
	JOIN kid k ON k.id=d.kid_id LEFT JOIN ai_review_job j ON j.completion_id=p.id `

// GET /api/admin/confirm/list 保留兼容字段，只展示最新待审和 AI 拒绝历史。
func (s *Server) confirmList(c *gin.Context) {
	items, rejected := []confirmItem{}, []confirmItem{}
	err := s.DB.Raw(confirmSelect+`WHERE d.status='submitted' AND p.confirm_status='pending' AND k.family_id=?
		AND NOT EXISTS (SELECT 1 FROM task_completion newer WHERE newer.daily_task_id=d.id AND newer.id>p.id)
		ORDER BY p.submitted_at DESC,p.id DESC LIMIT 50`, famOf(c)).Scan(&items).Error
	if err == nil {
		err = s.DB.Raw(confirmSelect+`WHERE p.confirm_status='rejected' AND p.review_result<>'' AND k.family_id=?
			ORDER BY p.submitted_at DESC,p.id DESC LIMIT 20`, famOf(c)).Scan(&rejected).Error
	}
	if err == nil {
		err = attachReviewViews(s.DB.DB, items)
	}
	if err == nil {
		err = attachReviewViews(s.DB.DB, rejected)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "rejected": rejected})
}

// POST /api/admin/confirm/:id  { approve: bool, reason }
func (s *Server) confirmTask(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Approve      bool   `json:"approve"`
		Reason       string `json:"reason"`
		CompletionID int64  `json:"completion_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusInternalServerError
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		db := &store.DB{DB: tx}
		var dt store.DailyTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND kid_id IN (SELECT id FROM kid WHERE family_id=?)", id, famOf(c)).First(&dt).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				status = http.StatusNotFound
			}
			return err
		}
		if dt.Status != "submitted" {
			status = http.StatusBadRequest
			return fmt.Errorf("task status %s", dt.Status)
		}
		var latest store.TaskCompletion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("daily_task_id=?", dt.ID).Order("id DESC").First(&latest).Error; err != nil {
			return err
		}
		if latest.ConfirmStatus != "pending" || (req.CompletionID != 0 && req.CompletionID != latest.ID) {
			status = http.StatusConflict
			return fmt.Errorf("提交已更新或已处理，请刷新")
		}
		// 旧 submitted 空结果记录在提前人审时也要明确展示 canceled。
		var job store.AIReviewJob
		if err := tx.Where("completion_id=?", latest.ID).First(&job).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			var snap engine.Snapshot
			if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
				return err
			}
			mode := snap.ConfirmMode
			if mode == "" {
				var tpl store.TaskTemplate
				if err := tx.First(&tpl, dt.TemplateID).Error; err != nil {
					return err
				}
				mode = tpl.ConfirmMode
			}
			if confirmModeContains(mode, 'b') && latest.ReviewResult == "" {
				var err error
				job, err = newReviewJob(tx, dt, latest)
				if err != nil {
					return err
				}
				job.Status, job.Stage = "canceled", "canceled"
				now := time.Now()
				job.FinishedAt = &now
				if err := tx.Create(&job).Error; err != nil {
					return err
				}
			}
		}
		if err := cancelReviewJob(tx, latest.ID); err != nil {
			return err
		}
		now := time.Now()
		confirmStatus := "rejected"
		if req.Approve {
			confirmStatus = "approved"
		}
		cols := map[string]any{"confirm_status": confirmStatus, "confirmed_at": &now}
		if !req.Approve {
			cols["reject_reason"] = req.Reason
		}
		if err := tx.Model(&store.TaskCompletion{}).Where("id=? AND confirm_status='pending'", latest.ID).
			Updates(cols).Error; err != nil {
			return err
		}
		// 猫的留言与审核同事务：审核成功但留言丢了，孩子就永远看不到反馈。
		// 只在家长确认后写，避免"AI 说很棒、家长却驳回"的矛盾。
		verdict := verdictOf(latest.ReviewResult)
		kind, content, score, highlights := kidMessageApproved, kidApprovedContent(verdict), verdict.Score, verdict.Highlights
		if !req.Approve {
			kind, content, score, highlights = kidMessageRejected, kidRejectedContent(req.Reason), 0, nil
		}
		if err := writeKidMessage(tx, dt.KidID, latest.ID, kind, snapshotName(dt.Snapshot), content, score, highlights); err != nil {
			return err
		}
		if req.Approve {
			return engine.Settle(db, dt.ID, 0)
		}
		if err := tx.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "pending").Error; err != nil {
			return err
		}
		db.AddEvent(dt.KidID, "task_rejected", map[string]any{"task_id": dt.ID, "reason": req.Reason})
		return nil
	})
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if req.Approve {
		c.JSON(http.StatusOK, gin.H{"settled": true})
	} else {
		c.JSON(http.StatusOK, gin.H{"rejected": true})
	}
}

// GET /api/admin/ledger 家庭星星流水（join 孩子名）
func (s *Server) ledger(c *gin.Context) {
	var items []ledgerItem
	if err := s.DB.Raw(`SELECT l.id, COALESCE(k.name,'') AS kid_name, l.date,
		COALESCE(l.daily_task_id,0), l.source, l.base, l.date_multiplier, l.streak_multiplier, l.final, l.created_at
		FROM settlement_ledger l JOIN kid k ON k.id=l.kid_id
		WHERE k.family_id=? ORDER BY l.id DESC LIMIT 50`, famOf(c)).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type ledgerItem struct {
	ID               int64     `json:"id"`
	KidName          string    `json:"kid_name"`
	Date             string    `json:"date"`
	DailyTaskID      int64     `json:"daily_task_id"`
	Source           string    `json:"source"`
	Base             int       `json:"base"`
	DateMultiplier   float64   `json:"date_multiplier"`
	StreakMultiplier float64   `json:"streak_multiplier"`
	Final            int       `json:"final"`
	CreatedAt        time.Time `json:"created_at"`
}

// GET /api/admin/ledger/:kid_id 单孩子流水
func (s *Server) ledgerKid(c *gin.Context) {
	var items []store.SettlementLedger
	if err := s.DB.Where("kid_id=?", c.Param("kid_id")).Order("id DESC").Limit(50).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// missingRequiredSteps 必须步骤缺证据检查（返回缺失提示语，空=齐全）
func missingRequiredSteps(snapshot, evidenceJSON string) (string, error) {
	var snap engine.Snapshot
	if err := json.Unmarshal([]byte(snapshot), &snap); err != nil {
		return "", fmt.Errorf("任务快照无效: %w", err)
	}
	steps, err := engine.ParseSteps(engine.NormalizeSteps(snap.EvidenceSteps, snap.EvidenceType))
	if err != nil {
		return "", err
	}
	var evList []*struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if !strings.HasPrefix(strings.TrimSpace(evidenceJSON), "[") {
		return "", fmt.Errorf("evidence 必须是 JSON 数组")
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &evList); err != nil {
		return "", fmt.Errorf("evidence: %w", err)
	}
	got := map[string]int{}
	for _, e := range evList {
		if e == nil {
			return "", fmt.Errorf("evidence 数组元素必须是对象")
		}
		if strings.TrimSpace(e.Path) != "" {
			got[e.Type]++
		}
	}
	var missing []string
	for _, st := range steps {
		if !st.Required {
			continue
		}
		if got[st.Kind] == 0 {
			missing = append(missing, st.Label)
		} else {
			got[st.Kind]--
		}
	}
	return strings.Join(missing, "、"), nil
}
