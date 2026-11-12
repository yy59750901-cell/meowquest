package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"meowquest/backend/internal/store"
)

var BusinessLocation = time.FixedZone("Asia/Shanghai", 8*3600)

func BusinessDate(now time.Time) string { return now.In(BusinessLocation).Format("2006-01-02") }

func taskDay(date string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", date, BusinessLocation)
}

func SubmissionOpen(date string, now time.Time) bool {
	day, err := taskDay(date)
	return err == nil && !now.Before(day) && now.Before(day.AddDate(0, 0, 1))
}

func ReviewOpen(date string, now time.Time) bool {
	day, err := taskDay(date)
	return err == nil && !now.Before(day) && now.Before(day.AddDate(0, 0, 2))
}

// 所有任务事实写入口先锁孩子，随后才锁实例、completion/job、pet。
// 只能在外层事务内调用；不创建隐式嵌套事务。
func LockKid(db *store.DB, kidID int64) error {
	if err := db.RequireTaskTransaction(); err != nil {
		return err
	}
	var kid store.Kid
	return db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&kid, kidID).Error
}

func ExtraReward(snap Snapshot, extraSec int) int {
	if snap.ExtraMinutes <= 0 || extraSec < snap.ExtraMinutes*60 {
		return 0
	}
	extra := extraSec / (snap.ExtraMinutes * 60) * snap.ExtraStars
	if snap.ExtraCap > 0 && extra > snap.ExtraCap {
		extra = snap.ExtraCap
	}
	return extra
}

// LockedNow 必须在孩子锁后调用。生产传时钟而非ticker采样时间；持久化
// watermark阻止晚到补扫/时钟回拨把已经判定的截止时间倒退。
func LockedNow(db *store.DB, kidID int64, clock func() time.Time) (time.Time, error) {
	if err := db.RequireTaskTransaction(); err != nil {
		return time.Time{}, err
	}
	now := clock()
	pet, err := db.AccountLocked(kidID)
	if err != nil {
		return time.Time{}, err
	}
	if pet == nil {
		return now, nil
	}
	if pet.StreakEvaluatedNS != nil && now.UnixNano() < *pet.StreakEvaluatedNS {
		now = time.Unix(0, *pet.StreakEvaluatedNS)
	}
	// BIGINT纳秒避免PG微秒/MySQL毫秒datetime精度在午夜舍入时改变截止判断。
	if pet.StreakEvaluatedNS == nil || now.UnixNano() > *pet.StreakEvaluatedNS {
		if err := db.UpdatePet(kidID, map[string]any{"streak_evaluated_ns": now.UnixNano()}); err != nil {
			return time.Time{}, err
		}
	}
	return now, nil
}

// Settle 兼容调用名；明确只供任务领域外层事务使用。
func Settle(db *store.DB, dailyTaskID int64, extraBase int) error {
	_, err := SettleWithClock(db, dailyTaskID, extraBase, time.Now)
	return err
}

// SettleAt 用于显式时间测试；生产必须传递时钟，在锁后再取时间。
func SettleAt(db *store.DB, dailyTaskID int64, extraBase int, now time.Time) (bool, error) {
	return SettleWithClock(db, dailyTaskID, extraBase, func() time.Time { return now })
}

