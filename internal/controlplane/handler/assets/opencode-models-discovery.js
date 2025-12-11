// src/ui/toast-notifier.ts
class ToastNotifier {
  client;
  directory;
  constructor(client, directory) {
    this.client = client;
    this.directory = directory;
  }
  async showToast(message, variant, title, duration) {
    try {
      if (this.client?.tui?.showToast) {
        await this.client.tui.showToast({
          body: { title, message, variant, duration: duration || 3000 },
          query: { directory: this.directory }
        });
      }
    } catch {}
  }
  async success(message, title, duration) {
    await this.showToast(message, "success", title, duration || 3000);
  }
  async error(message, title, duration) {
    await this.showToast(message, "error", title, duration || 5000);
  }
  async warning(message, title, duration) {
    await this.showToast(message, "warning", title, duration || 4000);
  }
  async info(message, title, duration) {
    await this.showToast(message, "info", title, duration || 3000);
  }
  async progress(message, title, progress) {
    const msg = progress !== undefined ? `${message} (${progress}%)` : message;
    const dur = progress !== undefined ? 0 : 2000;
    await this.showToast(msg, "info", title, dur);
  }
  async detailed(options) {
    await this.showToast(options.message, options.variant || "info", options.title, options.duration);
  }
}

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
      signal: AbortSignal.timeout(3000)
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
async function discoverModelInfoFromProvider(baseURL, apiKey, endpoint = "/v1/model/info") {
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
      signal: AbortSignal.timeout(3000)
    });
    if (!response.ok) {
      return { ok: false, data: undefined };
    }
    const data = await response.json();
    return {
      ok: true,
      data
    };
  } catch {
    return { ok: false, data: undefined };
  }
}
async function autoDetectOpenAICompatibleProvider() {
  const candidates = [
    { name: "LM Studio", ports: [1234, 8080, 11434] },
    { name: "Ollama", ports: [11434] },
    { name: "LocalAI", ports: [8080] }
  ];
  for (const candidate of candidates) {
    for (const port of candidate.ports) {
      const baseURL = `http://127.0.0.1:${port}`;
      const discovery = await discoverModelsFromProvider(baseURL);
      if (discovery.ok) {
        return { name: candidate.name, baseURL };
      }
    }
  }
  return null;
}
function isOpenAICompatibleProvider(provider) {
  return provider && typeof provider === "object" && provider.npm === "@ai-sdk/openai-compatible";
}
function hasOpenAICompatibleURL(provider) {
  if (!provider || typeof provider !== "object")
    return false;
  const baseURL = provider.options?.baseURL || "";
  return /\/v1(\/|$)/.test(baseURL);
}
function hasModelsDiscoveryEndpoint(provider) {
  if (!provider || typeof provider !== "object")
    return false;
  const endpoint = provider.options?.modelsDiscovery?.endpoint;
  return typeof endpoint === "string" && endpoint.length > 0;
}
function canDiscoverModels(provider) {
  return isOpenAICompatibleProvider(provider) || hasOpenAICompatibleURL(provider) || hasModelsDiscoveryEndpoint(provider);
}

