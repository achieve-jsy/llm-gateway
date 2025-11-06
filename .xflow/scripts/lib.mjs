import { readdir, readFile, stat, mkdir, writeFile } from "fs/promises";
import { resolve, join, dirname } from "path";
import { existsSync } from "fs";
import { fileURLToPath } from "url";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
export const SPECFLOW_ROOT = resolve(SCRIPT_DIR, "..");
export const PROJECT_ROOT = resolve(SPECFLOW_ROOT, "..");
export const SPEC_ROOT = join(PROJECT_ROOT, "xspec");

export const TEMPLATES_DIR = join(SPECFLOW_ROOT, "templates");
export const MODULES_DIR = join(SPEC_ROOT, "modules");
export const CHANGES_DIR = join(SPEC_ROOT, "changes");
export const ARCHIVE_DIR = join(SPEC_ROOT, "archive");

export function exists(p) {
  return existsSync(p);
}

export async function ensureDir(p) {
  if (!existsSync(p)) {
    await mkdir(p, { recursive: true });
    return true;
  }
  return false;
}

/**
 * Minimal YAML parser for change.yaml.
 * Handles flat key-value pairs and the affected_modules list.
 * Not a general-purpose parser — intentionally kept simple.
 */
export function parseChangeYaml(content) {
  const result = { affected_modules: [] };
  let currentModule = null;

  for (const raw of content.split("\n")) {
    const line = raw.trimEnd();
    if (!line || line.startsWith("#")) continue;

    // List item inside affected_modules
    if (/^\s+-\s+name:/.test(line)) {
      currentModule = { name: line.match(/name:\s*"?([^"]+)"?/)?.[1]?.trim() };
      result.affected_modules.push(currentModule);
      continue;
    }
    if (currentModule && /^\s+change_type:/.test(line)) {
      currentModule.change_type = line.match(/change_type:\s*"?([^"]+)"?/)?.[1]?.trim();
      continue;
    }
    if (currentModule && /^\s+type:/.test(line)) {
      // Legacy field — map to change_type for backward compatibility
      const val = line.match(/type:\s*"?([^"]+)"?/)?.[1]?.trim();
      const mapped = val === "new" ? "added" : val;
      currentModule.change_type = currentModule.change_type || mapped;
      continue;
    }
    if (currentModule && /^\s+module_type:/.test(line)) {
      currentModule.module_type = line.match(/module_type:\s*"?([^"]+)"?/)?.[1]?.trim();
      continue;
    }
    if (currentModule && /^\s+summary:/.test(line)) {
      currentModule.summary = line.match(/summary:\s*"?([^"]*)"?/)?.[1]?.trim();
      continue;
    }

    // Top-level key: value (skip keys with list values like "affected_modules:")
    if (!line.startsWith(" ") && !line.startsWith("\t")) {
      currentModule = null;
      if (line.match(/^affected_modules:\s*$/)) continue;
      const m = line.match(/^(\w+):\s*"?([^"]*)"?\s*$/);
      if (m) result[m[1]] = m[2].trim();
    }
  }
  return result;
}

export async function readChangeYaml() {
  const p = join(CHANGES_DIR, "change.yaml");
  if (!existsSync(p)) return null;
  const content = await readFile(p, "utf-8");
  return parseChangeYaml(content);
}

/**
 * Minimal block-style YAML parser for nested maps.
 *
 * Supports:
 *   - Nested maps with consistent indentation (2 spaces recommended)
 *   - Scalar values: key: value  /  key: "quoted value"
 *   - Comments (# ...) and blank lines
 *
 * Does NOT support:
 *   - Arrays / sequences (use explicit map keys instead)
 *   - Flow-style maps ({ ... })
 *   - Multiline strings / anchors / merge keys
 *
 * Throws an Error with line number on malformed input.
 */
