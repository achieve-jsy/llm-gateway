-- 042_api_keys_add_dept_id: api_keys 表加 dept_id，与 departments.id 对应，用于统计按部门聚合
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS dept_id TEXT NOT NULL DEFAULT '';

-- 对已有记录，通过 users → departments 回填 dept_id
UPDATE api_keys ak
SET dept_id = d.id::text
FROM users u
JOIN departments d ON d.id = u.dept_id
WHERE ak.user_id = u.id
  AND ak.dept_id = ''
  AND d.id IS NOT NULL;
