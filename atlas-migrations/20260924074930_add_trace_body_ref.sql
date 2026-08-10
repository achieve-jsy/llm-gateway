-- Modify "call_traces" table
ALTER TABLE "public"."call_traces" ADD COLUMN "body_ref_dt" date NULL, ADD COLUMN "body_ref_inst" text NULL, ADD COLUMN "body_ref_part" integer NULL, ADD COLUMN "body_ref_row" integer NULL, ADD COLUMN "body_missing" boolean NOT NULL DEFAULT false, ADD COLUMN "body_bytes" integer NULL;
