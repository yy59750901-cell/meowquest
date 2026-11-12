package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

func multicatRouter(s *Server, kid store.Kid) *gin.Engine {
	r := taskTestRouter(s, kid.FamilyID, kid.ID)
	r.POST("/api/pet/adopt", s.adoptAdditional)
	r.POST("/api/pet/feed", s.feed)
	r.POST("/api/pet/touch", s.touch)
	r.POST("/api/chat", s.chat)
	r.GET("/api/chat/history", s.chatHistory)
	r.POST("/api/wish/:id/redeem", s.redeemWish)
	r.POST("/api/wish/:id/cancel", s.cancelWish)
	return r
}

func multicatFixture(t *testing.T) (*Server, store.Kid, []store.Pet, time.Time) {
	t.Helper()
	t.Setenv("MAX_PETS_PER_KID", "2")
	s, kid, now := streakFixture(t, 0, false)
	mustTaskCreate(t, s.DB, &store.SpeciesConfig{Species: "black", DisplayName: "黑猫", Enabled: 1})
	id, err := s.DB.AdoptAdditionalPet(kid.ID, "第二猫", "black", "安静")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Updates(map[string]any{"hunger": 40, "mood": 40, "last_tick": now}).Error; err != nil {
		t.Fatal(err)
	}
	pets := rawMulticats(t, s, kid.ID)
	if len(pets) != 2 || pets[1].ID != id {
		t.Fatalf("领养错误: %+v", pets)
	}
	return s, kid, pets, now
}

func rawMulticats(t *testing.T, s *Server, kidID int64) []store.Pet {
	t.Helper()
	var pets []store.Pet
	if err := s.DB.Where("kid_id=?", kidID).Order("id").Find(&pets).Error; err != nil {
		t.Fatal(err)
	}
	return pets
}

func multicatAccount(t *testing.T, s *Server, kidID int64) store.KidAccount {
	t.Helper()
	a, err := s.DB.Account(kidID)
	if err != nil || a == nil {
		t.Fatalf("账户读取失败: %v", err)
	}
	return *a
}

func requireMulticatAccount(t *testing.T, s *Server, kidID, balance, earned int64) {
	t.Helper()
	a := multicatAccount(t, s, kidID)
	if a.Balance != balance || a.EarnedTotal != earned {
		t.Fatalf("共享账户错误: %+v", a)
	}
}

