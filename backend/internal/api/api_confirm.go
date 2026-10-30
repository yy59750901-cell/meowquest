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
			reviewer := *s
			reviewer.DB = db
			v, rerr := reviewer.runReview(dt, EvidenceRow{Evidence: ev, DurationSec: duration})
			if rerr != nil {
				result = gin.H{"waiting": "parent_confirm", "ai": "unavailable"}
			} else {
				resultJSON, err := json.Marshal(v)
				if err != nil {
					return err
				}
				cols := map[string]any{"review_result": string(resultJSON)}
				if !v.Pass {
					now := time.Now()
					cols["confirm_status"], cols["confirmed_at"], cols["reject_reason"] = "rejected", &now, "AI："+v.Reason
				}
				if err := tx.Model(&store.TaskCompletion{}).Where("id=?", comp.ID).Updates(cols).Error; err != nil {
					return err
				}
				db.AddEvent(dt.KidID, "ai_review", map[string]any{"task": snap.Name, "pass": v.Pass, "score": v.Score})
				if !v.Pass {
					result = gin.H{"redo": true, "ai_pass": false, "ai_reason": v.Reason, "kid_feedback": v.KidFeedback}
					return nil
				}
				result = gin.H{"waiting": "parent_confirm", "ai_pass": true, "ai_reason": v.Reason,
					"ai_score": v.Score, "kid_feedback": v.KidFeedback}
			}
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
	ID           int64     `json:"id"`
	KidName      string    `json:"kid_name"`
	Date         string    `json:"date"`
	Snapshot     string    `json:"snapshot"` // PG Scan 兼容：string（前端 parse）
	Duration     int       `json:"duration_sec"`
	ReviewResult string    `json:"review_result"`
	Evidence     string    `json:"evidence"` // PG Scan 兼容：string
	SubmittedAt  time.Time `json:"submitted_at"`
}

// GET /api/admin/confirm/list 待确认任务（含证据/时长/快照）
func (s *Server) confirmList(c *gin.Context) {
	var items []confirmItem
	if err := s.DB.Raw(`SELECT d.id, COALESCE(k.name,'') AS kid_name, d.date, d.snapshot, p.duration_sec, COALESCE(p.review_result,'') AS review_result, p.evidence, p.submitted_at
		FROM daily_task d
		JOIN task_completion p ON p.daily_task_id=d.id
		JOIN kid k ON k.id = d.kid_id
		WHERE d.status='submitted' AND p.confirm_status='pending' AND k.family_id=?
		ORDER BY p.submitted_at DESC LIMIT 50`, famOf(c)).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// AI 初审退回历史（孩子可重做覆盖；家长可见没通过的原因）
	var rejected []confirmItem
	s.DB.Raw(`SELECT d.id, COALESCE(k.name,'') AS kid_name, d.date, d.snapshot, p.duration_sec,
			COALESCE(p.review_result,'') AS review_result, p.evidence, p.submitted_at
		FROM daily_task d
		JOIN task_completion p ON p.daily_task_id=d.id AND p.confirm_status='rejected'
		JOIN kid k ON k.id = d.kid_id
		WHERE p.review_result != '' AND k.family_id=?
		ORDER BY p.submitted_at DESC LIMIT 20`, famOf(c)).Scan(&rejected)

	c.JSON(http.StatusOK, gin.H{"items": items, "rejected": rejected})
}

// POST /api/admin/confirm/:id  { approve: bool, reason }
func (s *Server) confirmTask(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
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
		now := time.Now()
		confirmStatus := "rejected"
		if req.Approve {
			confirmStatus = "approved"
		}
		cols := map[string]any{"confirm_status": confirmStatus, "confirmed_at": &now}
		if !req.Approve {
			cols["reject_reason"] = req.Reason
		}
		if err := tx.Model(&store.TaskCompletion{}).Where("daily_task_id=? AND confirm_status='pending'", dt.ID).
			Updates(cols).Error; err != nil {
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