// src/utils/validation/validate-config.ts
function validateConfig(config) {
  const errors = [];
  const warnings = [];
  if (!config || typeof config !== "object") {
    errors.push("Config must be an object");
    return { isValid: false, errors, warnings };
  }
  if (config.provider && typeof config.provider === "object") {
    for (const [providerName, providerConfig] of Object.entries(config.provider)) {
      const p = providerConfig;
      const forceDiscoveryEnabled = p.options?.modelsDiscovery?.enabled === true;
      if (forceDiscoveryEnabled || canDiscoverModels(p)) {
        if (!p.options?.baseURL) {
          warnings.push(`Provider '${providerName}' missing baseURL`);
        }
        if (p.models && typeof p.models !== "object") {
          errors.push(`Provider '${providerName}' models must be an object`);
        }
      }
    }
  }
  return {
    isValid: errors.length === 0,
    errors,
    warnings
  };
}
// src/utils/validation/validate-hook-input.ts
function validateHookInput(hookName, input) {
  const errors = [];
  const warnings = [];
  if (!input || typeof input !== "object") {
    errors.push(`${hookName}: Input must be an object`);
    return { isValid: false, errors, warnings };
  }
  switch (hookName) {
    case "chat.params":
      if (!input.sessionID || typeof input.sessionID !== "string") {
        errors.push("chat.params: sessionID is required and must be a string");
      }
      if (!input.model || typeof input.model !== "object") {
        errors.push("chat.params: model is required and must be an object");
      } else {
        if (!input.model.id || typeof input.model.id !== "string") {
          errors.push("chat.params: model.id is required and must be a string");
        }
      }
      if (!input.provider || typeof input.provider !== "object") {
        errors.push("chat.params: provider is required and must be an object");
      } else {
        if (!input.provider.info || !input.provider.info.id) {
          warnings.push("chat.params: provider.info.id is missing");
        }
      }
      break;
    case "config":
      break;
    case "event":
      if (!input.event || typeof input.event !== "object") {
        errors.push("event: event is required and must be an object");
      } else if (!input.event.type) {
        warnings.push("event: event.type is missing");
      }
      break;
  }
  return {
    isValid: errors.length === 0,
    errors,
    warnings
  };
}
// src/utils/format-model-name.ts
function extractModelOwner(modelId) {
  const parts = modelId.split("/");
  if (parts.length > 1) {
    return parts[0];
  }
  return;
}
function formatModelName(model) {
  const { id } = model;
  const parts = id.split("/");
  const modelPart = parts.length > 1 ? parts[1] : parts[0];
  const acronyms = new Set(["gpt", "oss", "api", "gguf", "ggml", "nomic", "vl", "it", "mlx"]);
  const tokens = modelPart.split(/[-_]/).filter(Boolean).map((token) => {
    const lowerToken = token.toLowerCase();
    if (acronyms.has(lowerToken)) {
      return token.toUpperCase();
    }
    if (/^\d+[bkmg]$/i.test(token)) {
      return token.toUpperCase();
    }
    if (/^q\d+$/i.test(token)) {
      return token.toUpperCase();
    }
    if (/^\d+\.\d+/.test(token)) {
      return token;
    }
    if (/^[a-z]\d+[a-z]$/i.test(token) || /^\d+[a-z]$/i.test(token)) {
      return token.toUpperCase();
    }
    return token.charAt(0).toUpperCase() + token.slice(1).toLowerCase();
  }).join(" ");
  return tokens;
}

// src/utils/index.ts
function categorizeModel(modelId) {
  const lowerId = modelId.toLowerCase();
  if (lowerId.includes("embedding") || lowerId.includes("embed")) {
    return "embedding";
  }
  if (lowerId.includes("gpt") || lowerId.includes("llama") || lowerId.includes("claude") || lowerId.includes("qwen") || lowerId.includes("mistral") || lowerId.includes("gemma") || lowerId.includes("phi") || lowerId.includes("falcon")) {
    return "chat";
  }
  return "unknown";
}

// src/utils/discovery-report.ts
import { homedir } from "node:os";
import { join, dirname } from "node:path";
import { writeFile, mkdir, readFile } from "node:fs/promises";

// src/version.ts
var PLUGIN_VERSION = "0.9.0-llmgw.3";