func openMulticatTestDB(t *testing.T, dsn string) *store.DB {
	t.Helper()
	db, err := store.OpenDB(dsn, "", "")
	if err != nil {
		t.Fatalf("真实OpenDB启动失败: %v", err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func TestMultiPetOpenDBEmptyAndRepeatedStartup(t *testing.T) {
	raw, dsn := taskIntegrationConnection(t)
	if raw.Migrator().HasTable(&store.Pet{}) {
		t.Fatal("测试必须从空schema启动")
	}
	if err := store.PreparePetOwnership(raw.DB); err != nil {
		t.Fatal(err)
	}
	if raw.Migrator().HasTable(&store.Pet{}) {
		t.Fatal("准备方法不应创建表")
	}
	for i := 0; i < 2; i++ {
		db := openMulticatTestDB(t, dsn)
		for _, model := range []any{&store.Pet{}, &store.KidAccount{}, &store.FeedRequest{}, &store.ChatMessage{}} {
			if !db.Migrator().HasTable(model) {
				t.Fatalf("首次/重复启动缺表: %T", model)
			}
		}
		for _, model := range []any{&store.Family{}, &store.Kid{}, &store.Pet{}, &store.KidAccount{}} {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("空库凭空生成业务数据: %T %d/%v", model, count, err)
			}
		}
	}
}

func TestMultiPetOpenDBLegacyOwnerUniqueBeforeAutoMigrate(t *testing.T) {
	for _, kind := range []string{"自定义约束", "默认约束", "自定义索引"} {
		t.Run(kind, func(t *testing.T) {
			raw, dsn := taskIntegrationConnection(t)
			if err := raw.AutoMigrate(&store.Family{}, &store.Kid{}, &store.Pet{}, &store.ChatMessage{}, &store.OwnedItem{}, &store.SceneItem{}, &store.SettlementLedger{}); err != nil {
				t.Fatal(err)
			}
			family := store.Family{Code: "772233", Name: "旧数据启动测试"}
			mustTaskCreate(t, raw, &family)
			kid := store.Kid{FamilyID: family.ID, Name: "旧孩子"}
			mustTaskCreate(t, raw, &kid)
			now := time.Now().Truncate(time.Second)
			watermark := now.UnixNano()
			old := store.Pet{KidID: kid.ID, Name: "旧猫", Species: "black", Balance: 23, EarnedTotal: 680, Level: 7, Streak: 6,
				StreakBaseDate: "2026-11-01", StreakBaseValue: 4, StreakEvaluatedNS: &watermark, Hunger: 41, Mood: 32, LastTick: &now}
			mustTaskCreate(t, raw, &old)
			if err := raw.First(&old, old.ID).Error; err != nil {
				t.Fatal(err)
			}
			mustTaskCreate(t, raw, &store.ChatMessage{KidID: kid.ID, Role: "user", Content: "第一猫旧记忆", CreatedAt: now})
			mustTaskCreate(t, raw, &store.OwnedItem{KidID: kid.ID, ItemKey: "nest"})
			mustTaskCreate(t, raw, &store.SceneItem{KidID: kid.ID, ItemKey: "nest", X: 210, Y: 300})
			mustTaskCreate(t, raw, &store.SettlementLedger{KidID: kid.ID, Source: "task", Final: 47, DeltaBalance: 47, DeltaEarned: 47})
			ddl := "ALTER TABLE pet ADD CONSTRAINT old_pet_owner UNIQUE(kid_id)"
			if kind == "默认约束" {
				ddl = "ALTER TABLE pet ADD CONSTRAINT uni_pet_kid_id UNIQUE(kid_id)"
			}
			if kind == "自定义索引" {
				ddl = "CREATE UNIQUE INDEX old_pet_owner_index ON pet(kid_id)"
			}
			if err := raw.Exec(ddl).Error; err != nil {
				t.Fatal(err)
			}
			if err := raw.Exec("ALTER TABLE pet ADD CONSTRAINT keep_pet_owner_name UNIQUE(kid_id,name)").Error; err != nil {
				t.Fatal(err)
			}
			if kind == "自定义约束" {
				// 先证明fixture能重现旧顺序：GORM错误地DROP默认名，尚未运行准备方法。
				if err := raw.AutoMigrate(&store.Pet{}); err == nil || !strings.Contains(err.Error(), "uni_pet_kid_id") {
					t.Fatalf("fixture未复现自定义唯一约束启动错误: %v", err)
				}
			}
			for i := 0; i < 2; i++ {
				db := openMulticatTestDB(t, dsn)
				var saved store.Pet
				if err := db.First(&saved, old.ID).Error; err != nil || !reflect.DeepEqual(saved, old) {
					t.Fatalf("真实启动更改旧猫: %+v/%v", saved, err)
				}
				var account store.KidAccount
				if err := db.First(&account, "kid_id=?", kid.ID).Error; err != nil {
					t.Fatal(err)
				}
				if account.Balance != 23 || account.EarnedTotal != 680 || account.Level != 7 || account.Streak != 6 || account.StreakBaseValue != 4 || account.PrimaryPetID != old.ID || account.StreakEvaluatedNS == nil || *account.StreakEvaluatedNS != watermark {
					t.Fatalf("启动迁移丢成长: %+v", account)
				}
				var message store.ChatMessage
				if err := db.First(&message).Error; err != nil || message.PetID != old.ID || message.Content != "第一猫旧记忆" {
					t.Fatalf("启动丢失第一猫历史: %+v/%v", message, err)
				}
				for _, model := range []any{&store.OwnedItem{}, &store.SceneItem{}, &store.SettlementLedger{}} {
					var count int64
					if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
						t.Fatalf("启动更改家具流水: %T %d/%v", model, count, err)
					}
				}
				if !db.Migrator().HasConstraint(&store.Pet{}, "keep_pet_owner_name") || !db.Migrator().HasConstraint(&store.Pet{}, "pet_pkey") {
					t.Fatal("准备方法误删组合唯一约束或主键")
				}
				if db.Migrator().HasConstraint(&store.Pet{}, "old_pet_owner") || db.Migrator().HasConstraint(&store.Pet{}, "uni_pet_kid_id") || db.Migrator().HasIndex(&store.Pet{}, "old_pet_owner_index") {
					t.Fatal("真实启动仍残留单猫唯一限制")
				}
			}
			db := openMulticatTestDB(t, dsn)
			mustTaskCreate(t, db, &store.SpeciesConfig{Species: "black", Enabled: 1})
			if _, err := db.AdoptAdditionalPet(kid.ID, "第二猫", "black", ""); err != nil {
				t.Fatalf("真实启动后仍不能追加猫: %v", err)
			}
			if err := db.UpdatePet(kid.ID, map[string]any{"balance": 19}); err != nil {
				t.Fatal(err)
			}
			reopened := openMulticatTestDB(t, dsn)
			requireMulticatAccount(t, &Server{DB: reopened}, kid.ID, 19, 680)
			if pets := rawMulticats(t, &Server{DB: reopened}, kid.ID); len(pets) != 2 || pets[1].Balance != 0 || pets[1].EarnedTotal != 0 {
				t.Fatalf("重复启动复制账户或丢猫: %+v", pets)
			}
		})
	}
}

func TestMultiPetFeedCommittedSnapshotFailureAndSameIDRetry(t *testing.T) {
	for _, stage := range []string{"对账", "快照末尾计数"} {
		t.Run(stage, func(t *testing.T) {
			s, kid, _, _ := multicatFixture(t)
			r := multicatRouter(s, kid)
			if stage == "对账" {
				// Feed事务不访问daily_task；真实PG错误只在随后snapshot对账时发生。
				if err := s.DB.Exec("ALTER TABLE daily_task RENAME TO unavailable_daily_task").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.DB.Callback().Query().Before("gorm:query").Register("fail_snapshot_final_count", func(tx *gorm.DB) {
					if tx.Statement.Table == "kid_streak_day" {
						if _, ok := tx.Statement.Dest.(*int64); ok {
							tx.AddError(errors.New("模拟快照末尾计数失败"))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			const requestID = "提交后快照失败"
			w := taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"request_id": requestID})
			requireTaskStatus(t, w, 500)
			var failure struct {
				RequestID string `json:"request_id"`
				Committed bool   `json:"committed"`
				Fed       bool   `json:"fed"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure.RequestID != requestID || !failure.Committed || !failure.Fed || !strings.Contains(failure.Error, "已提交") {
				t.Fatalf("提交后500未明确语义: %s/%v", w.Body, err)
			}
			requireMulticatAccount(t, s, kid.ID, 90, 100)
			for _, pet := range rawMulticats(t, s, kid.ID) {
				if pet.Hunger != 70 || pet.Mood != 45 {
					t.Fatalf("500伪装回滚或状态错误: %+v", pet)
				}
			}
			var count int64
			if err := s.DB.Model(&store.FeedRequest{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("提交记录缺失: %d/%v", count, err)
			}
			// 后端仍故障时重试也必须回显同一请求已提交，不重复扣款。
			requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"request_id": requestID}), 500)
			requireMulticatAccount(t, s, kid.ID, 90, 100)
			if stage == "对账" {
				if err := s.DB.Exec("ALTER TABLE unavailable_daily_task RENAME TO daily_task").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.DB.Callback().Query().Remove("fail_snapshot_final_count"); err != nil {
					t.Fatal(err)
				}
			}
			w = taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"request_id": requestID})
			requireTaskStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"request_id":"`+requestID+`"`) || !strings.Contains(w.Body.String(), `"fed":true`) {
				t.Fatalf("恢复后未返回相同请求结果: %s", w.Body)
			}
			requireMulticatAccount(t, s, kid.ID, 90, 100)
			for _, pet := range rawMulticats(t, s, kid.ID) {
				if pet.Hunger != 70 || pet.Mood != 45 {
					t.Fatal("恢复重试二次加饱腹/心情")
				}
			}
			if err := s.DB.Model(&store.FeedRequest{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("恢复后重复请求记录")
			}
		})
	}
}

func TestMultiPetFeedPreCommitFailureDoesNotClaimCommitted(t *testing.T) {
	s, kid, _, _ := multicatFixture(t)
	if err := s.DB.Exec("ALTER TABLE feed_request RENAME TO unavailable_feed_request").Error; err != nil {
		t.Fatal(err)
	}
	w := taskTestRequest(multicatRouter(s, kid), http.MethodPost, "/api/pet/feed", map[string]any{"request_id": "事务内失败"})
	requireTaskStatus(t, w, 500)
	if strings.Contains(w.Body.String(), `"committed":true`) {
		t.Fatalf("未提交却宣称已提交: %s", w.Body)
	}
	requireMulticatAccount(t, s, kid.ID, 100, 100)
}

func TestMultiPetMigrationIdempotentPreservesLegacy(t *testing.T) {
	for _, constraint := range []bool{false, true} {
		t.Run(fmt.Sprint(constraint), func(t *testing.T) {
			s, kid, now := streakFixture(t, 6, false)
			if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 23, "earned_total": 680, "level": 7}); err != nil {
				t.Fatal(err)
			}
			// 重新构造真正的旧结构：只有旧猫成长列，没有孩子账户行。
			if err := s.DB.Where("kid_id=?", kid.ID).Delete(&store.KidAccount{}).Error; err != nil {
				t.Fatal(err)
			}
			old := rawMulticats(t, s, kid.ID)[0]
			mustTaskCreate(t, s.DB, &store.OwnedItem{KidID: kid.ID, ItemKey: "nest"})
			mustTaskCreate(t, s.DB, &store.SceneItem{KidID: kid.ID, ItemKey: "nest", X: 200, Y: 300})
			mustTaskCreate(t, s.DB, &store.ChatMessage{KidID: kid.ID, Role: "user", Content: "旧猫记忆", CreatedAt: now})
			mustTaskCreate(t, s.DB, &store.SettlementLedger{KidID: kid.ID, Source: "task", Final: 47, DeltaBalance: 47, DeltaEarned: 47})
			ddl := "CREATE UNIQUE INDEX idx_pet_kid_id ON pet(kid_id)"
			if constraint {
				ddl = "ALTER TABLE pet ADD CONSTRAINT old_pet_owner UNIQUE(kid_id)"
			}
			if err := s.DB.Exec(ddl).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.PreparePetOwnership(s.DB.DB); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := store.MigrateMultiPet(s.DB.DB); err != nil {
					t.Fatal(err)
				}
			}
			if got := rawMulticats(t, s, kid.ID)[0]; !reflect.DeepEqual(got, old) {
				t.Fatalf("迁移更改旧猫: %+v / %+v", old, got)
			}
			a := multicatAccount(t, s, kid.ID)
			if a.Balance != 23 || a.EarnedTotal != 680 || a.Level != 7 || a.Streak != 6 || a.PrimaryPetID != old.ID || a.StreakBaseDate != old.StreakBaseDate {
				t.Fatalf("迁移丢成长: %+v", a)
			}
			newPet := store.Pet{KidID: kid.ID, Name: "新猫"}
			mustTaskCreate(t, s.DB, &newPet)
			var chat store.ChatMessage
			if err := s.DB.First(&chat).Error; err != nil || chat.PetID != old.ID {
				t.Fatalf("老历史未归第一猫: %+v/%v", chat, err)
			}
			for _, table := range []any{&store.OwnedItem{}, &store.SceneItem{}, &store.SettlementLedger{}} {
				var n int64
				if err := s.DB.Model(table).Count(&n).Error; err != nil || n != 1 {
					t.Fatalf("旧家具流水丢失: %T %d/%v", table, n, err)
				}
			}
			if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 19}); err != nil {
				t.Fatal(err)
			}
			// 影子即使被旧写法污染，也不得再次成为账户真源。
			if err := s.DB.Model(&store.Pet{}).Where("id=?", old.ID).Update("balance", 999).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.MigrateMultiPet(s.DB.DB); err != nil {
				t.Fatal(err)
			}
			requireMulticatAccount(t, s, kid.ID, 19, 680)
			_, pets, _, err := s.DB.PetSnapshotAt(kid.ID, 0, now)
			if err != nil || pets[0].Balance != 19 || pets[1].Balance != 19 {
				t.Fatalf("读取未投影真源: %+v/%v", pets, err)
			}
		})
	}
}

