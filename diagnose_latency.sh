#!/usr/bin/env bash
# diagnose_latency.sh
# 诊断 LLM Gateway 单次 vs 并发延迟差异
# 用法: ./diagnose_latency.sh <api_key> <model_name> [gateway_url]
#
# 输出每个请求的各阶段耗时，找到 1s→5s 的真正原因

set -euo pipefail

KEY="${1:?usage: $0 <api_key> <model_name> [gateway_url]}"
MODEL="${2:?usage: $0 <api_key> <model_name> [gateway_url]}"
GW="${3:-http://127.0.0.1:8080}"

PAYLOAD=$(cat <<EOF
{"model":"$MODEL","messages":[{"role":"user","content":"hi, reply with one word"}],"max_tokens":5,"stream":false}
EOF
)

# 颜色
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'; NC='\033[0m'; BOLD='\033[1m'

banner() { echo -e "\n${BOLD}══════════════════════════════════════════${NC}"; echo -e "${BOLD} $1${NC}"; echo -e "${BOLD}══════════════════════════════════════════${NC}"; }

# 单次请求，输出详细计时
single_request() {
  local label="$1"
  # curl -w 输出各阶段耗时（单位秒）
  result=$(curl -s -w "\n---TIMING---\ntime_namelookup:%{time_namelookup}\ntime_connect:%{time_connect}\ntime_appconnect:%{time_appconnect}\ntime_pretransfer:%{time_pretransfer}\ntime_starttransfer:%{time_starttransfer}\ntime_total:%{time_total}\nhttp_code:%{http_code}\n---END---" \
    -X POST "$GW/v1/chat/completions" \
    -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" \
    --max-time 30 2>&1)

  body=$(echo "$result" | sed '/---TIMING---/,$d')
  timing=$(echo "$result" | sed -n '/---TIMING---/,/---END---/p' | tail -n +2 | head -n -1)

  http_code=$(echo "$timing" | grep http_code | cut -d: -f2)
  t_total=$(echo "$timing" | grep time_total | cut -d: -f2)
  t_ttfb=$(echo "$timing" | grep time_starttransfer | cut -d: -f2)
  t_connect=$(echo "$timing" | grep time_connect | cut -d: -f2)
  t_dns=$(echo "$timing" | grep time_namelookup | cut -d: -f2)

  # 转为毫秒（awk 做浮点乘法）
  ms_total=$(awk "BEGIN{printf \"%.0f\", $t_total * 1000}")
  ms_ttfb=$(awk "BEGIN{printf \"%.0f\", $t_ttfb * 1000}")
  ms_connect=$(awk "BEGIN{printf \"%.0f\", $t_connect * 1000}")

  # 着色
  color=$GREEN
  [[ $ms_total -gt 3000 ]] && color=$YELLOW
  [[ $ms_total -gt 8000 ]] && color=$RED

  echo -e "  ${label}: ${color}${ms_total}ms${NC} (TTFB:${ms_ttfb}ms connect:${ms_connect}ms) HTTP:${http_code}"

  # 检查是否有错误/限流
  if echo "$body" | grep -q '"429"\|rate_limit\|Rate limit\|too many'; then
    echo -e "  ${RED}⚠ 上游返回 429 Rate Limit${NC}"
  fi
  if echo "$body" | grep -q '"503"\|service_unavailable\|unavailable'; then
    echo -e "  ${RED}⚠ 服务不可用 503${NC}"
  fi
  if echo "$body" | grep -q '"error"'; then
    err=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('error',{}).get('message','?'))" 2>/dev/null || echo "parse failed")
    echo -e "  ${RED}⚠ 错误: $err${NC}"
  fi

  echo "$ms_total"
}

# ── 1. 基线：单次请求 ──────────────────────────────────────────────────────
banner "Step 1: 单次请求基线（3次取平均）"
echo "  Gateway: $GW  Model: $MODEL"
echo ""

total_single=0
for i in 1 2 3; do
  ms=$(single_request "请求 $i")
  total_single=$((total_single + ms))
  sleep 0.5
done
avg_single=$((total_single / 3))
echo ""
echo -e "  平均: ${BOLD}${avg_single}ms${NC}"

# ── 2. 并发 3 个请求 ────────────────────────────────────────────────────────
banner "Step 2: 并发 3 个请求（同时发出）"
echo "  测试并发下各请求的耗时差异"
echo ""

tmpdir=$(mktemp -d)
trap "rm -rf $tmpdir" EXIT

# 并发发出 3 个请求，记录各自耗时
for i in 1 2 3; do
  (
    start_ns=$(date +%s%N 2>/dev/null || date +%s)
    resp=$(curl -s -w "\n%{time_total}\n%{http_code}\n%{time_starttransfer}" \
      -X POST "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $KEY" \
      -H "Content-Type: application/json" \
      -d "$PAYLOAD" \
      --max-time 30 2>&1)

    t_total=$(echo "$resp" | tail -3 | head -1)
    http_code=$(echo "$resp" | tail -2 | head -1)
    t_ttfb=$(echo "$resp" | tail -1)
    body=$(echo "$resp" | head -n -3)

    ms_total=$(awk "BEGIN{printf \"%.0f\", $t_total * 1000}")
    ms_ttfb=$(awk "BEGIN{printf \"%.0f\", $t_ttfb * 1000}")

    has_429=""
    has_err=""
    if echo "$body" | grep -q '"429"\|rate_limit\|Rate limit\|too many'; then
      has_429=" ${RED}[429 RATE LIMITED]${NC}"
    fi
    if echo "$body" | grep -q '"error"'; then
      err_msg=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('error',{}).get('type','?')+': '+d.get('error',{}).get('message','?')[:80])" 2>/dev/null || echo "error")
      has_err=" [ERR: $err_msg]"
    fi

    echo -e "  并发请求 $i: ${ms_total}ms (TTFB:${ms_ttfb}ms) HTTP:${http_code}${has_429}${has_err}"
  ) > "$tmpdir/result_$i.txt" 2>&1 &
done
wait

# 输出结果
for i in 1 2 3; do
  cat "$tmpdir/result_$i.txt" 2>/dev/null || echo "  并发请求 $i: 无结果"
done

# ── 3. 连续并发 3 轮（看是否随时间变长）──────────────────────────────────────
banner "Step 3: 连续 3 轮并发（每轮 3 并发，间隔 2s）"
echo "  观察是否随轮次升高（TTL 到期 + 限流叠加效应）"
echo ""

for round in 1 2 3; do
  echo -e "  ${BOLD}第 $round 轮:${NC}"
  for i in 1 2 3; do
    (
      resp=$(curl -s -w "\n%{time_total}\n%{http_code}" \
        -X POST "$GW/v1/chat/completions" \
        -H "Authorization: Bearer $KEY" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" \
        --max-time 30 2>&1)
      t_total=$(echo "$resp" | tail -2 | head -1)
      http_code=$(echo "$resp" | tail -1)
      body=$(echo "$resp" | head -n -2)
      ms=$(awk "BEGIN{printf \"%.0f\", $t_total * 1000}")

      tag=""
      if echo "$body" | grep -q '"429"\|rate_limit\|too many'; then
        tag="${RED}[429]${NC}"
      elif echo "$body" | grep -q '"error"'; then
        tag="${YELLOW}[ERR]${NC}"
      fi
      echo -e "    请求 $i: ${ms}ms HTTP:${http_code} $tag"
    ) &
  done
  wait
  echo ""
  [[ $round -lt 3 ]] && sleep 2
done

# ── 4. 检查 retry 退避（单独发一个可能触发 429 的）──────────────────────────
banner "Step 4: 快速连发 10 次（检查是否有 429 触发重试退避）"
echo "  观察哪次开始出现 429 或延迟突变"
echo ""

for i in $(seq 1 10); do
  ms=$(single_request "请求 $i")
  sleep 0.1  # 100ms 间隔
done

# ── 5. 汇总建议 ────────────────────────────────────────────────────────────
banner "诊断汇总"
echo ""
echo -e "  单次基线平均: ${BOLD}${avg_single}ms${NC}"
echo ""
echo "  如果 Step 2 中某个并发请求比基线慢 2x 以上："
echo "    → 原因 A: 上游 LLM 触发 429，retry 退避（1s+2s+4s）"
echo "    → 原因 B: 连接池耗尽，等待 TCP 建连"
echo ""
echo "  如果 Step 4 中第 N 次起出现 429："
echo "    → 上游 RPM/TPM 上限就是 N 次左右，需要更多渠道或降低并发"
echo ""
echo "  如果所有请求都慢（无 429）："
echo "    → 原因 C: 上游 LLM 本身在高并发下推理变慢（服务端排队）"
echo "    → 原因 D: 控制面压力大，鉴权/GetKeyGroups 响应变慢"
