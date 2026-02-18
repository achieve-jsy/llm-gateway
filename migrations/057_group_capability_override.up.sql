ALTER TABLE model_groups
  ADD COLUMN is_capability_override BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN override_supports_vision BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN override_supports_function_calling BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN override_supports_tool_choice BOOLEAN NOT NULL DEFAULT FALSE;
