package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"meowquest/backend/internal/store"
)

// Snapshot 物化时的模板快照（结算可追溯，改历史规则不影响已生成任务）
type Snapshot struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Category     string `json:"category"`
	GoalType     string `json:"goal_type"`
	GoalValue    int    `json:"goal_value"`
	EvidenceType string `json:"evidence_type"`
	ConfirmMode  string `json:"confirm_mode"`
	BaseScore    int    `json:"base_score"`
	ExtraMinutes int    `json:"extra_minutes"`
	ExtraStars   int    `json:"extra_stars"`
	ExtraCap     int    `json:"extra_cap"`
	DateMode     string `json:"date_mode"`
	Weekdays     string `json:"weekdays"`
	WeeklyQuota  int    `json:"weekly_quota"`
}

// Materialize 物化指定日期（遍历所有孩子：模板×指派 → daily_task(kid_id)）
// 幂等：UNIQUE(date, kid_id, template_id)
func Materialize(db *store.DB, date time.Time) error {
	dateStr := date.Format("2006-01-02")
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := date.AddDate(0, 0, -(weekday - 1))
	weekStart := monday.Format("2006-01-02")
	weekEnd := monday.AddDate(0, 0, 7).Format("2006-01-02")

	var kids []store.Kid
	if err := db.Find(&kids).Error; err != nil {
		return err
	}
	for _, kid := range kids {
		if err := materializeKid(db, kid, dateStr, weekday, weekStart, weekEnd); err != nil {
			return fmt.Errorf("kid %d: %w", kid.ID, err)
		}
	}
	return nil
}

func materializeKid(db *store.DB, kid store.Kid, dateStr string, weekday int, weekStart, weekEnd string) error {
	// 该孩子当日请假
	var leaveCount int64
	db.Model(&store.LeaveRecord{}).Where("`date`=? AND kid_id=?", dateStr, kid.ID).Count(&leaveCount)
	isLeave := leaveCount > 0

	templates, err := db.TemplatesOfKid(kid.ID)
	if err != nil {
		return err
	}
	for _, t := range templates {
		sch := t.Schedule
		if sch == nil {
			continue
		}
		if sch.ValidFrom != nil && dateStr < *sch.ValidFrom {
			continue
		}
		if sch.ValidTo != nil && dateStr > *sch.ValidTo {
			continue
		}
		hit := false
		switch sch.DateMode {
		case "weekly_quota":
			var done int64
			db.Model(&store.DailyTask{}).
				Where("kid_id=? AND template_id=? AND `date`>=? AND `date`<? AND status='approved'",
					kid.ID, t.ID, weekStart, weekEnd).Count(&done)
			hit = sch.WeeklyQuota > 0 && done < int64(sch.WeeklyQuota)
		default: // weekday_fixed
			for _, d := range splitWeekdays(sch.Weekdays) {
				if d == weekday {
					hit = true
					break
				}
			}
		}
		if !hit {
			continue
		}
		snap := Snapshot{ID: t.ID, Name: t.Name, Icon: t.Icon, Category: t.Category,
			GoalType: t.GoalType, GoalValue: t.GoalValue, EvidenceType: t.EvidenceType,
			ConfirmMode: t.ConfirmMode, BaseScore: t.BaseScore,
			ExtraMinutes: t.ExtraMinutes, ExtraStars: t.ExtraStars, ExtraCap: t.ExtraCap,
			DateMode: sch.DateMode, Weekdays: sch.Weekdays, WeeklyQuota: sch.WeeklyQuota}
		snapJSON, _ := json.Marshal(snap)
		var quotaDone int64
		db.Model(&store.DailyTask{}).
			Where("kid_id=? AND template_id=? AND `date`>=? AND `date`<? AND status='approved'",
				kid.ID, t.ID, weekStart, weekEnd).Count(&quotaDone)
		status := "pending"
		if isLeave {
			status = "skipped"
		}
		dt := store.DailyTask{Date: dateStr, KidID: kid.ID, TemplateID: t.ID,
			Snapshot: string(snapJSON), Status: status, QuotaDone: int(quotaDone)}
		res := db.Where(store.DailyTask{Date: dateStr, KidID: kid.ID, TemplateID: t.ID}).FirstOrCreate(&dt)
		if res.Error != nil {
			return fmt.Errorf("materialize %s tpl %d: %w", dateStr, t.ID, res.Error)
		}
		if isLeave && res.RowsAffected == 0 {
			db.Model(&store.DailyTask{}).
				Where("`date`=? AND kid_id=? AND template_id=? AND status='pending'", dateStr, kid.ID, t.ID).
				Update("status", "skipped")
		}
	}
	return nil
}

// DateMultiplier 当日倍数（全局策略：周末/节假日）
func DateMultiplier(db *store.DB, date time.Time) float64 {
	dateStr := date.Format("2006-01-02")
	var rp store.RewardPolicy
	if err := db.Where("match_type='date' AND match_value=?", dateStr).First(&rp).Error; err == nil && rp.Multiplier > 0 {
		return rp.Multiplier
	}
	typ := ""
	wd := int(date.Weekday())
	if wd == 0 || wd == 6 {
		typ = "weekend"
	} else {
		var n int64
		db.Model(&store.Holiday{}).Where("`date`=?", dateStr).Count(&n)
		if n > 0 {
			typ = "holiday"
		}
	}
	if typ != "" {
		var rp2 store.RewardPolicy
		if err := db.Where("match_type='date_type' AND match_value=?", typ).First(&rp2).Error; err == nil && rp2.Multiplier > 0 {
			return rp2.Multiplier
		}
	}
	return 1
}

// StartCron 每日 00:05 物化 + 启动补物化
func StartCron(db *store.DB) {
	if err := Materialize(db, time.Now()); err != nil {
		log.Printf("[materialize] today: %v", err)
	}
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, now.Location())
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			time.Sleep(next.Sub(now))
			if err := Materialize(db, time.Now()); err != nil {
				log.Printf("[materialize] cron: %v", err)
			}
			CloseDay(db)
		}
	}()
}

func splitWeekdays(s string) []int {
	var out []int
	cur, has := 0, false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			cur = cur*10 + int(c-'0')
			has = true
		} else if has {
			out = append(out, cur)
			cur, has = 0, false
		}
	}
	if has {
		out = append(out, cur)
	}
	return out
}