// src/utils/discovery-report.ts
var SNAPSHOT_PATH = join(homedir(), ".config", "opencode", "plugins", "models-discovery.snapshot.json");
function modelHasVision(model) {
  if (model?.attachment === true)
    return true;
  const input = model?.modalities?.input;
  return Array.isArray(input) && input.includes("image");
}
async function readPreviousSnapshot() {
  try {
    const content = await readFile(SNAPSHOT_PATH, "utf8");
    return JSON.parse(content);
  } catch {
    return null;
  }
}
function findNewModels(currentProviders, previousSnapshot) {
  if (!previousSnapshot?.providers) {
    return [];
  }
  const newModels = [];
  const previousModelIds = new Set;
  for (const provData of Object.values(previousSnapshot.providers)) {
    if (provData.models) {
      for (const modelId of Object.keys(provData.models)) {
        previousModelIds.add(modelId);
      }
    }
  }
  for (const provider of currentProviders) {
    for (const modelId of Object.keys(provider.models)) {
      if (!previousModelIds.has(modelId)) {
        newModels.push(modelId);
      }
    }
  }
  return newModels;
}
function summarizeProviders(providers) {
  const summary = { models: 0, vision: 0, toolCall: 0, reasoning: 0 };
  for (const provider of providers) {
    for (const model of Object.values(provider.models)) {
      summary.models++;
      if (modelHasVision(model))
        summary.vision++;
      if (model?.tool_call === true)
        summary.toolCall++;
      if (model?.reasoning === true)
        summary.reasoning++;
    }
  }
  return summary;
}
async function writeDiscoverySnapshot(providers, summary, logger) {
  const snapshot = {
    generatedAt: new Date().toISOString(),
    pluginVersion: PLUGIN_VERSION,
    summary,
    providers: providers.reduce((acc, provider) => {
      acc[provider.name] = {
        baseURL: provider.baseURL,
        modelCount: Object.keys(provider.models).length,
        models: Object.fromEntries(Object.entries(provider.models).map(([id, model]) => [
          id,
          {
            name: model?.name,
            modalities: model?.modalities,
            attachment: model?.attachment === true,
            tool_call: model?.tool_call === true,
            reasoning: model?.reasoning === true,
            limit: model?.limit
          }
        ]))
      };
      return acc;
    }, {})
  };
  try {
    await mkdir(dirname(SNAPSHOT_PATH), { recursive: true });
    await writeFile(SNAPSHOT_PATH, JSON.stringify(snapshot, null, 2) + `
`, "utf8");
    return SNAPSHOT_PATH;
  } catch (error) {
    logger.warn("failed to write discovery snapshot", {
      path: SNAPSHOT_PATH,
      error: error instanceof Error ? error.message : String(error)
    });
    return null;
  }
}

