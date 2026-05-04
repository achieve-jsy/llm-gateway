-- Add optional model-level manual cache pricing overrides.
-- NULL preserves current pricing behavior: catalog cache prices are used when available,
-- otherwise cost calculation falls back to the regular input price.
ALTER TABLE "models"
  ADD COLUMN "cache_read_price" numeric(14,6) NULL,
  ADD COLUMN "cache_write_price" numeric(14,6) NULL;
