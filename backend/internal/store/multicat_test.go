package store

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMaxPetsPerKid(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{{"", 2}, {"bad", 2}, {"0", 2}, {"-3", 2}, {"2", 2}, {"3", 3}, {"12", 12}} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("MAX_PETS_PER_KID", test.value)
			if got := MaxPetsPerKid(); got != test.want {
				t.Fatalf("上限=%d，期望%d", got, test.want)
			}
		})
	}
}

func TestPetIdentityValidation(t *testing.T) {
	for _, name := range []string{"", "   ", strings.Repeat("猫", 33), "猫\n猫", "猫\x00", string([]byte{0xff})} {
		if _, _, err := validatePetIdentity(name, "black", ""); err == nil {
			t.Fatalf("无效名字未拒绝: %q", name)
		}
	}
	name, personality, err := validatePetIdentity("  黑黑  ", "black", "")
	if err != nil || name != "黑黑" || personality != "热情开朗" {
		t.Fatalf("名字修剪或默认性格错误: %q/%q/%v", name, personality, err)
	}
	if _, _, err := validatePetIdentity("猫", "black", strings.Repeat("猫", 65)); err == nil {
		t.Fatal("过长性格未拒绝")
	}
}

func TestDecayPerPetAndGrowthProjection(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	firstTick, secondTick := now.Add(-10*time.Hour), now.Add(-2*time.Hour)
	pets := []Pet{{ID: 1, Hunger: 80, Mood: 40, HungerDecay: 2, LastTick: &firstTick},
		{ID: 2, Hunger: 95, Mood: 95, HungerDecay: 3, LastTick: &secondTick}}
	for i := range pets {
		decayPet(&pets[i], now)
	}
	if pets[0].Hunger != 60 || pets[0].Mood != 35 || pets[1].Hunger != 89 || pets[1].Mood != 94 {
		t.Fatalf("独立衰减错误: %+v", pets)
	}
	if decayPet(&pets[0], now) || decayPet(&pets[1], now.Add(-time.Minute)) {
		t.Fatal("重复Tick或回拨改变了状态")
	}
	account := KidAccount{KidID: 4, Balance: 17, EarnedTotal: 231, Level: 3, Streak: 5, PrimaryPetID: 1}
	original := account
	for i := range pets {
		projectGrowth(&pets[i], &account)
	}
	if !reflect.DeepEqual(original, account) || pets[0].Balance != 17 || pets[1].EarnedTotal != 231 || pets[1].Hunger != 89 || pets[0].Mood != 35 {
		t.Fatal("成长投影更改账户或覆盖了猫独立状态")
	}
	if _, err := selectPet(pets, 3); err == nil {
		t.Fatal("越权猫ID未拒绝")
	}
	if pet, _ := selectPet(pets, 0); pet.ID != 1 {
		t.Fatal("未默认最早猫")
	}
}

func TestAccountMigrationCopiesOnlyLegacyGrowth(t *testing.T) {
	watermark := int64(101)
	legacy := Pet{ID: 5, KidID: 8, Balance: 23, EarnedTotal: 680, Level: 7, Streak: 6,
		StreakBaseDate: "2026-11-01", StreakBaseValue: 4, StreakEvaluatedNS: &watermark, Hunger: 41, Mood: 32}
	account := accountFromPet(legacy)
	if account.PrimaryPetID != 5 || account.Balance != 23 || account.EarnedTotal != 680 || account.Level != 7 || account.Streak != 6 || account.StreakBaseValue != 4 || *account.StreakEvaluatedNS != 101 {
		t.Fatalf("迁移丢失旧成长: %+v", account)
	}
	if legacy.Hunger != 41 || legacy.Mood != 32 {
		t.Fatal("迁移触碰了独立猫状态")
	}
}
