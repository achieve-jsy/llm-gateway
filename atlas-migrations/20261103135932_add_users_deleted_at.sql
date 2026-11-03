-- users 软删除（EHR 花名册把离职人员软删）。
-- 手写而非 atlas-diff：本机没有 docker，diff 起的临时 Postgres 跑不起来；
-- 列类型/索引与 tools/atlas-loader 输出的 schema 逐字对齐（deleted_at timestamptz + idx_users_deleted_at）。
ALTER TABLE "users" ADD COLUMN "deleted_at" timestamptz NULL;
CREATE INDEX IF NOT EXISTS "idx_users_deleted_at" ON "users" ("deleted_at");
