package store

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
}

// OpenMySQL 连接并初始化（自动建库 + AutoMigrate）
func OpenMySQL(dsn string) (*DB, error) {
	// 先去库名建库
	serverDSN := splitBeforeDB(dsn)
	raw, err := gorm.Open(mysql.Open(serverDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := raw.Exec(`CREATE DATABASE IF NOT EXISTS meowquest CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`).Error; err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	if sqlDB, err := raw.DB(); err == nil {
		sqlDB.Close()
	}

	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := gdb.AutoMigrate(
		&DeviceToken{}, &Pet{}, &SpeciesConfig{},
		&TaskTemplate{}, &TaskSchedule{}, &RewardPolicy{},
		&Holiday{}, &LeaveRecord{}, &DailyTask{}, &TaskCompletion{},
		&SettlementLedger{}, &Wish{}, &SceneItem{}, &OwnedItem{}, &PetEvent{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{gdb}, nil
}

func splitBeforeDB(dsn string) string {
	// root:@tcp(127.0.0.1:3306)/meowquest?... → root:@tcp(127.0.0.1:3306)/
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '/' {
			return dsn[:i+1]
		}
	}
	return dsn
}

// ===== 宠物 =====

// Tick 惰性衰减：读时结算（hunger -decay/h，mood -0.5/h），无 cron
func (d *DB) Tick() (*Pet, error) {
	var pet Pet
	if err := d.First(&pet).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	last := pet.LastTick
	if last == nil {
		last = &pet.HatchedAt
	}
	hours := time.Since(*last).Hours()
	if hours <= 0 {
		return &pet, nil
	}
	pet.Hunger = clampMin(pet.Hunger-pet.HungerDecay*hours, 0)
	pet.Mood = clampMin(pet.Mood-0.5*hours, 0)
	now := time.Now()
	pet.LastTick = &now
	if err := d.Model(&Pet{}).Where("id=?", pet.ID).
		Updates(map[string]any{"hunger": pet.Hunger, "mood": pet.Mood, "last_tick": now}).Error; err != nil {
		return nil, err
	}
	return &pet, nil
}

func clampMin(v, min float64) float64 {
	if v < min {
		return min
	}
	return v
}

func (d *DB) GetPet() (*Pet, error) {
	return d.Tick()
}

func (d *DB) HasPet() (bool, error) {
	var n int64
	d.Model(&Pet{}).Count(&n)
	return n > 0, nil
}

// AdoptPet 领养（仅首次；礼包 30 星）
func (d *DB) AdoptPet(name, species, personality string) (int64, error) {
	if ok, _ := d.HasPet(); ok {
		return 0, fmt.Errorf("already adopted")
	}
	pet := &Pet{Name: name, Species: species, Personality: personality,
		Hunger: 100, Mood: 100, Balance: 30, EarnedTotal: 30}
	if err := d.Create(pet).Error; err != nil {
		return 0, err
	}
	d.AddEvent("adopt", map[string]any{"name": name, "species": species})
	return pet.ID, nil
}

func (d *DB) UpdatePet(cols map[string]any) error {
	return d.Model(&Pet{}).Where("id>0").Updates(cols).Error
}

// ===== 品种 =====

func (d *DB) SpeciesList() ([]SpeciesConfig, error) {
	var list []SpeciesConfig
	err := d.Where("enabled=1").Order("sort").Find(&list).Error
	return list, err
}

func (d *DB) SeedSpecies(list []SpeciesConfig) error {
	for i, s := range list {
		d.Where(SpeciesConfig{Species: s.Species}).
			Assign(SpeciesConfig{DisplayName: s.DisplayName, Description: s.Description, Sort: i}).
			FirstOrCreate(&SpeciesConfig{})
	}
	return nil
}

// ===== 星星 =====

// AddStars 星星入账（事务：流水 + 双记账）
func (d *DB) AddStars(source string, base int, dateMul, streakMul float64, dailyTaskID int64) error {
	final := int(float64(base)*dateMul*streakMul + 0.5)
	var dtid *int64
	if dailyTaskID > 0 {
		dtid = &dailyTaskID
	}
	return d.Transaction(func(tx *gorm.DB) error {
		led := SettlementLedger{Date: today(), DailyTaskID: dtid, Source: source,
			Base: base, DateMultiplier: dateMul, StreakMultiplier: streakMul,
			Final: final, DeltaBalance: final, DeltaEarned: final}
		if err := tx.Create(&led).Error; err != nil {
			return err
		}
		return tx.Model(&Pet{}).Where("id>0").
			Updates(map[string]any{"balance": gorm.Expr("balance+?", final), "earned_total": gorm.Expr("earned_total+?", final)}).Error
	})
}

// SpendStars 消费（条件更新防超扣）
func (d *DB) SpendStars(amount int) error {
	res := d.Model(&Pet{}).Where("balance>=?", amount).
		Update("balance", gorm.Expr("balance-?", amount))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("insufficient balance")
	}
	return nil
}

// FeedPet 喂食：扣 10 星、饱腹 +30、心情 +5
func (d *DB) FeedPet() error {
	if _, err := d.Tick(); err != nil {
		return err
	}
	res := d.Model(&Pet{}).Where("balance>=10").Updates(map[string]any{
		"balance": gorm.Expr("balance-10"),
		"hunger":  gorm.Expr("LEAST(100, hunger+30)"),
		"mood":    gorm.Expr("LEAST(100, mood+5)"),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("insufficient stars")
	}
	d.AddEvent("feed", map[string]any{"stars": 10})
	return nil
}

// TouchPet 摸头：心情 +5，每日限 3 次
func (d *DB) TouchPet() (left int, err error) {
	if _, err = d.Tick(); err != nil {
		return 0, err
	}
	var n int64
	d.Model(&PetEvent{}).Where("type='touch' AND day=?", today()).Count(&n)
	if n >= 3 {
		return 0, nil
	}
	if err = d.Model(&Pet{}).Where("id>0").
		Update("mood", gorm.Expr("LEAST(100, mood+5)")).Error; err != nil {
		return 0, err
	}
	d.AddEvent("touch", nil)
	return int(3 - n - 1), nil
}

func (d *DB) EventCount(day, typ string) int64 {
	var n int64
	d.Model(&PetEvent{}).Where("type=? AND day=?", typ, day).Count(&n)
	return n
}

// AddEvent 宠物事件（日记/记忆素材）
func (d *DB) AddEvent(typ string, payload map[string]any) {
	pj := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			pj = string(b)
		}
	}
	d.Create(&PetEvent{Type: typ, Payload: pj, Day: today()})
}

func (d *DB) EventsSince(day string) ([]PetEvent, error) {
	var list []PetEvent
	err := d.Where("day>=?", day).Order("id DESC").Limit(100).Find(&list).Error
	return list, err
}

func today() string { return time.Now().Format("2006-01-02") }
