#!/usr/bin/env node

/**
 * xf-validate.mjs — 产出自检脚本（结构化检查）
 *
 * 用法:
 *   node .xflow/scripts/xf-validate.mjs --phase clarify
 *   node .xflow/scripts/xf-validate.mjs --phase detail
 *
 * 文件清单来自 CATALOG.yaml（通过 xf-manifest.mjs 查询），
 * validate-rules.yaml 仅保留 placeholders / semantic_checks / change_files 等真正的"校验规则"。
 *
 * 校验检查点语义（validate 的 --phase → CATALOG.yaml 的 phase 集合）:
 *   clarify → { clarify }                    // 只检查 spec.md
 *   detail  → { design, detail }             // 检查 hld.md 及所有 detail 阶段产物
 *                                            // （因为 detail 检查点时 /xf/design 应已执行）
 *
 * 输出 JSON:
 *   { "phase": "clarify", "pass": true/false, "summary": {...}, "results": [...] }
 *
 * 退出码:
 *   0 = 全部通过
 *   1 = 有 failure
 */

import { readFile } from "fs/promises";
import { join } from "path";
import { existsSync } from "fs";
import {
  SPECFLOW_ROOT,
  CHANGES_DIR,
  TEMPLATES_DIR,
  readChangeYaml,
  output,
  fatal,
  parseArgs,
  printUsageAndExit,
  handleHelpFlag,
  assertAllowedArgs,
} from "./lib.mjs";

const ALLOWED_ARGS = ["help", "phase"];
import { loadCatalog, buildManifest } from "./xf-manifest.mjs";

const TIMEOUT_MS = 15_000;
const timer = setTimeout(() => {
  console.error("[xflow] Error: xf-validate.mjs timed out after 15s");
  process.exit(1);
}, TIMEOUT_MS);

const USAGE = {
  usage: "node .xflow/scripts/xf-validate.mjs --phase <clarify|detail>",
  description:
    "在阶段门禁前对当前活跃变更（xspec/changes/）执行结构化产出自检：\n" +
    "  - 必填文件存在性 / 必填章节存在性\n" +
    "  - 占位符未被替换的检测\n" +
    "  - validate-rules.yaml 里声明的语义检查\n" +
    "结果以 JSON 输出。退出码非 0 表示有 failure，AI 必须根据 failures 修补后重新自检。",
  required: {
    "--phase <stage>": "校验检查点（不是模块的内部 phase 字段）",
  },
  optional: {
    "--help, -h": "打印本帮助并退出",
  },
  values: {
    "--phase":
      "clarify | detail（clarify 检查点覆盖 phase=clarify 的产物；detail 检查点覆盖 phase=design/detail）",
  },
  examples: [
    "node .xflow/scripts/xf-validate.mjs --phase clarify",
    "node .xflow/scripts/xf-validate.mjs --phase detail",
  ],
  notes: [
    "本脚本依赖 xspec/changes/change.yaml 与 .xflow/validate-rules.yaml，缺失时直接退出 1",
    "文件清单来自 CATALOG.yaml；CATALOG.yaml 中 validations.skip_required_sections=true 的模块会跳过章节检查",
  ],
};

// validate phase → catalog phases that should be checked at this checkpoint
const CHECKPOINT_PHASES = {
  clarify: ["clarify"],
  detail: ["design", "detail"],
};

// ── Minimal YAML parser for validate-rules.yaml ──
// Kept inline here to avoid coupling validate-rules.yaml format to CATALOG.yaml parser.
// validate-rules.yaml uses list syntax (- item) which CATALOG.yaml does not.

