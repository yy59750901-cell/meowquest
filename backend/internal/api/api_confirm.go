package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

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
	var dt store.DailyTask
	if err := s.DB.First(&dt, "id=?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if dt.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task status " + dt.Status})
		return
	}
	var tpl store.TaskTemplate
	if err := s.DB.First(&tpl, dt.TemplateID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ev := "[]"
	if len(req.Evidence) > 0 {
		ev = string(req.Evidence)
	}
	comp := store.TaskCompletion{DailyTaskID: dt.ID, Evidence: ev, DurationSec: req.DurationSec}
	if err := s.DB.Create(&comp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var snap engine.Snapshot
	json.Unmarshal([]byte(dt.Snapshot), &snap)

	// 自动确认（a）
	if confirmModeContains(tpl.ConfirmMode, 'a') {
		extraBase := 0
		if snap.ExtraMinutes > 0 && req.ExtraSec >= snap.ExtraMinutes*60 {
			n := req.ExtraSec / (snap.ExtraMinutes * 60) * snap.ExtraStars
			if snap.ExtraCap > 0 && n > snap.ExtraCap {
				n = snap.ExtraCap
			}
			extraBase = n
		}
		now := time.Now()
		s.DB.Model(&store.TaskCompletion{}).Where("id=?", comp.ID).
			Updates(map[string]any{"confirm_status": "auto", "confirmed_at": &now})
		if err := engine.Settle(s.DB, dt.ID, extraBase); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"settled": true})
		return
	}
	// 家长确认（c）→ 待确认
	s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "submitted")
	c.JSON(http.StatusOK, gin.H{"waiting": "parent_confirm"})
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
	ID          int64           `json:"id"`
	Date        string          `json:"date"`
	Snapshot    json.RawMessage `json:"snapshot"`
	Duration    int             `json:"duration_sec"`
	Evidence    json.RawMessage `json:"evidence"`
	SubmittedAt time.Time       `json:"submitted_at"`
}

// GET /api/admin/confirm/list 待确认任务（含证据/时长/快照）
func (s *Server) confirmList(c *gin.Context) {
	var items []confirmItem
	if err := s.DB.Raw(`SELECT d.id, d.date, d.snapshot, p.duration_sec, p.evidence, p.submitted_at
		FROM daily_task d JOIN task_completion p ON p.daily_task_id=d.id
		WHERE d.status='submitted' AND p.confirm_status='pending'
		ORDER BY p.submitted_at DESC LIMIT 50`).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
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
	var dt store.DailyTask
	if err := s.DB.First(&dt, "id=?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if dt.Status != "submitted" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task status " + dt.Status})
		return
	}
	if req.Approve {
		now := time.Now()
		s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=? AND confirm_status='pending'", dt.ID).
			Updates(map[string]any{"confirm_status": "approved", "confirmed_at": &now})
		if err := engine.Settle(s.DB, dt.ID, 0); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"settled": true})
		return
	}
	// 驳回：退回未完成，可重新提交（不限次数）
	now := time.Now()
	s.DB.Model(&store.TaskCompletion{}).Where("daily_task_id=? AND confirm_status='pending'", dt.ID).
		Updates(map[string]any{"confirm_status": "rejected", "confirmed_at": &now, "reject_reason": req.Reason})
	s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "pending")
	s.DB.AddEvent("task_rejected", map[string]any{"task_id": dt.ID, "reason": req.Reason})
	c.JSON(http.StatusOK, gin.H{"rejected": true})
}

// GET /api/admin/ledger 星星流水（家长看板）
func (s *Server) ledger(c *gin.Context) {
	var items []store.SettlementLedger
	if err := s.DB.Order("id DESC").Limit(50).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