export function parseCatalogYaml(content) {
  const lines = content.split("\n");
  const root = {};
  const stack = [{ indent: -1, obj: root }];

  for (let lineNo = 0; lineNo < lines.length; lineNo++) {
    const raw = lines[lineNo].replace(/\r$/, "");
    if (!raw.trim() || raw.trim().startsWith("#")) continue;

    const indent = raw.search(/\S/);
    const trimmed = raw.trim();

    // Strip trailing inline comment (only when preceded by whitespace, and not inside quotes)
    let line = trimmed;
    const inlineCommentIdx = findInlineComment(line);
    if (inlineCommentIdx !== -1) line = line.slice(0, inlineCommentIdx).trimEnd();

    if (indent % 2 !== 0) {
      throw new Error(`YAML 解析错误（第 ${lineNo + 1} 行）：缩进必须是 2 的倍数，实际 ${indent}`);
    }

    const colonIdx = line.indexOf(":");
    if (colonIdx === -1) {
      throw new Error(`YAML 解析错误（第 ${lineNo + 1} 行）：期望 "key: value" 或 "key:" 格式，实际 "${line}"`);
    }

    const key = line.slice(0, colonIdx).trim();
    const valPart = line.slice(colonIdx + 1).trim();

    // Pop stack until we find the parent (indent < current)
    while (stack.length > 0 && stack[stack.length - 1].indent >= indent) {
      stack.pop();
    }
    if (stack.length === 0) {
      throw new Error(`YAML 解析错误（第 ${lineNo + 1} 行）：缩进层次异常`);
    }

    const parent = stack[stack.length - 1].obj;

    if (valPart === "") {
      const child = {};
      parent[key] = child;
      stack.push({ indent, obj: child });
    } else {
      parent[key] = unquoteScalar(valPart);
    }
  }

  return root;
}

function findInlineComment(line) {
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (ch === "'" && !inDouble) inSingle = !inSingle;
    else if (ch === '"' && !inSingle) inDouble = !inDouble;
    else if (ch === "#" && !inSingle && !inDouble) {
      if (i === 0 || /\s/.test(line[i - 1])) return i;
    }
  }
  return -1;
}

function unquoteScalar(s) {
  if (s.length >= 2) {
    if (s.startsWith('"') && s.endsWith('"')) return s.slice(1, -1);
    if (s.startsWith("'") && s.endsWith("'")) return s.slice(1, -1);
  }
  return s;
}

/**
 * List immediate subdirectory names under a path.
 */
export async function listDirs(dir) {
  if (!existsSync(dir)) return [];
  const entries = await readdir(dir, { withFileTypes: true });
  return entries.filter((e) => e.isDirectory()).map((e) => e.name);
}

/**
 * List files (non-directory) under a path.
 */
export async function listFiles(dir) {
  if (!existsSync(dir)) return [];
  const entries = await readdir(dir, { withFileTypes: true });
  return entries.filter((e) => e.isFile()).map((e) => e.name);
}

/**
 * Recursively copy a directory tree from src to dest.
 */
export async function copyDir(src, dest) {
  await ensureDir(dest);
  const entries = await readdir(src, { withFileTypes: true });
  const copied = [];
  for (const entry of entries) {
    const srcPath = join(src, entry.name);
    const destPath = join(dest, entry.name);
    if (entry.isDirectory()) {
      copied.push(...(await copyDir(srcPath, destPath)));
    } else {
      const content = await readFile(srcPath);
      await writeFile(destPath, content);
      copied.push(destPath);
    }
  }
  return copied;
}

/**
 * Remove all contents inside a directory, keeping the directory itself.
 * Optionally writes a .gitkeep to keep it tracked.
 */
export async function emptyDir(dir, keepGitkeep = true) {
  if (!existsSync(dir)) return;
  const { rm } = await import("fs/promises");
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    if (keepGitkeep && entry.name === ".gitkeep") continue;
    await rm(join(dir, entry.name), { recursive: true, force: true });
  }
  if (keepGitkeep && !existsSync(join(dir, ".gitkeep"))) {
    await writeFile(join(dir, ".gitkeep"), "");
  }
}

/**
 * Print JSON result to stdout and exit.
 */
