#!/bin/bash
# 启动测试服务脚本

set -e

echo "🚀 启动测试服务（端口 18080/18081）"
echo "======================================"

# 加载测试环境变量
if [ ! -f .env.test ]; then
    echo "❌ 错误：.env.test 不存在，请先运行 ./scripts/prepare-test-env.sh"
    exit 1
fi

# 导出环境变量
set -a
source .env.test
set +a

# 检查端口是否被占用
if lsof -Pi :18080 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo "❌ 端口 18080 已被占用"
    lsof -Pi :18080 -sTCP:LISTEN
    exit 1
fi

if lsof -Pi :18081 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo "❌ 端口 18081 已被占用"
    lsof -Pi :18081 -sTCP:LISTEN
    exit 1
fi

# 创建日志目录
mkdir -p logs/test

# 启动 Control Plane
echo ""
echo "📡 启动 Control Plane (端口 18081)..."
./bin/control-plane-test > logs/test/control-plane.log 2>&1 &
CP_PID=$!
echo "Control Plane PID: $CP_PID"

# 等待 Control Plane 启动
sleep 3
if ! kill -0 $CP_PID 2>/dev/null; then
    echo "❌ Control Plane 启动失败"
    cat logs/test/control-plane.log
    exit 1
fi

# 检查健康状态
for i in {1..30}; do
    if curl -s http://localhost:18081/healthz > /dev/null 2>&1; then
        echo "✅ Control Plane 已就绪"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ Control Plane 健康检查超时"
        cat logs/test/control-plane.log
        kill $CP_PID 2>/dev/null
        exit 1
    fi
    sleep 1
done

# 启动 Gateway
echo ""
echo "🌐 启动 Gateway (端口 18080)..."
./bin/gateway-test > logs/test/gateway.log 2>&1 &
GW_PID=$!
echo "Gateway PID: $GW_PID"

# 等待 Gateway 启动
sleep 3
if ! kill -0 $GW_PID 2>/dev/null; then
    echo "❌ Gateway 启动失败"
    cat logs/test/gateway.log
    kill $CP_PID 2>/dev/null
    exit 1
fi

# 检查健康状态
for i in {1..30}; do
    if curl -s http://localhost:18080/health > /dev/null 2>&1; then
        echo "✅ Gateway 已就绪"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "❌ Gateway 健康检查超时"
        cat logs/test/gateway.log
        kill $GW_PID $CP_PID 2>/dev/null
        exit 1
    fi
    sleep 1
done

# 保存 PID 到文件
echo $CP_PID > /tmp/llm-gateway-test-cp.pid
echo $GW_PID > /tmp/llm-gateway-test-gw.pid

echo ""
echo "======================================"
echo "✅ 测试服务已启动"
echo ""
echo "服务信息："
echo "  Control Plane: http://localhost:18081 (PID: $CP_PID)"
echo "  Gateway:       http://localhost:18080 (PID: $GW_PID)"
echo ""
echo "日志文件："
echo "  Control Plane: logs/test/control-plane.log"
echo "  Gateway:       logs/test/gateway.log"
echo ""
echo "停止服务: ./scripts/stop-test-services.sh"
echo "查看日志: tail -f logs/test/*.log"
EOF
