-- Add period_type to expansion_requests to support daily/weekly/monthly quota adjustment requests
ALTER TABLE "expansion_requests" ADD COLUMN "period_type" character varying(16) NOT NULL DEFAULT 'monthly';
