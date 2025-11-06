#!/usr/bin/env node

/**
 * xf-gate.mjs — 阶段门禁检查
 *
 * 输出 JSON:
 *   { "pass": true }
 *   { "pass": false, "reason": "clarify 阶段尚未审批" }
 *
 * 退出码:
 *   0 = 通过
 *   1 = 未通过 / 参数错误
 */

import { existsSync, readFileSync } from "fs";
import { join, dirname, resolve } from "path";
import { fileURLToPath } from "url";
import {
  parseArgs,
  printUsageAndExit,
  handleHelpFlag,
  assertAllowedArgs,
} from "./lib.mjs";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(SCRIPT_DIR, "..", "..");
const GATES_DIR = join(PROJECT_ROOT, "xspec", "changes", ".gates");

const GATES = {
  design: { requires: ["clarify"] },
  task: { requires: ["detail"] },
};

const ALLOWED_ARGS = ["help", "to"];

const USAGE = {
  usage: "node .xflow/scripts/xf-gate.mjs --to <target-stage>",
  description:
    "检查目标阶段的门禁是否已通过（即前置阶段是否已通过 xflow approve）。\n" +
    "未配置门禁的阶段直接返回 pass=true。",
  required: {
    "--to <stage>": "目标阶段（即接下来要进入的阶段）",
  },
  optional: {
    "--help, -h": "打印本帮助并退出",
  },
  values: {
    "--to": Object.keys(GATES).join(" | ") + "（其余阶段无门禁，等价于 pass=true）",
  },
  examples: [
    "node .xflow/scripts/xf-gate.mjs --to design   # 进入 design 前检查 clarify 是否审批",
    "node .xflow/scripts/xf-gate.mjs --to task     # 进入 task 前检查 detail 是否审批",
  ],
  notes: [
    "门禁记录由 `xflow approve` 写入 xspec/changes/.gates/<stage>.json",
    "禁止 AI 手工创建 / 修改 / 读取 .gates/ 内的文件",
    "退出码 0=通过，1=未通过或参数错误（参数错误时同时打印用法）",
  ],
};

function checkGate(targetStage) {
  const gateDef = GATES[targetStage];
  if (!gateDef) {
    return { pass: true };
  }

  for (const requiredStage of gateDef.requires) {
    const gateFile = join(GATES_DIR, `${requiredStage}.json`);

    if (!existsSync(gateFile)) {
      return {
        pass: false,
        reason: `${requiredStage} 阶段尚未审批。请先审查产出并执行 xflow approve`,
      };
    }

    try {
      const record = JSON.parse(readFileSync(gateFile, "utf-8"));

      if (record.stage !== requiredStage) {
        return {
          pass: false,
          reason: `门禁文件 stage 不匹配：期望 ${requiredStage}，实际 ${record.stage}`,
        };
      }

      if (record.status !== "approved") {
        return {
          pass: false,
          reason: `${requiredStage} 阶段审批状态为 ${record.status}，需要 approved`,
        };
      }
    } catch (err) {
      return {
        pass: false,
        reason: `读取门禁文件失败：${gateFile} — ${err.message}`,
      };
    }
  }

  return { pass: true };
}

const args = parseArgs(process.argv.slice(2));
handleHelpFlag(args, USAGE);
assertAllowedArgs(args, ALLOWED_ARGS, USAGE);

if (!args.to) {
  printUsageAndExit(USAGE, "缺少 --to 参数");
}

const result = checkGate(args.to);
console.log(JSON.stringify(result));
process.exit(result.pass ? 0 : 1);