// src/utils/model-info/litellm.ts
function hasUsableNumber(value) {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}
function modelInfoScore(modelId, entry) {
  const info = entry.model_info ?? {};
  const modelIdLower = modelId.toLowerCase();
  let score = 0;
  if (entry.model_name === modelId)
    score += 8;
  if (info.key === modelId)
    score += 6;
  if (entry.litellm_params?.model === modelId)
    score += 4;
  if (entry.litellm_params?.model?.endsWith(`/${modelId}`))
    score += 2;
  if (entry.model_name?.toLowerCase() === modelIdLower)
    score += 3;
  if (info.key?.toLowerCase() === modelIdLower)
    score += 2;
  if (entry.litellm_params?.model?.toLowerCase() === modelIdLower)
    score += 2;
  if (entry.litellm_params?.model?.toLowerCase().endsWith(`/${modelIdLower}`))
    score += 1;
  if (info.mode === "chat")
    score += 5;
  if (hasUsableNumber(info.max_input_tokens) || hasUsableNumber(info.max_tokens))
    score += 10;
  if (info.supports_reasoning === true)
    score += 4;
  return score;
}
function buildModelInfoMap(entries) {
  const result = new Map;
  for (const entry of entries) {
    const keys = new Set;
    if (entry.model_name)
      keys.add(entry.model_name);
    if (entry.model_info?.key)
      keys.add(entry.model_info.key);
    if (entry.litellm_params?.model) {
      keys.add(entry.litellm_params.model);
      const parts = entry.litellm_params.model.split("/");
      if (parts.length > 1)
        keys.add(parts.slice(1).join("/"));
      keys.add(parts[parts.length - 1]);
    }
    for (const key of keys) {
      for (const lookupKey of new Set([key, key.toLowerCase()])) {
        const existing = result.get(lookupKey);
        if (!existing || modelInfoScore(lookupKey, entry) > modelInfoScore(lookupKey, existing)) {
          result.set(lookupKey, entry);
        }
      }
    }
  }
  return result;
}
function createReasoningVariants(info) {
  if (info.supports_reasoning !== true || !info.supported_openai_params?.includes("reasoning_effort")) {
    return;
  }
  const variants = {};
  if (info.supports_none_reasoning_effort === true)
    variants.none = { reasoningEffort: "none" };
  if (info.supports_minimal_reasoning_effort === true)
    variants.minimal = { reasoningEffort: "minimal" };
  if (info.supports_low_reasoning_effort !== false)
    variants.low = { reasoningEffort: "low" };
  variants.medium = { reasoningEffort: "medium" };
  variants.high = { reasoningEffort: "high" };
  if (info.supports_xhigh_reasoning_effort === true)
    variants.xhigh = { reasoningEffort: "xhigh" };
  if (info.supports_max_reasoning_effort === true)
    variants.max = { reasoningEffort: "max" };
  return Object.keys(variants).length > 0 ? variants : undefined;
}
function normalizeModalityList(values) {
  if (!Array.isArray(values))
    return;
  const normalized = values.filter((value) => typeof value === "string" && value.length > 0).map((value) => value.toLowerCase());
  return normalized.length > 0 ? Array.from(new Set(normalized)) : undefined;
}
function deriveInputModalities(info) {
  const explicit = normalizeModalityList(info.input_modalities) ?? normalizeModalityList(info.supported_modalities);
  if (explicit) {
    return explicit.includes("text") ? explicit : ["text", ...explicit];
  }
  if (info.supports_vision === true) {
    return ["text", "image"];
  }
  if (info.supports_vision === false) {
    return ["text"];
  }
  return;
}
function applyLiteLLMModelInfo(modelConfig, entry) {
  const info = entry?.model_info;
  if (!info)
    return;
  const contextLimit = hasUsableNumber(info.max_input_tokens) ? info.max_input_tokens : info.max_tokens;
  const outputLimit = hasUsableNumber(info.max_output_tokens) ? info.max_output_tokens : info.max_tokens;
  if (hasUsableNumber(contextLimit) && hasUsableNumber(outputLimit)) {
    modelConfig.limit = {
      context: contextLimit,
      input: hasUsableNumber(info.max_input_tokens) ? info.max_input_tokens : undefined,
      output: outputLimit
    };
  }
  if (info.supports_reasoning === true) {
    modelConfig.reasoning = true;
  }
  if (info.supports_function_calling === true || info.supports_tool_choice === true) {
    modelConfig.tool_call = true;
  }
  const inputModalities = deriveInputModalities(info);
  if (inputModalities) {
    const outputModalities = normalizeModalityList(info.output_modalities) ?? ["text"];
    modelConfig.modalities = {
      input: inputModalities,
      output: outputModalities
    };
  }
  if (info.supports_vision === true || inputModalities?.includes("image")) {
    modelConfig.attachment = true;
  }
  const variants = createReasoningVariants(info);
  if (variants) {
    modelConfig.variants = variants;
  }
}
function getModelInfo(modelInfoById, modelId) {
  return modelInfoById.get(modelId) ?? modelInfoById.get(modelId.toLowerCase());
}
function createLiteLLMModelInfoEnricher(data, options) {
  const response = data;
  const modelInfoById = buildModelInfoMap(Array.isArray(response?.data) ? response.data : []);
  return {
    shouldSkipModel(modelId) {
      if (!options.filterNonChat)
        return false;
      const mode = getModelInfo(modelInfoById, modelId)?.model_info?.mode;
      return typeof mode === "string" && mode.length > 0 && mode !== "chat";
    },
    applyModelInfo(modelConfig, modelId) {
      applyLiteLLMModelInfo(modelConfig, getModelInfo(modelInfoById, modelId));
    }
  };
}

// src/utils/model-info/index.ts
var MODEL_INFO_ENRICHERS = {
  litellm: createLiteLLMModelInfoEnricher
};
function createModelInfoEnricher(format, data, options) {
  return MODEL_INFO_ENRICHERS[format]?.(data, options);
}
function isSupportedModelInfoFormat(format) {
  return MODEL_INFO_ENRICHERS[format] !== undefined;
}

