#!/usr/bin/env node

import { mkdir, readFile, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(SCRIPT_DIR, "../../..");
const DEFAULT_PROFILE = "xuanwu-activity";

export const COVERAGE_STATUS = {
  OK: "ok",
  VALID_WITH_UNRESOLVED: "valid_with_unresolved",
  INVALID_REQUIRES_FALLBACK: "invalid_requires_fallback",
};

export const SIDE_CONFIG = {
  client: {
    title: "客户端 freeze item coverage map",
    docId: "client-coverage-map",
    output: "task_client_coverage_map.md",
    index: "task_client_freeze_index.md",
    diagnostics: "runtime/task_client_coverage_reconcile_diagnostics.json",
    legacyExceptions: [
      "task_client_freeze_foundation_exceptions.md",
      "task_client_freeze_fgui_exceptions.md",
      "task_client_freeze_peripheral_exceptions.md",
    ],
    legacyLedgers: [
      "task_client_part_data_fgui_coverage.md",
      "task_client_part_state_flow_coverage.md",
      "task_client_part_entry_coverage.md",
      "task_client_part_peripheral_coverage.md",
    ],
  },
  server: {
    title: "服务端 freeze item coverage map",
    docId: "server-coverage-map",
    output: "task_server_coverage_map.md",
    index: "task_server_freeze_index.md",
    diagnostics: "runtime/task_server_coverage_reconcile_diagnostics.json",
    legacyExceptions: [
      "task_server_freeze_contract_exceptions.md",
      "task_server_freeze_storage_exceptions.md",
    ],
    legacyLedgers: [
      "task_server_part_core_coverage.md",
      "task_server_part_service_coverage.md",
    ],
  },
};

const ALLOWED_LEDGER_STATUSES = new Set(["covered", "blocked", "intentionally_merged", "unresolved", "missing"]);

const STATUS_ORDER = {
  missing: 0,
  unresolved: 1,
  intentionally_merged: 2,
  covered: 3,
  blocked: 4,
};

export async function buildCoverageMap({
  module = "activity",
  profile,
  side = "client",
  root = PROJECT_ROOT,
  bundlePath,
} = {}) {
  const config = SIDE_CONFIG[side];
  if (!config) throw new Error(`Unknown side: ${side}`);

  const changeDir = join(root, "xspec", "changes", module);
  const resolvedProfile = await resolveCoverageProfile({ root, module, profile });
  const inputs = await resolveCoverageInputs({ root, module, profile: resolvedProfile, side, bundlePath });
  const diagnostics = createDiagnostics({ side, module, profile: resolvedProfile, config, inputSource: inputs.source });

  diagnostics.expected_inputs = {
    freeze_index: shorten(root, inputs.indexPath),
    exceptions: inputs.exceptionPaths.map((value) => shorten(root, value)),
    ledgers: inputs.ledgerPaths.map((value) => shorten(root, value)),
  };

  const missingInputs = [
    ...checkRequiredPath(inputs.indexPath, "freeze_index"),
    ...inputs.exceptionPaths.flatMap((value) => checkRequiredPath(value, "exception")),
    ...inputs.ledgerPaths.flatMap((value) => checkRequiredPath(value, "ledger")),
  ];

  if (missingInputs.length > 0) {
    diagnostics.reason_codes.push("missing_required_inputs");
    diagnostics.errors.push(...missingInputs);
    return await finalizeCoverageResult({
      root,
      config,
      diagnostics,
      outputPath: join(changeDir, config.output),
      status: COVERAGE_STATUS.INVALID_REQUIRES_FALLBACK,
      rows: [],
    });
  }

  diagnostics.files_read.freeze_index = 1;
  diagnostics.files_read.exceptions = inputs.exceptionPaths.length;
  diagnostics.files_read.ledgers = inputs.ledgerPaths.length;

  const indexRows = parseFreezeIndex(await readFile(inputs.indexPath, "utf-8"), inputs.indexPath);
  if (indexRows.length === 0) {
    diagnostics.reason_codes.push("empty_freeze_index");
    diagnostics.errors.push({
      code: "empty_freeze_index",
      kind: "freeze_index",
      path: shorten(root, inputs.indexPath),
      message: "freeze index produced 0 usable rows",
    });
    return await finalizeCoverageResult({
      root,
      config,
      diagnostics,
      outputPath: join(changeDir, config.output),
      status: COVERAGE_STATUS.INVALID_REQUIRES_FALLBACK,
      rows: [],
    });
  }

  const exceptionIds = await readExceptionIds(inputs.exceptionPaths);
  const ledgerRows = await readLedgerRows(inputs.ledgerPaths);
  const reconciled = reconcile(indexRows, ledgerRows.rows, exceptionIds, {
    diagnostics,
    root,
  });
  const stats = countCoverageRows(reconciled.rows);
  diagnostics.freeze_total = indexRows.length;
  diagnostics.stats = stats;
  diagnostics.unresolved_without_ledger = reconciled.unresolvedWithoutLedger;

  validateCoverageResult({
    diagnostics,
    freezeTotal: indexRows.length,
    ledgerErrors: ledgerRows.errors,
    rows: reconciled.rows,
    stats,
    unresolvedWithoutLedger: reconciled.unresolvedWithoutLedger,
    root,
  });

  const status =
    diagnostics.errors.length > 0
      ? COVERAGE_STATUS.INVALID_REQUIRES_FALLBACK
      : stats.unresolved > 0
        ? COVERAGE_STATUS.VALID_WITH_UNRESOLVED
        : COVERAGE_STATUS.OK;
  diagnostics.status = status;

  const content = renderCoverageMap({
    side,
    title: config.title,
    rows: reconciled.rows,
    stats,
    diagnostics,
  });

  return await finalizeCoverageResult({
    root,
    config,
    diagnostics,
    outputPath: join(changeDir, config.output),
    status,
    rows: reconciled.rows,
    content,
  });
}

export function reconcile(indexRows, ledgerRows, exceptionIds = new Set(), options = {}) {
  const coverageById = new Map();
  const root = options.root || PROJECT_ROOT;
  const diagnostics = options.diagnostics;

  for (const row of ledgerRows) {
    if (!row.freeze_id || row.freeze_id === "—") continue;
    const normalized = normalizeLedgerRow(row);
    if (!normalized) continue;

    if (diagnostics && !ALLOWED_LEDGER_STATUSES.has(normalized.status)) {
      diagnostics.reason_codes.push("invalid_ledger_status");
      diagnostics.errors.push({
        code: "invalid_ledger_status",
        kind: "ledger",
        path: shorten(root, row._path || row._file || "<memory>"),
        freeze_id: normalized.freeze_id,
        message: `unsupported ledger status: ${normalized.status}`,
      });
      continue;
    }

    const existing = coverageById.get(normalized.freeze_id);
    if (!existing) {
      coverageById.set(normalized.freeze_id, normalized);
      continue;
    }

    coverageById.set(normalized.freeze_id, mergeCoverage(existing, normalized));
  }

  let unresolvedWithoutLedger = 0;
  const rows = indexRows.map((item) => {
    const coverage = coverageById.get(item.freeze_id);
    if (!coverage) {
      const note = exceptionIds.has(item.freeze_id) ? "exception item requires gap-scan" : "no matching coverage ledger row";
      if (!exceptionIds.has(item.freeze_id)) unresolvedWithoutLedger += 1;
      return {
        ...item,
        covered_by_task_ids: "—",
        coverage_status: "unresolved",
        notes: note,
      };
    }

    return {
      ...item,
      covered_by_task_ids: coverage.taskIds.size > 0 ? [...coverage.taskIds].join(",") : "—",
      coverage_status: coverage.status,
      notes: coverage.notes.size > 0 ? [...coverage.notes].join("；") : "—",
    };
  });

  return { rows, unresolvedWithoutLedger };
}

async function resolveCoverageInputs({ root, module, profile, side, bundlePath }) {
  const config = SIDE_CONFIG[side];
  const changeDir = join(root, "xspec", "changes", module);
  const declared = await loadInputsFromBundle({ root, module, profile, side, bundlePath });
  if (declared) {
    return {
      source: "bundle",
      indexPath: resolve(root, declared.index),
      exceptionPaths: declared.exceptions.map((value) => resolve(root, value)),
      ledgerPaths: declared.ledgers.map((value) => resolve(root, value)),
    };
  }

  return {
    source: "legacy-config",
    indexPath: join(changeDir, config.index),
    exceptionPaths: config.legacyExceptions.map((value) => findDeclaredOrLegacy(changeDir, value)),
    ledgerPaths: config.legacyLedgers.map((value) => findDeclaredOrLegacy(changeDir, value)),
  };
}

async function loadInputsFromBundle({ root, module, profile, side, bundlePath }) {
  const config = SIDE_CONFIG[side];
  const bundleFile = bundlePath
    ? resolve(root, bundlePath)
    : getDefaultBundlePaths({ profile, module }).map((value) => resolve(root, value)).find((value) => existsSync(value));
  if (!bundleFile || !existsSync(bundleFile)) return null;

  const content = await readFile(bundleFile, "utf-8");
  const inputs = extractDocInputFiles(content, config.docId);
  if (inputs.length === 0) return null;

  const normalized = inputs.map((value) => normalizeBundlePath(value, module));
  const index = normalized.find((value) => value.endsWith(`${sep}${config.index}`) || value.endsWith(`/${config.index}`) || value === config.index);
  const exceptions = normalized.filter((value) => /_exceptions\.md$/.test(value));
  const ledgers = normalized.filter((value) => /_coverage\.md$/.test(value) && !/_coverage_map\.md$/.test(value));

  if (!index || exceptions.length === 0 || ledgers.length === 0) return null;

  return { index, exceptions, ledgers, bundleFile };
}

function getDefaultBundlePaths({ profile, module }) {
  const candidates = [];
  if (profile) candidates.push(`xwRef/xuanwu/${profile}/task/bundle.yaml`);
  if (module && module !== profile) candidates.push(`xwRef/xuanwu/${module}/task/bundle.yaml`);
  candidates.push(`xwRef/xuanwu/${DEFAULT_PROFILE}/task/bundle.yaml`);
  return [...new Set(candidates)];
}

async function resolveCoverageProfile({ root, module, profile }) {
  if (profile) return profile;

  const change = await readChangeYamlForRoot(root);
  const moduleMeta = change?.affected_modules?.find((item) => item.name === module);
  return moduleMeta?.module_type || change?.project_profile || DEFAULT_PROFILE;
}

async function readChangeYamlForRoot(root) {
  const filePath = join(root, "xspec", "changes", "change.yaml");
  if (!existsSync(filePath)) return null;

  const content = await readFile(filePath, "utf-8");
  const result = { affected_modules: [] };
  let currentModule = null;

  for (const raw of content.split(/\r?\n/)) {
    const line = raw.trimEnd();
    if (!line || line.trimStart().startsWith("#")) continue;

    if (/^\s+-\s+name:/.test(line)) {
      currentModule = { name: unquote(line.match(/name:\s*([^#]+)/)?.[1]) };
      result.affected_modules.push(currentModule);
      continue;
    }
    if (currentModule && /^\s+module_type:/.test(line)) {
      currentModule.module_type = unquote(line.match(/module_type:\s*([^#]+)/)?.[1]);
      continue;
    }
    if (!line.startsWith(" ") && !line.startsWith("\t")) {
      currentModule = null;
      const match = line.match(/^project_profile:\s*([^#]+)/);
      if (match) result.project_profile = unquote(match[1]);
    }
  }

  return result;
}

function unquote(value) {
  return String(value ?? "").trim().replace(/^["']|["']$/g, "");
}

function extractDocInputFiles(content, docId) {
  const lines = content.split(/\r?\n/);
  const start = lines.findIndex((line) => line.trim() === `- doc_id: ${docId}`);
  if (start === -1) return [];

  let inInputFiles = false;
  const inputs = [];
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    if (/^- doc_id:\s+/.test(trimmed)) break;
    if (/^input_files:\s*$/.test(trimmed)) {
      inInputFiles = true;
      continue;
    }
    if (!inInputFiles) continue;
    if (/^[a-z_]+:\s*/.test(trimmed) || /^depends_on:\s*$/.test(trimmed)) break;
    const match = trimmed.match(/^-\s+(.+)$/);
    if (match) inputs.push(match[1].trim());
  }

  return inputs;
}

function normalizeBundlePath(value, module) {
  return value.replace(/\{module\}/g, module);
}

function findDeclaredOrLegacy(changeDir, baseName) {
  const runtimePath = join(changeDir, "runtime", baseName);
  if (existsSync(runtimePath)) return runtimePath;
  return join(changeDir, baseName);
}

function checkRequiredPath(path, kind) {
  if (existsSync(path)) return [];
  return [
    {
      code: "missing_required_input",
      kind,
      path,
      message: `missing required ${kind}: ${path}`,
    },
  ];
}

function parseFreezeIndex(content) {
  return parseAnyTables(content)
    .filter((row) => row.freeze_id && row.freeze_id !== "—" && row.requirement_summary && row.acceptance)
    .map((row) => ({
      freeze_id: row.freeze_id,
      facet: row.facet || "—",
      source_design: row.source_design || "—",
    }));
}

async function readExceptionIds(paths) {
  const ids = new Set();
  for (const path of paths) {
    for (const row of parseAnyTables(await readFile(path, "utf-8"))) {
      if (row.freeze_id && row.freeze_id !== "—") ids.add(row.freeze_id);
    }
  }
  return ids;
}

async function readLedgerRows(paths) {
  const rows = [];
  const errors = [];

  for (const path of paths) {
    const content = await readFile(path, "utf-8");
    const parsed = parseAnyTables(content).map((row) => ({ ...row, _path: path }));
    if (parsed.length === 0 && !hasFreezeTable(content)) {
      errors.push({
        code: "ledger_schema_drift",
        kind: "ledger",
        path,
        message: "ledger file does not contain a parseable freeze_id table",
      });
      continue;
    }
    rows.push(...parsed);
  }

  return { rows, errors };
}

function normalizeLedgerRow(row) {
  const taskIds = splitTaskIds(row.task_id || row.task_ids || row.covered_by_task_ids || "");
  const rawStatus = row.coverage_status || row.status || row.result || "";
  let status = normalizeStatus(rawStatus);

  if (!status && taskIds.size > 0) status = "covered";
  if (!status) return null;

  const notes = new Set();
  for (const key of ["notes", "blocked_reason_/_intentionally_merged_reason", "blocked_reason", "intentionally_merged_reason", "handoff_reason", "实现说明"]) {
    if (row[key] && row[key] !== "—") notes.add(row[key]);
  }

  return {
    freeze_id: row.freeze_id,
    status,
    taskIds,
    notes,
  };
}

function mergeCoverage(left, right) {
  const status = STATUS_ORDER[right.status] > STATUS_ORDER[left.status] ? right.status : left.status;
  return {
    freeze_id: left.freeze_id,
    status,
    taskIds: new Set([...left.taskIds, ...right.taskIds]),
    notes: new Set([...left.notes, ...right.notes]),
  };
}

function normalizeStatus(value) {
  const normalized = String(value ?? "").trim().toLowerCase();
  if (!normalized || normalized === "—") return null;
  if (normalized.includes("blocked")) return "blocked";
  if (normalized.includes("intentionally_merged")) return "intentionally_merged";
  if (normalized.includes("covered")) return "covered";
  if (normalized.includes("unresolved")) return "unresolved";
  if (normalized.includes("missing")) return "missing";
  return normalized;
}

function splitTaskIds(value) {
  const ids = new Set();
  for (const part of String(value ?? "").split(/[,\s，、;]+/)) {
    const id = part.trim();
    if (!id || id === "—" || id === "-") continue;
    ids.add(id);
  }
  return ids;
}

function validateCoverageResult({ diagnostics, freezeTotal, ledgerErrors, rows, stats, unresolvedWithoutLedger, root }) {
  if (ledgerErrors.length > 0) {
    diagnostics.reason_codes.push("schema_drift");
    diagnostics.errors.push(
      ...ledgerErrors.map((error) => ({
        ...error,
        path: shorten(root, error.path),
      })),
    );
  }

  const total = stats.covered + stats.blocked + stats.intentionally_merged + stats.unresolved + stats.missing;
  if (freezeTotal !== total) {
    diagnostics.reason_codes.push("stats_not_conserved");
    diagnostics.errors.push({
      code: "stats_not_conserved",
      kind: "coverage_map",
      message: `freeze_total=${freezeTotal} but coverage_total=${total}`,
    });
  }

  if (rows.length !== freezeTotal) {
    diagnostics.reason_codes.push("coverage_size_mismatch");
    diagnostics.errors.push({
      code: "coverage_size_mismatch",
      kind: "coverage_map",
      message: `coverage rows=${rows.length} but freeze_total=${freezeTotal}`,
    });
  }

  if (stats.missing > 0) {
    diagnostics.reason_codes.push("missing_status_present");
    diagnostics.errors.push({
      code: "missing_status_present",
      kind: "coverage_map",
      message: `coverage map contains missing=${stats.missing} before gap-scan`,
    });
  }

  if (freezeTotal > 0 && unresolvedWithoutLedger / freezeTotal >= 0.8) {
    diagnostics.reason_codes.push("mass_unresolved_from_missing_ledger");
    diagnostics.errors.push({
      code: "mass_unresolved_from_missing_ledger",
      kind: "coverage_map",
      message: `unresolved_without_ledger=${unresolvedWithoutLedger}/${freezeTotal}`,
    });
  }
}

function countCoverageRows(rows) {
  const counts = {
    total: rows.length,
    covered: 0,
    blocked: 0,
    intentionally_merged: 0,
    unresolved: 0,
    missing: 0,
  };

  for (const row of rows) counts[row.coverage_status] += 1;
  return counts;
}

function renderCoverageMap({ side, title, rows, stats, diagnostics }) {
  const lines = [
    `# ${title}`,
    "",
    `> coverage_reconcile_status: ${diagnostics.status}`,
    `> coverage_reconcile_source: ${diagnostics.input_source}`,
    `> coverage_reconcile_reason_codes: ${diagnostics.reason_codes.length > 0 ? diagnostics.reason_codes.join(", ") : "none"}`,
    "",
    "| freeze_id | facet | source_design | covered_by_task_ids | coverage_status | notes |",
    "|-----------|-------|---------------|---------------------|-----------------|-------|",
  ];

  for (const row of rows) {
    lines.push(
      `| ${[
        row.freeze_id,
        row.facet,
        row.source_design,
        row.covered_by_task_ids,
        row.coverage_status,
        row.notes,
      ].map(escapeCell).join(" | ")} |`,
    );
  }

  lines.push("");
  lines.push(
    `[SHARD-STATS] ${side}-coverage-map: total=${stats.total}, covered=${stats.covered}, blocked=${stats.blocked}, intentionally_merged=${stats.intentionally_merged}, unresolved=${stats.unresolved}, missing=${stats.missing}`,
  );
  lines.push("");
  return lines.join("\n");
}

function createDiagnostics({ side, module, profile, config, inputSource }) {
  return {
    side,
    module,
    profile,
    status: COVERAGE_STATUS.INVALID_REQUIRES_FALLBACK,
    input_source: inputSource,
    freeze_total: 0,
    files_read: {
      freeze_index: 0,
      exceptions: 0,
      ledgers: 0,
    },
    expected_inputs: {
      freeze_index: null,
      exceptions: [],
      ledgers: [],
    },
    stats: {
      total: 0,
      covered: 0,
      blocked: 0,
      intentionally_merged: 0,
      unresolved: 0,
      missing: 0,
    },
    unresolved_without_ledger: 0,
    reason_codes: [],
    errors: [],
    diagnostics_output: config.diagnostics,
  };
}

async function finalizeCoverageResult({ root, config, diagnostics, outputPath, status, rows, content }) {
  diagnostics.status = status;
  diagnostics.reason_codes = [...new Set(diagnostics.reason_codes)];
  diagnostics.error_count = diagnostics.errors.length;

  const diagnosticsPath = resolve(root, "xspec", "changes", diagnostics.module, config.diagnostics);
  await mkdir(dirname(diagnosticsPath), { recursive: true });
  await writeFile(diagnosticsPath, `${JSON.stringify(diagnostics, null, 2)}\n`, "utf-8");

  if (content) {
    await mkdir(dirname(outputPath), { recursive: true });
    await writeFile(outputPath, content, "utf-8");
  }

  return {
    side: diagnostics.side,
    status,
    outputPath,
    diagnosticsPath,
    total: diagnostics.freeze_total,
    stats: diagnostics.stats,
    rows,
    content,
    diagnostics,
  };
}

function parseAnyTables(content) {
  const lines = content.split(/\r?\n/);
  const rows = [];

  for (let i = 0; i < lines.length; i++) {
    const headers = splitMarkdownRow(lines[i]).map(normalizeHeader);
    if (!headers.includes("freeze_id")) continue;

    for (let j = i + 1; j < lines.length; j++) {
      if (!lines[j].trim()) break;
      if (!lines[j].includes("|")) break;
      const cells = splitMarkdownRow(lines[j]);
      if (isSeparatorRow(cells)) continue;
      if (cells.length < headers.length) continue;

      const row = {};
      for (let k = 0; k < headers.length; k++) row[headers[k]] = cleanCell(cells[k] ?? "");
      rows.push(row);
    }
  }

  return rows;
}

function hasFreezeTable(content) {
  return content.split(/\r?\n/).some((line) => {
    const headers = splitMarkdownRow(line).map(normalizeHeader);
    return headers.includes("freeze_id");
  });
}

function splitMarkdownRow(line) {
  const trimmed = line.trim();
  if (!trimmed.includes("|")) return [];
  return trimmed.replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim());
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

function escapeCell(value) {
  return String(value ?? "")
    .replace(/\|/g, "\\|")
    .replace(/\r?\n/g, " ")
    .trim();
}

function shorten(root, value) {
  if (!value) return value;
  const relativePath = relative(root, value);
  return relativePath && !relativePath.startsWith("..") ? relativePath : value;
}

function parseArgs(argv) {
  const options = { module: "activity", side: "client", root: PROJECT_ROOT };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--module") options.module = argv[++i];
    else if (arg === "--profile") options.profile = argv[++i];
    else if (arg === "--side") options.side = argv[++i];
    else if (arg === "--root") options.root = resolve(argv[++i]);
    else if (arg === "--bundle") options.bundlePath = argv[++i];
    else if (arg === "--help" || arg === "-h") options.help = true;
    else throw new Error(`Unknown argument: ${arg}`);
  }
  return options;
}

function printHelp() {
  console.log("Usage: node .xflow/scripts/xuanwu/xf-coverage-map.mjs [--module activity] [--profile xuanwu-activity] [--side client|server] [--root <repo-root>] [--bundle <bundle-yaml>]");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const options = parseArgs(process.argv.slice(2));
    if (options.help) {
      printHelp();
      process.exit(0);
    }
    const result = await buildCoverageMap(options);
    console.log(
      `COVERAGE_MAP_RESULT ${JSON.stringify({
        side: result.side,
        status: result.status,
        profile: result.diagnostics.profile,
        output: result.outputPath,
        diagnostics: result.diagnosticsPath,
        freeze_total: result.total,
        files_read: result.diagnostics.files_read,
        stats: result.stats,
        reason_codes: result.diagnostics.reason_codes,
      })}`,
    );
    process.exit(result.status === COVERAGE_STATUS.INVALID_REQUIRES_FALLBACK ? 2 : 0);
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}
