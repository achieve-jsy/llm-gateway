#!/bin/bash
# 回滚脚本 - 在 prod 服务器 (192.0.2.10) 执行
# 用法: ./rollback.sh [VERSION]
#   VERSION: 要回滚到的镜像 tag（如 v0.1.0），不填则回滚到上一个版本
#
# 示例:
#   ./rollback.sh v0.1.0    # 回滚到指定版本
#   ./rollback.sh           # 回滚到上一个部署版本

set -e

DEPLOY_PATH="/opt/llm-gateway"
REGISTRY="192.0.2.10:5000"

cd "${DEPLOY_PATH}"

if [ -n "$1" ]; then
    TARGET_TAG="$1"
    echo "回滚到指定版本: ${TARGET_TAG}"
else
    # 从当前运行的容器获取上一个版本
    CURRENT=$(docker inspect llm-gw-gateway --format '{{.Config.Image}}' 2>/dev/null | cut -d: -f2)
    echo "当前版本: ${CURRENT}"
    echo "请指定回滚目标版本，如: ./rollback.sh v0.1.0"
    echo ""
    echo "Registry 中可用的镜像:"
    curl -sf http://${REGISTRY}/v2/llm-gateway/tags/list 2>/dev/null | python3 -m json.tool || echo "(无法查询 Registry)"
    exit 1
fi

echo "=== 开始回滚到 ${TARGET_TAG} ==="

# 拉取目标版本镜像
docker pull ${REGISTRY}/llm-gateway:${TARGET_TAG}
docker pull ${REGISTRY}/llm-control-plane:${TARGET_TAG}
docker pull ${REGISTRY}/llm-web:${TARGET_TAG}

# tag 为本地名
docker tag ${REGISTRY}/llm-gateway:${TARGET_TAG} llm-gateway:${TARGET_TAG}
docker tag ${REGISTRY}/llm-control-plane:${TARGET_TAG} llm-control-plane:${TARGET_TAG}
docker tag ${REGISTRY}/llm-web:${TARGET_TAG} llm-web:${TARGET_TAG}

# 用目标版本重启
IMAGE_TAG=${TARGET_TAG} docker compose up -d --remove-orphans

echo "=== 回滚完成 ==="
docker compose ps

# 健康检查
echo ""
echo "等待服务启动..."
sleep 10
if wget -q --spider http://localhost:8080/health 2>/dev/null || curl -sf http://localhost:80/health >/dev/null; then
    echo "✓ Gateway 健康"
else
    echo "✗ Gateway 健康检查失败！"
    exit 1
fi
