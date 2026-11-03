#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# P0 实测：调用链路正文归档 —— 压缩率 / 容量 / 点读时延
#
# 目的（回答三个决定性问题）：
#   ① 179GB TOAST 是否已被 pglz 压缩过？→ 决定归档"真实收益"的量级
#   ② 生产真实语料上各 codec 的压缩率与 bytes/行？→ 决定 /datalog 容量与 codec 选型
#   ③ 点读（解压 + 扫描命中）时延与峰值内存？→ 决定窗口大小上限与点读并发
#
# 安全性：**全程只读**（SELECT + COPY TO），唯一写入是 $WORKDIR。
#        按归档时区取"一天"，不会 detoast 全表；建议低峰执行。
#
# 用法：
#   DSN='postgres://...' ./diagnose_trace_archive.sh 2026-09-10
#   DSN='postgres://...' WORKDIR=/datalog/p0-bench ./diagnose_trace_archive.sh 2026-09-10 Asia/Shanghai
#   DSN='postgres://...' ./diagnose_trace_archive.sh --dry-run 2026-09-10
#
# 参考：docs/trace-archive-final-design.md §1/§12；docs/trace-archive-compression-design.md §4.4/§6.3
#
# 注意（踩过的三个坑，勿回退）：
#   1) psql 的 :var 插值只在 stdin/文件输入生效，`psql -c` 不插值 → 本脚本改为 shell 拼字面量
#   2) `\copy` 的 SQL 文本同样不做 :var 插值 → 同上
#   3) $VAR 紧跟全角字符会被 bash 吞进变量名 → 一律写 ${VAR}
# ═══════════════════════════════════════════════════════════════════════════════
set -euo pipefail

DRY_RUN=0; ARGS=()
for a in "$@"; do
  case "$a" in
    --dry-run) DRY_RUN=1 ;;
    *) ARGS+=("$a") ;;
  esac
done
DAY="${ARGS[0]:-$(date -v-2d +%F 2>/dev/null || date -d '2 days ago' +%F)}"
TZ_NAME="${ARGS[1]:-Asia/Shanghai}"
DSN="${DSN:-${DATABASE_URL:-}}"
WORKDIR="${WORKDIR:-/datalog/p0-bench}"

say()  { printf '\n=== %s\n' "$*"; }
note() { printf '    %s\n' "$*"; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
hr()   { printf '%s\n' "───────────────────────────────────────────────────────────────────────────"; }

# ── 算术（不依赖 bc；均带格式化）──
div() { awk -v a="$1" -v b="$2" -v f="${3:-%.2f}" 'BEGIN{printf f,(b==0?0:a/b)}'; }
mul() { awk -v a="$1" -v b="$2" -v f="${3:-%.2f}" 'BEGIN{printf f,a*b}'; }
gib() { awk -v a="$1" -v f="${2:-%.2f}" 'BEGIN{printf f,a/1073741824}'; }
mib() { awk -v a="$1" -v f="${2:-%.1f}" 'BEGIN{printf f,a/1048576}'; }
gt()  { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a>b)}'; }

