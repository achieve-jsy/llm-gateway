#!/bin/bash
# 停止测试服务并清理

echo "🛑 停止测试服务"
echo "======================================"

# 读取 PID
if [ -f /tmp/llm-gateway-test-cp.pid ]; then
    CP_PID=$(cat /tmp/llm-gateway-test-cp.pid)
    if kill -0 $CP_PID 2>/dev/null; then
        echo "停止 Control Plane (PID: $CP_PID)..."
        kill $CP_PID
        sleep 1
        kill -9 $CP_PID 2>/dev/null || true
    fi
    rm -f /tmp/llm-gateway-test-cp.pid
fi

if [ -f /tmp/llm-gateway-test-gw.pid ]; then
    GW_PID=$(cat /tmp/llm-gateway-test-gw.pid)
    if kill -0 $GW_PID 2>/dev/null; then
        echo "停止 Gateway (PID: $GW_PID)..."
        kill $GW_PID
        sleep 1
        kill -9 $GW_PID 2>/dev/null || true
    fi
    rm -f /tmp/llm-gateway-test-gw.pid
fi

# 清理端口（以防进程未完全退出）
echo "检查端口占用..."
if lsof -Pi :18080 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "强制释放端口 18080..."
    lsof -Pi :18080 -sTCP:LISTEN -t | xargs kill -9 2>/dev/null || true
fi

if lsof -Pi :18081 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "强制释放端口 18081..."
    lsof -Pi :18081 -sTCP:LISTEN -t | xargs kill -9 2>/dev/null || true
fi

echo ""
echo "======================================"
echo "✅ 测试服务已停止"
echo ""
echo "日志文件保留在: logs/test/"
echo "清理日志: rm -rf logs/test/"
EOF
