#!/usr/bin/env node

/**
 * xf-check-phase — 校验当前 phase 是否为目标命令的合法入口
 *
 * 设计意图：xflow 的 phase 是单调向前推进的（clarify → design → detail →
 * task → implement → archive）。每条 /xf/* 命令都有"允许的入口 phase 集合"，
 * 跑错了 phase 就该被立即挡下，避免如下事故：
 *
 *   - implement 完成但未归档时，agent 又跑 /xf/clarify 试图开新变更
 *     → 应先 /xf/archive 归档，否则两批不连贯的工作会被塞进同一个 changelog
 *   - design 还没审批，agent 越级跑 /xf/detail
 *     → xf-gate.mjs 已挡住"门禁审批"那条路，但 phase 入口也该单独挡
 *   - 已经 task 了，agent 又退回 /xf/design 改架构
 *     → phase 不应回退；改架构请走"开新变更 + design"
 *
 * 这是事故防御的"流程硬层"：xf-check-invariants.mjs 防的是结构异常（多 yaml /
 * 孤儿目录），xf-gate.mjs 防的是审批门禁（前一阶段是否 approve），本脚本防的
 * 是"同一变更内 phase 跳跃"和"在不该开新变更时开新变更"。
 *
 * Usage:
 *   node .xflow/scripts/xf-check-phase.mjs --command <name>          # 文本报告
 *   node .xflow/scripts/xf-check-phase.mjs --command <name> --json   # JSON 报告
 *
 * Exit codes:
 *   0 = 合法（当前 phase 在目标命令的允许入口集合内）
 *   2 = 违规（与一般错误的 1 区分，方便 shell / CI 判断）
 *   1 = 脚本本身错误（参数错 / IO 失败 / 命令名不识别）
 *
 * 与 xf-current-change.mjs 的关系：本脚本通过 import 复用其 getCurrentChange()，
 * 不重复解析 change.yaml。phase=null 表示无活跃变更。
 */

import { fileURLToPath } from "url";
import {
  output,
  parseArgs,
  printUsageAndExit,
  handleHelpFlag,
  assertAllowedArgs,
} from "./lib.mjs";
import { getCurrentChange } from "./xf-current-change.mjs";

// 特殊值：当前没有 change.yaml，相当于 phase 为"空"
const PHASE_NULL = "null";

/**
 * 各命令的允许入口 phase 集合（事实来源）。
 *
 * 设计原则：
 * - "开新工作"型命令（brainstorm / clarify / split）允许 PHASE_NULL（无活跃
 *   变更，即开新变更）和早期 phase（重入同一变更继续澄清）
 * - phase 推进型命令（design / detail / task / implement / archive）只允许
 *   "上一阶段已完成"和"自己阶段重入"
 * - analyze 是只读审计，不推进 phase，但只在 task 之后阶段有意义
 *
 * 与 .claude/commands/xf/*.md 的"前置条件"段落保持一一对应。改动这里时务必
 * 同步更新对应命令的前置条件文字描述。
 */
export const ALLOWED_PHASES = {
  brainstorm: [PHASE_NULL, "brainstorm", "clarify"],
  clarify: [PHASE_NULL, "brainstorm", "clarify"],
  split: [PHASE_NULL, "brainstorm", "clarify"],
  design: ["brainstorm", "clarify", "design"],
  detail: ["design", "detail"],
  task: ["detail", "task"],
  implement: ["task", "implement"],
  archive: ["implement", "archive"],
  analyze: ["task", "implement", "archive"],
};

const VALID_COMMANDS = Object.keys(ALLOWED_PHASES);
const ALLOWED_ARGS = ["help", "command", "json"];