function parseValidateRules(content) {
  const rules = { valid_empty_markers: [], phases: {} };
  let currentPath = [];
  let indentStack = [-1];

  for (const raw of content.split("\n")) {
    const line = raw.replace(/\r$/, "");
    if (!line.trim() || line.trim().startsWith("#")) continue;

    const indent = line.search(/\S/);
    const trimmed = line.trim();

    while (indentStack.length > 1 && indent <= indentStack[indentStack.length - 1]) {
      indentStack.pop();
      currentPath.pop();
    }

    if (trimmed.startsWith("- id:")) {
      const id = trimmed.match(/id:\s*"?([^"]+)"?/)?.[1]?.trim();
      const parent = getNestedValue(rules, currentPath);
      if (Array.isArray(parent)) {
        parent.push({ id });
      }
      continue;
    }

    if (trimmed.startsWith("- ")) {
      const val = trimmed.slice(2).replace(/^"(.*)"$/, "$1").trim();
      const parent = getNestedValue(rules, currentPath);
      if (Array.isArray(parent)) {
        parent.push(val);
      }
      continue;
    }

    if (trimmed.includes(":")) {
      const colonIdx = trimmed.indexOf(":");
      const key = trimmed.slice(0, colonIdx).trim();
      const valPart = trimmed.slice(colonIdx + 1).trim();

      if (!valPart) {
        indentStack.push(indent);
        currentPath.push(key);
        const parent = getNestedValue(rules, currentPath.slice(0, -1));
        if (parent && typeof parent === "object" && !Array.isArray(parent)) {
          if (!(key in parent)) parent[key] = {};
        }
      } else {
        const val = valPart.replace(/^"(.*)"$/, "$1").trim();
        const parent = getNestedValue(rules, currentPath);
        if (Array.isArray(parent) && parent.length > 0) {
          const lastItem = parent[parent.length - 1];
          if (typeof lastItem === "object") {
            lastItem[key] = val;
          }
        } else if (parent && typeof parent === "object") {
          parent[key] = val;
        }
      }
    }
  }

  // Post-process: ensure list fields are arrays
  for (const [, phase] of Object.entries(rules.phases)) {
    if (phase.change_files && !Array.isArray(phase.change_files)) phase.change_files = [];
    if (phase.placeholders && !Array.isArray(phase.placeholders)) phase.placeholders = [];
    if (phase.semantic_checks && !Array.isArray(phase.semantic_checks)) phase.semantic_checks = [];
  }

  return rules;
}

function getNestedValue(obj, path) {
  let current = obj;
  for (const key of path) {
    if (current == null || typeof current !== "object") return undefined;
    if (!(key in current)) {
      current[key] = {};
    }
    current = current[key];
  }
  return current;
}

// ── Check functions ──

function checkFileExists(filePath) {
  return existsSync(filePath);
}

