# meowquest · 喵星成长屋

给孩子做的任务养成 Web 应用：领养像素猫 → 完成每日任务赚星星 → 喂猫/买道具/兑换愿望；任务可拍照/录音提交，AI 先判定、家长再确认，结论会以"猫的留言"回到孩子手里。

**线上**：https://meow.example.com

## 技术栈

- **backend/**：Go + gin + GORM，**单二进制**（前端 embed 进二进制，~28MB）
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
- 证书 Let's Encrypt（SAN 含 meow.example.com，泛解析 DNS 零配置）
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
- 确认流水线：自动(a)/家长确认(c)/AI 初审+家长终审(b)。任务当日提交，审核截止为任务日的次日 24:00；驳回后可在任务当日重新提交材料，达标计时保留。全屏计时页 + 防挂机（随机弹窗 15s 不点暂停）
- 证据链路：拍照/录像走 file input（iPad 调起相机）；录音**内置录制器**（点一下开始、再点一下结束，可试听/重录）或上传音频文件 → `/api/upload`（限 20MB）
- AI 验收（b 模式）：提交即返回，判定丢进数据库队列异步跑；家长端可看状态、重试、翻历史；通过后家长确认才结算
- 猫的信箱：家长确认时把 AI 的话、评分、关键点写成留言，孩子端信箱红点提醒，随时可回看
- 奖励 = 基础分 × 日期倍数 × 连击系数(1+0.1n 封顶 1.5)；星星双记账（balance 消费 / earned_total 只增驱动等级）
- 愿望：孩子许愿 → 家长定价（日均健康度参考）→ 攒星 → 兑换 → 兑现
- 宠物：惰性衰减（饱腹/心情读时结算）；喂食放置式交互；道具可拖拽 + 猫自主互动（咬植物/追毛线/睡猫窝）

详细设计见 `docs/pet-quest-design.md`（注意其"实现状态修订"表）。

## AI 验收与反馈链路

```
孩子提交 → 立刻返回 submitted（不等 AI）
    ↓
ai_review_job 入队（租约 + 次数上限 + 过期租约回收，worker 每秒轮询）
    ↓
照片走 768px 缩略图 URL（原图直出会让模型侧下载超时）
录音走 ASR 转写，与照片一起交给判定模型
    ↓
结论（pass / score / 给家长的理由 / 给孩子的话 / 关键点）写回 completion
    ↓
家长端确认：通过 → 当日连击确定后结算星星；驳回 → 截止前补交，不用重新计时
    ↓
写 kid_message → 孩子端信箱红点 + 猫的留言（可回看）
```

- 聊天 / 验收 / ASR **三处接入方独立可配**（`MEOW_LLM_*` / `MEOW_REVIEW_*` / `MEOW_ASR_*`），见 `docs/CONFIG.md`
- 验收输出预算 `MEOW_REVIEW_MAX_TOKENS`（默认 1024；接思考型模型要调大，因为思考与正文共享这个预算）
- 审核通过与到账分开：`approved/pending` 表示已通过、奖励等待连击确定；确定后自动变为 `approved/settled`，不用再次审核。
- 每笔 task 奖励通过孩子级行锁、ReadCommitted 领域事务和已有流水检查保证只发一次；重复请求或补扫不会再次奖励。维护时不要绕过该入口直接写状态。

## 连击（日结果为唯一依据）

按北京时间的 `kid_id + daily_task.date` 计算，审核时间不改变任务归属日。连击参与资格和日期倍数保存于任务快照，不使用实时模板解释已提交任务。

| 日结果 | 判定 | 连击 |
|---|---|---|
| success | 当日任一参与连击任务通过 | +1；三个任务都完成也只 +1 |
| neutral | 有效整日请假，或提交窗口结束后无必做连击任务且无待审 | 不加不断 |
| pending | 当日仍可提交，或审核宽限内仍有待审且没有合格通过 | 不提前判断签，相关奖励待结算 |
| missed | 有固定日期连击任务，但提交/审核期限均已结束且无完成 | 归零 |

- `weekly_quota` 完成可贡献一个连击日，未做不单独导致断签；非连击任务不抢占每日推进资格，但仍享受最终日倍率。
- 当日已合格通过即 success，不用等当天其他任务；尚无候选的开放日期也不提前最终判 neutral，避免当天新增任务改变已发奖励。
- 前序日期待定时，后续成功日和奖励等待；补审、拒绝、请假或超时会自动解锁。用户界面显示待到账，不自动补扣已经发放的历史星星。
- `kid_streak_day` 保存日结果与 `streak_after`；`pet.streak` 是缓存。启动及每分钟统一补扫缺日、截止任务和待结算奖励，不依赖某次午夜 cron 恰好运行。
- 初次切换保存原连击基线，历史已通过任务和流水不重付、不回算。仍在宽限内的昨日待审纳入前序；旧快照缺少新字段按兼容默认处理，缺失调度历史无法精确还原。

回归测试需设置 `MEOWQUEST_TEST_POSTGRES_DSN`，否则数据库集成用例跳过；测试使用独立 schema，不连接生产。MySQL 事务明确使用 ReadCommitted，但当前验证仅覆盖 PostgreSQL 集成路径。

## 触屏约定（iPad / 手机）

页面可通过 VisualViewport 读取视觉视口缩放，但没有可靠的标准接口强制 Safari 恢复原比例，因此以预防误触为主。`KidHome` 的 `touch-action` 分布：

| 区域 | 值 | 说明 |
|---|---|---|
| `.room`（页面背景） | `manipulation` | 禁双击缩放，保留滚动与捏合 |
| `.scene-viewport` / `.scene-item` | `none` | 自绘拖拽区，手势全归页面自己处理 |
| `.inv-bar` | `pan-x` | 道具栏横向滚动 |
| `.panel-body` / `.chat-body` / `.modal-*` | `pan-y` | 纵向滚动 |
| `button` | `manipulation` | 禁双击 |

新增可点元素时检查其触摸区域及祖先链，避免遗漏 `touch-action` 防护；浏览器会综合触点及相关祖先的手势限制。可用 `elementFromPoint` 和 computed `touch-action` 初筛，Safari 真机手势仍需单独验收。

## P1 待做

- 页面内取流拍照（录音已内置，拍照仍走 file input）
- `evidence_check` 结果落字段并展示（判定里"材料是否齐全"目前只在正文里带一句）
- 无 `evidence_steps` 的单证据任务：点了直接弹文件选择器，录音入口得另做中间选择页
- 管理端孩子管理 UI、家长 PIN 修改、周报看板
