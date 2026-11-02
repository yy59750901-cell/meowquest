# meowquest 配置手册

> 密钥永远不进 git。本文档只记录"有哪些配置、去哪找/改"。

## 环境变量总表

| 变量 | 作用 | 本地 | 线上 |
|---|---|---|---|
| `MEOW_PG_DSN` | PG 连接 | `backend/.env` | systemd `/etc/systemd/system/meowquest.service` |
| `MEOW_ADDR` | 监听地址 | 127.0.0.1:8600 | 127.0.0.1:18600 |
| `MEOW_PUBLIC_URL` | 素材公网前缀（模型侧要能下载）| 本地无公网可不填（验收/ASR 不可用）| `https://meow.example.com` |
| `MEOW_ADMIN_PIN` | 旧单租户默认 PIN（现在按家庭存 PIN，仅 legacy 用）| 可不填 | 不设 |

**猫聊天**（`MEOW_LLM_*`）

| 变量 | 作用 | 默认 |
|---|---|---|
| `MEOW_LLM_API_KEY` | 平台 key | 空（不填则聊天/验收都不可用）|
| `MEOW_LLM_BASE_URL` | chat 兼容端点 | 千问 compatible-mode |
| `MEOW_LLM_MODEL` | 模型 | `qwen3.5-flash` |
| `MEOW_LLM_PROVIDER` | 协议：`openai` / `zhipu` | `openai` |
| `MEOW_LLM_MAX_TOKENS` | 聊天输出预算 | 300 |
| `MEOW_LLM_REASONING_EFFORT` | 思考强度（思考型接入方才用）| 不填 |

**语音转写**（`MEOW_ASR_*`，走原生 filetrans 接口，不是 chat 兼容端点）

| 变量 | 作用 | 默认 |
|---|---|---|
| `MEOW_ASR_BASE_URL` | ASR 原生端点 | 千问 `/api/v1` |
| `MEOW_ASR_API_KEY` | ASR key | 回落 `MEOW_LLM_API_KEY` |
| `MEOW_ASR_MODEL` | 转写模型 | `qwen-audio-3.0-asr-flash-filetrans` |

**AI 验收**（`MEOW_REVIEW_*`，**全部不填则沿用聊天侧配置**，保持旧行为）

| 变量 | 作用 | 默认 |
|---|---|---|
| `MEOW_REVIEW_PROVIDER` | 协议：`openai` / `zhipu` | 回落聊天侧 |
| `MEOW_REVIEW_BASE_URL` / `_API_KEY` / `_MODEL` | 验收专用接入方 | 回落聊天侧 |
| `MEOW_REVIEW_MAX_TOKENS` | 验收输出预算 | 1024（思考型模型会与思考共享预算，需调大）|
| `MEOW_REVIEW_REASONING_EFFORT` | 思考强度 | 不填 |

> 聊天使用独立的 `MEOW_LLM_*` 配置；验收可单独配 `MEOW_REVIEW_*`，未配置则回落聊天侧。切换平台需同时核对 `BASE_URL`、`API_KEY`、模型及协议，不能只改协议名；ASR 仍走 filetrans 原生接口，不会随聊天协议自动切换。

## 密钥在哪里

- **本地开发**：`backend/.env`（已 gitignore，从 `backend/.env.example` 复制模板填写）
- **线上**：`ssh kiki` → `sudo cat /etc/systemd/system/meowquest.service`（Environment= 行）
  - 改后需 `sudo systemctl daemon-reload && sudo systemctl restart meowquest`
  - （待办：迁移到 `/etc/meowquest.env` + `EnvironmentFile=`，文件 600 权限）
- **qwen 控制台**（轮换 key / 查用量）：platform.qianwenai.com → API Keys（key 属于千问 AI 平台，同账号兼容 chat/vision/ASR）

## 数据库

- **本地**：PostgreSQL 16（Postgres.app），端口 **5433**，用户 `meow` 免密，库 `meowquest`，数据目录 `~/meowquest-pgdata`
  - 启动：`pg_ctl -D ~/meowquest-pgdata -l ~/meowquest-pgdata/server.log -o "-p 5433" start`
- **线上**：复用 kiki 服务器 `kiki_chain-postgres-1` 容器（宿主 127.0.0.1:15433），独立库 `meowquest` + 用户 `meow`（密码见 systemd），与 hikiki_db 数据库级隔离
- 不设 `MEOW_PG_DSN` 自动回落 MySQL（本地 root@3306）

## 模型清单

| 用途 | 模型 | 计价 / 要点 |
|---|---|---|
| 猫聊天（SSE 流式）| qwen3.5-flash | 输入 ¥0.2/M、输出 ¥2/M（enable_thinking:false 必带）|
| AI 验收（看照片 + 读转写）| qwen3.5-flash vision（同模型，多模态消息）| 同上；输出预算 1024，可用 `MEOW_REVIEW_MAX_TOKENS` 调 |
| 语音转写 ASR | qwen-audio-3.0-asr-flash-filetrans | ¥0.00022/秒（异步任务 + 轮询模式）|

验收照片发的是 **768px 缩略图 URL**（`/api/media/thumbnail?path=...&size=768`），不是原图：

- 原图（几 MB）会让模型侧下载超时报 400 `Download multimodal file timed out`——这不是欠费
- 尺寸白名单只有 480（列表预览）和 768（验收），传别的或重复参数一律 400
- 缩略图按后缀 `.thumb.jpg` / `.review.jpg` 缓存在 uploads 目录，首次生成后长期复用

同一审核作业的上游 task_id 和成功转写进 `asr_cache`，失败重试可复用，避免不必要的重复转写；不同提交之间不承诺全局去重。

## 日常操作

```bash
# 本地起服务（读 backend/.env）
cd backend && go run .

# 发版（改前端先 build.sh）
cd frontend && ./build.sh && cd ../backend && ./deploy.sh

# 线上看日志
ssh kiki "sudo journalctl -u meowquest -f"
```
