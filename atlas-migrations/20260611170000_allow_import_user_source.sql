-- Allow 'import' as a valid source for users created via employee roster import
ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_source;
ALTER TABLE users ADD CONSTRAINT chk_users_source CHECK (source IN ('oidc', 'import'));