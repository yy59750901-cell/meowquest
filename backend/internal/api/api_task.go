package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

// ===== 家长端：任务配置 =====

type taskReq struct {
	Name          string `json:"name" binding:"required"`
	Icon          string `json:"icon"`
	Category      string `json:"category"`
	GoalType      string `json:"goal_type"`
	GoalValue     int    `json:"goal_value"`
	EvidenceType  string `json:"evidence_type"`
	ConfirmMode   string `json:"confirm_mode"`
	BaseScore     int    `json:"base_score"`
	StreakEnabled int    `json:"streak_enabled"`
	ExtraMinutes  int    `json:"extra_minutes"`
	ExtraStars    int    `json:"extra_stars"`
	ExtraCap      int    `json:"extra_cap"`
	Sort          int    `json:"sort"`
	Enabled       *bool  `json:"enabled"`
	DateMode      string `json:"date_mode"`
	Weekdays      string `json:"weekdays"`
	WeeklyQuota   int    `json:"weekly_quota"`
	ValidFrom     string `json:"valid_from"`
	ValidTo       string `json:"valid_to"`
}

func (r taskReq) toTemplate() store.TaskTemplate {
	enabled := 1
	if r.Enabled != nil && !*r.Enabled {
		enabled = 0
	}
	streak := r.StreakEnabled
	if streak == 0 {
		streak = 1
	}
	return store.TaskTemplate{
		Name: r.Name, Icon: defStr(r.Icon, "⭐"), Category: defStr(r.Category, "custom"),
		GoalType: defStr(r.GoalType, "free"), GoalValue: r.GoalValue,
		EvidenceType: defStr(r.EvidenceType, "none"), ConfirmMode: defStr(r.ConfirmMode, "a"),
		BaseScore: maxInt(r.BaseScore, 20), StreakEnabled: streak,
		ExtraMinutes: r.ExtraMinutes, ExtraStars: r.ExtraStars, ExtraCap: r.ExtraCap,
		Sort: r.Sort, Enabled: enabled,
	}
}

func (r taskReq) toSchedule(tid int64) store.TaskSchedule {
	var vf, vt *string
	if r.ValidFrom != "" {
		vf = &r.ValidFrom
	}
	if r.ValidTo != "" {
		vt = &r.ValidTo
	}
	return store.TaskSchedule{
		TemplateID: tid, DateMode: defStr(r.DateMode, "weekday_fixed"),
		Weekdays: defStr(r.Weekdays, "1,2,3,4,5"), WeeklyQuota: r.WeeklyQuota,
		ValidFrom: vf, ValidTo: vt,
	}
}

// POST /api/admin/task 创建（模板+调度一起），立即补物化
func (s *Server) createTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tpl := req.toTemplate()
	if err := s.DB.Create(&tpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sch := req.toSchedule(tpl.ID)
	if err := s.DB.Create(&sch).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusOK, gin.H{"id": tpl.ID, "materialize_error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": tpl.ID})
}

// PUT /api/admin/task/:id 修改（快照隔离：历史实例不受影响）
func (s *Server) updateTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tpl := req.toTemplate()
	if err := s.DB.Model(&store.TaskTemplate{}).Where("id=?", c.Param("id")).Updates(map[string]any{
		"name": tpl.Name, "icon": tpl.Icon, "category": tpl.Category,
		"goal_type": tpl.GoalType, "goal_value": tpl.GoalValue,
		"evidence_type": tpl.EvidenceType, "confirm_mode": tpl.ConfirmMode,
		"base_score": tpl.BaseScore, "streak_enabled": tpl.StreakEnabled,
		"extra_minutes": tpl.ExtraMinutes, "extra_stars": tpl.ExtraStars, "extra_cap": tpl.ExtraCap,
		"sort": tpl.Sort, "enabled": tpl.Enabled,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sch := req.toSchedule(0)
	if err := s.DB.Model(&store.TaskSchedule{}).Where("template_id=?", c.Param("id")).Updates(map[string]any{
		"date_mode": sch.DateMode, "weekdays": sch.Weekdays, "weekly_quota": sch.WeeklyQuota,
		"valid_from": sch.ValidFrom, "valid_to": sch.ValidTo,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "materialize_error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/admin/task 列表（含调度）
func (s *Server) listTasks(c *gin.Context) {
	var tpls []store.TaskTemplate
	if err := s.DB.Preload("Schedule").Order("sort,id").Find(&tpls).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tpls})
}

// POST /api/admin/reward_policy 倍数策略
func (s *Server) upsertPolicy(c *gin.Context) {
	var req struct {
		MatchType  string  `json:"match_type" binding:"required"`
		MatchValue string  `json:"match_value" binding:"required"`
		Multiplier float64 `json:"multiplier"`
		Note       string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rp := store.RewardPolicy{MatchType: req.MatchType, MatchValue: req.MatchValue}
	if err := s.DB.Where(&rp).Assign(store.RewardPolicy{Multiplier: req.Multiplier, Note: req.Note}).
		FirstOrCreate(&rp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/admin/leave 请假（当日冻结）
func (s *Server) setLeave(c *gin.Context) {
	var req struct {
		Date   string `json:"date" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lr := store.LeaveRecord{Date: req.Date, Reason: req.Reason}
	if err := s.DB.Where(&lr).Assign(lr).FirstOrCreate(&lr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.DB.Model(&store.DailyTask{}).Where("`date`=? AND status='pending'", req.Date).Update("status", "skipped")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/admin/materialize 手动物化
func (s *Server) rematerialize(c *gin.Context) {
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ===== 孩子端：今日任务 =====

// GET /api/tasks/today
func (s *Server) todayTasks(c *gin.Context) {
	date := time.Now().Format("2006-01-02")
	var tasks []store.DailyTask
	if err := s.DB.Where("`date`=?", date).Order("id").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	mul := engine.DateMultiplier(s.DB, time.Now())
	c.JSON(http.StatusOK, gin.H{"date": date, "date_multiplier": mul, "tasks": tasks})
}

// ===== 工具 =====
func defStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
