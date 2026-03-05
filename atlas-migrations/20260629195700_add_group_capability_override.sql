-- Add capability override fields to model_groups
ALTER TABLE "model_groups" ADD COLUMN "is_capability_override" boolean NOT NULL DEFAULT false;
ALTER TABLE "model_groups" ADD COLUMN "override_supports_vision" boolean NOT NULL DEFAULT false;
ALTER TABLE "model_groups" ADD COLUMN "override_supports_function_calling" boolean NOT NULL DEFAULT false;
ALTER TABLE "model_groups" ADD COLUMN "override_supports_tool_choice" boolean NOT NULL DEFAULT false;
