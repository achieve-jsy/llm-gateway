// src/v2/index.ts
import { appendFileSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// src/utils/openai-compatible-api.ts
var OPENAI_COMPATIBLE_MODELS_ENDPOINT = "/v1/models";
function normalizeBaseURL(baseURL) {
  let normalized = baseURL.replace(/\/+$/, "");
  if (normalized.endsWith("/v1")) {
    normalized = normalized.slice(0, -3);
  }
  return normalized;
}
function buildAPIURL(baseURL, endpoint = OPENAI_COMPATIBLE_MODELS_ENDPOINT) {
  const normalized = normalizeBaseURL(baseURL);
  return `${normalized}${endpoint}`;
}
async function discoverModelsFromProvider(baseURL, apiKey, endpoint = OPENAI_COMPATIBLE_MODELS_ENDPOINT) {
  try {
    const url = buildAPIURL(baseURL, endpoint);
    const headers = {
      "Content-Type": "application/json"
    };
    if (apiKey) {
      headers["Authorization"] = `Bearer ${apiKey}`;
    }
    const response = await fetch(url, {
      method: "GET",
      headers,
      signal: AbortSignal.timeout(15000)
    });
    if (!response.ok) {
      return { ok: false, models: [] };
    }
    const data = await response.json();
    return {
      ok: true,
      models: data.data ?? []
    };
  } catch {
    return { ok: false, models: [] };
  }
}

// src/v2/index.ts
var SIDECAR_FILE = "opencode-models-discovery.json";
var MODEL_INFO_ENDPOINT = "/v1/model/info";
var FETCH_TIMEOUT_MS = 15000;
var DEFAULT_MODALITIES = ["text"];
function sidecarPath() {
  try {
    return join(dirname(fileURLToPath(import.meta.url)), SIDECAR_FILE);
  } catch {
    return SIDECAR_FILE;
  }
}
function readDiscoveryConfig() {
  try {
    const raw = JSON.parse(readFileSync(sidecarPath(), "utf8"));
    if (!raw || typeof raw.providerID !== "string" || typeof raw.baseURL !== "string")
      return null;
    return {
      providerID: raw.providerID,
      providerName: typeof raw.providerName === "string" ? raw.providerName : raw.providerID,
      baseURL: raw.baseURL,
      apiKey: typeof raw.apiKey === "string" ? raw.apiKey : undefined
    };
  } catch {
    return null;
  }
}
function debug(message) {
  if (!process.env.OPENCODE_DISCOVERY_DEBUG)
    return;
  try {
    appendFileSync("/tmp/v2plugin.log", new Date().toISOString() + " " + message + `
`);
  } catch {}
}
function usable(value) {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}
function reasoningEfforts(mi) {
  if (mi.supports_reasoning !== true)
    return null;
  const params = Array.isArray(mi.supported_openai_params) ? mi.supported_openai_params : [];
  if (!params.includes("reasoning_effort"))
    return null;
  const efforts = [];
  if (mi.supports_none_reasoning_effort === true)
    efforts.push("none");
  if (mi.supports_minimal_reasoning_effort === true)
    efforts.push("minimal");
  if (mi.supports_low_reasoning_effort !== false)
    efforts.push("low");
  efforts.push("medium", "high");
  if (mi.supports_xhigh_reasoning_effort === true)
    efforts.push("xhigh");
  if (mi.supports_max_reasoning_effort === true)
    efforts.push("max");
  return efforts;
}
function buildV2Model(modelId, mi) {
  const mode = mi.mode;
  if (typeof mode === "string" && mode.length > 0 && mode !== "chat")
    return null;
  const caps = {};
  if (mi.supports_function_calling === true || mi.supports_tool_choice === true)
    caps.tools = true;
  const declared = mi.input_modalities ?? mi.supported_modalities;
  if (Array.isArray(declared) && declared.length > 0) {
    const inputs = declared.map((x) => String(x).toLowerCase());
    caps.input = inputs.includes("text") ? inputs : ["text", ...inputs];
  } else if (mi.supports_vision === true) {
    caps.input = ["text", "image"];
  } else if (mi.supports_vision === false) {
    caps.input = ["text"];
  }
  const declaredOutput = mi.output_modalities;
  caps.output = Array.isArray(declaredOutput) && declaredOutput.length > 0 ? declaredOutput.map((x) => String(x).toLowerCase()) : DEFAULT_MODALITIES;
  const efforts = reasoningEfforts(mi);
  if (efforts)
    caps.reasoning = { supported: true, effort: efforts };
  const model = { id: modelId, name: modelId, capabilities: caps };
  const ctxLimit = usable(mi.max_input_tokens) ? mi.max_input_tokens : mi.max_tokens;
  const outLimit = usable(mi.max_output_tokens) ? mi.max_output_tokens : mi.max_tokens;
  if (usable(ctxLimit) && usable(outLimit)) {
    model.limit = { context: Math.trunc(ctxLimit), output: Math.trunc(outLimit) };
  }
  return model;
}
async function fetchModelInfo(config) {
  const map = new Map;
  try {
    const headers = { "Content-Type": "application/json" };
    if (config.apiKey)
      headers.Authorization = `Bearer ${config.apiKey}`;
    const response = await fetch(buildAPIURL(config.baseURL, MODEL_INFO_ENDPOINT), {
      method: "GET",
      headers,
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS)
    });
    if (!response.ok)
      return map;
    const payload = await response.json();
    for (const entry of Array.isArray(payload?.data) ? payload.data : []) {
      const mi = entry?.model_info ?? {};
      const key = entry?.model_name || mi.key;
      if (!key)
        continue;
      map.set(key, mi);
      map.set(key.toLowerCase(), mi);
    }
  } catch {}
  return map;
}
async function discoverModels(config) {
  const discovered = await discoverModelsFromProvider(config.baseURL, config.apiKey);
  if (!discovered.ok)
    return [];
  const info = await fetchModelInfo(config);
  const models = [];
  for (const entry of discovered.models) {
    const id = entry?.id;
    if (!id)
      continue;
    const mi = info.get(id) ?? info.get(id.toLowerCase()) ?? {};
    const model = buildV2Model(id, mi);
    if (model)
      models.push(model);
  }
  return models;
}
var PLUGIN_ID = "llmgateway.models-discovery";
var v2_default = {
  id: PLUGIN_ID,
  async setup(ctx) {
    const config = readDiscoveryConfig();
    if (!config)
      return;
    const models = await discoverModels(config);
    debug("discovered " + models.length + ": " + models.map((m) => m.id).join(","));
    if (models.length === 0)
      return;
    ctx.provider.transform((registry) => {
      if (!registry.get(config.providerID)) {
        registry.add({
          info: {
            id: config.providerID,
            name: config.providerName ?? config.providerID,
            package: "@opencode/ai/providers/openai-compatible",
            activation: "enabled",
            headers: {},
            settings: { baseURL: config.baseURL, apiKey: config.apiKey ?? "" }
          },
          models: []
        });
        debug("added provider " + config.providerID);
      }
      for (const model of models) {
        registry.models.update(config.providerID, model.id, (target) => {
          const current = target.capabilities ?? {};
          const incoming = model.capabilities ?? {};
          target.modelID = model.id;
          target.name = model.name;
          target.capabilities = {
            ...current,
            ...incoming,
            tools: incoming.tools ?? current.tools ?? false,
            input: incoming.input ?? current.input ?? DEFAULT_MODALITIES,
            output: incoming.output ?? current.output ?? DEFAULT_MODALITIES
          };
          if (model.limit)
            target.limit = { ...target.limit ?? {}, ...model.limit };
        });
      }
    });
  }
};
export {
  readDiscoveryConfig,
  discoverModels,
  v2_default as default,
  buildV2Model
};
