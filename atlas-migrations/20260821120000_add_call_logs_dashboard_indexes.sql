-- atlas:txmode none

CREATE INDEX CONCURRENTLY "idx_call_logs_user_ts"
  ON "call_logs" ("user_id", "ts");

CREATE INDEX CONCURRENTLY "idx_call_logs_studio_ts"
  ON "call_logs" ("studio_id", "ts");
