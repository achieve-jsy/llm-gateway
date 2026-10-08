#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# A3（真实 ENOSPC）与**进程级 A7** 的隔离实例全流程（计划 4 / 任务 8 步骤 4）。
#
# ⛔ 只允许在**测试环境**的宿主上执行。它会在 `/srv/trace-a3.img` 上建一块**专用**回环
#    ext4（默认 1 GiB）、挂到 `/srv/trace-a3`，在这块盘上把空间真正写满（ENOSPC），
#    并起一个独立的网关实例（容器名 llm-a3-gateway，端口 8090）。
#    · **绝不允许**在 `/` 或生产 `/datalog` 上做填盘（2026-09-24 的事故形态；见 D66）。
#    · 设备护栏由 `trace-verify scratch-prepare` 在 `mount -o loop` **成功之后**执行：
#      目标必须与 `/` 和 `/datalog` **都不同设备**，否则立刻拒绝。
#    · 本实例**只跑 gateway**（复用测试环境已有的控制面/Redis/PG，刻意不定义 cp 服务
#      ⇒ 不存在 8091）；连接串一律来自环境变量，**没有默认值**。
#
# 前置（全部由操作者/CI 注入，本脚本不写任何密钥）：
#   export A3_DATABASE_URL=...        # 测试库
#   export A3_REDIS_ADDR=...          # 测试 Redis
#   export A3_REDIS_DB=...            # 测试环境没人用的库号
#   export A3_API_KEY=...             # 测试环境一个有效 key
#   # 可选：A3_REDIS_PASSWORD
# 另需在测试环境的控制面里先建一个**必然拒连**的上游（`base_url: http://127.0.0.1:9`）
# 与其上的 model/channel，并让一个对外模型名（下面用 a3-dead-upstream）指向它 ——
# 这样请求照样产生 trace 事件（上游失败不影响 trace 写入），A3 才有流量可写。
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

IMAGE=/srv/trace-a3.img
MOUNT=/srv/trace-a3
ARCHIVE_ROOT=/srv/trace-a3/trace-archive-v4
COMPOSE=deploy/verify/trace-a3/docker-compose.yml
CONTAINER=llm-a3-gateway
BASE_URL=http://127.0.0.1:8090
CHAT_URL=${BASE_URL}/v1/chat/completions
FILL=/srv/trace-a3/fill.bin
FILL_SIZE=980M
SEALER=/usr/local/bin/trace-sealer
MODEL=a3-dead-upstream

# run：每一步都打印它真正执行的命令与输出（破坏性步骤一律经此入口）。
run() {
  echo
  echo "\$ $*"
  "$@"
}