func TestMultiPetAdoptCapConcurrencyFreeAndExpandable(t *testing.T) {
	t.Setenv("MAX_PETS_PER_KID", "2")
	s, kid, _ := streakFixture(t, 2, false)
	mustTaskCreate(t, s.DB, &store.SpeciesConfig{Species: "black", Enabled: 1})
	before := rawMulticats(t, s, kid.ID)[0]
	start := make(chan struct{})
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			<-start
			_, err := s.DB.AdoptAdditionalPet(kid.ID, fmt.Sprintf("小猫%d", i), "black", "")
			results <- err
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < 16; i++ {
		if err := <-results; err == nil {
			success++
		} else if !errors.Is(err, store.ErrPetLimit) {
			t.Fatal(err)
		}
	}
	if success != 1 || len(rawMulticats(t, s, kid.ID)) != 2 {
		t.Fatalf("并发突破上限或领养失败: %d", success)
	}
	requireMulticatAccount(t, s, kid.ID, before.Balance, before.EarnedTotal)
	if after := rawMulticats(t, s, kid.ID)[0]; !reflect.DeepEqual(before, after) {
		t.Fatal("追加猫更改原猫")
	}
	second := rawMulticats(t, s, kid.ID)[1]
	if second.Balance != 0 || second.EarnedTotal != 0 {
		t.Fatal("新猫复制了钱包或赠星")
	}
	t.Setenv("MAX_PETS_PER_KID", "3")
	if _, err := s.DB.AdoptAdditionalPet(kid.ID, "第三猫", "black", ""); err != nil {
		t.Fatal(err)
	}
	w := taskTestRequest(multicatRouter(s, kid), http.MethodGet, "/api/pet", nil)
	requireTaskStatus(t, w, 200)
	var response struct {
		Pet     store.Pet        `json:"pet"`
		Pets    []store.Pet      `json:"pets"`
		Account store.KidAccount `json:"account"`
		Limit   int              `json:"pet_limit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Limit != 3 || len(response.Pets) != 3 || response.Pet.ID != before.ID || response.Account.Balance != before.Balance {
		t.Fatalf("snapshot合同错误: %s", w.Body)
	}
	for _, p := range response.Pets {
		if p.Balance != before.Balance || p.EarnedTotal != before.EarnedTotal || p.Streak != 2 {
			t.Fatalf("共享成长投影错误: %+v", p)
		}
	}
	other := store.Kid{FamilyID: kid.FamilyID, Name: "首次领养孩子"}
	mustTaskCreate(t, s.DB, &other)
	if _, err := s.DB.AdoptPet(other.ID, "第一猫", "black", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.AdoptPet(other.ID, "第二猫", "black", ""); err == nil {
		t.Fatal("旧首次领养入口追加了猫")
	}
	requireMulticatAccount(t, s, other.ID, 30, 30)
}

func TestMultiPetAdoptValidationAndContract(t *testing.T) {
	t.Setenv("MAX_PETS_PER_KID", "3")
	s, kid, _ := streakFixture(t, 0, false)
	mustTaskCreate(t, s.DB, &store.SpeciesConfig{Species: "black", Enabled: 1})
	mustTaskCreate(t, s.DB, &store.SpeciesConfig{Species: "disabled", Enabled: 1})
	if err := s.DB.Model(&store.SpeciesConfig{}).Where("species='disabled'").Update("enabled", 0).Error; err != nil {
		t.Fatal(err)
	}
	r := multicatRouter(s, kid)
	for _, body := range []map[string]any{{"name": " ", "species": "black"}, {"name": strings.Repeat("猫", 33), "species": "black"}, {"name": "猫", "species": "disabled"}, {"name": "猫", "species": "missing"}, {"name": "猫", "species": "black", "personality": strings.Repeat("猫", 65)}} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/pet/adopt", body), 400)
	}
	w := taskTestRequest(r, http.MethodPost, "/api/pet/adopt", map[string]any{"name": "第二猫", "species": "black", "kid_id": 999})
	requireTaskStatus(t, w, 200)
	var response struct {
		Adopted bool             `json:"adopted"`
		NewID   int64            `json:"new_pet_id"`
		Pet     store.Pet        `json:"pet"`
		Pets    []store.Pet      `json:"pets"`
		Account store.KidAccount `json:"account"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Adopted || response.NewID != response.Pet.ID || len(response.Pets) != 2 || response.Account.Balance != 100 || response.Pet.Personality != "热情开朗" {
		t.Fatalf("领养合同错误: %s", w.Body)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/pet/adopt", map[string]any{"name": " 第二猫 ", "species": "black"}), 400)
}

func TestMultiPetIndependentTickAndSharedGroupFeed(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	last := now.Add(-10 * time.Hour)
	if err := s.DB.Model(&store.Pet{}).Where("id=?", pets[0].ID).Updates(map[string]any{"hunger": 80, "mood": 40, "hunger_decay": 2, "last_tick": last}).Error; err != nil {
		t.Fatal(err)
	}
	last = now.Add(-2 * time.Hour)
	if err := s.DB.Model(&store.Pet{}).Where("id=?", pets[1].ID).Updates(map[string]any{"hunger": 95, "mood": 99, "hunger_decay": 3, "last_tick": last}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.TickAt(kid.ID, now); err != nil {
		t.Fatal(err)
	}
	pets = rawMulticats(t, s, kid.ID)
	if pets[0].Hunger != 60 || pets[0].Mood != 35 || pets[1].Hunger != 89 || pets[1].Mood != 98 {
		t.Fatalf("Tick覆盖了独立状态: %+v", pets)
	}
	result, err := s.DB.FeedGroupAt(kid.ID, pets[1].ID, "一次开饭", now)
	if err != nil || !result.Fed || result.AllFull {
		t.Fatalf("开饭结果: %+v/%v", result, err)
	}
	pets = rawMulticats(t, s, kid.ID)
	if pets[0].Hunger != 90 || pets[1].Hunger != 100 || pets[0].Mood != 40 || pets[1].Mood != 100 {
		t.Fatalf("群体开饭或上限错误: %+v", pets)
	}
	requireMulticatAccount(t, s, kid.ID, 90, 100)
	if _, err := s.DB.FeedGroupAt(kid.ID, pets[0].ID, "一次开饭", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := rawMulticats(t, s, kid.ID); !reflect.DeepEqual(got, pets) {
		t.Fatal("幂等重试又扣钱或更新状态")
	}
	requireMulticatAccount(t, s, kid.ID, 90, 100)
}

func TestMultiPetGroupFeedConcurrentIdempotentAndFullNoCost(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	start := make(chan struct{})
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		go func(i int) {
			<-start
			result, err := s.DB.FeedGroupAt(kid.ID, pets[i%2].ID, "同一请求", now)
			if err == nil && !result.Fed {
				err = errors.New("未返回原始fed结果")
			}
			results <- err
		}(i)
	}
	close(start)
	for i := 0; i < 24; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	requireMulticatAccount(t, s, kid.ID, 90, 100)
	current := rawMulticats(t, s, kid.ID)
	if current[0].Hunger != 70 || current[1].Hunger != 70 {
		t.Fatalf("重复加饱腹: %+v", current)
	}
	var count int64
	if err := s.DB.Model(&store.FeedRequest{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("重复请求流水: %d/%v", count, err)
	}
	if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Updates(map[string]any{"hunger": 100, "mood": 40, "last_tick": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 0}); err != nil {
		t.Fatal(err)
	}
	before := rawMulticats(t, s, kid.ID)
	result, err := s.DB.FeedGroupAt(kid.ID, 0, "全猫饱", now)
	if err != nil || result.Fed || !result.AllFull {
		t.Fatalf("全饱no-op失败: %+v/%v", result, err)
	}
	requireMulticatAccount(t, s, kid.ID, 0, 100)
	if got := rawMulticats(t, s, kid.ID); !reflect.DeepEqual(got, before) {
		t.Fatal("全饱还加心情或修改状态")
	}
}

func TestMultiPetFeedInsufficientAndFailureRollback(t *testing.T) {
	for _, failure := range []string{"余额不足", "pet_event", "feed_request"} {
		t.Run(failure, func(t *testing.T) {
			s, kid, _, now := multicatFixture(t)
			if failure == "余额不足" {
				if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 5}); err != nil {
					t.Fatal(err)
				}
			}
			last := now.Add(-time.Hour)
			if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Update("last_tick", last).Error; err != nil {
				t.Fatal(err)
			}
			before := rawMulticats(t, s, kid.ID)
			account := multicatAccount(t, s, kid.ID)
			if failure != "余额不足" {
				if err := s.DB.Exec("ALTER TABLE " + failure + " RENAME TO unavailable_" + failure).Error; err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.DB.FeedGroupAt(kid.ID, 0, "失败重试", now)
			if err == nil {
				t.Fatal("失败被吞掉")
			}
			if failure == "余额不足" && !errors.Is(err, store.ErrInsufficientStars) {
				t.Fatalf("余额错误不正确: %v", err)
			}
			if got := rawMulticats(t, s, kid.ID); !reflect.DeepEqual(got, before) {
				t.Fatal("失败残留饱腹/心情/衰减或影子余额")
			}
			if got := multicatAccount(t, s, kid.ID); !reflect.DeepEqual(got, account) {
				t.Fatal("失败残留账户扣款")
			}
			if failure != "余额不足" {
				if err := s.DB.Exec("ALTER TABLE unavailable_" + failure + " RENAME TO " + failure).Error; err != nil {
					t.Fatal(err)
				}
				if result, err := s.DB.FeedGroupAt(kid.ID, 0, "失败重试", now); err != nil || !result.Fed {
					t.Fatalf("失败后相同ID不能重试: %+v/%v", result, err)
				}
				requireMulticatAccount(t, s, kid.ID, 90, 100)
			}
		})
	}
}

func TestMultiPetFeedAndSelectionHTTPContractAndKidIsolation(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	other := store.Kid{FamilyID: kid.FamilyID, Name: "其他孩子"}
	mustTaskCreate(t, s.DB, &other)
	foreign := store.Pet{KidID: other.ID, Name: "别人猫", Balance: 77, LastTick: &now}
	mustTaskCreate(t, s.DB, &foreign)
	r := multicatRouter(s, kid)
	before := rawMulticats(t, s, kid.ID)
	for _, path := range []string{fmt.Sprintf("/api/pet?pet_id=%d", foreign.ID), fmt.Sprintf("/api/chat/history?pet_id=%d", foreign.ID)} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodGet, path, nil), 404)
	}
	for _, path := range []string{"/api/pet/feed", "/api/pet/touch", "/api/chat"} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{"pet_id": foreign.ID, "request_id": "越权", "content": "你好"}), 404)
	}
	if got := rawMulticats(t, s, kid.ID); !reflect.DeepEqual(got, before) {
		t.Fatal("越权请求修改了本猫状态")
	}
	var saved store.Pet
	if err := s.DB.First(&saved, foreign.ID).Error; err != nil || saved.Balance != 77 {
		t.Fatal("越权修改别人的余额")
	}
	w := taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"pet_id": pets[1].ID, "request_id": "http开饭"})
	requireTaskStatus(t, w, 200)
	var response struct {
		Pet     store.Pet        `json:"pet"`
		Pets    []store.Pet      `json:"pets"`
		Account store.KidAccount `json:"account"`
		Fed     bool             `json:"fed"`
		AllFull bool             `json:"all_full"`
		Limit   int              `json:"pet_limit"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Fed || response.AllFull || response.Pet.ID != pets[1].ID || response.Account.Balance != 90 || response.Limit != 2 || len(response.Pets) != 2 {
		t.Fatalf("开饭合同错误: %s", w.Body)
	}
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"request_id": "http开饭"}), 200)
	requireMulticatAccount(t, s, kid.ID, 90, 100)
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 5}); err != nil {
		t.Fatal(err)
	}
	w = taskTestRequest(r, http.MethodPost, "/api/pet/feed", map[string]any{"request_id": "钱不够"})
	requireTaskStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "星星不够") {
		t.Fatalf("错误未中文: %s", w.Body)
	}
	// 老客户端省略body和猫ID仍成功，默认第一猫；返回旧字段加snapshot。
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 100}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pet/feed", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireTaskStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Pet.ID != pets[0].ID {
		t.Fatal("旧客户端feed不兼容")
	}
}

func TestMultiPetTouchIndependentAndSharedConcurrentQuota(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	if left, err := s.DB.TouchPetAt(kid.ID, pets[1].ID, now); err != nil || left != 2 {
		t.Fatalf("抚摸失败: %d/%v", left, err)
	}
	got := rawMulticats(t, s, kid.ID)
	if got[0].Mood != 40 || got[1].Mood != 45 {
		t.Fatal("抚摸串猫")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, err := s.DB.TouchPetAt(kid.ID, pets[i%2].ID, now); errs <- err }(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got = rawMulticats(t, s, kid.ID)
	if got[0].Mood+got[1].Mood != 95 {
		t.Fatalf("超过孩子每日3次: %+v", got)
	}
	var count int64
	if err := s.DB.Model(&store.PetEvent{}).Where("kid_id=? AND type='touch'", kid.ID).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("抚摸额度流水: %d/%v", count, err)
	}
	if _, err := s.DB.TouchPetAt(kid.ID, 0, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestMultiPetChatIsolationLegacyAndSharedQuota(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	mustTaskCreate(t, s.DB, &store.ChatMessage{KidID: kid.ID, Role: "user", Content: "老历史", CreatedAt: now.Add(-48 * time.Hour)})
	r := multicatRouter(s, kid)
	for i, p := range pets {
		requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/chat", map[string]any{"pet_id": p.ID, "content": fmt.Sprintf("猫%d独立消息", i)}), 200)
	}
	for i, p := range pets {
		w := taskTestRequest(r, http.MethodGet, fmt.Sprintf("/api/chat/history?pet_id=%d", p.ID), nil)
		requireTaskStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), fmt.Sprintf("猫%d独立消息", i)) || strings.Contains(w.Body.String(), fmt.Sprintf("猫%d独立消息", 1-i)) {
			t.Fatalf("猫历史串了: %s", w.Body)
		}
		if strings.Contains(w.Body.String(), "老历史") != (i == 0) {
			t.Fatalf("老历史没有只归第一猫: %s", w.Body)
		}
	}
	if prompt := s.catSystemPrompt(kid.ID, &pets[1]); !strings.Contains(prompt, pets[1].Name) || strings.Contains(prompt, pets[0].Name) {
		t.Fatal("提示词身份不是选中猫")
	}
	start := make(chan struct{})
	results := make(chan error, 30)
	for i := 0; i < 30; i++ {
		go func(i int) {
			<-start
			_, err := s.DB.BeginPetChat(kid.ID, pets[i%2].ID, "共享额度", CHAT_DAILY_LIMIT, now)
			results <- err
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < 30; i++ {
		if err := <-results; err == nil {
			success++
		} else if !errors.Is(err, store.ErrChatLimit) {
			t.Fatal(err)
		}
	}
	if success != 13 {
		t.Fatalf("额度不共享或并发越限: 成功%d", success)
	}
	w := taskTestRequest(r, http.MethodPost, "/api/chat", map[string]any{"pet_id": pets[0].ID, "content": "额度已满"})
	requireTaskStatus(t, w, 429)
	for _, p := range pets {
		w := taskTestRequest(r, http.MethodGet, fmt.Sprintf("/api/chat/history?pet_id=%d", p.ID), nil)
		if !strings.Contains(w.Body.String(), `"left_today":0`) {
			t.Fatalf("两猫额度显示不一致: %s", w.Body)
		}
	}
	if _, err := s.DB.BeginPetChat(kid.ID, 0, "明天额度", CHAT_DAILY_LIMIT, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestMultiPetSharedTaskRewardShopAndWishDoNotMultiply(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	if !settleStreak(t, s, dt, now) {
		t.Fatal("未结算")
	}
	settleStreak(t, s, dt, now)
	requireMulticatAccount(t, s, kid.ID, 111, 111)
	got := rawMulticats(t, s, kid.ID)
	if got[0].Mood != 50 || got[1].Mood != 50 || got[1].Balance != 0 || got[1].EarnedTotal != 0 {
		t.Fatalf("任务奖励未全猫应用或重复账户: %+v", got)
	}
	r := multicatRouter(s, kid)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "cat-tree"}), 200)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "cat-tree"}), 400)
	requireMulticatAccount(t, s, kid.ID, 61, 111)
	wish := store.Wish{KidID: kid.ID, Title: "愿望", Status: "saving", Price: 20}
	mustTaskCreate(t, s.DB, &wish)
	path := fmt.Sprintf("/api/wish/%d/redeem", wish.ID)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{}), 200)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, path, map[string]any{}), 400)
	requireMulticatAccount(t, s, kid.ID, 41, 111)
	other := store.Kid{FamilyID: kid.FamilyID, Name: "另一个孩子"}
	mustTaskCreate(t, s.DB, &other)
	foreign := store.Wish{KidID: other.ID, Title: "别人愿望", Status: "saving", Price: 10}
	mustTaskCreate(t, s.DB, &foreign)
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, fmt.Sprintf("/api/wish/%d/redeem", foreign.ID), map[string]any{}), 400)
	requireMulticatAccount(t, s, kid.ID, 41, 111)
	_, snapshot, a, err := s.DB.PetSnapshotAt(kid.ID, pets[1].ID, now)
	if err != nil || a.Streak != 1 || snapshot[0].Balance != 41 || snapshot[1].Balance != 41 {
		t.Fatalf("任务钱包投影不一致: %+v/%v", snapshot, err)
	}
}

func TestMultiPetInvalidRequestsBeforeDatabase(t *testing.T) {
	r := multicatRouter(&Server{}, store.Kid{ID: 1, FamilyID: 1})
	for _, body := range []string{`{"request_id":null}`, `{"request_id":3}`, `{"request_id":{}}`, `{"request_id":""}`, `{"request_id":"   "}`, `{"request_id":"ok","pet_id":-1}`, `{"request_id":"ok","pet_id":"1"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/pet/feed", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		requireTaskStatus(t, w, 400)
	}
	for _, path := range []string{"/api/pet?pet_id=-1", "/api/pet?pet_id=abc", "/api/chat/history?pet_id=-1"} {
		requireTaskStatus(t, taskTestRequest(r, http.MethodGet, path, nil), 400)
	}
}

