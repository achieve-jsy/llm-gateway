-- EHR 部门一次性认领：老库 departments.dept_id 是"名字/导入前缀"而不是 EHR 部门 ID
-- （56 行里只有 1 行是真 UUID）。不认领的话，EHR 同步会按真实 ID 再插一套同名部门，
-- 组织树出现 37 对重名，且 400+ 用户的 users.dept_id 会挂在不进 EHR 的老行上。
--
-- 认领 = 就地改 dept_id（保住 departments.id → users.dept_id / user_roles.scope_id 全部不动）。
-- 认领后仍不在 EHR 里的老部门由同步的对账步骤软删（status='deleted'），行保留给历史用量 join。
-- 前置：atlas-migrations/20261104090000 已 apply；ai_app_departments 已有 EHR 全量（100 行）。
\pset pager off
BEGIN;

UPDATE departments d
   SET dept_id = a.department_id
  FROM ai_app_departments a
 WHERE a.department_type <> 2
   AND lower(btrim(a.name)) = lower(btrim(d.dept_name))
   AND d.dept_id <> a.department_id
   -- 只认领"名字型"dept_id：已经是 EHR UUID 的行（1 行）不动
   AND d.dept_id !~ '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-';

-- 自检：以下必须返回 0 行（同一 EHR 部门被两行认领会撞 dept_id 唯一约束）
--   SELECT dept_id FROM departments GROUP BY dept_id HAVING count(*) > 1;

COMMIT;
