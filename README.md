# meowquest · 喵星成长屋

给孩子做的任务养成 Web 应用：领养像素猫 → 完成每日任务赚星星 → 喂猫/买道具/兑换愿望。

**线上**：https://meow.keepthinking.me

## 技术栈

- **backend/**：Go + gin + GORM，**单二进制**（前端 embed 进二进制，~19MB）
- **frontend/**：Vue 3 + Vite + Vant + 像素猫帧动画引擎（hyclub 风格）
- **数据库**：PostgreSQL 16（**双库支持**：设 `MEOW_PG_DSN` 用 PG，不设回落 MySQL）

## 多租户模型

```
🏠 家庭（6位家庭码 + PIN）       ← 家长在管理端注册
 ├─ 👨‍👩 家长（凭码+PIN 进管理端，家庭内共用）
 └─ 🧒 孩子（凭家庭码进入，设备绑孩子，可多端/换人）
      └─ 🐱 猫/任务/星星/愿望/房间（孩子维度完全隔离）
```

- 任务模板家庭级 + **指派制**（task_assign：模板→孩子，新建默认指派全部孩子）
- 物化/结算/连击/断签 per-kid 遍历；确认队列/流水/愿望 join 孩子名
- 孩子端三段式进入：输家庭码 → 选"我是谁"（多端登录）→ 新孩子领养

## 线上部署（腾讯云 kiki）

```
浏览器 → nginx（conf.d/meow.conf, 443）→ 127.0.0.1:18600 → meowquest 二进制
                                          ↓
                          复用 kiki_chain-postgres-1 容器（宿主 127.0.0.1:15433）
                          独立库 meowquest + 用户 meow（与 hikiki_db 数据库级隔离）
```

- systemd 服务 `meowquest`（开机自启 + 崩溃自动拉起）
- 证书 Let's Encrypt（SAN 含 meow.keepthinking.me，泛解析 DNS 零配置）
- 二进制路径 `/home/ubuntu/meowquest/`，证据上传在 `uploads/`

## 发版（两条命令）

```bash
cd frontend && ./build.sh   # 改了前端才需要：构建+同步产物到 embed
cd backend && ./deploy.sh   # 交叉编译 → 上传 → 服务器替换重启 → 健康检查
```

## 本地开发

```bash
# PG（推荐）：先启动本地 PG
# pg_ctl -D ~/meowquest-pgdata -l ~/meowquest-pgdata/server.log -o "-p 5433" start
cd backend
MEOW_PG_DSN="host=127.0.0.1 port=5433 user=meow dbname=meowquest sslmode=disable" \
MEOW_ADDR=127.0.0.1:8600 go run .
# MySQL 模式：不设 MEOW_PG_DSN 自动回落（默认 root:@tcp(127.0.0.1:3306)/meowquest）
```

- 孩子端：`http://localhost:8600/`（PWA 可加主屏）
- 家长后台：`http://localhost:8600/#/admin`（注册家庭 → 家庭码 + PIN）

## 核心机制（摘要）

- 任务两调度模式：固定周几 / 每周 N 次（周锚点周一）；物化幂等 UNIQUE(date,kid_id,template_id)；**当天 pending 实例随配置即时刷新**（approved 保旧快照可追溯）
- 确认流水线：自动(a)/家长确认(c)，驳回退回重做，48h 宽限；全屏计时页 + 防挂机（随机弹窗 15s 不点暂停）
- 证据链路：拍照/录音/录像（file input capture → /api/upload → 确认队列查看）——AI 验收(b 模式)的地基
- 奖励 = 基础分 × 日期倍数 × 连击系数(1+0.1n 封顶 1.5)；星星双记账（balance 消费 / earned_total 只增驱动等级）
- 愿望：孩子许愿 → 家长定价（日均健康度参考）→ 攒星 → 兑换 → 兑现
- 宠物：惰性衰减（饱腹/心情读时结算）；喂食放置式交互；道具可拖拽 + 猫自主互动（咬植物/追毛线/睡猫窝）

详细设计见 `docs/pet-quest-design.md`（注意其"实现状态修订"表）。

## P1 待做

- 宠物对话接 LLM（每猫独立记忆——per-kid 地基已就绪）
- 页面内取流拍照（HTTPS 已具备条件，现在是 file input 兜底）
- 管理端孩子管理 UI、家长 PIN 修改、周报看板
