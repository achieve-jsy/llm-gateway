#!/usr/bin/env node

/**
 * xf-profile.mjs — 读取项目级 project_profile
 *
 * 用途：
 *   所有 xflow 命令与脚本统一通过此脚本获取当前项目的 project_profile，
 *   避免各命令自行解析 constitution.md 导致格式漂移。
 *
 * 用法：
 *   node .xflow/scripts/xf-profile.mjs
 *   node .xflow/scripts/xf-profile.mjs --value-only   # 仅输出 profile 值（便于 shell 消费）
 *
 * 解析规则：
 *   从 xspec/constitution.md 扫描第一行匹配 `project_profile` 的条目：
 *     - `- project_profile: xxx`
 *     - `- **project_profile**: xxx`
 *     - `project_profile: xxx`
 *     - `| project_profile | xxx |` （表格格式）
 *   值支持可选引号；大小写敏感；空白自动 trim。
 *
 * 回退规则：
 *   - constitution.md 不存在 → general
 *   - 字段缺失 → general
 *   - 值为空 → general
 *
 * 已知 profile 白名单（仅用于提示，脚本不强制限制）：
 *   - general
 *   - xuanwu-activity
 *
 * 输出 JSON（默认）：
 *   { "profile": "general", "source": "default" }
 *   { "profile": "xuanwu-activity", "source": "xspec/constitution.md" }
 *
 * 退出码:
 *   0 = 成功返回 profile（即使是 general 兜底）
 *   1 = 解析异常
 */

import { readFile } from "fs/promises";
import { join } from "path";
import { existsSync } from "fs";
import {
  SPEC_ROOT,
  output,
  fatal,
  parseArgs,
  printUsageAndExit,
  handleHelpFlag,
  assertAllowedArgs,
} from "./lib.mjs";

const CONSTITUTION_PATH = join(SPEC_ROOT, "constitution.md");
const DEFAULT_PROFILE = "general";
const KNOWN_PROFILES = new Set(["general", "xuanwu-activity"]);

const USAGE = {
  usage: "node .xflow/scripts/xf-profile.mjs [--value-only]",
  description:
    "读取 xspec/constitution.md 中的 project_profile 字段。所有 xflow 命令统一通过\n" +
    "此脚本获取当前项目 profile，避免各命令自行解析 markdown 导致格式漂移。",
  required: {},
  optional: {
    "--value-only": "仅输出 profile 值（便于 shell 内消费，例如 $(node ... --value-only)）",
    "--help, -h": "打印本帮助并退出",
  },
  examples: [
    "node .xflow/scripts/xf-profile.mjs",
    "node .xflow/scripts/xf-profile.mjs --value-only",
  ],
  notes: [
    "constitution.md 不存在 / 字段缺失 / 值为空均回退为 general",
    `已知 profile 白名单：${[...KNOWN_PROFILES].join(", ")}（不在白名单的值会按 general 流程执行）`,
  ],
};

const ALLOWED_ARGS = ["help", "value-only"];

/**
 * 从 constitution.md 内容中提取 project_profile 字段。
 * 返回匹配到的值，未匹配返回 null。
 */
export function extractProfile(content) {
  if (!content) return null;

  const patterns = [
    /^\s*-\s*\*\*project_profile\*\*\s*[:：]\s*["']?([A-Za-z0-9_-]+)["']?/m,
    /^\s*-\s*`?project_profile`?\s*[:：]\s*["']?([A-Za-z0-9_-]+)["']?/m,
    /^\s*project_profile\s*[:：]\s*["']?([A-Za-z0-9_-]+)["']?/m,
    /^\s*\|\s*`?project_profile`?\s*\|\s*["']?([A-Za-z0-9_-]+)["']?\s*\|/m,
  ];

  for (const re of patterns) {
    const match = content.match(re);
    if (match && match[1]) {
      const val = match[1].trim();
      if (val.length > 0) return val;
    }
  }
  return null;
}

export async function resolveProfile() {
  if (!existsSync(CONSTITUTION_PATH)) {
    return { profile: DEFAULT_PROFILE, source: "default", reason: "constitution.md 不存在" };
  }

  let content;
  try {
    content = await readFile(CONSTITUTION_PATH, "utf-8");
  } catch (err) {
    return { profile: DEFAULT_PROFILE, source: "default", reason: `读取失败: ${err.message}` };
  }

  const value = extractProfile(content);
  if (!value) {
    return { profile: DEFAULT_PROFILE, source: "default", reason: "project_profile 字段缺失" };
  }

  const result = { profile: value, source: "xspec/constitution.md" };
  if (!KNOWN_PROFILES.has(value)) {
    result.warning = `未知 profile "${value}"；已知列表：${[...KNOWN_PROFILES].join(", ")}。命令会按 general 流程执行，但 .xflow/profiles/${value}/ 目录若存在仍会被加载。`;
  }
  return result;
}

async function main() {
  const args = parseArgs();
  handleHelpFlag(args, USAGE);
  assertAllowedArgs(args, ALLOWED_ARGS, USAGE);

  let result;
  try {
    result = await resolveProfile();
  } catch (err) {
    fatal(`解析 project_profile 失败: ${err.message}`);
  }

  if (args["value-only"]) {
    console.log(result.profile);
    return;
  }
  output(result);
}

const isMain = import.meta.url === `file://${process.argv[1]}`;
if (isMain) {
  main().catch((err) => fatal(err.message));
}
