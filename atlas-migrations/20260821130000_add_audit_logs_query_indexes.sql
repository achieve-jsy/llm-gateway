-- 为审计日志查询补充组合索引，优化 scoped/按用户过滤 + 时间倒序分页的 List 查询。
CREATE INDEX "idx_audit_logs_dept_created" ON "public"."audit_logs" ("dept_id", "created_at" DESC);
CREATE INDEX "idx_audit_logs_user_created" ON "public"."audit_logs" ("user_id", "created_at" DESC);