# ── 入参校验（防 SQL 注入 + 早失败）──
[[ "$DAY"     =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "day 必须是 YYYY-MM-DD，收到：$DAY"
[[ "$TZ_NAME" =~ ^[A-Za-z0-9_/+-]+$ ]]           || die "tz 含非法字符：$TZ_NAME"

# 关键：天数边界必须按【归档时区】解释（设计 §0 决策 11）。用 shell 拼字面量，避开 psql 插值差异。
RANGE="ts >= ('${DAY}'::timestamp AT TIME ZONE '${TZ_NAME}') AND ts < (('${DAY}'::timestamp + interval '1 day') AT TIME ZONE '${TZ_NAME}')"
PROJ="json_build_object('request_id',request_id,'ts',ts,'request_headers',request_headers,'request_body',request_body,'response_body',response_body,'error_code',coalesce(error_code,''),'error_message',coalesce(error_message,''))::text"

# 抽样：一天原文可达数 GB，业务时段不宜全量导出/扫描。
#   SAMPLE_FRAC=0.03  —— 全日随机抽样（推荐，无时段偏置）
#   SAMPLE_LIMIT=3000 —— 按 ts 取当天前 N 行（会偏向凌晨批处理时段）
SAMPLE_LIMIT="${SAMPLE_LIMIT:-}"
SAMPLE_FRAC="${SAMPLE_FRAC:-}"
if [ -n "$SAMPLE_FRAC" ]; then
  [[ "$SAMPLE_FRAC" =~ ^0\.[0-9]+$|^1\.0+$ ]] || die "SAMPLE_FRAC 必须是 (0,1] 的小数，收到：$SAMPLE_FRAC"
  SRC="(SELECT * FROM call_traces WHERE ${RANGE} AND random() < ${SAMPLE_FRAC}) t"
  SRCTAG="全日随机抽样 ${SAMPLE_FRAC}"
elif [ -n "$SAMPLE_LIMIT" ]; then
  [[ "$SAMPLE_LIMIT" =~ ^[0-9]+$ ]] || die "SAMPLE_LIMIT 必须是整数，收到：$SAMPLE_LIMIT"
  SRC="(SELECT * FROM call_traces WHERE ${RANGE} ORDER BY ts LIMIT ${SAMPLE_LIMIT}) t"
  SRCTAG="抽样前 ${SAMPLE_LIMIT} 行（偏凌晨）"
else
  SRC="(SELECT * FROM call_traces WHERE ${RANGE}) t"
  SRCTAG="整天"
fi

command -v psql >/dev/null || die "psql 不存在"
[ -n "$DSN" ] || die "需要 DSN：DSN='postgres://...' 或 DATABASE_URL='...'"
psql() { command psql -X -q -v ON_ERROR_STOP=1 -d "$DSN" "$@"; }
q()    { command psql -X -tAq -v ON_ERROR_STOP=1 -d "$DSN" -c "$1"; }

hr
cat <<BANNER
 P0 归档实测   day=${DAY}   tz=${TZ_NAME}
 workdir=${WORKDIR}
 范围=${SRCTAG}
 模式=$( [ "$DRY_RUN" = 1 ] && echo 'DRY-RUN（只打印 SQL）' || echo '实际执行（只读 + 写 ${WORKDIR}）' )
BANNER
hr

if [ "$DRY_RUN" = 1 ]; then
  cat <<DRY
--- ① 表 / TOAST 体积与总行数 ---
SELECT pg_size_pretty(pg_total_relation_size('call_traces')) AS total,
       pg_size_pretty(pg_relation_size('call_traces'))       AS main_heap,
       pg_size_pretty(pg_relation_size(t.oid))               AS toast
FROM pg_class c JOIN pg_class t ON t.oid = c.reltoastrelid WHERE c.relname = 'call_traces';
SELECT count(*) FROM call_traces;
SHOW default_toast_compression;

--- ② 当天（按归档时区 ${TZ_NAME}）：行数 + 未压缩原文量 ---
SELECT count(*), pg_size_pretty(sum(octet_length(coalesce(request_body::text,''))+
                                    octet_length(coalesce(response_body::text,'')))) AS raw_text_bytes
FROM $SRC;

--- ③ 导出归档 jsonl（与 archive_service.go 的 archiveTraceLine 对齐）---
\\copy (SELECT $PROJ FROM $SRC ORDER BY request_id) TO '${WORKDIR}/day.jsonl'
DRY
  exit 0
fi

mkdir -p "$WORKDIR" || die "无法创建 ${WORKDIR}"
say "磁盘余量（确认足以放下 1 天的 jsonl 与中间产物）"
df -h "$WORKDIR" | sed 's/^/    /'

# ─────────────────────── ① 体积基线 ───────────────────────
say "① 表 / TOAST 体积"
psql <<SQL
SELECT pg_size_pretty(pg_total_relation_size('call_traces')) AS total,
       pg_size_pretty(pg_relation_size('call_traces'))       AS main_heap,
       pg_size_pretty(pg_relation_size(t.oid))               AS toast_size
FROM pg_class c JOIN pg_class t ON t.oid = c.reltoastrelid WHERE c.relname = 'call_traces';
SHOW default_toast_compression;
SQL

TOAST_BYTES=$(q "SELECT coalesce(pg_relation_size(t.oid),0) FROM pg_class c JOIN pg_class t ON t.oid=c.reltoastrelid WHERE c.relname='call_traces'")
TOTAL_ROWS=$(q "SELECT count(*) FROM call_traces")
note "TOAST=$(gib "${TOAST_BYTES:-0}" "%.2f") GiB (${TOAST_BYTES} 字节) / 总行数=${TOTAL_ROWS}"

# ─────────────────────── ② 当天原文量 ───────────────────────
say "② 当天 ${DAY}（归档时区 ${TZ_NAME}）原文量"
note "注意：pg_column_size 对 TOAST 值只回指针，故以 octet_length(::text) 计未压缩原文"
psql <<SQL
SELECT count(*) AS day_rows,
       pg_size_pretty(sum(octet_length(coalesce(request_body::text,''))+
                          octet_length(coalesce(response_body::text,'')))) AS raw_text_bytes
FROM $SRC;
SQL

DAY_STAT=$(q "SELECT count(*)||' '||coalesce(sum(octet_length(coalesce(request_body::text,''))+octet_length(coalesce(response_body::text,''))),0) FROM $SRC")
DAY_ROWS=$(echo "$DAY_STAT" | awk '{print $1}')
DAY_RAW=$(echo "$DAY_STAT"  | awk '{print $2}')
note "当天行数=${DAY_ROWS} 原文=$(gib "${DAY_RAW:-0}" "%.3f") GiB (${DAY_RAW} 字节)"

# 整天真实行数（走 idx_call_traces_ts，很廉价）：用于把点读时延外推到全天规模
DAY_FULL_ROWS=$(q "SELECT count(*) FROM call_traces WHERE ${RANGE}")
[ -n "$DAY_FULL_ROWS" ] || DAY_FULL_ROWS=0
[ "$DAY_FULL_ROWS" -gt 0 ] && note "当天真实行数（整天）=${DAY_FULL_ROWS}"

# ─────────────────────── ① 结论 ───────────────────────
say "① 结论：TOAST 是否已被 pglz 压缩（决定真实收益量级）"
if [ "${DAY_ROWS:-0}" -gt 0 ] && [ "${TOTAL_ROWS:-0}" -gt 0 ] && [ "${TOAST_BYTES:-0}" -gt 0 ]; then
  RAW_PER_ROW=$(div "$DAY_RAW" "$DAY_ROWS" "%.1f")
  STORED_PER_ROW=$(div "$TOAST_BYTES" "$TOTAL_ROWS" "%.1f")
  FACTOR=$(div "$RAW_PER_ROW" "$STORED_PER_ROW" "%.2f")
  RAW_TOTAL=$(mul "$RAW_PER_ROW" "$TOTAL_ROWS" "%.0f")
  note "未压缩原文/行 ≈ ${RAW_PER_ROW} B（按当天样本外推）"
  note "TOAST 已存/行   ≈ ${STORED_PER_ROW} B"
  note "推断 pglz 压缩系数 ≈ ${FACTOR}x"
  note "全表未压缩原文估算 ≈ $(gib "$RAW_TOTAL" "%.2f") GiB"
  if gt "$FACTOR" 1.5; then
    note ">>> TOAST 确已压缩：179GB 不是未压缩基线，§12 的『179GB→20~30GB』口径偏乐观"
  else
    note ">>> TOAST 基本未压缩（原文与存量同量级）"
  fi
else
  note "当天无数据或 TOAST 为 0，跳过（先确认 day/tz 是否正确）"
fi

# ─────────────────────── ③ 导出 ───────────────────────
RAW="$WORKDIR/day.jsonl"
say "③ 导出归档 jsonl → ${RAW}"
psql <<SQL
\\copy (SELECT $PROJ FROM $SRC ORDER BY request_id) TO '$RAW'
SQL
RAW_BYTES=$(wc -c < "$RAW" | tr -d ' ')
[ "$RAW_BYTES" -gt 0 ] || die "导出为空（0 字节）——先确认 day=${DAY} 与 tz=${TZ_NAME} 是否正确"
note "原始 jsonl = ${RAW_BYTES} 字节 ($(mib "$RAW_BYTES") MiB)"

# 峰值内存：优先 GNU time -v（Linux/prod），退回 -l（macOS）
TIMEV=""
if /usr/bin/time -v true >/dev/null 2>&1; then TIMEV="v"
elif /usr/bin/time -l true >/dev/null 2>&1; then TIMEV="l"; fi
peak() { # 输出 "<秒> <KB>"。用 -o 落盘隔离 time 的输出，避免命令自身 stderr 混入。
  local tf; tf=$(mktemp)
  local o_s="" o_m=""
  if   [ "$TIMEV" = "v" ]; then
    /usr/bin/time -o "$tf" -f '%e %M' "$@" >/dev/null 2>&1 || true
    o_s=$(awk 'NF>=2{print $1}' "$tf" | tail -1)
    o_m=$(awk 'NF>=2{print $2}' "$tf" | tail -1)
  elif [ "$TIMEV" = "l" ]; then
    /usr/bin/time -o "$tf" -l "$@" >/dev/null 2>&1 || true
    o_s=$(awk '$1 ~ /^[0-9.]+$/ && $2 == "real"{print $1}' "$tf")
    o_m=$(awk '/maximum resident/{print int($1/1024)}' "$tf")
  else
    # 宿主无 /usr/bin/time（本生产机就是如此）：仍给耗时，内存记 "-"
    local t0 t1
    t0=$(now); "$@" >/dev/null 2>&1 || true; t1=$(now)
    o_s=$(awk -v a="$t0" -v b="$t1" 'BEGIN{d=b-a; printf "%.2f",(d<0?0:d)}')
    o_m="-"
  fi
  rm -f "$tf"
  echo "${o_s:-?} ${o_m:-?}"
}
# 把 peak() 的 KB 字段渲染成 MB；非数字一律显示 "-"
mbcol() { echo "$1" | awk '{print ($2 ~ /^[0-9]+$/ ? sprintf("%.0f", $2/1024) : "-")}'; }
# 高精度当前时间：优先 bash5 的 EPOCHREALTIME，退回 GNU date
now() { if [ -n "${EPOCHREALTIME:-}" ]; then echo "$EPOCHREALTIME"; else date +%s.%N 2>/dev/null || date +%s; fi; }

# ─────────────────────── ④ codec 扫描 ───────────────────────
say "④ codec / 级别 / 窗口 扫描"
printf '    %-26s %14s %9s %8s %10s\n' "算法" "压缩后(B)" "压缩率" "耗时s" "峰值MB"
printf '    %-26s %14s %9s %8s %10s\n' "────" "────────" "─────" "────" "─────"
OUT="$WORKDIR/out.bin"
run() { local name="$1"; shift
  local t; t=$(peak "$@" 2>/dev/null || true)
  local sz=0
  [ -f "$OUT" ] && sz=$(stat -c%s "$OUT" 2>/dev/null || stat -f%z "$OUT" 2>/dev/null || echo 0)
  local r="n/a"; [ "$sz" -gt 0 ] && r=$(div "$RAW_BYTES" "$sz" "%.2f")
  printf '    %-26s %14s %9s %8s %10s\n' "$name" "$sz" "${r}x" "$(echo "$t"|awk '{print $1}')" "$(mbcol "$t")"
}
try() { command -v "$1" >/dev/null 2>&1; }
if try gzip; then
  run "gzip -6（现设计）"  sh -c "gzip -6 -c '$RAW' > '$OUT'"
  run "gzip -9"            sh -c "gzip -9 -c '$RAW' > '$OUT'"
fi
BEST=0
if try zstd; then
  run "zstd -3"            sh -c "zstd -3 -q -f -o '$OUT' '$RAW'"
  run "zstd -19"           sh -c "zstd -19 -q -f -o '$OUT' '$RAW'"
  run "zstd -22 --ultra"   sh -c "zstd -22 --ultra -q -f -o '$OUT' '$RAW'"
  run "zstd -19 --long=25" sh -c "zstd -19 --long=25 -q -f -o '$OUT' '$RAW'"
  run "zstd -19 --long=27" sh -c "zstd -19 --long=27 -q -f -o '$OUT' '$RAW'"
  run "zstd -22 --long=27" sh -c "zstd -22 --long=27 -q -f -o '$OUT' '$RAW'"
  BEST=$(stat -c%s "$OUT" 2>/dev/null || stat -f%z "$OUT" 2>/dev/null || echo 0)
else
  note "zstd 不存在，无法给出最优档"
fi
if try xz;     then run "xz -6"        sh -c "xz -6 -c '$RAW' > '$OUT'"; fi
if try brotli; then run "brotli -q 11" sh -c "brotli -q 11 -c '$RAW' > '$OUT'"; fi
if try lz4;    then run "lz4 -9"       sh -c "lz4 -9 -q -f '$RAW' '$OUT'"; fi

# ─────────────────────── ⑤ 冷点读时延 ───────────────────────
say "⑤ 冷点读时延（解压 + 扫描命中，模拟 usage_handler.Trace 两段式）"
if try zstd; then
  zstd -19 --long=27 -q -f -o "$WORKDIR/day.zst" "$RAW"
  # 点读成本与 rid 在文件中的位置成正比（两段式需从头解压扫描），
  # 所以必须测【首/中/尾】三点位——只测开头会严重低估最坏情况。
  # 注意：勿用 `... | head -5`，set -o pipefail 下 head 提前退出会让 sed 吃 SIGPIPE(141) 并中止脚本。
  NLINE=$(wc -l < "$RAW" | tr -d ' ')
  LAST_T=""
  for spec in "首:1" "中:$((NLINE / 2))" "尾:${NLINE}"; do
    tag=${spec%%:*}; pos=${spec#*:}
    rid=$(sed -n "${pos}p" "$RAW" | awk -F'"' '{print $4}')
    [ -n "$rid" ] || { note "位置 ${pos} 未取到 request_id，跳过"; continue; }
    t=$(peak sh -c "zstd -dc --long=27 '$WORKDIR/day.zst' | grep -m1 -F '$rid' >/dev/null")
    printf '    [%s] rid=%-34s 耗时=%ss 峰值MB=%s\n' "$tag" "$rid" "$(echo "$t"|awk '{print $1}')" "$(mbcol "$t")"
    # 记录"尾"位耗时（最坏情况），供下面按整天规模外推
    case "$(echo "$t" | awk '{print $1}')" in ''|'?') ;; *) [ "$tag" = "尾" ] && LAST_T=$(echo "$t" | awk '{print $1}') ;; esac
  done
  # 抽样文件只有整天的一小部分 → 点读最坏情况会被低估，按行数比例外推
  if [ "${DAY_FULL_ROWS:-0}" -gt 0 ] && [ "${DAY_ROWS:-0}" -gt 0 ] && [ "$DAY_FULL_ROWS" -gt "$DAY_ROWS" ]; then
    ratio=$(div "$DAY_FULL_ROWS" "$DAY_ROWS" "%.1f")
    note "本次为抽样（${DAY_ROWS}/${DAY_FULL_ROWS} 行，×${ratio}）→ 全天归档文件更大，点读更慢"
    [ -n "$LAST_T" ] && note "按比例外推：整天最坏点读 ≈ $(mul "$LAST_T" "$ratio" "%.1f")s（全文件解压扫描，未分块）"
    note ">>> 若该值过大，需在实现中改「分块压缩 + 每日 rid 索引」，使点读只解压 1 个分块"
  fi
else
  note "zstd 不存在，跳过"
fi

# ─────────────────────── 汇总外推 ───────────────────────
say "汇总：用实测值替换 §12 的口径"
if [ "${BEST:-0}" -gt 0 ] && [ "${DAY_ROWS:-0}" -gt 0 ]; then
  BPR=$(div "$BEST" "$DAY_ROWS" "%.2f")
  STOCK=$(mul "$BPR" "$TOTAL_ROWS" "%.0f")
  note "最优档压缩后 bytes/行 = ${BPR}"
  note "  存量外推(${TOTAL_ROWS} 行) ≈ $(gib "$STOCK" "%.2f") GiB"
  note "  当天压缩包 × 30 ≈ $(gib "$(mul "$BEST" 30 "%.0f")" "%.2f") GiB/月"
  note "  对比现值 TOAST = $(gib "$TOAST_BYTES" "%.2f") GiB"
  note "  → 节省倍数 ≈ $(div "$TOAST_BYTES" "$STOCK" "%.2f")x"
else
  note "数据不足，无法外推"
fi

say "完成。WORKDIR 内容"
ls -lh "$WORKDIR" | sed 's/^/    /'
cat <<'NEXT'

把 ①(含 pglz 系数) ④ ⑤ 与本汇总贴回即可定：
  · /datalog 容量（替换 §12 的 20~30GB）
  · codec 与窗口大小（受容器 2g 内存约束）
  · 点读时延是否仍满足"延迟可接受"
NEXT
