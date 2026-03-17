-- 将 dashscope (阿里百炼) 的 adapter 从 openai-compatible 改为 bailian
-- bailian adapter 内部包装了 openai.CompatibleProvider，Chat/Embedding 行为不变，
-- 只有 Rerank 走百炼原生 API (/api/v1/services/rerank/text-rerank/text-rerank)，
-- 因为 DashScope 兼容模式不支持 /rerank 端点。
UPDATE provider_definitions SET adapter = 'bailian' WHERE id = 'dashscope' AND adapter = 'openai-compatible';