// src/types/plugin-config.ts
var DEFAULT_DISCOVERY_CONFIG = {
  enabled: false
};
function shouldDiscoverProvider(providerName, filter) {
  if (filter.include.length > 0) {
    return filter.include.includes(providerName);
  }
  return !filter.exclude.includes(providerName);
}
function getProviderFilter(config) {
  return {
    include: config.providers?.include || [],
    exclude: config.providers?.exclude || []
  };
}
function getDiscoveryConfig(config) {
  return {
    enabled: config.discovery?.enabled ?? DEFAULT_DISCOVERY_CONFIG.enabled
  };
}
function shouldDiscoverProviderWithOverride(providerName, filter, globalEnabled, providerConfig) {
  if (providerConfig.enabled === true) {
    return true;
  }
  if (providerConfig.enabled === false) {
    return false;
  }
  if (!globalEnabled) {
    return false;
  }
  return shouldDiscoverProvider(providerName, filter);
}
function toRegExp(pattern, logger) {
  try {
    return new RegExp(pattern);
  } catch {
    if (logger) {
      logger.warn("Ignoring invalid model regex", { category: "filtering", pattern });
    } else {
      console.warn(`[opencode-models-discovery] Ignoring invalid model regex: ${pattern}`);
    }
    return null;
  }
}
function getModelRegexFilter(config, logger) {
  return {
    includeRegex: (config.models?.includeRegex || []).map((pattern) => toRegExp(pattern, logger)).filter((pattern) => pattern !== null),
    excludeRegex: (config.models?.excludeRegex || []).map((pattern) => toRegExp(pattern, logger)).filter((pattern) => pattern !== null)
  };
}
function getProviderModelRegexFilter(config, logger) {
  return {
    includeRegex: (config.models?.includeRegex || []).map((pattern) => toRegExp(pattern, logger)).filter((pattern) => pattern !== null),
    excludeRegex: (config.models?.excludeRegex || []).map((pattern) => toRegExp(pattern, logger)).filter((pattern) => pattern !== null)
  };
}
function shouldDiscoverModel(modelId, filter) {
  if (filter.includeRegex.length > 0) {
    return filter.includeRegex.some((pattern) => pattern.test(modelId));
  }
  if (filter.excludeRegex.length > 0) {
    return !filter.excludeRegex.some((pattern) => pattern.test(modelId));
  }
  return true;
}
function parsePluginConfig(rawConfig) {
  if (!rawConfig) {
    return {};
  }
  if (Array.isArray(rawConfig)) {
    if (rawConfig.length >= 2 && typeof rawConfig[0] === "string") {
      const configObj = rawConfig[1];
      if (configObj && typeof configObj === "object") {
        return configObj;
      }
    }
    return {};
  }
  if (typeof rawConfig === "object") {
    return rawConfig;
  }
  return {};
}

