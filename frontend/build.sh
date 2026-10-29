#!/bin/bash
# meowquest 前端构建：npm build + 同步产物到后端 embed 目录（发版前置步骤）
set -e
cd "$(dirname "$0")"

echo "[1/2] npm run build..."
npm run build

echo "[2/2] 同步产物到 backend/internal/web/dist..."
rm -rf ../backend/internal/web/dist
cp -r dist ../backend/internal/web/dist

echo "✅ 前端产物已同步（下一步：cd ../backend && ./deploy.sh 上线）"
