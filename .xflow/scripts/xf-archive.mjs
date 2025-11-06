#!/usr/bin/env node

/**
 * xf-archive — 归档的机械操作部分
 *
 * Usage:
 *   # 步骤 1：新增模块复制到 modules（在 AI 完成 delta 合并后调用）
 *   node .xflow/scripts/xf-archive.mjs --step copy-new --modules user-auth,login-page
 *
 *   # 步骤 2：搬运到 archive + 清空 changes
 *   node .xflow/scripts/xf-archive.mjs --step finalize
 *
 * Steps:
 *   copy-new  — 将 --modules 指定的新增模块从 xspec/changes/<name>/ 复制到 xspec/modules/<name>/,
 *               并为每个模块创建 module.yaml
 *   finalize  — 将 xspec/changes/ 整体复制到 xspec/archive/<change-id>/, 然后清空 xspec/changes/
 */

import {
  MODULES_DIR,
  CHANGES_DIR,
  ARCHIVE_DIR,
  exists,
  ensureDir,
  copyDir,
  emptyDir,
  readChangeYaml,
  output,
  fatal,
  parseArgs,
  printUsageAndExit,
  handleHelpFlag,
  assertAllowedArgs,
} from "./lib.mjs";

const ALLOWED_ARGS = ["help", "step", "modules"];

const USAGE = {
  usage: "node .xflow/scripts/xf-archive.mjs --step <copy-new|finalize> [选项]",
  description:
    "执行归档的机械操作。copy-new 把新增模块从 xspec/changes/ 复制到 xspec/modules/；\n" +
    "finalize 把 xspec/changes/ 整体搬到 xspec/archive/<change-id>/ 并清空 xspec/changes/。",
  required: {
    "--step <name>": "操作类型，必须是 copy-new 或 finalize",
  },
  optional: {
    "--modules <a,b,c>": "[copy-new 必填] 逗号分隔的新增模块名列表",
    "--help, -h": "打印本帮助并退出",
  },
  values: {
    "--step": "copy-new | finalize",
  },
  examples: [
    "node .xflow/scripts/xf-archive.mjs --step copy-new --modules user-auth,login-page",
    "node .xflow/scripts/xf-archive.mjs --step finalize",
  ],
  notes: [
    "copy-new 必须搭配 --modules；finalize 不接受 --modules（提供也会被忽略）",
    "xuanwu-activity profile 下 copy-new 自动跳过（活动不沉淀到 modules/）",
    "finalize 会清空 xspec/changes/，请确保 AI 已完成 delta 合并、changelog 生成等准备",
  ],
};
import { resolveProfile } from "./xf-profile.mjs";
import { join } from "path";
import { writeFile } from "fs/promises";

async function copyNew(moduleNames) {
  const change = await readChangeYaml();
  if (!change) fatal("未找到 xspec/changes/change.yaml，无活跃变更");

  // Profile 分支：xuanwu-activity 模式下，活动不沉淀到 modules/，避免
  // 下一次活动复用同名 activity 模块时把上一次内容当增量合并。
  // 只保留 finalize 阶段的整包归档到 archive/<change-id>/，modules/ 永远为空。
  const { profile } = await resolveProfile();
  if (profile === "xuanwu-activity") {
    output({
      success: true,
      action: "archive:copy-new",
      profile,
      change_id: change.id,
      results: moduleNames.map((name) => ({
        module: name,
        status: "skipped",
        reason: "project_profile=xuanwu-activity：活动不沉淀到 modules/，仅归档到 archive/",
      })),
      message: "玄武活动模式：跳过 copy-new，等待 finalize 直接搬运到 archive/",
    });
    return;
  }

  const changeId = change.id;
  const now = new Date().toISOString().slice(0, 10);
  const results = [];

  for (const name of moduleNames) {
    const src = join(CHANGES_DIR, name);
    if (!exists(src)) {
      results.push({ module: name, status: "skipped", reason: "源目录不存在" });
      continue;
    }

    const dest = join(MODULES_DIR, name);
    if (exists(dest)) {
      results.push({ module: name, status: "skipped", reason: "目标已存在（变更模块应由 AI 合并）" });
      continue;
    }

    const copied = await copyDir(src, dest);

    const meta = change.affected_modules?.find((m) => m.name === name);
    const moduleYaml = [
      `name: "${name}"`,
      `type: "${meta?.module_type || "unknown"}"`,
      `version: "1.0"`,
      `created_at: "${now}"`,
      `created_by_change: "${changeId}"`,
      `last_updated: "${now}"`,
      `change_history:`,
      `  - change_id: "${changeId}"`,
      `    date: "${now}"`,
      `    description: "初始创建"`,
    ].join("\n") + "\n";

    await writeFile(join(dest, "module.yaml"), moduleYaml);
    results.push({ module: name, status: "created", files_copied: copied.length + 1 });
  }

  output({
    success: true,
    action: "archive:copy-new",
    change_id: change.id,
    results,
    message: `新增模块复制完成`,
  });
}

async function finalize() {
  const change = await readChangeYaml();
  if (!change) fatal("未找到 xspec/changes/change.yaml，无活跃变更");

  const changeId = change.id;
  if (!changeId) fatal("xspec/changes/change.yaml 中缺少 id 字段");

  const archiveTarget = join(ARCHIVE_DIR, changeId);
  if (exists(archiveTarget)) {
    fatal(`归档目录已存在: xspec/archive/${changeId}（该变更可能已归档过）`);
  }

  const copied = await copyDir(CHANGES_DIR, archiveTarget);
  await emptyDir(CHANGES_DIR, true);

  const { profile } = await resolveProfile();
  output({
    success: true,
    action: "archive:finalize",
    profile,
    change_id: changeId,
    change_name: change.name || null,
    archive_path: archiveTarget,
    files_archived: copied.length,
    message: `变更 ${changeId}${change.name ? `（${change.name}）` : ""} 已归档到 archive/${changeId}/，changes/ 已清空`,
  });
}

async function main() {
  const args = parseArgs();
  handleHelpFlag(args, USAGE);
  assertAllowedArgs(args, ALLOWED_ARGS, USAGE);

  if (!args.step) printUsageAndExit(USAGE, "缺少 --step 参数");

  switch (args.step) {
    case "copy-new": {
      if (!args.modules)
        printUsageAndExit(USAGE, "--step copy-new 需要 --modules 参数（逗号分隔模块名）");
      const moduleNames = args.modules.split(",").map((s) => s.trim()).filter(Boolean);
      if (moduleNames.length === 0)
        printUsageAndExit(USAGE, "--modules 解析后为空，请提供至少一个模块名");
      await copyNew(moduleNames);
      break;
    }
    case "finalize":
      await finalize();
      break;
    default:
      printUsageAndExit(USAGE, `未知 --step "${args.step}"，合法值：copy-new | finalize`);
  }
}

main().catch((err) => {
  console.error(`[xflow] Error: ${err.message}`);
  process.exit(1);
});
