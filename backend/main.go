package main

import (
	"strings"
	"log"
	"os"

	"meowquest/backend/internal/api"
	"meowquest/backend/internal/engine"
	"meowquest/backend/internal/store"
)

func main() {
	loadDotEnv()
	pgDSN := envOr("MEOW_PG_DSN", "") // PG 优先（本地：host=127.0.0.1 port=5433 user=meow dbname=meowquest sslmode=disable）
	dsn := envOr("MEOW_MYSQL_DSN", "root:@tcp(127.0.0.1:3306)/meowquest?parseTime=true&charset=utf8mb4&loc=Local")
	pin := envOr("MEOW_ADMIN_PIN", "0426") // 家长 PIN，生产用环境变量覆盖
	addr := envOr("MEOW_ADDR", "127.0.0.1:8600")

	db, err := store.OpenDB(pgDSN, dsn, pin)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	// 初始品种（将来由家长后台增删）
	db.SeedSpecies([]store.SpeciesConfig{
		{Species: "orange", DisplayName: "橘猫", Description: "热情开朗，像小太阳"},
		{Species: "cow", DisplayName: "奶牛猫", Description: "呆萌可爱，自带表情包"},
		{Species: "black", DisplayName: "黑猫", Description: "高冷神秘，其实很黏人"},
		{Species: "white", DisplayName: "白猫", Description: "安静优雅，爱干净"},
	})

	srv := api.New(db, pin)
	engine.StartCron(db) // 每日 00:05 物化 + 启动补物化
	log.Printf("meowquest backend listening on %s (gorm + mysql ok)", addr)
	if err := srv.Router().Run(addr); err != nil {
		log.Fatal(err)
	}
}

// loadDotEnv 本地开发：加载 backend/.env（线上用 systemd EnvironmentFile，不走这里）
func loadDotEnv() {
	b, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
			if os.Getenv(k) == "" { // 真环境变量优先
				os.Setenv(k, v)
			}
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