// src/plugin/enhance-config.ts
async function enhanceConfig(config, client, toastNotifier, pluginConfig, logger) {
  try {
    const providers = config.provider || {};
    const openAICompatibleProviders = [];
    const providerFilter = getProviderFilter(pluginConfig);
    const modelRegexFilter = getModelRegexFilter(pluginConfig, logger.child({ category: "filtering" }));
    const discoveryConfig = getDiscoveryConfig(pluginConfig);
    const globalDiscoveryEnabled = discoveryConfig.enabled;
    for (const [providerName, providerConfig] of Object.entries(providers)) {
      const p = providerConfig;
      const providerDiscoveryConfig = p.options?.modelsDiscovery ?? {};
      const modelsEndpoint = providerDiscoveryConfig.endpoint ?? "/v1/models";
      const modelInfoEndpoint = providerDiscoveryConfig.modelInfoEndpoint;
      const modelInfoFormat = providerDiscoveryConfig.modelInfoFormat;
      const filterNonChat = providerDiscoveryConfig.filterNonChat !== false;
      const forceDiscoveryEnabled = providerDiscoveryConfig.enabled === true;
      if (!forceDiscoveryEnabled && !canDiscoverModels(p)) {
        continue;
      }
      if (!shouldDiscoverProviderWithOverride(providerName, providerFilter, globalDiscoveryEnabled, providerDiscoveryConfig)) {
        logger.debug(`Provider ${providerName} model discovery disabled by configuration`);
        continue;
      }
      let baseURL;
      let displayName = typeof p.name === "string" && p.name.trim() ? p.name.trim() : providerName;
      if (p.options?.baseURL) {
        baseURL = normalizeBaseURL(p.options.baseURL);
      } else {
        continue;
      }
      const apiKey = p.options?.apiKey;
      let models;
      const discovery = await discoverModelsFromProvider(baseURL, apiKey, modelsEndpoint);
      if (!discovery.ok) {
        logger.warn("Provider model discovery failed", {
          provider: providerName,
          baseURL,
          endpoint: modelsEndpoint
        });
        continue;
      }
      models = discovery.models;
      if (models.length === 0) {
        continue;
      }
      let modelInfoEnricher;
      if (modelInfoFormat && !isSupportedModelInfoFormat(modelInfoFormat)) {
        logger.warn("Unsupported provider model info format", {
          provider: providerName,
          format: modelInfoFormat
        });
      } else if (typeof modelInfoEndpoint === "string" && modelInfoEndpoint.length > 0 && modelInfoFormat) {
        const modelInfoDiscovery = await discoverModelInfoFromProvider(baseURL, apiKey, modelInfoEndpoint);
        if (modelInfoDiscovery.ok) {
          modelInfoEnricher = createModelInfoEnricher(modelInfoFormat, modelInfoDiscovery.data, { filterNonChat });
        } else {
          logger.warn("Provider model info discovery failed", {
            provider: providerName,
            baseURL,
            endpoint: modelInfoEndpoint,
            format: modelInfoFormat
          });
        }
      }
      const existingModels = p.models || {};
      const discoveredModels = {};
      let chatModelsCount = 0;
      const hasProviderModelRegexFilter = !!providerDiscoveryConfig.models?.includeRegex?.length || !!providerDiscoveryConfig.models?.excludeRegex?.length;
      const providerModelRegexFilter = getProviderModelRegexFilter(providerDiscoveryConfig, logger.child({ category: "filtering" }));
      let smartModelNameEnabled = providerDiscoveryConfig.smartModelName;
      if (smartModelNameEnabled === undefined) {
        smartModelNameEnabled = pluginConfig.smartModelName;
      }
      for (const model of models) {
        const modelKey = model.id;
        if (forceDiscoveryEnabled || !existingModels[modelKey]) {
          const activeModelRegexFilter = hasProviderModelRegexFilter ? providerModelRegexFilter : modelRegexFilter;
          if (!shouldDiscoverModel(model.id, activeModelRegexFilter)) {
            continue;
          }
          if (modelInfoEnricher?.shouldSkipModel(model.id)) {
            continue;
          }
          const modelType = categorizeModel(model.id);
          if (modelType === "embedding") {
            continue;
          }
          const owner = extractModelOwner(model.id);
          const modelConfig = {
            id: model.id,
            name: smartModelNameEnabled ? formatModelName(model) : model.id
          };
          if (owner) {
            modelConfig.organizationOwner = owner;
          }
          if (modelType === "chat") {
            chatModelsCount++;
          }
          modelInfoEnricher?.applyModelInfo(modelConfig, model.id);
          if (!modelConfig.modalities && modelType === "chat") {
            modelConfig.modalities = {
              input: ["text"],
              output: ["text"]
            };
          }
          discoveredModels[modelKey] = modelConfig;
        }
      }
      if (Object.keys(discoveredModels).length > 0) {
        p.models = forceDiscoveryEnabled ? discoveredModels : {
          ...existingModels,
          ...discoveredModels
        };
        openAICompatibleProviders.push({
          name: displayName,
          baseURL,
          models: discoveredModels
        });
      }
    }
    if (openAICompatibleProviders.length > 0) {
      const summary = summarizeProviders(openAICompatibleProviders);
      logger.info("Provider model discovery completed", {
        providerCount: openAICompatibleProviders.length,
        modelCount: summary.models,
        vision: summary.vision,
        toolCall: summary.toolCall,
        reasoning: summary.reasoning
      });
      const previousSnapshot = await readPreviousSnapshot();
      const newModels = findNewModels(openAICompatibleProviders, previousSnapshot);
      await writeDiscoverySnapshot(openAICompatibleProviders, summary, logger);
      setTimeout(() => {
        for (const prov of openAICompatibleProviders) {
          const provSummary = summarizeProviders([prov]);
          let message = `发现 ${provSummary.models} 个模型`;
          if (provSummary.vision > 0)
            message += ` · 图像 ${provSummary.vision}`;
          if (provSummary.toolCall > 0)
            message += ` · 工具 ${provSummary.toolCall}`;
          if (provSummary.reasoning > 0)
            message += ` · 推理 ${provSummary.reasoning}`;
          toastNotifier.info(message, `${prov.name} · v${PLUGIN_VERSION}`, 1e4).catch(() => {});
        }
        if (newModels.length > 0) {
          const modelList = newModels.length <= 5 ? newModels.join(", ") : `${newModels.slice(0, 5).join(", ")} 等 ${newModels.length} 个`;
          toastNotifier.success(`网关上新: ${modelList}`, "新模型", 15000).catch(() => {});
        }
      }, 5000);
    }
    if (Object.keys(providers).length === 0) {
      const detected = await autoDetectOpenAICompatibleProvider();
      if (detected) {
        logger.info("Detected OpenAI-compatible provider but found no configured providers", {
          provider: detected.name,
          baseURL: detected.baseURL
        });
      }
    }
  } catch (error) {
    logger.error("Unexpected error in enhanceConfig", {
      error: error instanceof Error ? error.message : String(error)
    });
    toastNotifier.warning("Plugin configuration failed", "Configuration Error").catch(() => {});
  }
}

