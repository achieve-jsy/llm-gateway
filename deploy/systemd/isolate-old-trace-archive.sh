#!/bin/bash
# 隔离旧的事后归档定时器（生产：trace-archive.timer，每天 04:30 调
# /root/daily-trace-archive.sh，脚本内 RETAIN_DAYS=5）。
#
# ⚠️ 本脚本**默认 dry-run**；真正动手必须 `--confirm`。
# 回滚：bash restore-old-trace-archive.sh --confirm
set -euo pipefail

OLD_TIMER=trace-archive.timer
OLD_SCRIPT=/root/daily-trace-archive.sh
NEW_ROOT=/datalog/trace-archive-v4
CONFIRM=0
[ "${1:-}" = "--confirm" ] && CONFIRM=1

echo "== 1) 现状留证（抄进上线记录）=="
systemctl is-enabled "$OLD_TIMER" || true
systemctl is-active  "$OLD_TIMER" || true
systemctl list-timers "$OLD_TIMER" --all --no-pager || true
echo "--- $OLD_SCRIPT（人工过目：它的删除谓词到底会删什么）---"
sed -n '1,200p' "$OLD_SCRIPT"

echo "== 2) 删除谓词审查：旧脚本绝不能删到新树 =="
# 只看删除类语句；命中新树或 /datalog 顶层 ⇒ 直接 STOP（这属于「先核实再上线」）。
HITS="$(grep -nE '(^|[^[:alnum:]_])(rm[[:space:]]+-[[:alnum:]]*[rf]|find[[:space:]].*-delete|rsync[[:space:]].*--delete)' "$OLD_SCRIPT" || true)"
echo "${HITS:-（未发现删除类语句）}"
if printf '%s\n' "$HITS" | grep -qE "$NEW_ROOT|/datalog([^/]|$)"; then
  echo "STOP: 旧脚本的删除谓词可能命中 $NEW_ROOT（或 /datalog 顶层）⇒ 禁止上线，先人工核对" >&2
  exit 3
fi

if [ "$CONFIRM" != "1" ]; then
  echo "[dry-run] 将要执行： systemctl disable --now $OLD_TIMER ; systemctl mask $OLD_TIMER"
  echo "[dry-run] 加 --confirm 才会真的动手"
  exit 0
fi

echo "== 3) 停用并 mask（幂等）=="
systemctl disable --now "$OLD_TIMER"
systemctl mask "$OLD_TIMER"
if systemctl is-active --quiet "$OLD_TIMER"; then
  echo "STOP: $OLD_TIMER 仍处于 active" >&2; exit 4
fi
systemctl list-timers --all --no-pager | grep -F "$OLD_TIMER" || echo "OK: 已从 list-timers 消失"
echo "完成。回滚： bash restore-old-trace-archive.sh --confirm"
