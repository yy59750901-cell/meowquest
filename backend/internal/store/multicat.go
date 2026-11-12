package store

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPetNotFound       = errors.New("这只猫不属于当前孩子或尚未领养")
	ErrPetLimit          = errors.New("领养名额已满啦")
	ErrInsufficientStars = errors.New("星星不够啦，统一开饭需要10颗星星")
	ErrChatLimit         = errors.New("今天聊得够多啦，猫要睡觉了，明天再来喵～")
)

func MaxPetsPerKid() int {
	n, err := strconv.Atoi(os.Getenv("MAX_PETS_PER_KID"))
	if err != nil || n < 1 {
		return 2
	}
	return n
}

// WithKidLock 与任务结算使用相同RC事务和孩子锁顺序；跨进程互斥，不靠内存锁。
func (d *DB) WithKidLock(kidID int64, fn func(*DB) error) error {
	return d.TaskTransaction(func(tx *gorm.DB) error {
		child := &DB{DB: tx}
		var kid Kid
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&kid, kidID).Error; err != nil {
			return err
		}
		return fn(child)
	})
}

func accountFromPet(p Pet) KidAccount {
	return KidAccount{KidID: p.KidID, PrimaryPetID: p.ID, Balance: p.Balance,
		EarnedTotal: p.EarnedTotal, Level: p.Level, Streak: p.Streak,
		StreakBaseDate: p.StreakBaseDate, StreakBaseValue: p.StreakBaseValue, StreakEvaluatedNS: p.StreakEvaluatedNS}
}