const USAGE = {
  usage: "node .xflow/scripts/xf-check-phase.mjs --command <name> [--json]",
  description:
    "校验当前活跃变更的 phase 是否为目标命令的合法入口。\n" +
    "phase 推进单调（clarify → ... → archive），跑错命令会被立即挡下。",
  required: {
    "--command <name>": "目标命令名（不含 /xf: 前缀）",
  },
  optional: {
    "--json": "以 JSON 格式输出（默认输出文本报告）",
    "--help, -h": "打印本帮助并退出",
  },
  values: {
    "--command": VALID_COMMANDS.join(" | "),
  },
  examples: [
    "node .xflow/scripts/xf-check-phase.mjs --command clarify",
    "node .xflow/scripts/xf-check-phase.mjs --command design --json",
  ],
  notes: [
    "退出码：0=合法，2=违规，1=脚本错误（与一般错误区分以便 shell / CI 判断）",
    "本脚本只读，不修改任何文件",
    "无活跃变更时 phase=null，对 brainstorm/clarify/split 合法（即开新变更）",
    "也可作为函数库 import：assertCommandPhase(commandName) / renderPhaseViolation(violation)",
  ],
};

/**
 * 校验 commandName 是否能在当前 phase 下执行。
 *
 * 返回 { ok: true } 或 { ok: false, violation: { ... } }，不抛异常。
 */
export async function assertCommandPhase(commandName) {
  const allowed = ALLOWED_PHASES[commandName];
  if (!allowed) {
    return {
      ok: false,
      violation: {
        kind: "unknown-command",
        command: commandName,
        message: `未识别的命令名 "${commandName}"。合法命令：${VALID_COMMANDS.join(", ")}`,
      },
    };
  }

  const change = await getCurrentChange();
  const currentPhase = change.active ? change.phase || PHASE_NULL : PHASE_NULL;

  if (allowed.includes(currentPhase)) {
    return {
      ok: true,
      command: commandName,
      current_phase: currentPhase,
      change_id: change.id,
      allowed_phases: allowed,
    };
  }

  return {
    ok: false,
    violation: {
      kind: "phase-mismatch",
      command: commandName,
      current_phase: currentPhase,
      allowed_phases: allowed,
      change: {
        active: change.active,
        id: change.id,
        name: change.name,
        phase: change.phase,
        affected_modules: change.affected_modules,
      },
    },
  };
}

/**
 * 渲染 violation 为多行人类可读字符串。重点是给 agent 一份可执行的修复路径。
 */
export function renderPhaseViolation(violation) {
  if (violation.kind === "unknown-command") {
    return `❌ ${violation.message}`;
  }

  const { command, current_phase, allowed_phases, change } = violation;
  const lines = [];
  lines.push(`❌ phase 入口不合法：/xf/${command} 不能在 phase=${current_phase} 时执行。`);
  lines.push("");
  lines.push("当前状态：");
  if (change.active) {
    lines.push(`  - 活跃变更：${change.id}${change.name ? `（${change.name}）` : ""}`);
    lines.push(`  - 当前 phase：${change.phase}`);
    if (change.affected_modules && change.affected_modules.length > 0) {
      const moduleNames = change.affected_modules.map((m) => m.name).join(", ");
      lines.push(`  - 受影响模块：${moduleNames}`);
    }
  } else {
    lines.push("  - 无活跃变更（xspec/changes/change.yaml 不存在）");
  }
  lines.push("");
  const allowedDisplay = allowed_phases
    .map((p) => (p === PHASE_NULL ? "null（无活跃变更）" : p))
    .join(" / ");
  lines.push(`合法入口 phase：${allowedDisplay}`);
  lines.push("");
  lines.push("建议处理：");
  for (const ln of suggestFix(command, current_phase, change)) {
    lines.push(`  - ${ln}`);
  }
  lines.push("");
  lines.push("如需协助，把上面这份报告贴给 AI 助手讨论修复路径。");
  return lines.join("\n");
}

/**
 * 给出针对性的修复建议。这里塞了对各种典型场景的小专家系统：根据 command +
 * current_phase 的组合给具体下一步，而不是泛泛的"看文档"。
 */
