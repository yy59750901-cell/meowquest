package api

import (
	"encoding/json"
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
	ReviewPrompt  string `json:"review_prompt"` // b 模式：AI 验收指令
	EvidenceSteps string `json:"evidence_steps"` // JSON [{kind,label,required}]
	AnalysisMode  string `json:"analysis_mode"`  // judge / score
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
	KidIDs        []int64 `json:"kid_ids"` // 指派孩子（空=全部）
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
		ReviewPrompt: r.ReviewPrompt, EvidenceSteps: engine.NormalizeSteps(r.EvidenceSteps, r.EvidenceType),
		AnalysisMode: defStr(r.AnalysisMode, "judge"),
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
	tpl.FamilyID = famOf(c)
	if err := s.DB.Create(&tpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sch := req.toSchedule(tpl.ID)
	if err := s.DB.Create(&sch).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 指派：kid_ids 空 = 指派给家庭全部现有孩子
	if len(req.KidIDs) == 0 {
		kids, _ := s.DB.KidsOfFamily(famOf(c))
		for _, k := range kids {
			s.DB.AssignTask(tpl.ID, k.ID)
		}
	} else {
		for _, kidID := range req.KidIDs {
			s.DB.AssignTask(tpl.ID, kidID)
		}
	}
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusOK, gin.H{"id": tpl.ID, "materialize_error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": tpl.ID, "today_hit": s.todayHit(tpl.ID)})
}

// todayHit 今天是否有孩子命中该模板（家长创建后的即时反馈）
func (s *Server) todayHit(templateID int64) int64 {
	var n int64
	s.DB.Model(&store.DailyTask{}).Where("template_id=? AND date=?", templateID, time.Now().Format("2006-01-02")).Count(&n)
	return n
}

// PUT /api/admin/task/:id 修改（快照隔离：历史实例不受影响）
func (s *Server) updateTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tpl := req.toTemplate()
	if err := s.DB.Model(&store.TaskTemplate{}).Where("id=? AND family_id=?", c.Param("id"), famOf(c)).Updates(map[string]any{
		"name": tpl.Name, "icon": tpl.Icon, "category": tpl.Category,
		"goal_type": tpl.GoalType, "goal_value": tpl.GoalValue,
		"evidence_type": tpl.EvidenceType, "confirm_mode": tpl.ConfirmMode, "review_prompt": tpl.ReviewPrompt, "evidence_steps": tpl.EvidenceSteps, "analysis_mode": tpl.AnalysisMode,
		"base_score": tpl.BaseScore, "streak_enabled": tpl.StreakEnabled,
		"extra_minutes": tpl.ExtraMinutes, "extra_stars": tpl.ExtraStars, "extra_cap": tpl.ExtraCap,
		"sort": tpl.Sort, "enabled": tpl.Enabled,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 当天未完成的实例刷新快照：删 pending 重建（保新配置立即生效；
	// approved/submitted 保旧快照不动——结算按完成当时规则，可追溯）
	today := time.Now().Format("2006-01-02")
	s.DB.Where("template_id=? AND date=? AND status='pending'", c.Param("id"), today).
		Delete(&store.DailyTask{})

	sch := req.toSchedule(0)
	if err := s.DB.Model(&store.TaskSchedule{}).Where("template_id=?", c.Param("id")).Updates(map[string]any{
		"date_mode": sch.DateMode, "weekdays": sch.Weekdays, "weekly_quota": sch.WeeklyQuota,
		"valid_from": sch.ValidFrom, "valid_to": sch.ValidTo,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "materialize_error": err.Error(), "today_hit": s.todayHit(atoi64(c.Param("id")))})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "today_hit": s.todayHit(atoi64(c.Param("id")))})
}

// GET /api/admin/task 列表（含调度）
func (s *Server) listTasks(c *gin.Context) {
	var tpls []store.TaskTemplate
	if err := s.DB.Preload("Schedule").Where("family_id=?", famOf(c)).Order("sort,id").Find(&tpls).Error; err != nil {
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
		KidID  int64  `json:"kid_id"` // 可选：只给这个孩子请假
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kids, _ := s.DB.KidsOfFamily(famOf(c))
	targets := kids
	if req.KidID > 0 {
		targets = []store.Kid{{ID: req.KidID}}
	}
	for _, k := range targets {
		lr := store.LeaveRecord{Date: req.Date, KidID: k.ID, Reason: req.Reason}
		s.DB.Where(&lr).Assign(lr).FirstOrCreate(&lr)
		s.DB.Model(&store.DailyTask{}).Where("date=? AND kid_id=? AND status='pending'", req.Date, k.ID).Update("status", "skipped")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/admin/task/:id/assign { kid_ids: [1,2], replace: true }
func (s *Server) assignTask(c *gin.Context) {
	var req struct {
		KidIDs  []int64 `json:"kid_ids" binding:"required"`
		Replace bool    `json:"replace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tid := c.Param("id")
	var tpl store.TaskTemplate
	if err := s.DB.Where("id=? AND family_id=?", tid, famOf(c)).First(&tpl).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if req.Replace {
		s.DB.Where("template_id=?", tpl.ID).Delete(&store.TaskAssign{})
	}
	for _, kidID := range req.KidIDs {
		s.DB.AssignTask(tpl.ID, kidID)
	}
	if err := engine.Materialize(s.DB, time.Now()); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "materialize_error": err.Error()})
		return
	}
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
	if err := s.DB.Where("date=? AND kid_id=?", date, kidOf(c)).Order("id").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// had_duration：计时任务曾有达标记录（AI 退回材料后重做免重新计时，只补材料）
	hadDur := map[int64]bool{}
	for _, t := range tasks {
		if t.Status == "pending" {
			var snap struct {
				GoalType  string `json:"goal_type"`
				GoalValue int    `json:"goal_value"`
			}
			json.Unmarshal([]byte(t.Snapshot), &snap)
			if snap.GoalType == "duration" && snap.GoalValue > 0 {
				var n int64
				s.DB.Model(&store.TaskCompletion{}).
					Where("daily_task_id=? AND duration_sec>=?", t.ID, snap.GoalValue*60).Count(&n)
				if n > 0 {
					hadDur[t.ID] = true
				}
			}
		}
	}
	mul := engine.DateMultiplier(s.DB, time.Now())
	c.JSON(http.StatusOK, gin.H{"date": date, "date_multiplier": mul, "tasks": tasks, "had_duration": hadDur})
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