// AccountLocked 仅在持有孩子锁的事务内调用；无账户时从最早旧猫初始化，绝不再次导入影子。
func (d *DB) AccountLocked(kidID int64) (*KidAccount, error) {
	if err := d.RequireTaskTransaction(); err != nil {
		return nil, err
	}
	var account KidAccount
	err := d.Where("kid_id=?", kidID).First(&account).Error
	if err == nil {
		return &account, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var pet Pet
	if err := d.Where("kid_id=?", kidID).Order("id").First(&pet).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	account = accountFromPet(pet)
	if err := d.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (d *DB) Account(kidID int64) (account *KidAccount, err error) {
	err = d.WithKidLock(kidID, func(tx *DB) error {
		account, err = tx.AccountLocked(kidID)
		return err
	})
	return
}

func (d *DB) syncPrimary(account *KidAccount) error {
	return d.Model(&Pet{}).Where("id=? AND kid_id=?", account.PrimaryPetID, account.KidID).Updates(map[string]any{
		"balance": account.Balance, "earned_total": account.EarnedTotal, "level": account.Level,
		"streak": account.Streak, "streak_base_date": account.StreakBaseDate,
		"streak_base_value": account.StreakBaseValue, "streak_evaluated_ns": account.StreakEvaluatedNS,
	}).Error
}

func (d *DB) UpdateAccountLocked(kidID int64, cols map[string]any) error {
	account, err := d.AccountLocked(kidID)
	if err != nil {
		return err
	}
	if account == nil {
		return ErrPetNotFound
	}
	if err := d.Model(&KidAccount{}).Where("kid_id=?", kidID).Updates(cols).Error; err != nil {
		return err
	}
	if err := d.Where("kid_id=?", kidID).First(account).Error; err != nil {
		return err
	}
	return d.syncPrimary(account)
}

func projectGrowth(p *Pet, a *KidAccount) {
	p.Balance, p.EarnedTotal, p.Level, p.Streak = a.Balance, a.EarnedTotal, a.Level, a.Streak
	p.StreakBaseDate, p.StreakBaseValue, p.StreakEvaluatedNS = a.StreakBaseDate, a.StreakBaseValue, a.StreakEvaluatedNS
}

func selectPet(pets []Pet, petID int64) (*Pet, error) {
	if len(pets) == 0 || petID < 0 {
		return nil, ErrPetNotFound
	}
	if petID == 0 {
		return &pets[0], nil
	}
	for i := range pets {
		if pets[i].ID == petID {
			return &pets[i], nil
		}
	}
	return nil, ErrPetNotFound
}

func decayPet(p *Pet, now time.Time) bool {
	last := p.LastTick
	if last == nil {
		last = &p.HatchedAt
	}
	hours := now.Sub(*last).Hours()
	if hours <= 0 {
		return false
	}
	p.Hunger = clampMin(p.Hunger-p.HungerDecay*hours, 0)
	p.Mood = clampMin(p.Mood-0.5*hours, 0)
	p.LastTick = &now
	return true
}

func (d *DB) tickPetsLocked(kidID int64, now time.Time) ([]Pet, error) {
	if err := d.RequireTaskTransaction(); err != nil {
		return nil, err
	}
	var pets []Pet
	if err := d.Where("kid_id=?", kidID).Order("id").Find(&pets).Error; err != nil {
		return nil, err
	}
	for i := range pets {
		if decayPet(&pets[i], now) {
			if err := d.Model(&Pet{}).Where("id=? AND kid_id=?", pets[i].ID, kidID).Updates(map[string]any{
				"hunger": pets[i].Hunger, "mood": pets[i].Mood, "last_tick": now,
			}).Error; err != nil {
				return nil, err
			}
		}
	}
	return pets, nil
}

func (d *DB) PetSnapshotAt(kidID, petID int64, now time.Time) (selected *Pet, pets []Pet, account *KidAccount, err error) {
	err = d.WithKidLock(kidID, func(tx *DB) error {
		var raw []Pet
		if err := tx.Where("kid_id=?", kidID).Order("id").Find(&raw).Error; err != nil {
			return err
		}
		if _, err := selectPet(raw, petID); err != nil {
			return err
		}
		pets, err = tx.tickPetsLocked(kidID, now)
		if err != nil {
			return err
		}
		account, err = tx.AccountLocked(kidID)
		if err != nil {
			return err
		}
		for i := range pets {
			projectGrowth(&pets[i], account)
		}
		selected, err = selectPet(pets, petID)
		return err
	})
	return
}

func validatePetIdentity(name, species, personality string) (string, string, error) {
	name = strings.TrimSpace(name)
	personality = strings.TrimSpace(personality)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 32 {
		return "", "", errors.New("猫咪名字需要1到32个字")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", "", errors.New("猫咪名字不能包含控制字符")
		}
	}
	if species == "" || utf8.RuneCountInString(species) > 16 {
		return "", "", errors.New("请选择可领养的品种")
	}
	if personality == "" {
		personality = "热情开朗"
	}
	if !utf8.ValidString(personality) || utf8.RuneCountInString(personality) > 64 {
		return "", "", errors.New("性格描述最多64个字")
	}
	return name, personality, nil
}

func (d *DB) adoptPet(kidID int64, name, species, personality string, firstOnly bool) (id int64, err error) {
	name, personality, err = validatePetIdentity(name, species, personality)
	if err != nil {
		return 0, err
	}
	err = d.WithKidLock(kidID, func(tx *DB) error {
		var pets []Pet
		if err := tx.Where("kid_id=?", kidID).Order("id").Find(&pets).Error; err != nil {
			return err
		}
		if firstOnly && len(pets) > 0 {
			return errors.New("已经领养过啦，请从猫咪页面追加领养")
		}
		if !firstOnly && len(pets) == 0 {
			return ErrPetNotFound
		}
		if len(pets) >= MaxPetsPerKid() {
			return ErrPetLimit
		}
		for _, pet := range pets {
			if strings.EqualFold(strings.TrimSpace(pet.Name), name) {
				return errors.New("已经有同名猫咪啦，请换个名字")
			}
		}
		var config SpeciesConfig
		if err := tx.Where("species=? AND enabled=1", species).First(&config).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("这个品种暂时不能领养")
			}
			return err
		}
		account, err := tx.AccountLocked(kidID)
		if err != nil {
			return err
		}
		pet := Pet{KidID: kidID, Name: name, Species: species, Personality: personality, Hunger: 100, Mood: 100}
		if err := tx.Create(&pet).Error; err != nil {
			return err
		}
		if account == nil {
			// 仅首次领养一次性30星；追加领养永不更新账户、不复制成长。
			account = &KidAccount{KidID: kidID, PrimaryPetID: pet.ID, Balance: 30, EarnedTotal: 30, Level: 1}
			if err := tx.Create(account).Error; err != nil {
				return err
			}
			if err := tx.syncPrimary(account); err != nil {
				return err
			}
		}
		id = pet.ID
		return tx.AddEvent(kidID, "adopt", map[string]any{"pet_id": id, "name": name, "species": species})
	})
	return
}

