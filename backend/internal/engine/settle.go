package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"meowquest/backend/internal/store"
)

// Settle 结算：奖励 = base × 日期倍数 × 连击系数，双记账入星星
// 调用时机：自动确认(a) / 家长通过(c) / AI 验收通过(b)
func Settle(db *store.DB, dailyTaskID int64, extraBase int) error {
	var dt store.DailyTask
	if err := db.First(&dt, dailyTaskID).Error; err != nil {
		return err
	}
	if dt.Status != "pending" && dt.Status != "submitted" {
		return fmt.Errorf("task not settleable, status=%s", dt.Status)
	}
	var tpl store.TaskTemplate
	if err := db.First(&tpl, dt.TemplateID).Error; err != nil {
		return err
	}

	var snap Snapshot
	json.Unmarshal([]byte(dt.Snapshot), &snap)

	// 当日连击：当日已有 approved 任务则不重复推进
	petRow, err := db.Tick()
	if err != nil {
		return err
	}
	streak := petRow.Streak
	var settledToday int64
	db.Model(&store.DailyTask{}).Where("`date`=? AND status='approved'", time.Now().Format("2006-01-02")).Count(&settledToday)
	if tpl.StreakEnabled == 1 && settledToday == 0 {
		streak++
		db.UpdatePet(map[string]any{"streak": streak})
	}
	streakMul := 1 + 0.1*float64(streak)
	if streakMul > 1.5 {
		streakMul = 1.5
	}

	dateMul := DateMultiplier(db, time.Now())
	base := snap.BaseScore + extraBase
	if err := db.AddStars("task", base, dateMul, streakMul, dailyTaskID); err != nil {
		return err
	}

	if err := db.Model(&store.DailyTask{}).Where("id=?", dailyTaskID).Update("status", "approved").Error; err != nil {
		return err
	}
	db.AddEvent("task_done", map[string]any{
		"task": snap.Name, "base": base, "date_mul": dateMul, "streak_mul": streakMul,
	})
	return checkLevelUp(db, petRow)
}

// checkLevelUp 升级检测：earned_total 阈值曲线 100 × 1.4^(lv-1)
func checkLevelUp(db *store.DB, row *store.Pet) error {
	if row == nil {
		return nil
	}
	var pet store.Pet
	if err := db.First(&pet).Error; err != nil {
		return err
	}
	level := pet.Level
	for {
		need := levelThreshold(level)
		if pet.EarnedTotal >= need {
			level++
			db.AddEvent("levelup", map[string]any{"level": level})
		} else {
			break
		}
	}
	if level != pet.Level {
		return db.UpdatePet(map[string]any{"level": level})
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

// CloseDay 日收尾（00:05 物化后执行）：断签评估 + 超宽限作废
func CloseDay(db *store.DB) {
	yStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// 断签：昨日有 streak_enabled 任务未完成（非 approved/skipped）→ 清零
	var broken int64
	db.Raw(`SELECT COUNT(*) FROM daily_task d JOIN task_template t ON t.id=d.template_id
		WHERE d.date=? AND t.streak_enabled=1 AND d.status NOT IN ('approved','skipped')`, yStr).Scan(&broken)
	if broken > 0 {
		var pet store.Pet
		if err := db.First(&pet).Error; err == nil && pet.Streak > 0 {
			db.UpdatePet(map[string]any{"streak": 0})
			db.AddEvent("streak_broken", map[string]any{"was": pet.Streak})
			log.Printf("[closeday] %s streak broken (was %d)", yStr, pet.Streak)
		}
	}

	// 48h 仍未确认的 submitted → expired
	dayBefore := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	db.Model(&store.DailyTask{}).Where("`date`=? AND status='submitted'", dayBefore).Update("status", "expired")
}
