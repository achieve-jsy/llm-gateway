#!/bin/bash
# 测试环境前端部署脚本
# 在本机（192.0.2.10）构建并部署前端到 /var/www/aimeter-test/dist/
#
# 用法: ./scripts/deploy-test-web.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
WEB_DIR="${PROJECT_DIR}/web"
DEPLOY_DIR="/var/www/aimeter-test/dist"

echo "=== 测试环境前端部署 ==="
echo "项目目录: ${WEB_DIR}"
echo "部署目录: ${DEPLOY_DIR}"
echo ""

# 1. 构建前端
echo "[1/3] 构建前端..."
cd "${WEB_DIR}"
npm install --prefer-offline 2>/dev/null || npm install
npm run build

echo ""

# 2. 部署到本机
echo "[2/3] 部署到 ${DEPLOY_DIR}..."
sudo mkdir -p "${DEPLOY_DIR}"
sudo rm -rf "${DEPLOY_DIR:?}"/*
sudo cp -r dist/* "${DEPLOY_DIR}/"

echo ""

# 3. 重载 nginx
echo "[3/3] 重载 nginx..."
NGINX_CONF="/www/server/nginx/conf/vhost/aimeter-test.conf"
CONF_SOURCE="${PROJECT_DIR}/deploy/docker/nginx-test.conf"

if [ ! -L "${NGINX_CONF}" ] && [ ! -f "${NGINX_CONF}" ]; then
    sudo ln -sf "${CONF_SOURCE}" "${NGINX_CONF}"
    echo "  已创建 nginx 配置软链: ${NGINX_CONF}"
fi

sudo nginx -t && sudo nginx -s reload
echo ""
echo "=== 部署完成 ==="
echo "访问地址: http://192.0.2.10:9080"
