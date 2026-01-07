-- Add bypass_org_check to api_keys: 豁免组织归属检查（供系统探针、平台内部 Key 使用）
ALTER TABLE "api_keys" ADD COLUMN "bypass_org_check" boolean NOT NULL DEFAULT false;

-- 将系统健康探针 key 设为豁免（该 key 无归属部门，需跳过 OrgCheckMiddleware）
UPDATE "api_keys" SET "bypass_org_check" = true WHERE "name" = '__system_health_probe__';