export function output(data) {
  console.log(JSON.stringify(data, null, 2));
}

/**
 * Print error to stderr and exit with code 1.
 */
export function fatal(message) {
  console.error(`[xflow] Error: ${message}`);
  process.exit(1);
}

/**
 * Parse CLI arguments into a simple { key: value } map.
 * Supports --key value, --flag (boolean true), and -h/--help shortcut.
 */
export function parseArgs(argv = process.argv.slice(2)) {
  const args = {};
  for (let i = 0; i < argv.length; i++) {
    const token = argv[i];
    if (token === "-h") {
      args.help = true;
      continue;
    }
    if (token.startsWith("--")) {
      const key = token.slice(2);
      const next = argv[i + 1];
      if (next && !next.startsWith("--") && next !== "-h") {
        args[key] = next;
        i++;
      } else {
        args[key] = true;
      }
    }
  }
  return args;
}

/**
 * Render a USAGE descriptor into a human-readable, AI-parseable block.
 *
 * The USAGE descriptor is a structured object so we can guarantee a uniform
 * format across all xflow scripts. AI agents (and humans) reading the error
 * output get the same skeleton every time:
 *
 *   用法: <oneliner>
 *
 *   说明:
 *     <description, optional, multi-line ok>
 *
 *   必填参数:
 *     --foo <value>    描述
 *
 *   可选参数:
 *     --bar            描述
 *
 *   合法值:
 *     --foo: a | b | c
 *
 *   示例:
 *     node script.mjs --foo a
 *     node script.mjs --foo b --bar
 *
 *   注意:
 *     - <gotcha 1>
 *     - <gotcha 2>
 *
 * Sections that are absent from the descriptor are skipped silently.
 */
export function renderUsage(usage) {
  const lines = [];
  if (usage.usage) lines.push(`用法: ${usage.usage}`);
  if (usage.description) {
    lines.push("");
    lines.push("说明:");
    for (const ln of usage.description.split("\n")) lines.push(`  ${ln}`);
  }
  if (usage.required && Object.keys(usage.required).length > 0) {
    lines.push("");
    lines.push("必填参数:");
    for (const [k, v] of Object.entries(usage.required)) lines.push(`  ${pad(k, 28)}${v}`);
  }
  if (usage.optional && Object.keys(usage.optional).length > 0) {
    lines.push("");
    lines.push("可选参数:");
    for (const [k, v] of Object.entries(usage.optional)) lines.push(`  ${pad(k, 28)}${v}`);
  }
  if (usage.values && Object.keys(usage.values).length > 0) {
    lines.push("");
    lines.push("合法值:");
    for (const [k, v] of Object.entries(usage.values)) lines.push(`  ${pad(k, 28)}${v}`);
  }
  if (usage.examples && usage.examples.length > 0) {
    lines.push("");
    lines.push("示例:");
    for (const ex of usage.examples) lines.push(`  ${ex}`);
  }
  if (usage.notes && usage.notes.length > 0) {
    lines.push("");
    lines.push("注意:");
    for (const n of usage.notes) lines.push(`  - ${n}`);
  }
  return lines.join("\n");
}

function pad(s, width) {
  if (s.length >= width) return s + "  ";
  return s + " ".repeat(width - s.length);
}

/**
 * Print error message + USAGE block to stderr and exit with code 1.
 *
 * Design intent: when an AI agent invokes an xflow script with wrong/missing
 * args, the script's exit output should give the AI everything it needs to
 * self-correct without consulting external docs. So we always echo the full
 * USAGE descriptor next to the error.
 */
export function printUsageAndExit(usage, errorMessage) {
  console.error(`[xflow] Error: ${errorMessage}`);
  console.error("");
  console.error(renderUsage(usage));
  process.exit(1);
}

/**
 * Print USAGE block to stdout and exit cleanly. Used for explicit `--help` /
 * `-h` invocations where the user/AI is querying the spec, not erroring out.
 */
