# meowquest 配置手册

> 密钥永远不进 git。本文档只记录"有哪些配置、去哪找/改"。

## 环境变量总表

| 变量 | 作用 | 本地 | 线上 |
|---|---|---|---|
| `MEOW_PG_DSN` | PG 连接 | `backend/.env` | systemd `/etc/systemd/system/meowquest.service` |
| `MEOW_LLM_API_KEY` | qwen 平台 key（聊天+验收+ASR） | 同上 | 同上 |
| `MEOW_LLM_BASE_URL` | chat 兼容端点（默认值内置）| 可不填 | 可不填 |
| `MEOW_LLM_MODEL` | 聊天/验收模型（默认 qwen3.5-flash）| 可不填 | 可不填 |
| `MEOW_ASR_BASE_URL` | ASR 原生端点（默认值内置）| 可不填 | 可不填 |
| `MEOW_PUBLIC_URL` | ASR 需要的公网前缀 | 本地无公网可不填（ASR 不可用）| `https://meow.example.com` |
| `MEOW_ADDR` | 监听地址 | 127.0.0.1:8600 | 127.0.0.1:18600 |
| `MEOW_PIN` | 旧单租户默认 PIN（仅 MySQL legacy）| 可不填 | 不设 |

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

| 用途 | 模型 | 计价 |
|---|---|---|
| 猫聊天（SSE 流式）| qwen3.5-flash | 输入 ¥0.2/M、输出 ¥2/M（enable_thinking:false 必带）|
| AI 验收（看照片）| qwen3.5-flash vision（同模型，多模态消息）| 同上 |
| 语音转写 ASR | qwen-audio-3.0-asr-flash-filetrans | ¥0.00022/秒（异步任务+轮询模式）|

## 日常操作

```bash
# 本地起服务（读 backend/.env）
cd backend && go run .

# 发版（改前端先 build.sh）
cd frontend && ./build.sh && cd ../backend && ./deploy.sh

# 线上看日志
ssh kiki "sudo journalctl -u meowquest -f"
```
