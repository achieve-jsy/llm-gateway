-- atlas-migrations/20261016120100_backfill_call_logs_call_type.sql
-- Feature: 回填 call_logs.call_type 的历史行（20261016120000 加列之后的第二步），
--          与加列拆成两个迁移，避免 DML 的失败/超时连带回滚 DDL（见上一个迁移的注释）。
--
-- ── 口径（与方案 A11 一致：端点优先、model 后缀兜底、不覆盖已有值、chat 行不动）──────────
-- ① **只回填 `call_type IS NULL` 的行**：`WHERE "call_type" IS NULL` 保证**已写入的值永不覆盖**
--    （无论已有值来自新代码采集还是上一次回填，包括空串 ''——空串是"端点不识别"的合法结论，
--    同样不覆盖）。
-- ② **端点优先**：`inbound_endpoint` 是入站路径，是结构化事实。call_logs 没有 method 列，
--    但历史行**只可能**来自 Gin 实际路由命中的请求，而下列路径的 (method, path) 组合是唯一的，
--    因此仅凭路径即可确定操作：
--      /v1/contents/generations/tasks            → 只有 POST（创建）
--      /v1/contents/generations/tasks/{task_id}  → 只有 GET（查询）
--      /v1/assets/{action}                       → 只有 POST
--      /v1/images/generations、/v1/images/edits  → 只有 POST
--      /v1/videos                                → 只有 POST（列表 GET 未实现，不产生日志）
--      /v1/videos/{task_id}、/v1/videos/{task_id}/content → 只有 GET
--    正则与 `observability.DeriveCallType` 逐条对应（含尾斜杠容忍、素材 Action 大小写敏感、
--    多段素材路径退回桶值 `video.asset`、拼接后超列宽退回桶值）。
-- ③ **`model` 后缀兜底**：历史 `model` 里混有伪模型名（`组名:CreateTask` / `组名:GetTask` /
--    `seedance-task:GetTask` / `seedance-asset:{Action}`，见 `internal/gateway/handler/video_handler.go`
--    与 `asset_handler.go` 的 `c.Set("model", ...)`）。端点缺失或为透传路径时按后缀判定；
--    `seedance-asset:{Action}` 的 Action 取「单段 + 拼接后 ≤ 64 字节」，超长/多段/空 Action 一律
--    退回桶值 `video.asset`（列宽保护，与 `maxCallTypeLen` 的**字节**口径一致，故用 octet_length
--    而不是 length；varchar(64) 按字符计数，用字节口径更严格因而更安全）。
--    注意 `seedance-task:GetTask` 已被 `%:GetTask` 覆盖，因此**刻意不**把读侧过滤用的
--    `model LIKE 'seedance-task:%'`（internal/observability/service/rollup_service.go:696 的
--    ApplySeedanceLogFilter）搬进来做更宽的匹配——`seedance-task:` 后面若不是已知操作，归不出
--    确定取值，按"不猜测"原则留 NULL，而不是编一个值。
-- ④ **chat 行不动**：chat/embeddings/rerank/models/model_info 等端点**不在**本回填范围内
--    （方案 A11 原文"chat 行不动"）。除"不猜"之外还有运维理由：call_logs 是写入最热的表且
--    体量最大（见 20260821120000 的看板索引迁移），给全表每一行都写一次新列值会产生一次
--    与表等量的写入放大；本次只写"判定不出来就没法统计"的媒体历史行（少量）。
--    将来若确实要给 chat 历史行补值，另起迁移并单独评估代价。
--
-- ── 幂等性（可重复执行 / 迁移重放不改变结果）────────────────────────────────────────────
-- 本文件是**单条** UPDATE，且 WHERE 的判定条件与 CASE 的判定条件是**逐条相同**的表达式，
-- CASE 的**每个**分支都返回非 NULL（最后两个兜底分支覆盖了所有被选中的行）。因此有：
--   1. 首次执行：被选中的每一行都得到**非 NULL** 的 call_type。
--   2. 再次执行（或任何次数的重放）：`WHERE call_type IS NULL` 不再匹配这些行，
--      语句影响行数为 **0**，表内容逐字节不变。
-- 即"选择性 + 单调赋值"共同保证结果与执行次数无关，无需额外幂等键或临时表。
-- 另：`ADD COLUMN IF NOT EXISTS` 使 DDL 也可重放；两条迁移在已有该列/已回填的库上均为 no-op。
-- 实测证据（本机 PG 18 + Atlas v1.2.3，13 行构造样本 + 二次执行 0 行）见提交说明与交付报告。
--
-- 执行语义：保持 Atlas 默认的"整文件一个事务"——回填要么全成功要么全回滚，不会留下半张表
-- 已回填、半张表 NULL 的中间态。若某环境因表过大导致超时，回滚是安全的（可重跑），
-- 期间的代价只是历史行仍为 NULL（读侧按 inbound_endpoint 兜底，功能不受影响）。
--
-- 回滚（本项目不使用 Atlas down 机制）：本迁移只写 call_type，回滚即
--   UPDATE "call_logs" SET "call_type" = NULL WHERE <与下方 WHERE 相同>;
-- 生产优先用新迁移修复。

