package store

import "time"

// GORM 模型（表名与已建库一致，AutoMigrate 增量兼容）

type DeviceToken struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Token     string    `gorm:"size:64;uniqueIndex" json:"token"`
	CreatedAt time.Time `json:"created_at"`
}
func (DeviceToken) TableName() string { return "device_token" }

type Pet struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"size:32" json:"name"`
	Species      string    `gorm:"size:16" json:"species"`
	Personality  string    `gorm:"size:64;default:热情开朗" json:"personality"`
	Level        int64     `gorm:"default:1" json:"level"`
	EarnedTotal  int64     `json:"earned_total"` // 累计获得（只增不减，驱动等级）
	Balance      int64     `json:"balance"`      // 余额（消费扣减）
	Hunger       float64   `gorm:"default:100" json:"hunger"`
	Mood         float64   `gorm:"default:100" json:"mood"`
	HungerDecay  float64    `gorm:"default:2.0;column:hunger_decay" json:"-"`
	Streak       int64     `json:"streak"`
	LastTick     *time.Time `gorm:"column:last_tick" json:"-"`
	HatchedAt    time.Time `json:"hatched_at"`
}
func (Pet) TableName() string { return "pet" }

type SpeciesConfig struct {
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Species     string `gorm:"size:16;uniqueIndex" json:"species"`
	DisplayName string `gorm:"size:32" json:"display_name"`
	Description string `gorm:"size:128" json:"description"`
	Enabled     int    `gorm:"default:1" json:"enabled"`
	Sort        int    `json:"sort"`
}
func (SpeciesConfig) TableName() string { return "species_config" }

type TaskTemplate struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"size:64" json:"name"`
	Icon         string    `gorm:"size:16;default:⭐" json:"icon"`
	Category     string    `gorm:"size:16;default:custom" json:"category"`
	GoalType     string    `gorm:"size:16;default:free;column:goal_type" json:"goal_type"` // duration/count/free
	GoalValue    int       `gorm:"column:goal_value" json:"goal_value"`
	EvidenceType string    `gorm:"size:32;default:none;column:evidence_type" json:"evidence_type"`
	ConfirmMode  string    `gorm:"size:8;default:a;column:confirm_mode" json:"confirm_mode"` // a / c / bc
	BaseScore    int       `gorm:"column:base_score" json:"base_score"`
	StreakEnabled int      `gorm:"default:1;column:streak_enabled" json:"streak_enabled"`
	ExtraMinutes int       `gorm:"default:0;column:extra_minutes" json:"extra_minutes"`
	ExtraStars   int       `gorm:"default:0;column:extra_stars" json:"extra_stars"`
	ExtraCap     int       `gorm:"default:0;column:extra_cap" json:"extra_cap"`
	Sort         int       `json:"sort"`
	Enabled      int       `gorm:"default:1" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Schedule     *TaskSchedule `gorm:"foreignKey:TemplateID" json:"schedule,omitempty"`
}
func (TaskTemplate) TableName() string { return "task_template" }

type TaskSchedule struct {
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TemplateID  int64  `gorm:"index;column:template_id" json:"template_id"`
	DateMode    string `gorm:"size:16;default:weekday_fixed;column:date_mode" json:"date_mode"` // weekday_fixed / weekly_quota
	Weekdays    string `gorm:"size:16;default:1,2,3,4,5,6,7" json:"weekdays"`
	WeeklyQuota int    `gorm:"default:0;column:weekly_quota" json:"weekly_quota"`
	ValidFrom   *string `gorm:"column:valid_from" json:"valid_from"`
	ValidTo     *string `gorm:"column:valid_to" json:"valid_to"`
}
func (TaskSchedule) TableName() string { return "task_schedule" }

type RewardPolicy struct {
	ID         int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchType  string  `gorm:"size:16;column:match_type" json:"match_type"` // date / date_type
	MatchValue string  `gorm:"size:32;column:match_value" json:"match_value"`
	Multiplier float64 `gorm:"default:1" json:"multiplier"`
	Note       string  `gorm:"size:128" json:"note"`
}
func (RewardPolicy) TableName() string { return "reward_policy" }

type Holiday struct {
	Date string `gorm:"primaryKey;size:10;column:date" json:"date"`
	Name string `gorm:"size:64" json:"name"`
}
func (Holiday) TableName() string { return "holiday" }

type LeaveRecord struct {
	Date   string `gorm:"primaryKey;size:10;column:date" json:"date"`
	Reason string `gorm:"size:128" json:"reason"`
}
func (LeaveRecord) TableName() string { return "leave_record" }

type DailyTask struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Date           string    `gorm:"size:10;uniqueIndex:uk_daily" json:"date"`
	TemplateID     int64     `gorm:"uniqueIndex:uk_daily;column:template_id" json:"template_id"`
	Snapshot       string    `json:"snapshot"`
	Status         string    `gorm:"size:16;default:pending" json:"status"` // pending/submitted/approved/rejected/skipped/expired
	StreakSnapshot int       `gorm:"default:0;column:streak_snapshot" json:"-"`
	QuotaDone      int       `gorm:"default:0;column:quota_done" json:"quota_done"`
	CreatedAt      time.Time `json:"created_at"`
}
func (DailyTask) TableName() string { return "daily_task" }

type TaskCompletion struct {
	ID           int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	DailyTaskID  int64      `gorm:"index;column:daily_task_id" json:"daily_task_id"`
	Evidence     string     `json:"evidence"`
	DurationSec  int        `gorm:"column:duration_sec" json:"duration_sec"`
	SubmittedAt  time.Time  `gorm:"autoCreateTime;column:submitted_at" json:"submitted_at"`
	ConfirmStatus string   `gorm:"size:16;default:pending;column:confirm_status" json:"confirm_status"`
	ConfirmedAt  *time.Time `gorm:"column:confirmed_at" json:"confirmed_at"`
	RejectReason string     `gorm:"size:128;column:reject_reason" json:"reject_reason"`
}
func (TaskCompletion) TableName() string { return "task_completion" }

type SettlementLedger struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Date           string    `gorm:"size:10;column:date" json:"date"`
	DailyTaskID    *int64    `gorm:"column:daily_task_id" json:"daily_task_id"`
	Source         string    `gorm:"size:32" json:"source"` // task/task_extra/wish_refund
	Base           int       `json:"base"`
	DateMultiplier float64   `gorm:"column:date_multiplier" json:"date_multiplier"`
	StreakMultiplier float64 `gorm:"column:streak_multiplier" json:"streak_multiplier"`
	Final          int       `json:"final"`
	DeltaBalance   int       `gorm:"column:delta_balance" json:"delta_balance"`
	DeltaEarned    int       `gorm:"column:delta_earned" json:"delta_earned"`
	CreatedAt      time.Time `json:"created_at"`
}
func (SettlementLedger) TableName() string { return "settlement_ledger" }

type Wish struct {
	ID           int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Title        string     `gorm:"size:128" json:"title"`
	Photo        string     `json:"photo"`
	Status       string     `gorm:"size:16;default:pending" json:"status"` // pending/approved/saving/redeem_requested/fulfilled/rejected
	Price        int        `json:"price"`
	HopedPrice   int        `gorm:"column:hoped_price" json:"hoped_price"`
	RejectReason string     `gorm:"size:128;column:reject_reason" json:"reject_reason"`
	CreatedAt    time.Time  `json:"created_at"`
	RedeemedAt   *time.Time `gorm:"column:redeemed_at" json:"redeemed_at"`
	FulfilledAt  *time.Time `gorm:"column:fulfilled_at" json:"fulfilled_at"`
}
func (Wish) TableName() string { return "wish" }

type SceneItem struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ItemKey   string    `gorm:"size:32;column:item_key" json:"item_key"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	PlacedAt  time.Time `gorm:"autoCreateTime;column:placed_at" json:"placed_at"`
}
func (SceneItem) TableName() string { return "scene_item" }

type OwnedItem struct {
	ItemKey  string    `gorm:"primaryKey;size:32;column:item_key" json:"item_key"`
	BoughtAt time.Time `gorm:"autoCreateTime;column:bought_at" json:"bought_at"`
}
func (OwnedItem) TableName() string { return "owned_item" }

type PetEvent struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Type      string    `gorm:"size:32;index:idx_pet_event_day,priority:1" json:"type"`
	Payload   string    `json:"payload"`
	Day       string    `gorm:"size:10;index:idx_pet_event_day,priority:2" json:"day"`
	CreatedAt time.Time `json:"created_at"`
}
func (PetEvent) TableName() string { return "pet_event" }
