#!/usr/bin/env node

import { mkdir, readFile, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(SCRIPT_DIR, "../../..");

export const SIDE_CONFIG = {
  client: {
    output: "task_client_freeze_index.md",
    idPattern: /^C-/,
    shards: [
      {
        freeze: "task_client_freeze_foundation.md",
        exceptions: "runtime/task_client_freeze_foundation_exceptions.md",
      },
      {
        freeze: "task_client_freeze_fgui.md",
        exceptions: "runtime/task_client_freeze_fgui_exceptions.md",
      },
      {
        freeze: "task_client_freeze_peripheral.md",
        exceptions: "runtime/task_client_freeze_peripheral_exceptions.md",
      },
    ],
  },
  server: {
    output: "task_server_freeze_index.md",
    idPattern: /^S-/,
    shards: [
      {
        freeze: "task_server_freeze_contract.md",
        exceptions: "runtime/task_server_freeze_contract_exceptions.md",
      },
      {
        freeze: "task_server_freeze_storage.md",
        exceptions: "runtime/task_server_freeze_storage_exceptions.md",
      },
    ],
  },
};

const REQUIRED_COLUMNS = [
  "freeze_id",
  "facet",
  "source_design",
  "requirement_summary",
  "acceptance",
];

const INDEX_COLUMNS = [
  "freeze_id",
  "facet",
  "source_design",
  "requirement_summary",
  "acceptance",
];

export class FreezeIndexFallbackError extends Error {
  constructor(errors) {
    super(formatFallbackErrors(errors));
    this.name = "FreezeIndexFallbackError";
    this.errors = errors;
  }
}

export function parseMarkdownTable(section, file = "<memory>") {
  const rowTables = parseAnyTables(section)
    .filter((row) => row.freeze_id && row.facet)
    .map(normalizeFreezeRow)
    .filter((row) => !isPlaceholderFreezeRow(row));
  if (rowTables.length > 0) {
    return rowTables;
  }

  const rows = [];
  for (const table of parseFieldValueTables(section)) {
    if (table.freeze_id && table.facet) {
      const normalized = normalizeFreezeRow(table);
      if (!isPlaceholderFreezeRow(normalized)) rows.push(normalized);
    }
  }

  return rows;
}

export function extractFreezeConstraints(content, file = "<memory>") {
  // Main "## Freeze Constraints" section is mandatory
  const mainSection = extractSection(content, "Freeze Constraints");
  if (!mainSection) {
    throw new Error(`${file}: missing "## Freeze Constraints" section`);
  }

  const mainRows = parseMarkdownTable(mainSection, file);
  if (mainRows.length === 0) {
    throw new Error(`${file}: missing Freeze Constraints markdown table`);
  }

  // IDs already captured from main section — used to deduplicate supplement sections
  const mainIds = new Set(mainRows.map((r) => r.freeze_id).filter((id) => id && !isMissing(id)));

  // Supplement from Cross-Cutting / Unclassified and Blocked sections.
  // Items already present in the main table are silently skipped: files often
  // document cross-cutting items redundantly in both the main freeze table and
  // the summary section, which should not cause duplicate-id errors.
  const supplementRows = [];
  for (const heading of ["Cross-Cutting / Unclassified", "Blocked"]) {
    const section = extractSection(content, heading);
    if (!section) continue;
    for (const row of parseMarkdownTable(section, file)) {
      if (!mainIds.has(row.freeze_id)) {
        supplementRows.push(row);
      }
    }
  }

  return [...mainRows, ...supplementRows];
}

export function extractExceptionIds(content) {
  const ids = new Set();
  for (const row of parseAnyTables(content)) {
    const id = row.freeze_id || row.id;
    if (id && id !== "—") ids.add(id);
  }
  return ids;
}

export async function buildFreezeIndex({ module = "activity", side = "all", root = PROJECT_ROOT } = {}) {
  const sides = side === "all" ? ["client", "server"] : [side];
  const allResults = [];
  const allErrors = [];

  for (const currentSide of sides) {
    const config = SIDE_CONFIG[currentSide];
    if (!config) throw new Error(`Unknown side: ${currentSide}`);

    const changeDir = join(root, "xspec", "changes", module);
    const result = await buildSideIndex({ module, side: currentSide, config, changeDir });
    allResults.push(result);
    allErrors.push(...result.errors);
  }

  if (allErrors.length > 0) {
    throw new FreezeIndexFallbackError(allErrors);
  }

  for (const result of allResults) {
    await mkdir(dirname(result.outputPath), { recursive: true });
    await writeFile(result.outputPath, result.content, "utf-8");
  }

  return allResults;
}

async function buildSideIndex({ module, side, config, changeDir }) {
  const errors = [];
  const items = [];
  const seen = new Map();

  for (const shard of config.shards) {
    const freezePath = join(changeDir, shard.freeze);
    const exceptionsPath = join(changeDir, shard.exceptions);

    if (!existsSync(freezePath)) {
      errors.push(errorItem({ side, file: freezePath, reason: "missing freeze shard" }));
      continue;
    }
    if (!existsSync(exceptionsPath)) {
      errors.push(errorItem({ side, file: exceptionsPath, reason: "missing exceptions file" }));
      continue;
    }

    let freezeRows = [];
    let exceptionIds = new Set();

    try {
      freezeRows = extractFreezeConstraints(await readFile(freezePath, "utf-8"), freezePath);
    } catch (error) {
      errors.push(errorItem({ side, file: freezePath, reason: error.message }));
      continue;
    }

    try {
      exceptionIds = extractExceptionIds(await readFile(exceptionsPath, "utf-8"));
    } catch (error) {
      errors.push(errorItem({ side, file: exceptionsPath, reason: error.message }));
      continue;
    }

    for (const row of freezeRows) {
      const freezeId = row.freeze_id;
      validateFreezeRow({ row, side, file: freezePath, idPattern: config.idPattern, errors });

      if (!freezeId || freezeId === "—") continue;

      if (seen.has(freezeId)) {
        errors.push(
          errorItem({
            side,
            file: freezePath,
            freeze_id: freezeId,
            reason: `duplicate freeze_id; first seen in ${seen.get(freezeId)}`,
          }),
        );
      } else {
        seen.set(freezeId, freezePath);
      }

      if (requiresException(row) && !exceptionIds.has(freezeId)) {
        errors.push(
          errorItem({
            side,
            file: exceptionsPath,
            freeze_id: freezeId,
            reason: "required exception item is missing",
          }),
        );
      }

      items.push({
        freeze_id: freezeId,
        facet: row.facet,
        source_design: row.source_design,
        requirement_summary: row.requirement_summary,
        acceptance: row.acceptance,
      });
    }
  }

  return {
    module,
    side,
    errors,
    items,
    outputPath: join(changeDir, config.output),
    content: renderIndex({ side, items }),
  };
}

function validateFreezeRow({ row, side, file, idPattern, errors }) {
  if (!isCandidateFreezeRow(row, idPattern)) return;

  for (const column of REQUIRED_COLUMNS) {
    if (isMissing(row[column])) {
      errors.push(errorItem({ side, file, freeze_id: row.freeze_id, reason: `missing ${column}` }));
    }
  }

  if (!isMissing(row.freeze_id) && !idPattern.test(row.freeze_id)) {
    errors.push(
      errorItem({
        side,
        file,
        freeze_id: row.freeze_id,
        reason: `invalid ${side} freeze_id prefix`,
      }),
    );
  }
}

function normalizeFreezeRow(row) {
  return {
    ...row,
    requirement_summary: row.requirement_summary || row.requirement || row.summary || "",
    blocked_reason: row.blocked_reason || "—",
    cross_cutting: row.cross_cutting || "no",
  };
}

function isCandidateFreezeRow(row, idPattern) {
  const freezeId = String(row.freeze_id ?? "").trim();
  if (isMissing(freezeId)) return false;
  if (!idPattern.test(freezeId)) return true;

  const supportingColumns = [
    row.facet,
    row.source_design,
    row.requirement_summary || row.requirement || row.summary,
    row.acceptance,
    row.blocked_reason,
    row.cross_cutting,
  ];
  return supportingColumns.some((value) => !isMissing(value));
}

function isPlaceholderFreezeRow(row) {
  const freezeId = String(row.freeze_id ?? "").trim();
  const facet = String(row.facet ?? "").trim();
  const hasCoreFields = !isMissing(row.source_design) || !isMissing(row.requirement_summary) || !isMissing(row.acceptance);

  return (freezeId === "—" || freezeId === "-" || facet === "—" || facet === "-") && !hasCoreFields;
}

function parseFieldValueTables(content) {
  const rows = parseAnyTables(content);
  const tables = [];
  let current = null;

  for (const row of rows) {
    if (!Object.hasOwn(row, "字段") || !Object.hasOwn(row, "值")) continue;

    const key = normalizeHeader(row["字段"]);
    const value = row["值"];
    if (key === "freeze_id") {
      if (current?.freeze_id) tables.push(current);
      current = {};
    }
    if (!current) current = {};
    current[key] = value;
  }

  if (current?.freeze_id) tables.push(current);
  return tables;
}

function renderIndex({ side, items }) {
  const title = side === "client" ? "客户端 freeze index" : "服务端 freeze index";
  const lines = [
    `# ${title}`,
    "",
    "| freeze_id | facet | source_design | requirement_summary | acceptance |",
    "|-----------|-------|---------------|---------------------|------------|",
  ];

  for (const item of items) {
    lines.push(`| ${INDEX_COLUMNS.map((column) => escapeCell(item[column])).join(" | ")} |`);
  }

  lines.push("");
  lines.push(`[FREEZE-INDEX-STATS] side=${side}, freeze_total=${items.length}`);
  lines.push("");
  return lines.join("\n");
}

function extractSection(content, heading) {
  const lines = content.split(/\r?\n/);
  // Support both "## Heading" and "## N. Heading" (numbered section prefix)
  const pattern = new RegExp(`^##\\s+(?:\\d+\\.\\s+)?${escapeRegExp(heading)}\\s*$`, "i");
  const start = lines.findIndex((line) => pattern.test(line.trim()));
  if (start === -1) return null;

  const endOffset = lines.slice(start + 1).findIndex((line) => /^##\s+/.test(line.trim()));
  const end = endOffset === -1 ? lines.length : start + 1 + endOffset;
  return lines.slice(start + 1, end).join("\n");
}

function parseAnyTables(content) {
  const lines = content.split(/\r?\n/);
  const rows = [];

  for (let i = 0; i < lines.length; i++) {
    const headers = splitMarkdownRow(lines[i]).map(normalizeHeader);
    if (!headers.includes("freeze_id") && !headers.includes("id") && !(headers.includes("字段") && headers.includes("值"))) continue;

    for (let j = i + 1; j < lines.length; j++) {
      if (!lines[j].trim()) break;
      if (!lines[j].includes("|")) break;
      let cells = splitMarkdownRow(lines[j]);
      if (isSeparatorRow(cells)) continue;
      if (cells.length < headers.length) continue;
      cells = alignCellsToHeaders(cells, headers);

      const row = {};
      for (let k = 0; k < headers.length; k++) row[headers[k]] = cleanCell(cells[k] ?? "");
      rows.push(row);
    }
  }

  return rows;
}

function alignCellsToHeaders(cells, headers) {
  if (cells.length <= headers.length) return cells;
  const mergeIndex = headers.findIndex((header) => header === "requirement_summary" || header === "requirement");
  if (mergeIndex === -1) return cells.slice(0, headers.length);

  const extra = cells.length - headers.length;
  return [
    ...cells.slice(0, mergeIndex),
    cells.slice(mergeIndex, mergeIndex + extra + 1).join("|"),
    ...cells.slice(mergeIndex + extra + 1),
  ];
}

function splitMarkdownRow(line) {
  const trimmed = line.trim();
  if (!trimmed.includes("|")) return [];
  // Replace escaped pipes \| with a placeholder so they are not treated as
  // column delimiters. The placeholder is restored as a literal | after splitting.
  const ESCAPED_PIPE = "\x00";
  const escaped = trimmed.replace(/\\\|/g, ESCAPED_PIPE);
  const withoutOuter = escaped.replace(/^\|/, "").replace(/\|$/, "");
  return withoutOuter
    .split("|")
    .map((cell) => cell.trim().replace(new RegExp(ESCAPED_PIPE, "g"), "|"));
}

function normalizeHeader(header) {
  return cleanCell(header)
    .replace(/`/g, "")
    .trim()
    .toLowerCase()
    .replace(/\s+/g, "_");
}

function cleanCell(cell) {
  return String(cell ?? "").replace(/<br\s*\/?>/gi, " / ").trim();
}

function isSeparatorRow(cells) {
  return cells.length > 0 && cells.every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

function isMissing(value) {
  const normalized = String(value ?? "").trim();
  return normalized === "" || normalized === "—" || normalized === "-" || normalized.toUpperCase() === "TBD" || normalized === "待确认";
}

function requiresException(row) {
  return (
    String(row.cross_cutting ?? "").trim().toLowerCase() === "yes" ||
    String(row.facet ?? "").trim().toLowerCase() === "unclassified" ||
    !["", "—", "-", "none", "no"].includes(String(row.blocked_reason ?? "").trim().toLowerCase())
  );
}

function escapeCell(value) {
  return String(value ?? "")
    .replace(/\|/g, "\\|")
    .replace(/\r?\n/g, " ")
    .trim();
}

function errorItem({ side, file, freeze_id = "—", reason }) {
  return { side, file, freeze_id, reason };
}

function formatFallbackErrors(errors) {
  const lines = ["FREEZE_INDEX_FALLBACK"];
  for (const error of errors) {
    lines.push(`side=${error.side}`);
    lines.push(`file=${error.file}`);
    lines.push(`freeze_id=${error.freeze_id}`);
    lines.push(`reason=${error.reason}`);
    lines.push("");
  }
  return lines.join("\n").trimEnd();
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function parseArgs(argv) {
  const options = { module: "activity", side: "all", root: PROJECT_ROOT };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--module") options.module = argv[++i];
    else if (arg === "--side") options.side = argv[++i];
    else if (arg === "--root") options.root = resolve(argv[++i]);
    else if (arg === "--help" || arg === "-h") options.help = true;
    else throw new Error(`Unknown argument: ${arg}`);
  }
  return options;
}

function printHelp() {
  console.log(`Usage: node .xflow/scripts/xuanwu/xf-freeze-index.mjs [--module activity] [--side client|server|all] [--root <repo-root>]`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const options = parseArgs(process.argv.slice(2));
    if (options.help) {
      printHelp();
      process.exit(0);
    }
    const results = await buildFreezeIndex(options);
    for (const result of results) {
      console.log(`FREEZE_INDEX_OK side=${result.side} output=${result.outputPath} freeze_total=${result.items.length}`);
    }
  } catch (error) {
    console.error(error.message);
    // exit 2 = FREEZE_INDEX_FALLBACK (recoverable, AI subagent should take over)
    // exit 1 = unexpected crash / argument error
    process.exit(error.name === "FreezeIndexFallbackError" ? 2 : 1);
  }
}