func (d *DB) AdoptAdditionalPet(kidID int64, name, species, personality string) (int64, error) {
	return d.adoptPet(kidID, name, species, personality, false)
}

func (d *DB) SpendStarsLocked(kidID int64, amount int) error {
	if amount <= 0 {
		return errors.New("消费星星必须为正数")
	}
	account, err := d.AccountLocked(kidID)
	if err != nil {
		return err
	}
	if account == nil || account.Balance < int64(amount) {
		return ErrInsufficientStars
	}
	return d.UpdateAccountLocked(kidID, map[string]any{"balance": gorm.Expr("balance-?", amount)})
}

func (d *DB) FeedGroupAt(kidID, petID int64, requestID string, now time.Time) (result FeedRequest, err error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || !utf8.ValidString(requestID) {
		return result, errors.New("request_id需要1到128字节的非空字符串")
	}
	err = d.WithKidLock(kidID, func(tx *DB) error {
		var raw []Pet
		if err := tx.Where("kid_id=?", kidID).Order("id").Find(&raw).Error; err != nil {
			return err
		}
		if _, err := selectPet(raw, petID); err != nil {
			return err
		}
		err := tx.Where("kid_id=? AND request_id=?", kidID, requestID).First(&result).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		pets, err := tx.tickPetsLocked(kidID, now)
		if err != nil {
			return err
		}
		result = FeedRequest{KidID: kidID, RequestID: requestID, AllFull: true}
		for _, pet := range pets {
			if pet.Hunger < 100 {
				result.AllFull = false
			}
		}
		if !result.AllFull {
			if err := tx.SpendStarsLocked(kidID, 10); err != nil {
				return err
			}
			if err := tx.Model(&Pet{}).Where("kid_id=?", kidID).Updates(map[string]any{
				"hunger": gorm.Expr("LEAST(100, hunger+30)"), "mood": gorm.Expr("LEAST(100, mood+5)"),
			}).Error; err != nil {
				return err
			}
			result.Fed = true
			if err := tx.AddEvent(kidID, "feed", map[string]any{"stars": 10, "request_id": requestID}); err != nil {
				return err
			}
		}
		return tx.Create(&result).Error
	})
	return
}

func (d *DB) TouchPetAt(kidID, petID int64, now time.Time) (left int, err error) {
	err = d.WithKidLock(kidID, func(tx *DB) error {
		pets, err := tx.tickPetsLocked(kidID, now)
		if err != nil {
			return err
		}
		pet, err := selectPet(pets, petID)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&PetEvent{}).Where("kid_id=? AND type='touch' AND day=?", kidID, petBusinessDate(now)).Count(&count).Error; err != nil {
			return err
		}
		if count >= 3 {
			return nil
		}
		if err := tx.Model(&Pet{}).Where("kid_id=? AND id=?", kidID, pet.ID).Update("mood", gorm.Expr("LEAST(100, mood+5)")).Error; err != nil {
			return err
		}
		left = int(2 - count)
		return tx.Create(&PetEvent{KidID: kidID, Type: "touch", Day: petBusinessDate(now), Payload: fmt.Sprintf(`{"pet_id":%d}`, pet.ID)}).Error
	})
	return
}

func petBusinessDate(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
}

