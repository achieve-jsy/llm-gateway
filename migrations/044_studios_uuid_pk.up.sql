-- 044_studios_uuid_pk.up.sql
-- 将 studios.id 从手填 varchar 改为 UUID 自动生成

-- 1. 清空测试数据
DELETE FROM dept_studio_map;
UPDATE departments SET studio_id = NULL;
DELETE FROM studios;

-- 2. 先删除依赖约束，再改主键
ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_studio_id_fkey;
ALTER TABLE dept_studio_map DROP CONSTRAINT IF EXISTS dept_studio_map_studio_id_fkey;
ALTER TABLE studios DROP CONSTRAINT studios_pkey CASCADE;

-- 3. 修改 studios.id 为 UUID 自动生成
ALTER TABLE studios ALTER COLUMN id DROP DEFAULT;
ALTER TABLE studios ALTER COLUMN id TYPE uuid USING gen_random_uuid();
ALTER TABLE studios ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE studios ADD PRIMARY KEY (id);

-- 4. 修改 departments.studio_id 为 uuid
ALTER TABLE departments ALTER COLUMN studio_id TYPE uuid USING NULL;

-- 5. 修改 dept_studio_map.studio_id 为 uuid
ALTER TABLE dept_studio_map ALTER COLUMN studio_id TYPE uuid USING studio_id::uuid;

-- 6. 重建外键约束
ALTER TABLE departments ADD CONSTRAINT departments_studio_id_fkey
  FOREIGN KEY (studio_id) REFERENCES studios(id) ON DELETE SET NULL;

ALTER TABLE dept_studio_map ADD CONSTRAINT dept_studio_map_studio_id_fkey
  FOREIGN KEY (studio_id) REFERENCES studios(id);
