#!/bin/bash
# 回滚「隔离旧的归档定时器」：把 isolate-old-trace-archive.sh 做过的事原样撤销
#（unmask → enable --now → 复核 is-enabled / is-active / list-timers 又能看到它）。
#
# 生产：trace-archive.timer，每天 04:30 调 /root/daily-trace-archive.sh
#（脚本内 RETAIN_DAYS=5）。隔离窗口内 /datalog 没有 retention ⇒ 回滚的判据
# 是「旧定时器重新出现在 list-timers 里」，不是「命令没报错」。
#
# ⚠️ 本脚本**默认 dry-run**；真正动手必须 `--confirm`。
# 对应正向脚本：bash isolate-old-trace-archive.sh --confirm
set -euo pipefail

OLD_TIMER=trace-archive.timer
OLD_SCRIPT=/root/daily-trace-archive.sh
NEW_ROOT=/datalog/trace-archive-v4
CONFIRM=0
[ "${1:-}" = "--confirm" ] && CONFIRM=1

echo "== 1) 现状留证（抄进回滚记录）=="
systemctl is-enabled "$OLD_TIMER" || true
systemctl is-active  "$OLD_TIMER" || true
systemctl list-timers "$OLD_TIMER" --all --no-pager || true
echo "--- $OLD_SCRIPT 仍在场？（归档动作本身没有被本脚本改动）---"
[ -f "$OLD_SCRIPT" ] && echo "OK: $OLD_SCRIPT 仍在场" || echo "WARN: $OLD_SCRIPT 不见了"

if [ "$CONFIRM" != "1" ]; then
  echo "[dry-run] 将要执行：解除 mask、然后 enable --now $OLD_TIMER"
  echo "[dry-run] 加 --confirm 才会真的动手"
  exit 0
fi

echo "== 2) 解除 mask 并重新启用（幂等）=="
systemctl unmask "$OLD_TIMER"
systemctl enable --now "$OLD_TIMER"
if ! systemctl is-enabled --quiet "$OLD_TIMER"; then
  echo "STOP: $OLD_TIMER 未能重新 enable（回滚不完整，请人工核对）" >&2; exit 5
fi
if ! systemctl is-active --quiet "$OLD_TIMER"; then
  echo "STOP: $OLD_TIMER 未能重新 active（回滚不完整，请人工核对）" >&2; exit 6
fi
systemctl list-timers --all --no-pager | grep -F "$OLD_TIMER"
echo "完成。重新隔离： bash isolate-old-trace-archive.sh --confirm"