// SettleWithClock 审批与到账分离，所有新奖励等待任务日最终结果。
func SettleWithClock(db *store.DB, dailyTaskID int64, extraBase int, clock func() time.Time) (bool, error) {
	if err := db.RequireTaskTransaction(); err != nil {
		return false, err
	}
	var dt store.DailyTask
	if err := db.First(&dt, dailyTaskID).Error; err != nil {
		return false, err
	}
	if err := LockKid(db, dt.KidID); err != nil {
		return false, err
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&dt, dailyTaskID).Error; err != nil {
		return false, err
	}
	now, err := LockedNow(db, dt.KidID, clock)
	if err != nil {
		return false, err
	}
	// 必须在写 approved 前固定基线，不能把本次审批误认为切换前已计数。
	if _, err := initBaseline(db, dt.KidID, now); err != nil {
		return false, err
	}
	if dt.Status != "approved" {
		if dt.Status != "pending" && dt.Status != "submitted" {
			return false, fmt.Errorf("task not settleable, status=%s", dt.Status)
		}
		if !ReviewOpen(dt.Date, now) {
			return false, fmt.Errorf("已超过任务日审核截止")
		}
		if err := db.Model(&store.DailyTask{}).Where("id=?", dt.ID).Updates(map[string]any{
			"status": "approved", "settlement_status": "pending", "settlement_extra": extraBase,
		}).Error; err != nil {
			return false, err
		}
	}
	if err := ReconcileKidLocked(db, dt.KidID, now); err != nil {
		return false, err
	}
	var count int64
	if err := db.Model(&store.SettlementLedger{}).Where("daily_task_id=? AND source='task'", dt.ID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// EnsureStreakBaseline 在事实变更前调用，避免首次入口是拒绝/请假时
// 把刚刚存在的宽限内待审日漏出切换范围。孩子可能尚未领养。
func EnsureStreakBaseline(db *store.DB, kidID int64, now time.Time) error {
	if err := db.RequireTaskTransaction(); err != nil {
		return err
	}
	pet, err := db.AccountLocked(kidID)
	if err != nil || pet == nil {
		return err
	}
	if pet.StreakBaseDate != "" {
		return nil
	}
	_, err = initBaseline(db, kidID, now)
	return err
}

// 切换前 approved/ledger 永不补发或重算。默认从切换日开始；仅纳入仍在
// 审核宽限内的昨日 submitted。更早实例不回放连击，按原任务日超时终结。
// 从缓存扣除回放范围内已计数日的信用，回放时按所属日正常+1；已有流水不动。
func initBaseline(db *store.DB, kidID int64, now time.Time) (*store.KidAccount, error) {
	pet, err := db.AccountLocked(kidID)
	if err != nil {
		return nil, err
	}
	if pet == nil {
		return nil, store.ErrPetNotFound
	}
	if pet.StreakBaseDate != "" {
		return pet, nil
	}
	start := BusinessDate(now)
	yesterday := now.In(BusinessLocation).AddDate(0, 0, -1).Format("2006-01-02")
	var open []store.DailyTask
	if err := db.Where("kid_id=? AND date=? AND status='submitted'", kidID, yesterday).Find(&open).Error; err != nil {
		return nil, err
	}
	for _, dt := range open {
		snap, err := streakSnapshot(dt)
		if err != nil {
			return nil, err
		}
		if *snap.StreakEnabled == 1 && ReviewOpen(dt.Date, now) {
			start = yesterday
			break
		}
	}
	var tasks []store.DailyTask
	if err := db.Where("kid_id=? AND date>=? AND date<=?", kidID, start, BusinessDate(now)).Find(&tasks).Error; err != nil {
		return nil, err
	}
	counted := map[string]bool{}
	for _, dt := range tasks {
		var n int64
		if err := db.Model(&store.SettlementLedger{}).Where("daily_task_id=? AND source='task'", dt.ID).Count(&n).Error; err != nil {
			return nil, err
		}
		snap, err := streakSnapshot(dt)
		if err != nil {
			return nil, err
		}
		if *snap.StreakEnabled == 1 && (dt.Status == "approved" || n > 0) {
			counted[dt.Date] = true
		}
	}
	pet.StreakBaseDate = start
	pet.StreakBaseValue = pet.Streak - int64(len(counted))
	if pet.StreakBaseValue < 0 {
		pet.StreakBaseValue = 0
	}
	if err := db.UpdatePet(kidID, map[string]any{"streak_base_date": start, "streak_base_value": pet.StreakBaseValue}); err != nil {
		return nil, err
	}
	for date := range counted {
		row := store.KidStreakDay{KidID: kidID, Date: date, State: "success", CountedAtBaseline: true}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return nil, err
		}
	}
	return pet, nil
}

func streakSnapshot(dt store.DailyTask) (Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal([]byte(dt.Snapshot), &snap); err != nil {
		return snap, fmt.Errorf("task %d snapshot: %w", dt.ID, err)
	}
	// 旧已提交快照不改写、不追读可变模板；缺字段采用原默认开启/普通日倍数。
	if snap.StreakEnabled == nil {
		enabled := 1
		snap.StreakEnabled = &enabled
	}
	if snap.DateMultiplier <= 0 {
		snap.DateMultiplier = 1
	}
	return snap, nil
}

func expireTasks(db *store.DB, kidID int64, now time.Time) error {
	var tasks []store.DailyTask
	if err := db.Where("kid_id=? AND status IN ('pending','submitted')", kidID).Order("date,id").Find(&tasks).Error; err != nil {
		return err
	}
	for _, dt := range tasks {
		day, err := taskDay(dt.Date)
		if err != nil {
			return err
		}
		cutoff := day.AddDate(0, 0, 1)
		if dt.Status == "submitted" {
			cutoff = day.AddDate(0, 0, 2)
		}
		if now.Before(cutoff) {
			continue
		}
		terminal := "expired"
		var rejected int64
		if err := db.Model(&store.TaskCompletion{}).Where("daily_task_id=? AND confirm_status='rejected'", dt.ID).Count(&rejected).Error; err != nil {
			return err
		}
		if dt.Status == "pending" && rejected > 0 {
			terminal = "rejected"
		}
		if err := db.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("status", terminal).Error; err != nil {
			return err
		}
		if err := db.Model(&store.TaskCompletion{}).Where("daily_task_id=? AND confirm_status='pending'", dt.ID).
			Updates(map[string]any{"confirm_status": "expired", "confirmed_at": now, "reject_reason": "已超过任务日截止"}).Error; err != nil {
			return err
		}
		if err := db.Model(&store.AIReviewJob{}).Where("daily_task_id=? AND status IN ('queued','running','failed')", dt.ID).
			Updates(map[string]any{"status": "canceled", "stage": "expired", "finished_at": now, "lease_token": "", "lease_expires_at": nil}).Error; err != nil {
			return err
		}
	}
	return nil
}

func dayState(db *store.DB, kidID int64, date string, tasks []store.DailyTask, now time.Time) (string, error) {
	fixed, submitted := false, false
	for _, dt := range tasks {
		snap, err := streakSnapshot(dt)
		if err != nil {
			return "", err
		}
		if *snap.StreakEnabled != 1 {
			continue
		}
		if dt.Status == "approved" {
			return "success", nil
		}
		if dt.Status == "skipped" {
			continue
		}
		if snap.DateMode != "weekly_quota" {
			fixed = true
		}
		if dt.Status == "submitted" && ReviewOpen(date, now) {
			submitted = true
		}
	}
	var leave int64
	if err := db.Model(&store.LeaveRecord{}).Where("kid_id=? AND date=?", kidID, date).Count(&leave).Error; err != nil {
		return "", err
	}
	if leave > 0 {
		return "neutral", nil
	}
	// 提交窗口内调度未冻结，即使暂时没有连击候选，也可能新增资格。
	// 仅已有success或整日请假能提前确定日结果。
	if submitted || SubmissionOpen(date, now) {
		return "pending", nil
	}
	if fixed {
		return "missed", nil
	}
	return "neutral", nil
}

// ReconcileKidLocked 调用者已持有孩子锁。补齐切换后缺日，按日期推进，
// pending 只阻塞最终 streak_after 和后续奖励；同日任一通过即成功。
func ReconcileKidLocked(db *store.DB, kidID int64, now time.Time) error {
	if err := db.RequireTaskTransaction(); err != nil {
		return err
	}
	effective, err := LockedNow(db, kidID, func() time.Time { return now })
	if err != nil {
		return err
	}
	now = effective
	var count int64
	if err := db.Model(&store.Pet{}).Where("kid_id=?", kidID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		var kid store.Kid
		if err := db.First(&kid, kidID).Error; err != nil {
			return err
		}
		day := now.In(BusinessLocation)
		wd := int(day.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := day.AddDate(0, 0, -(wd - 1))
		if err := materializeKid(db, kid, BusinessDate(day), wd, BusinessDate(monday), BusinessDate(monday.AddDate(0, 0, 7))); err != nil {
			return err
		}
		return expireTasks(db, kidID, now)
	}
	pet, err := initBaseline(db, kidID, now)
	if err != nil {
		return err
	}
	if err = expireTasks(db, kidID, now); err != nil {
		return err
	}
	start, err := taskDay(pet.StreakBaseDate)
	if err != nil {
		return err
	}
	today := BusinessDate(now)
	var kid store.Kid
	if err = db.First(&kid, kidID).Error; err != nil {
		return err
	}
	var days []store.KidStreakDay
	if err = db.Where("kid_id=? AND date>=? AND date<=?", kidID, pet.StreakBaseDate, today).Find(&days).Error; err != nil {
		return err
	}
	stored := map[string]store.KidStreakDay{}
	for _, day := range days {
		stored[day.Date] = day
	}
	factsFrom := today
	// 只补缺日及今日，不把已终结历史日反复物化。
	for day := start; BusinessDate(day) <= today; day = day.AddDate(0, 0, 1) {
		date := BusinessDate(day)
		row, exists := stored[date]
		if (!exists || row.State == "pending") && date < factsFrom {
			factsFrom = date
		}
		if exists && date != today {
			continue
		}
		wd := int(day.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := day.AddDate(0, 0, -(wd - 1))
		if err = materializeKid(db, kid, BusinessDate(day), wd, BusinessDate(monday), BusinessDate(monday.AddDate(0, 0, 7))); err != nil {
			return err
		}
	}
	if err = expireTasks(db, kidID, now); err != nil {
		return err
	}
	var tasks []store.DailyTask
	if err = db.Where("kid_id=? AND date<=? AND (date>=? OR settlement_status='pending')", kidID, today, factsFrom).Order("date,id").Find(&tasks).Error; err != nil {
		return err
	}
	byDate := map[string][]store.DailyTask{}
	for _, dt := range tasks {
		byDate[dt.Date] = append(byDate[dt.Date], dt)
	}
	streak := pet.StreakBaseValue
	unprocessedCredits := 0
	for _, day := range days {
		if day.CountedAtBaseline {
			unprocessedCredits++
		}
	}
	wait := ""
	after := map[string]int64{}
	waits := map[string]string{}
	for day := start; BusinessDate(day) <= today; day = day.AddDate(0, 0, 1) {
		date := BusinessDate(day)
		row := stored[date]
		state := row.State
		if row.ID == 0 || row.State == "pending" || date == today {
			state, err = dayState(db, kidID, date, byDate[date], now)
			if err != nil {
				return err
			}
		}
		// 已成功日不可被编辑、请假或剩余审核撤销。
		if row.State == "success" {
			state = "success"
		}
		previousWait := wait
		var value *int64
		if wait == "" && state != "pending" {
			if state == "success" {
				streak++
			}
			if row.CountedAtBaseline {
				unprocessedCredits--
			}
			if state == "missed" {
				// 断签：连击从有到无的当天扣 20 心情。row.State 已是 missed 说明本日扣过；
				// streak>0 保证连续漏做只扣一次，也不惩罚本来就没有连击的孩子。
				if streak > 0 && row.State != "missed" {
					if err = db.Model(&store.Pet{}).Where("kid_id=?", kidID).
						Update("mood", gorm.Expr("GREATEST(0, mood-20)")).Error; err != nil {
						return err
					}
				}
				streak = 0
			}
			v := streak
			value = &v
			after[date] = streak
		} else if wait == "" {
			wait = date
		}
		waits[date] = previousWait
		if row.ID == 0 {
			row = store.KidStreakDay{KidID: kidID, Date: date}
		}
		changed := row.ID == 0 || row.State != state || (row.StreakAfter == nil) != (value == nil)
		if row.StreakAfter != nil && value != nil && *row.StreakAfter != *value {
			changed = true
		}
		row.State, row.StreakAfter = state, value
		if changed {
			if err = db.Save(&row).Error; err != nil {
				return err
			}
		}
	}
	// 前置待审仍挡着已计数日时保留旧缓存；不能把扣信用后的before冒充最终连击。
	if wait == "" || unprocessedCredits == 0 {
		if err = db.UpdatePet(kidID, map[string]any{"streak": streak}); err != nil {
			return err
		}
	}
	for _, dt := range tasks {
		if dt.Status != "approved" || dt.SettlementStatus != "pending" {
			continue
		}
		snap, err := streakSnapshot(dt)
		if err != nil {
			return err
		}
		var n int64
		if err = db.Model(&store.SettlementLedger{}).Where("daily_task_id=? AND source='task'", dt.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			if err = markSettled(db, dt.ID, dt.StreakSnapshot); err != nil {
				return err
			}
			continue
		}
		blocking := waits[dt.Date]
		taskStreak := pet.StreakBaseValue
		if dt.Date >= pet.StreakBaseDate {
			if v, ok := after[dt.Date]; ok {
				taskStreak = v
			} else if blocking == "" {
				blocking = dt.Date
			}
		} else {
			// legacy 范围外的日不回放；基线已经代表其前序，不能等待未来日。
			blocking = ""
		}
		if blocking != "" {
			if err = db.Model(&store.DailyTask{}).Where("id=?", dt.ID).Update("settlement_wait_date", blocking).Error; err != nil {
				return err
			}
			continue
		}
		mul := 1 + 0.1*float64(taskStreak)
		if mul > 1.5 {
			mul = 1.5
		}
		base := snap.BaseScore + dt.SettlementExtra
		final := int(float64(base)*snap.DateMultiplier*mul + 0.5)
		taskID := dt.ID
		// 发奖前先按结算时钟补齐惰性衰减，否则长期不上线时旧 mood 会把 +10 顶到 100 上限。
		// 同一事务内后续任务重入时 hours=0，不会重复扣衰减。
		if _, err := db.TickAt(kidID, now); err != nil {
			return err
		}
		// 幂等由孩子锁 + task 流水 existing 检查保证；不对有历史重复/NULL/非task的流水强加索引。
		led := store.SettlementLedger{KidID: kidID, Date: dt.Date, DailyTaskID: &taskID, Source: "task", Base: base,
			DateMultiplier: snap.DateMultiplier, StreakMultiplier: mul, Final: final, DeltaBalance: final, DeltaEarned: final}
		if err = db.Create(&led).Error; err != nil {
			return err
		}
		// 任务完成的唯一发奖点：与星星同一事务、由 task 流水保证幂等，每个任务只 +10 心情。
		if err = db.UpdateAccountLocked(kidID, map[string]any{
			"balance": gorm.Expr("balance+?", final), "earned_total": gorm.Expr("earned_total+?", final),
		}); err != nil {
			return err
		}
		if err = db.Model(&store.Pet{}).Where("kid_id=?", kidID).
			Update("mood", gorm.Expr("LEAST(100, mood+10)")).Error; err != nil {
			return err
		}
		if err = markSettled(db, dt.ID, int(taskStreak)); err != nil {
			return err
		}
		if err = db.AddEvent(kidID, "task_done", map[string]any{"task": snap.Name, "base": base, "date_mul": snap.DateMultiplier, "streak_mul": mul}); err != nil {
			return err
		}
	}
	return checkLevelUp(db, kidID, pet)
}

func markSettled(db *store.DB, id int64, streak int) error {
	return db.Model(&store.DailyTask{}).Where("id=?", id).Updates(map[string]any{
		"settlement_status": "settled", "settlement_wait_date": "", "streak_snapshot": streak}).Error
}

func ReconcileKid(db *store.DB, kidID int64, now time.Time) error {
	return ReconcileKidWithClock(db, kidID, func() time.Time { return now })
}

func ReconcileKidWithClock(db *store.DB, kidID int64, clock func() time.Time) error {
	return db.TaskTransaction(func(tx *gorm.DB) error {
		child := &store.DB{DB: tx}
		if err := LockKid(child, kidID); err != nil {
			return err
		}
		now, err := LockedNow(child, kidID, clock)
		if err != nil {
			return err
		}
		return ReconcileKidLocked(child, kidID, now)
	})
}

func Reconcile(db *store.DB, now time.Time) error {
	return ReconcileWithClock(db, func() time.Time { return now })
}

func ReconcileWithClock(db *store.DB, clock func() time.Time) error {
	var kids []store.Kid
	if err := db.Order("id").Find(&kids).Error; err != nil {
		return err
	}
	var failures []error
	for _, kid := range kids {
		if err := ReconcileKidWithClock(db, kid.ID, clock); err != nil {
			failures = append(failures, fmt.Errorf("kid %d: %w", kid.ID, err))
		}
	}
	return errors.Join(failures...)
}

func CloseDay(db *store.DB) error { return ReconcileWithClock(db, time.Now) }

func checkLevelUp(db *store.DB, kidID int64, row *store.KidAccount) error {
	if row == nil {
		return nil
	}
	pet, err := db.AccountLocked(kidID)
	if err != nil {
		return err
	}
	level := pet.Level
	for pet.EarnedTotal >= levelThreshold(level) {
		level++
		if err := db.AddEvent(kidID, "levelup", map[string]any{"level": level}); err != nil {
			return err
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
