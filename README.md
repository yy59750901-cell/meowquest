# meowquest · 喵星成长屋

给孩子做的任务养成 Web 应用：领养像素猫 → 完成每日任务赚星星 → 喂猫/买道具/兑换愿望。

## 技术栈

- **backend/**：Go + gin + GORM + MySQL（单二进制，前端已 embed）
- **frontend/**：Vue 3 + Vite + Vant + 像素猫引擎（hyclub 风格帧动画）

## 运行

```bash
# 后端（需要本地 MySQL，root 空密码；自动建库 meowquest + 建表 + 物化任务）
cd backend
go build -o meowquest .
MEOW_ADMIN_PIN=0426 MEOW_ADDR=0.0.0.0:8600 ./meowquest
```

- 孩子端：`http://<ip>:8600/`（免登录，领养后设备 token 保存 1 年，支持 PWA 加主屏）
- 家长后台：`http://<ip>:8600/#/admin`（PIN 见 `MEOW_ADMIN_PIN`）

## 重新构建前端

```bash
cd frontend
npm install && npm run build
rm -rf ../backend/internal/web/dist
cp -r dist ../backend/internal/web/dist
cd ../backend && go build -o meowquest .
```

（`backend/internal/web/dist` 是 embed 所需的前端产物，随仓库提交以保证 clone 即可编译）

## 核心机制

- 任务两调度模式：固定周几 / 每周 N 次（周锚点周一，达标置灰不断签）
- 确认流水线：自动(a) / 家长确认(c)（可组合），驳回退回可重做，宽限次日 24:00
- 奖励 = 基础分 × 日期倍数(周末/节假日/生日) × 连击系数(1+0.1n，封顶 1.5)
- 星星币双记账：balance 消费 / earned_total 只增不减驱动等级
- 愿望系统：孩子许愿 → 家长定价（按近 30 天日均估算达成天数）→ 攒星 → 兑换 → 兑现
- 宠物：惰性衰减（饱腹/心情读时结算），喂食放置交互，道具可拖拽摆放+猫自主互动
- 每日 00:05 物化 cron + 日收尾（断签评估、超宽限作废）

设计文档见仓库 `docs/`（任务三维度、状态机、数据表、分期规划）。
