ALTER TABLE model_groups
  DROP COLUMN IF EXISTS is_capability_override,
  DROP COLUMN IF EXISTS override_supports_vision,
  DROP COLUMN IF EXISTS override_supports_function_calling,
  DROP COLUMN IF EXISTS override_supports_tool_choice;