// src/utils/self-update.ts
import { homedir as homedir2 } from "node:os";
import { join as join2, dirname as dirname2 } from "node:path";
import { writeFile as writeFile2, mkdir as mkdir2, rm } from "node:fs/promises";
var selfUpdateStarted = false;
var VERSION_PATH = "/api/v1/opencode-config/plugin/version";
var PLUGIN_PATH = "/api/v1/opencode-config/plugin";
function originFromBaseURL(baseURL) {
  try {
    const u = new URL(baseURL);
    if (!u.protocol || !u.host)
      return null;
    return `${u.protocol}//${u.host}`;
  } catch {
    return null;
  }
}
function pickGatewayOrigin(config) {
  const providers = config?.provider;
  if (!providers || typeof providers !== "object")
    return null;
  for (const value of Object.values(providers)) {
    const p = value;
    if (p?.options?.modelsDiscovery?.enabled === true && typeof p?.options?.baseURL === "string") {
      const origin = originFromBaseURL(p.options.baseURL);
      if (origin)
        return origin;
    }
  }
  return null;
}
async function fetchText(url, timeoutMs) {
  const controller = new AbortController;
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const resp = await fetch(url, { signal: controller.signal });
    if (!resp.ok)
      return null;
    return await resp.text();
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}
async function maybeSelfUpdate(config, toastNotifier, logger) {
  if (selfUpdateStarted)
    return;
  selfUpdateStarted = true;
  const origin = pickGatewayOrigin(config);
  if (!origin) {
    logger.debug("self-update skipped: no discovery-enabled provider with baseURL");
    return;
  }
  const remoteRaw = await fetchText(origin + VERSION_PATH, 3000);
  if (!remoteRaw) {
    logger.debug("self-update skipped: version endpoint unavailable", { origin });
    return;
  }
  const remote = remoteRaw.trim();
  if (!remote || remote === PLUGIN_VERSION) {
    logger.debug("self-update: already up to date", { version: PLUGIN_VERSION });
    return;
  }
  logger.info("self-update: newer plugin version found", { local: PLUGIN_VERSION, remote });
  const code = await fetchText(origin + PLUGIN_PATH, 8000);
  if (!code || code.length < 1024) {
    logger.warn("self-update: failed to download new bundle", { origin });
    return;
  }
  const target = join2(homedir2(), ".config", "opencode", "plugins", "opencode-models-discovery.js");
  const legacy = join2(homedir2(), ".config", "opencode", "plugin", "opencode-models-discovery.js");
  try {
    await mkdir2(dirname2(target), { recursive: true });
    await writeFile2(target, code, "utf8");
    await rm(legacy, { force: true }).catch(() => {});
  } catch (error) {
    logger.warn("self-update: failed to write plugin file", {
      target,
      error: error instanceof Error ? error.message : String(error)
    });
    return;
  }
  logger.info("self-update: plugin updated, restart required", { from: PLUGIN_VERSION, to: remote, target });
  toastNotifier.info(`模型发现插件已更新 ${PLUGIN_VERSION} → ${remote}，重启 opencode 生效`, "LLM Gateway", 1e4).catch(() => {});
}