# traffic：打 $1 次**会失败**的 chat 请求（上游拒连 ⇒ 502），把 "http_code time_total"
# 逐行写到 $2。请求会失败，但 trace 事件照样产生 —— 这正是 A3 需要的写入负载。
traffic() {
  local n="$1" out="$2"
  seq 1 "$n" | xargs -P8 -I{} curl -s -o /dev/null -w '%{http_code} %{time_total}\n' \
    -X POST "$CHAT_URL" \
    -H "Authorization: Bearer $A3_API_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"${MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"a3\"}]}" > "$out"
}

# perc：从 stdin 的一列数值里算 n/p50/p95/p99/max（索引夹紧，不依赖 gawk 扩展）。
perc() {
  awk '
    { v[NR] = $1 }
    END {
      if (NR == 0) { print "n=0"; exit }
      p50 = int(NR * 0.50); if (p50 < 1) p50 = 1
      p95 = int(NR * 0.95); if (p95 < 1) p95 = 1
      p99 = int(NR * 0.99); if (p99 < 1) p99 = 1
      printf "n=%d p50=%.3f p95=%.3f p99=%.3f max=%.3f\n", NR, v[p50], v[p95], v[p99], v[NR]
    }'
}

# hist_show：打印某一轮的延迟直方图。**不做自动判定** —— 判定由人看数字（见交付报告）。
hist_show() {
  echo
  echo "--- 延迟直方图 [$1]（$2 次请求的 curl time_total，秒）---"
  awk '{print $2}' "$3" | sort -n | perc
}

# cleanup：**即使中途失败也必须清理**（trap EXIT）。它是唯一允许删镜像/目录的入口：
# 停容器 → `scratch-cleanup --confirm`（umount → losetup -d → rm -f 镜像 → rmdir 挂载点）
# → 复核「挂载点不再是挂载点、镜像已删」。
cleanup() {
  local rc=$?
  echo
  echo "=== 清理（trap EXIT，进入时 rc=$rc）==="
  run docker compose -f "$COMPOSE" down || true
  run go run ./cmd/trace-verify scratch-cleanup --image "$IMAGE" --mount "$MOUNT" --confirm || true
  echo "--- 清理后复核 ---"
  findmnt /srv/trace-a3 || echo "OK: findmnt /srv/trace-a3 无输出（挂载点已不再是挂载点）"
  if [ -e /srv/trace-a3.img ]; then
    echo "!!! 镜像仍在：$IMAGE（必须人工处理，不得留下 1 GiB 残留）"
  else
    echo "OK: 镜像已删"
  fi
  echo "=== 清理结束（原退出码 $rc）==="
}

# trap 必须在**第一个破坏性步骤之前**注册：否则中途失败会留下挂载中的回环设备与
# 占用中的 1 GiB 镜像，下一个跑 A3 的人会直接踩到残留。
trap cleanup EXIT

echo "=== 步骤 0：环境自查与门槛 ==="
run uname -a
for v in A3_DATABASE_URL A3_REDIS_ADDR A3_REDIS_DB A3_API_KEY; do
  if [ -z "${!v:-}" ]; then
    echo "!!! 缺少环境变量 $v（见本脚本头部的前置说明）"
    exit 2
  fi
done
echo "OK: A3_* 环境变量齐备（DATABASE_URL / REDIS_ADDR / REDIS_DB / API_KEY）"
echo "⛔ 停止点 1：确认本机是**测试环境**、/srv 空闲 ≥ 2 GiB、docker 可用。"
run docker version --format '{{.Server.Version}}'

echo
echo "=== 步骤 1：准备专用回环 ext4（容量预检 → 建镜像 → mkfs → mount → **挂载后**护栏 → 归档根）==="
run go run ./cmd/trace-verify scratch-prepare --image /srv/trace-a3.img --mount /srv/trace-a3 --size 1G --confirm

echo
echo "=== 步骤 2：复核归档根确实落在回环 FS 上（TARGET 必须是 /srv/trace-a3，不是 /）==="
run findmnt -T /srv/trace-a3/trace-archive-v4 --output TARGET,SOURCE,FSTYPE
run stat -c '%d %n' /srv/trace-a3/trace-archive-v4
run stat -c '%d %n' / /datalog
echo "⛔ 停止点 2：上面前三行的设备号必须两两不同（回环 FS ≠ / ≠ /datalog），否则停手。"

echo
echo "=== 步骤 3：起隔离网关实例（只跑 gateway；端口 8090）==="
run docker compose -f deploy/verify/trace-a3/docker-compose.yml up -d --wait

echo
echo "=== 步骤 4：记录基线（/health、/ready、正文根的设备号）==="
run curl -s -o /dev/null -w 'health=%{http_code}\n' "$BASE_URL/health"
run curl -s -o /dev/null -w 'ready=%{http_code}\n' "$BASE_URL/ready"
run stat -c '%d %n' /srv/trace-a3/trace-archive-v4
run df -h /srv/trace-a3

echo
echo "=== 步骤 5：填满盘（先 fallocate 占掉 980M，再靠写入吃掉最后几十 MB）+ 造流量 ==="
echo "⛔ 停止点 3：确认上面的控制面里已存在 base_url=http://127.0.0.1:9 的上游与 ${MODEL}。"
run traffic 200 /tmp/a3-lat-before.txt
hist_show 填盘前 200 /tmp/a3-lat-before.txt
run fallocate -l "$FILL_SIZE" "$FILL"
run df -h /srv/trace-a3
echo "--- 2000 次会失败的请求（上游拒连；trace 事件照样产生）---"
run traffic 2000 /tmp/a3-lat-during.txt
run df -h /srv/trace-a3

echo
echo "=== 步骤 6：观察闩锁（期望**恰好一条** [trace-body] ERROR；不做自动判定）==="
run bash -c 'docker logs llm-a3-gateway 2>&1 | grep "\[trace-body\] ERROR" || echo "(还没有闩锁日志)"'
run bash -c 'docker logs llm-a3-gateway 2>&1 | grep -c "\[trace-body\] ERROR" || true'
run curl -s -o /dev/null -w 'health=%{http_code}\n' "$BASE_URL/health"
run curl -s -o /dev/null -w 'ready=%{http_code}\n' "$BASE_URL/ready"
hist_show 填盘后 2000 /tmp/a3-lat-during.txt
echo "（延迟前后对比由人看两个直方图；本脚本不做自动判定 —— 见交付报告。）"

echo
echo "=== 步骤 7：观察降级（body_missing 计数 vs 总数）==="
run psql "$A3_DATABASE_URL" -c 'SELECT count(*) AS missing_rows FROM call_traces WHERE body_missing IS TRUE AND body_ref_part IS NULL;'
run psql "$A3_DATABASE_URL" -c 'SELECT count(*) AS total_rows FROM call_traces;'

echo
echo "=== 步骤 8：观察 spool 保留（闩锁期间的未封块必须还在盘上）==="
run bash -c 'ls -la /srv/trace-a3/trace-archive-v4/spool/dt=*/inst=blue/ | head' || true

echo
echo "=== 步骤 9：释放空间并观察恢复（删掉 fill.bin，等 1–2 个 tick）==="
run rm -f "$FILL"
run df -h /srv/trace-a3
run sleep 25
run bash -c 'ls -la /srv/trace-a3/trace-archive-v4/spool/dt=*/inst=blue/ | tail' || true
run bash -c 'docker logs llm-a3-gateway 2>&1 | grep "正文落盘已恢复" || echo "!!! 没看到「已恢复」日志"'

echo
echo "=== 步骤 10：A7 进程级（两个 --once 撞锁 + 边写边封）==="
run bash -c 'set +e
  /usr/local/bin/trace-sealer --root /srv/trace-a3/trace-archive-v4 --min-age 3h --once > /tmp/a3-sealer-1.log 2>&1 &
  p1=$!
  /usr/local/bin/trace-sealer --root /srv/trace-a3/trace-archive-v4 --min-age 3h --once > /tmp/a3-sealer-2.log 2>&1
  rc2=$?
  wait $p1; rc1=$?
  echo "第一个 --once 退出码=$rc1；第二个 --once 退出码=$rc2（期望 1）"
  echo "--- 第二个 --once 的 stderr ---"
  cat /tmp/a3-sealer-2.log
  grep -q "已有另一个 sealer 持有锁" /tmp/a3-sealer-2.log && echo "OK: 撞锁消息在场" || { echo "!!! 第二个 --once 没有报撞锁"; exit 1; }'
echo "（「边写边封：所有 ref 都有落点」：持续 traffic 的同时只留一个 sealer 在跑，"
echo "  然后按步骤 8 的文件系统断言复核 spool/产物；读侧就绪时改用点读断言。）"

echo
echo "=== 步骤 11：清理由 trap cleanup EXIT 保证（停容器 → scratch-cleanup --confirm）==="
echo "本行之后脚本自然结束，EXIT trap 会执行清理并复核「挂载点不再是挂载点、镜像已删」。"