func TestMultiPetKidAuthenticationAndFullHTTPNoCost(t *testing.T) {
	s, kid, pets, now := multicatFixture(t)
	gin.SetMode(gin.TestMode)
	r := s.Router()
	requireTaskStatus(t, taskTestRequest(r, http.MethodPost, "/api/pet/adopt", map[string]any{"name": "第三猫", "species": "black"}), 401)
	if err := s.DB.SaveToken("multicat-test-token", kid.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.Pet{}).Where("kid_id=?", kid.ID).Updates(map[string]any{"hunger": 100, "last_tick": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.UpdatePet(kid.ID, map[string]any{"balance": 0}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/pet/feed", strings.NewReader(fmt.Sprintf(`{"request_id":"full-http","pet_id":%d}`, pets[1].ID)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kid-Token", "multicat-test-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireTaskStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `"fed":false`) || !strings.Contains(w.Body.String(), `"all_full":true`) {
			t.Fatalf("全饱合同错误: %s", w.Body)
		}
	}
	requireMulticatAccount(t, s, kid.ID, 0, 100)
}

func TestMultiPetShopDoesNotWaitForUnrelatedKidLock(t *testing.T) {
	s, kid, _, _ := multicatFixture(t)
	other := store.Kid{FamilyID: kid.FamilyID, Name: "不相关孩子"}
	mustTaskCreate(t, s.DB, &other)
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		finished <- s.DB.WithKidLock(other.ID, func(tx *store.DB) error { close(held); <-release; return nil })
	}()
	waitTaskSignal(t, held)
	buyDone := make(chan *httptest.ResponseRecorder, 1)
	r := multicatRouter(s, kid)
	go func() { buyDone <- taskTestRequest(r, http.MethodPost, "/api/shop/buy", map[string]any{"key": "nest"}) }()
	select {
	case w := <-buyDone:
		requireTaskStatus(t, w, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("购买依赖了不相关孩子的锁")
	}
	unblock()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	requireMulticatAccount(t, s, kid.ID, 70, 100)
}

func TestMultiPetShopFailureRollsBackSharedWallet(t *testing.T) {
	s, kid, _, _ := multicatFixture(t)
	if err := s.DB.Exec("ALTER TABLE pet_event RENAME TO unavailable_pet_event").Error; err != nil {
		t.Fatal(err)
	}
	w := taskTestRequest(multicatRouter(s, kid), http.MethodPost, "/api/shop/buy", map[string]any{"key": "cat-tree"})
	requireTaskStatus(t, w, 400)
	requireMulticatAccount(t, s, kid.ID, 100, 100)
	var count int64
	if err := s.DB.Model(&store.OwnedItem{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("失败残留商品")
	}
}

func TestMultiPetGroupRewardRollbackAfterAccountWrite(t *testing.T) {
	s, kid, _, now := multicatFixture(t)
	dt := streakTask(t, s, kid, now, 1, "a", "weekday_fixed", "pending")
	before := rawMulticats(t, s, kid.ID)
	if err := s.DB.Exec("ALTER TABLE pet_event RENAME TO unavailable_pet_event").Error; err != nil {
		t.Fatal(err)
	}
	err := s.DB.TaskTransaction(func(tx *gorm.DB) error { _, err := engine.SettleAt(&store.DB{DB: tx}, dt.ID, 0, now); return err })
	if err == nil {
		t.Fatal("任务事件失败被吞")
	}
	requireMulticatAccount(t, s, kid.ID, 100, 100)
	if got := rawMulticats(t, s, kid.ID); !reflect.DeepEqual(got, before) {
		t.Fatal("失败残留任务心情或影子")
	}
	var count int64
	if err := s.DB.Model(&store.SettlementLedger{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("任务失败残留流水")
	}
}