// src/plugin/config-hook.ts
function createConfigHook(client, toastNotifier, pluginConfig, logger) {
  return async (config) => {
    if (config && (Object.isFrozen?.(config) || Object.isSealed?.(config))) {
      logger.warn("Config object is frozen or sealed; cannot modify directly");
      return;
    }
    const validation = validateConfig(config);
    if (!validation.isValid) {
      logger.error("Invalid config provided", { errors: validation.errors });
      toastNotifier.error("Plugin configuration is invalid", "Configuration Error").catch(() => {});
      return;
    }
    if (validation.warnings.length > 0) {
      logger.warn("Config warnings", { warnings: validation.warnings });
    }
    maybeSelfUpdate(config, toastNotifier, logger.child({ category: "self-update" }));
    const discoveryPromise = enhanceConfig(config, client, toastNotifier, pluginConfig, logger.child({ category: "discovery" }));
    const timeoutMs = 5000;
    try {
      await Promise.race([
        discoveryPromise,
        new Promise((resolve) => {
          setTimeout(() => resolve(), timeoutMs);
        })
      ]);
    } catch (error) {
      logger.error("Config enhancement failed", {
        error: error instanceof Error ? error.message : String(error)
      });
    }
  };
}

// src/plugin/event-hook.ts
function createEventHook(logger) {
  return async ({ event }) => {
    const validation = validateHookInput("event", { event });
    if (!validation.isValid) {
      logger.error("Invalid event input", { errors: validation.errors });
      return;
    }
    if (event.type === "session.created" || event.type === "session.updated") {}
  };
}

// src/plugin/chat-params-hook.ts
function createChatParamsHook(toastNotifier, pluginConfig) {
  return async (input, output) => {};
}

// src/plugin/logger.ts
var SERVICE_NAME = "opencode-models-discovery";
function getConsoleMethod(level) {
  if (level === "error") {
    return console.error;
  }
  if (level === "warn") {
    return console.warn;
  }
  if (level === "debug") {
    return console.debug;
  }
  return console.info;
}
function mergeExtra(baseExtra, extra) {
  const merged = {
    ...baseExtra,
    ...extra
  };
  return Object.keys(merged).length > 0 ? merged : undefined;
}
function fallbackToConsole(level, message, extra) {
  const log = getConsoleMethod(level);
  const prefix = `[${SERVICE_NAME}] ${message}`;
  if (extra && Object.keys(extra).length > 0) {
    log(prefix, extra);
    return;
  }
  log(prefix);
}
function createPluginLogger(client, baseExtra = {}) {
  const log = (level, message, extra) => {
    const mergedExtra = mergeExtra(baseExtra, extra);
    try {
      if (client?.app?.log) {
        client.app.log({
          body: {
            service: SERVICE_NAME,
            level,
            message,
            extra: mergedExtra
          }
        }).catch(() => {
          fallbackToConsole(level, message, mergedExtra);
        });
        return;
      }
    } catch {}
    fallbackToConsole(level, message, mergedExtra);
  };
  return {
    debug(message, extra) {
      log("debug", message, extra);
    },
    info(message, extra) {
      log("info", message, extra);
    },
    warn(message, extra) {
      log("warn", message, extra);
    },
    error(message, extra) {
      log("error", message, extra);
    },
    child(extra) {
      return createPluginLogger(client, mergeExtra(baseExtra, extra) || {});
    }
  };
}

// src/plugin/index.ts
var ModelDiscoveryPlugin = async (input, options) => {
  const { client, directory } = input;
  const logger = createPluginLogger(client, { category: "plugin" });
  if (!client || typeof client !== "object") {
    logger.error("Invalid client provided to plugin");
    return {
      config: async () => {},
      event: async () => {},
      "chat.params": async () => {}
    };
  }
  logger.info("Model discovery plugin initialized", { directory });
  const pluginConfig = parsePluginConfig(options || {});
  if (pluginConfig.discovery?.enabled === false) {
    logger.info("Discovery disabled by configuration", { category: "config" });
  }
  const toastNotifier = new ToastNotifier(client, directory);
  return {
    config: createConfigHook(client, toastNotifier, pluginConfig, logger.child({ category: "config" })),
    event: createEventHook(logger.child({ category: "event" })),
    "chat.params": createChatParamsHook(toastNotifier, pluginConfig)
  };
};
export {
  ModelDiscoveryPlugin
};
