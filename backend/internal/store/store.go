package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
}

// TaskTransaction 任务领域统一使用RC；MySQL默认RR会让锁前identity查询
// 建立的读视图污染锁后的普通判断。禁止嵌套，避免隔离级别设置形同虚设。
func (d *DB) TaskTransaction(fn func(*gorm.DB) error) error {
	if _, ok := d.Statement.ConnPool.(gorm.TxCommitter); ok {
		return fmt.Errorf("任务事务不能嵌套")
	}
	return d.Transaction(func(tx *gorm.DB) error {
		// Set返回的链式实例不可作为共享查询根；恢复可克隆Session防止条件串到下一次查询。
		return fn(tx.Set("meowquest:task_transaction", true).Session(&gorm.Session{}))
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func (d *DB) RequireTaskTransaction() error {
	_, inTx := d.Statement.ConnPool.(gorm.TxCommitter)
	marked, _ := d.Get("meowquest:task_transaction")
	if !inTx || marked != true {
		return fmt.Errorf("必须使用ReadCommitted任务事务")
	}
	return nil
}

// OpenDB 双库：PG DSN 优先，否则 MySQL（自动建库 + AutoMigrate + 存量迁移）
func OpenDB(pgDSN, mysqlDSN, defaultPIN string) (*DB, error) {
	if pgDSN != "" {
		return openPG(pgDSN)
	}
	return openMySQL(mysqlDSN, defaultPIN)
}

// openPG：连接（库需先建好：createdb meowquest），AutoMigrate 即开张
func openPG(dsn string) (*DB, error) {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := autoMigrateAll(gdb); err != nil {
		return nil, err
	}
	if err := MigrateMultiPet(gdb); err != nil {
		return nil, err
	}
	return &DB{gdb}, nil
}

// openMySQL 连接并初始化（自动建库 + AutoMigrate + 存量数据迁移）
func openMySQL(dsn string, defaultPIN string) (*DB, error) {
	raw, err := gorm.Open(mysql.Open(splitBeforeDB(dsn)), &gorm.Config{
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
	if err := autoMigrateAll(gdb); err != nil {
		return nil, err
	}
	if err := migrateLegacy(gdb, defaultPIN); err != nil {
		return nil, fmt.Errorf("migrate legacy: %w", err)
	}
	if err := MigrateMultiPet(gdb); err != nil {
		return nil, err
	}
	return &DB{gdb}, nil
}

func autoMigrateAll(gdb *gorm.DB) error {
	// PG/MySQL共用准备顺序：旧归属唯一限制须在GORM查看Pet列元数据前移除。
	if err := PreparePetOwnership(gdb); err != nil {
		return err
	}
	return gdb.AutoMigrate(
		&Family{}, &Kid{}, &TaskAssign{}, &DeviceToken{},
		&Pet{}, &KidAccount{}, &FeedRequest{}, &SpeciesConfig{}, &TaskTemplate{}, &TaskSchedule{}, &RewardPolicy{},
		&Holiday{}, &LeaveRecord{}, &DailyTask{}, &KidStreakDay{}, &TaskCompletion{}, &AIReviewJob{},
		&SettlementLedger{}, &Wish{}, &SceneItem{}, &OwnedItem{}, &PetEvent{}, &ChatMessage{}, &KidMessage{},
	)
}

// migrateLegacy 存量单租户数据 → 默认家庭 + 默认孩子（幂等）
func migrateLegacy(gdb *gorm.DB, defaultPIN string) error {
	var familyCount int64
	gdb.Model(&Family{}).Count(&familyCount)
	if familyCount > 0 {
		return nil // 已是多租户结构
	}
	// 全新库（无任何存量业务数据）不迁移：等家长自行注册家庭
	var petCount, tplCount int64
	gdb.Model(&Pet{}).Count(&petCount)
	gdb.Model(&TaskTemplate{}).Count(&tplCount)
	if petCount == 0 && tplCount == 0 {
		return nil
	}
	// 旧索引/主键 DDL（AutoMigrate 不会改已有索引；错误容忍=幂等重跑）
	gdb.Exec(`ALTER TABLE daily_task DROP INDEX uk_daily`)
	gdb.Exec(`ALTER TABLE daily_task ADD UNIQUE KEY uk_daily (date, kid_id, template_id)`)
	gdb.Exec(`ALTER TABLE owned_item DROP PRIMARY KEY`)
	gdb.Exec(`ALTER TABLE owned_item ADD UNIQUE KEY uk_owned (kid_id, item_key)`)
	gdb.Exec(`ALTER TABLE leave_record DROP PRIMARY KEY`)
	gdb.Exec(`ALTER TABLE leave_record ADD UNIQUE KEY uk_leave (date, kid_id)`)

	// 默认家庭（code 固定 888888，存量使用者凭码进入）
	fam := Family{Code: "888888", PIN: defaultPIN, Name: "我的家"}
	if err := gdb.Create(&fam).Error; err != nil {
		return err
	}
	kid := Kid{FamilyID: fam.ID, Name: "宝贝"}
	if err := gdb.Create(&kid).Error; err != nil {
		return err
	}
	// 回填归属
	gdb.Exec(`UPDATE task_template SET family_id=? WHERE family_id=0 OR family_id IS NULL`, fam.ID)
	for _, tbl := range []string{"pet", "daily_task", "task_completion", "settlement_ledger",
		"wish", "scene_item", "owned_item", "pet_event", "leave_record", "device_token"} {
		gdb.Exec(fmt.Sprintf(`UPDATE %s SET kid_id=? WHERE kid_id=0 OR kid_id IS NULL`, tbl), kid.ID)
	}
	// 指派：存量模板全部指派给默认孩子
	gdb.Exec(`INSERT IGNORE INTO task_assign (template_id, kid_id) SELECT id, ? FROM task_template`, kid.ID)
	return nil
}

func splitBeforeDB(dsn string) string {
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '/' {
			return dsn[:i+1]
		}
	}
	return dsn
}

// ===== 家庭 / 孩子 =====

var ErrNotFound = errors.New("not found")

// FamilyLogin 家庭码 + PIN → 家庭（家长登录）
func (d *DB) FamilyLogin(code, pin string) (*Family, error) {
	var f Family
	if err := d.Where("code=? AND pin=?", code, pin).First(&f).Error; err != nil {
		return nil, ErrNotFound
	}
	return &f, nil
}

// FamilyByCode 凭家庭码找家庭（孩子端进入）
func (d *DB) FamilyByCode(code string) (*Family, error) {
	var f Family
	if err := d.Where("code=?", code).First(&f).Error; err != nil {
		return nil, ErrNotFound
	}
	return &f, nil
}

// CreateFamily 注册新家庭（返回含家庭码）
func (d *DB) CreateFamily(name, pin string) (*Family, error) {
	for i := 0; i < 10; i++ {
		code := strconv.Itoa(100000 + mathrand.IntN(900000))
		f := Family{Code: code, PIN: pin, Name: name}
		res := d.Where(Family{Code: code}).FirstOrCreate(&f)
		if res.RowsAffected > 0 {
			return &f, nil
		}
	}
	return nil, errors.New("code conflict, retry")
}

func (d *DB) KidsOfFamily(familyID int64) ([]Kid, error) {
	var kids []Kid
	err := d.Where("family_id=?", familyID).Order("id").Find(&kids).Error
	return kids, err
}

func (d *DB) CreateKid(familyID int64, name string) (*Kid, error) {
	kid := Kid{FamilyID: familyID, Name: name}
	if err := d.Create(&kid).Error; err != nil {
		return nil, err
	}
	return &kid, nil
}

// ===== 任务指派（家庭模板 → 孩子） =====

func (d *DB) AssignTask(templateID, kidID int64) error {
	return d.Where(TaskAssign{TemplateID: templateID, KidID: kidID}).
		FirstOrCreate(&TaskAssign{}).Error
}

func (d *DB) UnassignTask(templateID, kidID int64) error {
	return d.Where("template_id=? AND kid_id=?", templateID, kidID).Delete(&TaskAssign{}).Error
}

func (d *DB) AssignsOfTemplate(templateID int64) []int64 {
	var out []int64
	d.Model(&TaskAssign{}).Where("template_id=?", templateID).Pluck("kid_id", &out)
	return out
}

// TemplatesOfKid 孩子的全部指派模板（含调度）
func (d *DB) TemplatesOfKid(kidID int64) ([]TaskTemplate, error) {
	var tpls []TaskTemplate
	err := d.Preload("Schedule").
		Joins("JOIN task_assign a ON a.template_id = task_template.id AND a.kid_id = ?", kidID).
		Where("task_template.enabled = 1").Order("task_template.sort, task_template.id").
		Find(&tpls).Error
	return tpls, err
}

// ===== 宠物（per-kid） =====

// Tick 惰性衰减：读时结算（hunger -decay/h，mood -0.5/h），无 cron
func (d *DB) Tick(kidID int64) (*Pet, error) {
	return d.TickAt(kidID, time.Now())
}

// TickAt 用调用方时钟结算，供事务内与结算判定时间保持一致。
func (d *DB) TickAt(kidID int64, now time.Time) (pet *Pet, err error) {
	apply := func(tx *DB) error {
		pets, tickErr := tx.tickPetsLocked(kidID, now)
		if tickErr != nil || len(pets) == 0 {
			return tickErr
		}
		account, accountErr := tx.AccountLocked(kidID)
		if accountErr != nil {
			return accountErr
		}
		projectGrowth(&pets[0], account)
		pet = &pets[0]
		return nil
	}
	if _, inTx := d.Statement.ConnPool.(gorm.TxCommitter); inTx {
		err = apply(d)
	} else {
		err = d.WithKidLock(kidID, apply)
	}
	return
}

func clampMin(v, min float64) float64 {
	if v < min {
		return min
	}
	return v
}

func (d *DB) GetPet(kidID int64) (*Pet, error) {
	return d.Tick(kidID)
}

func (d *DB) HasPet(kidID int64) (bool, error) {
	var n int64
	err := d.Model(&Pet{}).Where("kid_id=?", kidID).Count(&n).Error
	return n > 0, err
}

// AdoptPet 领养（kid 维度；礼包 30 星）
func (d *DB) AdoptPet(kidID int64, name, species, personality string) (int64, error) {
	return d.adoptPet(kidID, name, species, personality, true)
}

// UpdatePet 保留旧成长写入口；只接受账户字段，猫状态必须显式按猫ID写入。
func (d *DB) UpdatePet(kidID int64, cols map[string]any) error {
	for key := range cols {
		switch key {
		case "balance", "earned_total", "level", "streak", "streak_base_date", "streak_base_value", "streak_evaluated_ns":
		default:
			return fmt.Errorf("猫独立字段不能按孩子更新: %s", key)
		}
	}
	if _, inTx := d.Statement.ConnPool.(gorm.TxCommitter); inTx {
		return d.UpdateAccountLocked(kidID, cols)
	}
	return d.WithKidLock(kidID, func(tx *DB) error { return tx.UpdateAccountLocked(kidID, cols) })
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

// ===== 星星（per-kid） =====

func (d *DB) AddStars(kidID int64, source string, base int, dateMul, streakMul float64, dailyTaskID int64) error {
	final := int(float64(base)*dateMul*streakMul + 0.5)
	var dtid *int64
	if dailyTaskID > 0 {
		dtid = &dailyTaskID
	}
	return d.WithKidLock(kidID, func(tx *DB) error {
		led := SettlementLedger{KidID: kidID, Date: today(), DailyTaskID: dtid, Source: source,
			Base: base, DateMultiplier: dateMul, StreakMultiplier: streakMul,
			Final: final, DeltaBalance: final, DeltaEarned: final}
		if err := tx.Create(&led).Error; err != nil {
			return err
		}
		return tx.UpdateAccountLocked(kidID, map[string]any{"balance": gorm.Expr("balance+?", final), "earned_total": gorm.Expr("earned_total+?", final)})
	})
}

func (d *DB) SpendStars(kidID int64, amount int) error {
	return d.WithKidLock(kidID, func(tx *DB) error { return tx.SpendStarsLocked(kidID, amount) })
}

func (d *DB) FeedPet(kidID int64) error {
	_, err := d.FeedGroupAt(kidID, 0, "legacy-"+NewToken(), time.Now())
	return err
}

func (d *DB) TouchPet(kidID int64) (left int, err error) {
	return d.TouchPetAt(kidID, 0, time.Now())
}

func (d *DB) EventCount(kidID int64, day, typ string) int64 {
	var n int64
	d.Model(&PetEvent{}).Where("type=? AND day=? AND kid_id=?", typ, day, kidID).Count(&n)
	return n
}

func (d *DB) AddEvent(kidID int64, typ string, payload map[string]any) error {
	pj := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			pj = string(b)
		}
	}
	return d.Create(&PetEvent{KidID: kidID, Type: typ, Payload: pj, Day: today()}).Error
}

func (d *DB) EventsSince(kidID int64, day string) ([]PetEvent, error) {
	var list []PetEvent
	err := d.Where("kid_id=? AND day>=?", kidID, day).Order("id DESC").Limit(100).Find(&list).Error
	return list, err
}

// ===== 认证 =====

// NewToken 随机设备 token（孩子端，绑 kid）
func NewToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *DB) SaveToken(token string, kidID int64) error {
	return d.Where(DeviceToken{Token: token}).
		Assign(DeviceToken{KidID: kidID}).
		FirstOrCreate(&DeviceToken{Token: token, KidID: kidID}).Error
}

// KidOfToken token → kidID（0=无效）
func (d *DB) KidOfToken(token string) int64 {
	if token == "" {
		return 0
	}
	var t DeviceToken
	if err := d.Where("token=?", token).First(&t).Error; err != nil {
		return 0
	}
	return t.KidID
}

// ===== 家长 token：无状态 HMAC（payload 含 family，重启不失效） =====
var adminSecret = []byte("meowquest-admin-v1")

// NewAdminToken = exp.familyID.sig
func NewAdminToken(familyID int64) string {
	exp := time.Now().Add(24 * time.Hour).Unix()
	msg := fmt.Sprintf("%d.%d", exp, familyID)
	return msg + "." + signAdmin(msg)
}

func signAdmin(msg string) string {
	mac := hmac.New(sha256.New, adminSecret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// FamilyOfAdminToken 返回 familyID（0=无效）
func FamilyOfAdminToken(token string) int64 {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return 0
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0
	}
	if signAdmin(parts[0]+"."+parts[1]) != parts[2] {
		return 0
	}
	fid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return fid
}

// ===== 猫聊天 =====

// ChatHistory 兼容无猫ID的旧调用，只返回第一只猫的对话。
func (d *DB) ChatHistory(kidID int64, limit int) ([]ChatMessage, error) {
	return d.PetChatHistory(kidID, 0, limit)
}

func today() string { return time.Now().Format("2006-01-02") }