// PreparePetOwnership 必须先于Pet的AutoMigrate；GORM会误把自定义UNIQUE约束名当成默认名移除。
// 只移除孩子归属的单列唯一限制，不修改列、主键或业务数据；全新库无需准备。
func PreparePetOwnership(gdb *gorm.DB) error {
	if !gdb.Migrator().HasTable(&Pet{}) {
		return nil
	}
	// PG的GetIndexes故意排除约束支撑索引，必须另查单列UNIQUE约束。
	if gdb.Dialector.Name() == "postgres" {
		var names []string
		if err := gdb.Raw(`SELECT c.conname FROM pg_constraint c
			JOIN pg_class t ON t.oid=c.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace
			JOIN pg_attribute a ON a.attrelid=t.oid AND a.attname='kid_id'
			WHERE n.nspname=current_schema() AND t.relname='pet' AND c.contype='u' AND c.conkey=ARRAY[a.attnum]`).Scan(&names).Error; err != nil {
			return err
		}
		for _, name := range names {
			if err := gdb.Migrator().DropConstraint(&Pet{}, name); err != nil {
				return err
			}
		}
	}
	indexes, err := gdb.Migrator().GetIndexes(&Pet{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, ok := index.Unique()
		primary, _ := index.PrimaryKey()
		columns := index.Columns()
		if ok && unique && !primary && len(columns) == 1 && columns[0] == "kid_id" {
			// MySQL的UNIQUE统一用DROP INDEX，兼容不支持DROP CONSTRAINT的版本。
			if err := gdb.Migrator().DropIndex(&Pet{}, index.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// MigrateMultiPet 只在schema创建完成及旧租户归属迁移之后回填账户和第一猫历史。
func MigrateMultiPet(gdb *gorm.DB) error {
	var kids []Kid
	if err := gdb.Order("id").Find(&kids).Error; err != nil {
		return err
	}
	db := &DB{DB: gdb}
	for _, kid := range kids {
		if err := db.WithKidLock(kid.ID, func(tx *DB) error {
			account, err := tx.AccountLocked(kid.ID)
			if err != nil || account == nil {
				return err
			}
			return tx.Model(&ChatMessage{}).Where("kid_id=? AND (pet_id=0 OR pet_id IS NULL)", kid.ID).Update("pet_id", account.PrimaryPetID).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) PetChatHistory(kidID, petID int64, limit int) ([]ChatMessage, error) {
	var pet Pet
	query := d.Where("kid_id=?", kidID)
	if petID != 0 {
		query = query.Where("id=?", petID)
	}
	if err := query.Order("id").First(&pet).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPetNotFound
		}
		return nil, err
	}
	var primary Pet
	if err := d.Where("kid_id=?", kidID).Order("id").First(&primary).Error; err != nil {
		return nil, err
	}
	query = d.Where("kid_id=? AND pet_id=?", kidID, pet.ID)
	if primary.ID == pet.ID {
		query = d.Where("kid_id=? AND (pet_id=? OR pet_id=0 OR pet_id IS NULL)", kidID, pet.ID)
	}
	list := []ChatMessage{}
	if err := query.Order("id DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// BeginPetChat 在孩子锁下预留两条额度，避免两个猫的并发流式请求越过每日上限。
func (d *DB) BeginPetChat(kidID, petID int64, content string, limit int, now time.Time) (selected *Pet, err error) {
	err = d.WithKidLock(kidID, func(tx *DB) error {
		pets, err := tx.tickPetsLocked(kidID, now)
		if err != nil {
			return err
		}
		selected, err = selectPet(pets, petID)
		if err != nil {
			return err
		}
		account, err := tx.AccountLocked(kidID)
		if err != nil {
			return err
		}
		used, err := tx.chatUsedLocked(account, now)
		if err != nil {
			return err
		}
		if used+2 > limit {
			return ErrChatLimit
		}
		if err := tx.Model(&KidAccount{}).Where("kid_id=?", kidID).Updates(map[string]any{"chat_day": petBusinessDate(now), "chat_used": used + 2}).Error; err != nil {
			return err
		}
		projectGrowth(selected, account)
		return tx.Create(&ChatMessage{KidID: kidID, PetID: selected.ID, Role: "user", Content: content, CreatedAt: now}).Error
	})
	return
}

func (d *DB) chatUsedLocked(account *KidAccount, now time.Time) (int, error) {
	day := petBusinessDate(now)
	if account.ChatDay == day {
		return account.ChatUsed, nil
	}
	start, err := time.ParseInLocation("2006-01-02", day, time.FixedZone("Asia/Shanghai", 8*3600))
	if err != nil {
		return 0, err
	}
	var count int64
	err = d.Model(&ChatMessage{}).Where("kid_id=? AND created_at>=? AND created_at<?", account.KidID, start, start.AddDate(0, 0, 1)).Count(&count).Error
	return int(count), err
}

func (d *DB) ChatUsedAt(kidID int64, now time.Time) (used int, err error) {
	err = d.WithKidLock(kidID, func(tx *DB) error {
		account, err := tx.AccountLocked(kidID)
		if err != nil {
			return err
		}
		if account == nil {
			return ErrPetNotFound
		}
		used, err = tx.chatUsedLocked(account, now)
		return err
	})
	return
}

func (d *DB) SavePetChat(kidID, petID int64, role, content string) error {
	return d.Create(&ChatMessage{KidID: kidID, PetID: petID, Role: role, Content: content}).Error
}
