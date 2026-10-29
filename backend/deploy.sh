#!/bin/bash
# meowquest 一键上线：交叉编译 → scp → 服务器替换重启 → 健康检查
# 前提：改过前端先跑 frontend/build.sh（产物会 embed 进本二进制）
# 服务器配置用环境变量可覆盖：MEOW_SERVER（默认 kiki）
set -e
cd "$(dirname "$0")"

# 真实服务器/域名放 backend/.deploy.local（gitignore，模板见 .deploy.local.example）
[ -f "$(dirname "$0")/.deploy.local" ] && source "$(dirname "$0")/.deploy.local"
SERVER=${MEOW_SERVER:-your-server-alias}
REMOTE_DIR=${MEOW_REMOTE_DIR:-/home/ubuntu/meowquest}
URL=${MEOW_URL:-https://meow.example.com}

echo "[1/4] 交叉编译 linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/meowquest-linux .

echo "[2/4] 上传到 $SERVER..."
scp -o BatchMode=yes /tmp/meowquest-linux "$SERVER:$REMOTE_DIR/meowquest.new"

echo "[3/4] 服务器替换并重启（~5 秒停机）..."
ssh -o BatchMode=yes "$SERVER" "
sudo systemctl stop meowquest && \
mv $REMOTE_DIR/meowquest.new $REMOTE_DIR/meowquest && \
chmod +x $REMOTE_DIR/meowquest && \
sudo systemctl start meowquest
"

echo "[4/4] 健康检查..."
sleep 3
ssh -o BatchMode=yes "$SERVER" "systemctl is-active meowquest || (sudo journalctl -u meowquest -n 20 --no-pager; exit 1)"
curl -s -o /dev/null -w "线上: $URL → %{http_code}\n" "$URL/"

echo "✅ 部署完成：$URL"
