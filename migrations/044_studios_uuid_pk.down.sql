-- 044_studios_uuid_pk.down.sql
ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_studio_id_fkey;
ALTER TABLE departments ALTER COLUMN studio_id TYPE varchar(64) USING studio_id::text;

ALTER TABLE dept_studio_map DROP CONSTRAINT IF EXISTS dept_studio_map_studio_id_fkey;
ALTER TABLE dept_studio_map ALTER COLUMN studio_id TYPE varchar(64) USING studio_id::text;

ALTER TABLE studios DROP CONSTRAINT studios_pkey;
ALTER TABLE studios ALTER COLUMN id DROP DEFAULT;
ALTER TABLE studios ALTER COLUMN id TYPE varchar(64) USING id::text;
ALTER TABLE studios ADD PRIMARY KEY (id);