UPDATE "call_logs"
SET "call_type" = CASE
        -- ① 端点优先：结构化事实
        WHEN "inbound_endpoint" ~ '^/v1/contents/generations/tasks/*$' THEN 'video.create'
        WHEN "inbound_endpoint" ~ '^/v1/contents/generations/tasks/[^/]+/*$' THEN 'video.get'
        WHEN "inbound_endpoint" ~ '^/v1/assets/.+$' THEN
            CASE
                WHEN "inbound_endpoint" ~ '^/v1/assets/[^/]+/*$' THEN
                    CASE
                        WHEN octet_length('video.asset.' || substring("inbound_endpoint" from '^/v1/assets/([^/]+)')) > 64
                            THEN 'video.asset'
                        ELSE 'video.asset.' || substring("inbound_endpoint" from '^/v1/assets/([^/]+)')
                    END
                -- 多段（/v1/assets/a/b）不是合法路由；与 DeriveCallType 一致退回桶值，不猜具体 Action
                ELSE 'video.asset'
            END
        WHEN "inbound_endpoint" ~ '^/v1/images/generations/*$' THEN 'image.generations'
        WHEN "inbound_endpoint" ~ '^/v1/images/edits/*$' THEN 'image.edits'
        WHEN "inbound_endpoint" ~ '^/v1/videos/*$' THEN 'video.submit'
        -- /content 必须先于「/{task_id} 查询」判断（两者不重叠，此处只为表达意图）
        WHEN "inbound_endpoint" ~ '^/v1/videos/[^/]+/content/*$' THEN 'video.content'
        WHEN "inbound_endpoint" ~ '^/v1/videos/[^/]+/*$' THEN 'video.get'
        -- ② model 后缀兜底：端点缺失/透传时仍可判定
        WHEN "model" LIKE 'seedance-asset:%' THEN
            CASE
                -- 空 Action（`seedance-asset:`）：归不出具体操作，退桶值
                WHEN coalesce(substring("model" from '^seedance-asset:(.+)$'), '') = '' THEN 'video.asset'
                -- 多段 Action：不是合法素材 Action，退桶值
                WHEN substring("model" from '^seedance-asset:(.+)$') LIKE '%/%' THEN 'video.asset'
                -- 拼接后超列宽（按字节）：退桶值，保证整行 call_logs 仍可写入
                WHEN octet_length('video.asset.' || substring("model" from '^seedance-asset:(.+)$')) > 64 THEN 'video.asset'
                ELSE 'video.asset.' || substring("model" from '^seedance-asset:(.+)$')
            END
        WHEN "model" LIKE '%:CreateTask' THEN 'video.create'
        WHEN "model" LIKE '%:GetTask' THEN 'video.get'
        ELSE NULL
    END
-- 只回填历史空值：NULL = 迁移前未写过的行；已有值（含空串 ''）一律不覆盖
WHERE "call_type" IS NULL
  -- 与上方 CASE 的判定条件逐条相同，保证"被选中的行必被赋非 NULL 值"（幂等性证明的前提）
  AND (
        "inbound_endpoint" ~ '^/v1/contents/generations/tasks/*$'
     OR "inbound_endpoint" ~ '^/v1/contents/generations/tasks/[^/]+/*$'
     OR "inbound_endpoint" ~ '^/v1/assets/.+$'
     OR "inbound_endpoint" ~ '^/v1/images/generations/*$'
     OR "inbound_endpoint" ~ '^/v1/images/edits/*$'
     OR "inbound_endpoint" ~ '^/v1/videos/*$'
     OR "inbound_endpoint" ~ '^/v1/videos/[^/]+/*$'
     OR "inbound_endpoint" ~ '^/v1/videos/[^/]+/content/*$'
     OR "model" LIKE '%:CreateTask'
     OR "model" LIKE '%:GetTask'
     OR "model" LIKE 'seedance-asset:%'
  );