export function printHelpAndExit(usage) {
  console.log(renderUsage(usage));
  process.exit(0);
}

/**
 * If args.help is truthy, print USAGE and exit 0. Convenience helper for
 * scripts so they can do `handleHelpFlag(args, USAGE)` as the first thing in
 * main().
 */
export function handleHelpFlag(args, usage) {
  if (args.help) printHelpAndExit(usage);
}

/**
 * Levenshtein 距离（迭代版，O(m*n) 时空，足够 CLI 参数名场景）。
 *
 * 仅用于 assertAllowedArgs 的"你是不是想写"建议，不暴露为公开 API。
 */
function levenshtein(a, b) {
  if (a === b) return 0;
  if (!a) return b.length;
  if (!b) return a.length;
  const m = a.length;
  const n = b.length;
  // 滚动数组：prev / curr 各 n+1 长度
  let prev = new Array(n + 1);
  let curr = new Array(n + 1);
  for (let j = 0; j <= n; j++) prev[j] = j;
  for (let i = 1; i <= m; i++) {
    curr[0] = i;
    for (let j = 1; j <= n; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      curr[j] = Math.min(
        curr[j - 1] + 1, // insert
        prev[j] + 1, // delete
        prev[j - 1] + cost, // substitute
      );
    }
    [prev, curr] = [curr, prev];
  }
  return prev[n];
}

/**
 * 在 allowed 列表里找一个最接近 input 的候选，距离 > maxDist 时返回 null。
 *
 * 设计偏保守（默认 maxDist=2）：避免给出离谱的推荐误导 agent。比较时大小写不敏感
 * 也忽略风格差异（连字符 / 下划线 / 驼峰统一规范化），这样 `--moduleType`、
 * `--module_type`、`--ModuleType` 都能映射到 `--module-type`。
 */
function suggestSimilar(input, allowed, maxDist = 2) {
  const norm = (s) => s.toLowerCase().replace(/[-_]/g, "");
  const target = norm(input);
  let best = null;
  let bestDist = Infinity;
  for (const cand of allowed) {
    const d = levenshtein(target, norm(cand));
    if (d < bestDist) {
      bestDist = d;
      best = cand;
    }
  }
  return bestDist <= maxDist ? best : null;
}

/**
 * 校验 args 中只包含 allowed 列表里的 key，否则立刻 exit 1 + 打印错误 + USAGE。
 *
 * 设计意图：xflow 脚本主要由 AI agent 调用。agent 最常见的错法不是值不对，而是
 * **参数名拼错**（`--moduleType` vs `--module-type`、漏写连字符、大小写错）。
 * `parseArgs` 默默接受未知 key，导致后续业务校验给出"必填参数缺失"这种误导性
 * 报错，agent 反复重试都对不上。本 helper 的核心价值是：
 *   1. 立刻 exit 1：让 agent 100% 感知到失败
 *   2. 给候选项："未知参数 --moduleType（你是不是想写 --module-type？）"
 *   3. 附带列出全部合法参数，agent 看一眼就能自我纠正
 *
 * 用法：
 *   const args = parseArgs();
 *   handleHelpFlag(args, USAGE);
 *   assertAllowedArgs(args, ["help", "module-type", "phase", "all-phases"], USAGE);
 *   // 此后 args 里只可能有 allowed 列表里的 key
 *
 * 注意：allowed 必须包含 "help"（因为 -h/--help 都会被 parseArgs 解析为 args.help）。
 */
export function assertAllowedArgs(args, allowed, usage) {
  const allowedSet = new Set(allowed);
  for (const k of Object.keys(args)) {
    if (allowedSet.has(k)) continue;
    const hint = suggestSimilar(k, allowed);
    const allowedDisplay = allowed.map((a) => `--${a}`).join(" / ");
    const msg = hint
      ? `未知参数 --${k}（你是不是想写 --${hint}？）\n合法参数：${allowedDisplay}`
      : `未知参数 --${k}\n合法参数：${allowedDisplay}`;
    printUsageAndExit(usage, msg);
  }
}
