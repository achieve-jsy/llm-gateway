-- atlas:txmode none

CREATE INDEX CONCURRENTLY "idx_call_logs_user_ts"
  ON "call_logs" ("user_id", "ts");