function extractSections(content) {
  const sections = [];
  for (const line of content.split("\n")) {
    const match = line.match(/^##\s+(.+)/);
    if (match) sections.push(match[1].trim());
  }
  return sections;
}

function checkRequiredSections(fileContent, templateContent) {
  const templateSections = extractSections(templateContent);
  const fileSections = extractSections(fileContent);
  const missing = [];

  for (const section of templateSections) {
    const sectionName = section.replace(/^\d+\.\s*/, "").trim();
    const found = fileSections.some(s => {
      const normalized = s.replace(/^\d+\.\s*/, "").trim();
      return normalized === sectionName || normalized.includes(sectionName) || sectionName.includes(normalized);
    });
    if (!found) missing.push(section);
  }
  return missing;
}

function checkPlaceholders(content, placeholders) {
  const found = [];
  for (const ph of placeholders) {
    const escaped = ph.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const regex = new RegExp(escaped, "g");
    if (regex.test(content)) found.push(ph);
  }
  return found;
}

function checkEmptySections(content, validMarkers) {
  const lines = content.split("\n");
  const empty = [];

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(/^##\s+(.+)/);
    if (!match) continue;

    const sectionTitle = match[1].trim();
    let hasContent = false;
    let j = i + 1;

    while (j < lines.length && !lines[j].match(/^##\s/)) {
      const trimmed = lines[j].trim();
      if (trimmed && !trimmed.startsWith(">") && !trimmed.startsWith("---")) {
        if (validMarkers.some(m => trimmed.includes(m))) {
          hasContent = true;
          break;
        }
        if (trimmed.length > 3 && !trimmed.startsWith("|") || (trimmed.startsWith("|") && !trimmed.includes(":---"))) {
          hasContent = true;
          break;
        }
      }
      j++;
    }

    if (!hasContent) empty.push(sectionTitle);
  }
  return empty;
}

function checkYamlFields(changeData) {
  const missing = [];
  const requiredTop = ["id", "name", "phase"];
  for (const field of requiredTop) {
    if (!changeData[field]) missing.push(field);
  }

  if (!changeData.affected_modules || changeData.affected_modules.length === 0) {
    missing.push("affected_modules");
  } else {
    for (let i = 0; i < changeData.affected_modules.length; i++) {
      const mod = changeData.affected_modules[i];
      for (const field of ["name", "change_type", "module_type", "summary"]) {
        if (!mod[field]) missing.push(`affected_modules[${i}].${field}`);
      }
    }
  }

  return missing;
}

function checkDeltaBaseline(content) {
  return content.includes("> 基线：") || content.includes("> 基线:");
}

// ── Template resolution ──

function resolveTemplatePath(moduleType, fileName) {
  const specific = join(TEMPLATES_DIR, moduleType, fileName);
  if (existsSync(specific)) return specific;
  const fallback = join(TEMPLATES_DIR, "_default", fileName);
  if (existsSync(fallback)) return fallback;
  return null;
}

// ── Main ──

async function main() {
  const args = parseArgs();
  handleHelpFlag(args, USAGE);
  assertAllowedArgs(args, ALLOWED_ARGS, USAGE);

  if (!args.phase) printUsageAndExit(USAGE, "缺少 --phase 参数");

  const checkpointPhases = CHECKPOINT_PHASES[args.phase];
  if (!checkpointPhases) {
    printUsageAndExit(
      USAGE,
      `非法 --phase "${args.phase}"，合法值：${Object.keys(CHECKPOINT_PHASES).join(" | ")}`,
    );
  }

  const rulesPath = join(SPECFLOW_ROOT, "validate-rules.yaml");
  if (!existsSync(rulesPath)) fatal("未找到 .xflow/validate-rules.yaml");

  const rulesContent = await readFile(rulesPath, "utf-8");
  const rules = parseValidateRules(rulesContent);

  const phaseConfig = rules.phases?.[args.phase];
  if (!phaseConfig) fatal(`validate-rules.yaml 中未定义 phase: ${args.phase}`);

  const changeData = await readChangeYaml();
  if (!changeData) fatal("未找到 xspec/changes/change.yaml");

  const catalog = await loadCatalog();

  const validMarkers = rules.valid_empty_markers || [];
  const placeholders = phaseConfig.placeholders || [];
  const results = [];

  // ── Check change_files (e.g. change.yaml) ──
  if (phaseConfig.change_files) {
    for (const fileName of phaseConfig.change_files) {
      if (fileName === "change.yaml") {
        const filePath = join(CHANGES_DIR, "change.yaml");

        if (!checkFileExists(filePath)) {
          results.push({ file: "changes/change.yaml", rule: "file-exists", status: "fail", detail: "文件不存在" });
          continue;
        }
        results.push({ file: "changes/change.yaml", rule: "file-exists", status: "pass" });

        const missingFields = checkYamlFields(changeData);
        if (missingFields.length > 0) {
          results.push({ file: "changes/change.yaml", rule: "yaml-fields", status: "fail", detail: `缺少字段：${missingFields.join(", ")}` });
        } else {
          results.push({ file: "changes/change.yaml", rule: "yaml-fields", status: "pass" });
        }

        const yamlContent = await readFile(filePath, "utf-8");
        const phFound = checkPlaceholders(yamlContent, placeholders);
        if (phFound.length > 0) {
          results.push({ file: "changes/change.yaml", rule: "no-placeholder", status: "fail", detail: `残留占位符：${phFound.join(", ")}` });
        } else {
          results.push({ file: "changes/change.yaml", rule: "no-placeholder", status: "pass" });
        }
      }
    }
  }

  // ── Check module files via manifest ──
  const modules = changeData.affected_modules || [];

  for (const mod of modules) {
    const { name: modName, change_type: changeType, module_type: moduleType } = mod;

    // Query manifest for this module, filtered to the phases relevant at this checkpoint.
    const manifest = buildManifest({
      catalog,
      moduleType,
      phase: null,
      allPhases: true,
      changeType,
    });
    const checkpointFiles = manifest.files.filter((f) => checkpointPhases.includes(f.phase));

    for (const fileEntry of checkpointFiles) {
      const fileName = fileEntry.name;
      const status = fileEntry.status; // required | optional | dynamic
      const filePath = join(CHANGES_DIR, modName, fileName);
      const relPath = `changes/${modName}/${fileName}`;

      if (!checkFileExists(filePath)) {
        if (status === "optional") {
          results.push({ file: relPath, rule: "file-exists", status: "skip", detail: "按需文件，不存在则跳过" });
          continue;
        }
        if (status === "dynamic") {
          // SKILL / 外部工具按需产出；xflow 不强制其存在，缺失即视为本次未触发该产物。
          results.push({ file: relPath, rule: "file-exists", status: "skip", detail: "运行时按需产物，不存在则跳过" });
          continue;
        }
        results.push({ file: relPath, rule: "file-exists", status: "fail", detail: "文件不存在" });
        continue;
      }
      results.push({ file: relPath, rule: "file-exists", status: "pass" });

      const content = await readFile(filePath, "utf-8");

      // no-placeholder
      const phFound = checkPlaceholders(content, placeholders);
      if (phFound.length > 0) {
        results.push({ file: relPath, rule: "no-placeholder", status: "fail", detail: `残留占位符：${phFound.join(", ")}` });
      } else {
        results.push({ file: relPath, rule: "no-placeholder", status: "pass" });
      }

      // delta-baseline
      if (fileName.includes(".delta.")) {
        if (!checkDeltaBaseline(content)) {
          results.push({ file: relPath, rule: "delta-baseline", status: "fail", detail: "缺少基线引用（> 基线：...）" });
        } else {
          results.push({ file: relPath, rule: "delta-baseline", status: "pass" });
        }
      }

      // required-sections (only for full files, not deltas)
      //
      // 当 CATALOG.yaml 中该 module_type 声明 validations.skip_required_sections: true 时跳过。
      // 适用于"章节 SSOT 在模块类型之外"的场景（如玄武 SKILL 按 bundle 模板生成内容，章节结构
      // 由 SKILL 的 reviewer Agent 负责校验，与 .xflow/templates/<type>/*.md 骨架天然错位）。
      if (!fileName.includes(".delta.")) {
        if (manifest.validations?.skip_required_sections) {
          results.push({
            file: relPath,
            rule: "required-sections",
            status: "skip",
            detail: `按 CATALOG.yaml 配置跳过（module_type=${manifest.resolved_type} 的章节校验由 SKILL/bundle 负责）`,
          });
        } else {
          const templatePath = resolveTemplatePath(manifest.resolved_type, fileName);
          if (templatePath) {
            const templateContent = await readFile(templatePath, "utf-8");
            const missingSections = checkRequiredSections(content, templateContent);
            if (missingSections.length > 0) {
              results.push({ file: relPath, rule: "required-sections", status: "fail", detail: `缺少章节：${missingSections.join(", ")}` });
            } else {
              results.push({ file: relPath, rule: "required-sections", status: "pass" });
            }
          }
        }
      }

      // no-empty-section
      const emptySections = checkEmptySections(content, validMarkers);
      if (emptySections.length > 0) {
        results.push({ file: relPath, rule: "no-empty-section", status: "fail", detail: `空章节：${emptySections.join(", ")}` });
      } else {
        results.push({ file: relPath, rule: "no-empty-section", status: "pass" });
      }
    }
  }

  const passed = results.filter(r => r.status === "pass").length;
  const failed = results.filter(r => r.status === "fail").length;
  const skipped = results.filter(r => r.status === "skip").length;
  const total = results.length;

  const report = {
    phase: args.phase,
    pass: failed === 0,
    summary: { total, passed, failed, skipped },
    failures: results.filter(r => r.status === "fail"),
  };

  clearTimeout(timer);
  output(report);
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  clearTimeout(timer);
  console.error(`[xflow] Error: ${err.message}`);
  process.exit(1);
});