function suggestFix(command, currentPhase, change) {
  const suggestions = [];

  // 场景 1：implement 阶段（已实现完）想开新工作 → 必须先 archive
  if (
    ["brainstorm", "clarify", "split"].includes(command) &&
    ["implement", "archive"].includes(currentPhase)
  ) {
    suggestions.push(
      "新需求场景：先执行 `/xf/archive` 归档当前变更，再开新变更",
    );
    suggestions.push(
      "严格反对：把已 implement 的变更回退到 clarify、或把新模块"
        + "\"并入\"当前 affected_modules——会让 changelog 包含两批不连贯的工作",
    );
    return suggestions;
  }

  // 场景 2：design / detail / task 阶段想再开 clarify
  if (
    ["brainstorm", "clarify", "split"].includes(command) &&
    ["design", "detail", "task"].includes(currentPhase)
  ) {
    suggestions.push(
      `当前变更已进入 ${currentPhase} 阶段，phase 单调推进不允许回退到 clarify`,
    );
    suggestions.push(
      `如果只是想补几个 clarify 问题：直接在当前 ${currentPhase} 阶段的对话里讨论，必要时人工调整 spec.md / spec.delta.md`,
    );
    suggestions.push(
      `如果是新需求：先执行对应的推进命令完成当前变更（接下来应跑 \`/xf/${currentPhase}\`），归档后再开新变更`,
    );
    return suggestions;
  }

  // 场景 3：phase 推进命令越级（如 phase=clarify 时跑 /xf/detail）
  if (
    ["design", "detail", "task", "implement", "archive"].includes(command)
  ) {
    const phaseOrder = ["clarify", "design", "detail", "task", "implement", "archive"];
    const currentIdx = phaseOrder.indexOf(currentPhase);
    const targetIdx = phaseOrder.indexOf(command);
    if (currentIdx >= 0 && targetIdx >= 0 && targetIdx > currentIdx + 1) {
      const nextPhase = phaseOrder[currentIdx + 1];
      suggestions.push(
        `phase 不能越级：当前 ${currentPhase}，目标 ${command}，中间漏了 ${nextPhase}`,
      );
      suggestions.push(`下一步应执行 \`/xf/${nextPhase}\` 推进到下一阶段`);
      return suggestions;
    }
    if (currentIdx >= 0 && targetIdx >= 0 && targetIdx < currentIdx) {
      suggestions.push(
        `phase 不能回退：当前 ${currentPhase}，目标 ${command} 是更早的阶段`,
      );
      suggestions.push(
        `如果当前变更需要修正：直接在 ${currentPhase} 阶段的对话里调整对应制品；不要回退 phase`,
      );
      suggestions.push(
        `如果是新需求：先归档当前变更，再开新变更`,
      );
      return suggestions;
    }
  }

  // 场景 4：phase=null（无活跃变更）跑了推进命令
  if (currentPhase === PHASE_NULL && command !== "brainstorm" && command !== "clarify" && command !== "split") {
    suggestions.push(`当前没有活跃变更，请先执行 \`/xf/clarify\`（或 \`/xf/brainstorm\`）开新变更`);
    return suggestions;
  }

  // 兜底
  suggestions.push(
    `当前 phase=${currentPhase} 与命令 /xf/${command} 不兼容。请检查 phase 流转：clarify → design → detail → task → implement → archive`,
  );
  return suggestions;
}

async function main() {
  const args = parseArgs();
  handleHelpFlag(args, USAGE);
  assertAllowedArgs(args, ALLOWED_ARGS, USAGE);

  if (!args.command || args.command === true) {
    printUsageAndExit(USAGE, "缺少 --command 参数（合法值见 values）");
  }

  const result = await assertCommandPhase(args.command);

  if (args.json) {
    output(result);
  } else if (result.ok) {
    console.log(
      `✓ phase=${result.current_phase} 是 /xf/${result.command} 命令的合法入口`,
    );
  } else if (result.violation.kind === "unknown-command") {
    console.error(`[xflow] Error: ${result.violation.message}`);
    process.exit(1);
  } else {
    console.log(renderPhaseViolation(result.violation));
  }

  if (result.ok) process.exit(0);
  if (result.violation.kind === "unknown-command") process.exit(1);
  process.exit(2);
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1];
if (isMain) {
  main().catch((err) => {
    console.error(`[xflow] Error: ${err.message}`);
    process.exit(1);
  });
}
