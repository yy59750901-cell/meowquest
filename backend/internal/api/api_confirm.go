package api

import (
	"strings"
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

	// 证据步骤校验：必须步骤缺证据 → 引导孩子补齐
	if missing := missingRequiredSteps(dt.Snapshot, ev); missing != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "还差：" + missing})
		return
	}
	comp := store.TaskCompletion{DailyTaskID: dt.ID, KidID: dt.KidID, Evidence: ev, DurationSec: req.DurationSec}
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
	// AI 验收（b）：判定 → 通过进家长终审队列；不过退回重做（无限重试）
	if confirmModeContains(tpl.ConfirmMode, 'b') {
		v, rerr := s.runReview(dt, EvidenceRow{Evidence: ev, DurationSec: req.DurationSec})
		if rerr != nil {
			// AI 走神：降级为普通家长确认（不让孩子的努力卡在机器故障上）
			s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "submitted")
			c.JSON(http.StatusOK, gin.H{"waiting": "parent_confirm", "ai": "unavailable"})
			return
		}
		resultJSON, _ := json.Marshal(v)
		if v.Pass {
			s.DB.Model(&store.TaskCompletion{}).Where("id=?", comp.ID).
				Update("review_result", string(resultJSON))
			s.DB.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", "submitted")
			s.DB.AddEvent(dt.KidID, "ai_review", map[string]any{"task": snap.Name, "pass": true, "score": v.Score})
			c.JSON(http.StatusOK, gin.H{"waiting": "parent_confirm", "ai_pass": true, "ai_reason": v.Reason,
				"ai_score": v.Score, "kid_feedback": v.KidFeedback})
		} else {
			// 退回重做：completion 标记 rejected，任务回 pending
			now := time.Now()
			s.DB.Model(&store.TaskCompletion{}).Where("id=?", comp.ID).
				Updates(map[string]any{"review_result": string(resultJSON), "confirm_status": "rejected", "confirmed_at": &now, "reject_reason": "AI：" + v.Reason})
			s.DB.AddEvent(dt.KidID, "ai_review", map[string]any{"task": snap.Name, "pass": false})
			c.JSON(http.StatusOK, gin.H{"redo": true, "ai_pass": false, "ai_reason": v.Reason, "kid_feedback": v.KidFeedback})
		}
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
	var dt store.DailyTask
	if err := s.DB.Raw(`SELECT d.* FROM daily_task d JOIN kid k ON k.id=d.kid_id
		WHERE d.id=? AND k.family_id=?`, id, famOf(c)).Scan(&dt).Error; err != nil || dt.ID == 0 {
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
	s.DB.AddEvent(dt.KidID, "task_rejected", map[string]any{"task_id": dt.ID, "reason": req.Reason})
	c.JSON(http.StatusOK, gin.H{"rejected": true})
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
	ID          int64     `json:"id"`
	KidName     string    `json:"kid_name"`
	Date        string    `json:"date"`
	DailyTaskID int64     `json:"daily_task_id"`
	Source      string    `json:"source"`
	Base        int       `json:"base"`
	DateMultiplier float64 `json:"date_multiplier"`
	StreakMultiplier float64 `json:"streak_multiplier"`
	Final       int       `json:"final"`
	CreatedAt   time.Time `json:"created_at"`
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
func missingRequiredSteps(snapshot, evidenceJSON string) string {
	var snap struct {
		EvidenceSteps string `json:"evidence_steps"`
	}
	json.Unmarshal([]byte(snapshot), &snap)
	var steps []struct {
		Kind     string `json:"kind"`
		Label    string `json:"label"`
		Required bool   `json:"required"`
	}
	json.Unmarshal([]byte(orDefault(snap.EvidenceSteps, "[]")), &steps)
	if len(steps) == 0 {
		return "" // 旧单证据任务不校验
	}
	got := map[string]int{}
	var evList []struct {
		Type string `json:"type"`
	}
	json.Unmarshal([]byte(orDefault(evidenceJSON, "[]")), &evList)
	for _, e := range evList {
		got[e.Type]++
	}
	var missing []string
	for _, st := range steps {
		if st.Required && got[st.Kind] == 0 {
			missing = append(missing, st.Label)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return strings.Join(missing, "、")
}
