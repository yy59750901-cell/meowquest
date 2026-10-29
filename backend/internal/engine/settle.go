package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"meowquest/backend/internal/store"
)

// Settle 结算（per-kid）：奖励 = base × 日期倍数 × 连击系数，双记账入星星
func Settle(db *store.DB, dailyTaskID int64, extraBase int) error {
	var dt store.DailyTask
	if err := db.First(&dt, dailyTaskID).Error; err != nil {
		return err
	}
	if dt.Status != "pending" && dt.Status != "submitted" {
		return fmt.Errorf("task not settleable, status=%s", dt.Status)
	}
	kidID := dt.KidID

	var snap Snapshot
	json.Unmarshal([]byte(dt.Snapshot), &snap)

	// 当日连击（per-kid）：当日已有 approved 任务则不重复推进
	petRow, err := db.Tick(kidID)
	if err != nil {
		return err
	}
	if petRow == nil {
		return fmt.Errorf("no pet for kid %d", kidID)
	}
	streak := petRow.Streak
	var settledToday int64
	db.Model(&store.DailyTask{}).
		Where("kid_id=? AND date=? AND status='approved'", kidID, time.Now().Format("2006-01-02")).
		Count(&settledToday)
	if settledToday == 0 { // 首个结算任务推进连击（streak_enabled 判断看模板）
		var tpl store.TaskTemplate
		if err := db.First(&tpl, dt.TemplateID).Error; err == nil && tpl.StreakEnabled == 1 {
			streak++
			db.UpdatePet(kidID, map[string]any{"streak": streak})
		}
	}
	streakMul := 1 + 0.1*float64(streak)
	if streakMul > 1.5 {
		streakMul = 1.5
	}

	dateMul := DateMultiplier(db, time.Now())
	base := snap.BaseScore + extraBase
	if err := db.AddStars(kidID, "task", base, dateMul, streakMul, dailyTaskID); err != nil {
		return err
	}

	if err := db.Model(&store.DailyTask{}).Where("id=?", dailyTaskID).Update("status", "approved").Error; err != nil {
		return err
	}
	db.AddEvent(kidID, "task_done", map[string]any{
		"task": snap.Name, "base": base, "date_mul": dateMul, "streak_mul": streakMul,
	})
	return checkLevelUp(db, kidID, petRow)
}

// checkLevelUp 升级检测（per-kid）：earned_total 阈值曲线 100 × 1.4^(lv-1)
func checkLevelUp(db *store.DB, kidID int64, row *store.Pet) error {
	if row == nil {
		return nil
	}
	var pet store.Pet
	if err := db.Where("kid_id=?", kidID).First(&pet).Error; err != nil {
		return err
	}
	level := pet.Level
	for {
		need := levelThreshold(level)
		if pet.EarnedTotal >= need {
			level++
			db.AddEvent(kidID, "levelup", map[string]any{"level": level})
		} else {
			break
		}
	}
	if level != pet.Level {
		return db.UpdatePet(kidID, map[string]any{"level": level})
	}
	return nil
}

func levelThreshold(level int64) int64 {
	need := 100.0
	for i := int64(1); i < level; i++ {
		need *= 1.4
	}
	return int64(need)
}

// CloseDay 日收尾（遍历所有孩子）：断签评估 + 超宽限作废
func CloseDay(db *store.DB) {
	yStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	var kids []store.Kid
	db.Find(&kids)
	for _, kid := range kids {
		// 断签：昨日有 streak_enabled 指派任务未完成 → 清零
		var broken int64
		db.Raw(`SELECT COUNT(*) FROM daily_task d JOIN task_template t ON t.id=d.template_id
			WHERE d.date=? AND d.kid_id=? AND t.streak_enabled=1 AND d.status NOT IN ('approved','skipped')`,
			yStr, kid.ID).Scan(&broken)
		if broken > 0 {
			var pet store.Pet
			if err := db.Where("kid_id=?", kid.ID).First(&pet).Error; err == nil && pet.Streak > 0 {
				db.UpdatePet(kid.ID, map[string]any{"streak": 0})
				db.AddEvent(kid.ID, "streak_broken", map[string]any{"was": pet.Streak})
				log.Printf("[closeday] %s kid %d streak broken (was %d)", yStr, kid.ID, pet.Streak)
			}
		}
	}

	// 48h 仍未确认的 submitted → expired（全家庭）
	dayBefore := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	db.Model(&store.DailyTask{}).Where("date=? AND status='submitted'", dayBefore).Update("status", "expired")
}
