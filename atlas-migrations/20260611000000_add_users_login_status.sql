-- Add login_status to users for tracking employee import pending vs SSO active
ALTER TABLE "users" ADD COLUMN "login_status" character varying(16) NOT NULL DEFAULT 'active';