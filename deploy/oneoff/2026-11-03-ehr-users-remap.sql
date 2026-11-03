-- 一次性：把挂在"已软删老部门"下的成员，按 EHR 花名册改挂到真实部门行。
--
-- 背景：老库 departments.dept_id 原本是部门名；18 个老部门在 EHR 里已不存在（撤并/改名），
-- 部门 ID 认领（2026-11-03-ehr-department-claim.sql）+ 首轮同步后它们被软删（status='deleted'），
-- 这些成员在组织树上就找不到自己的节点了（历史 key/用量快照不受影响，那是字符串快照）。
-- 前置：首轮同步已成功（departments 里已有真实 EHR 部门行、ehr_employees 有全量）。
\pset pager off
BEGIN;

-- 干跑：先看要动多少人
--   SELECT count(*) FROM users u
--     JOIN ehr_employees e ON e.employee_id = u.employee_id AND u.employee_id <> ''
--     JOIN departments d ON d.dept_id = e.department_id AND d.status <> 'deleted'
--    WHERE u.dept_id IN (SELECT id FROM departments WHERE status = 'deleted');

UPDATE users u
   SET dept_id = d.id,
       updated_at = now()
  FROM ehr_employees e
  JOIN departments d ON d.dept_id = e.department_id AND d.status <> 'deleted'
 WHERE u.employee_id = e.employee_id
   AND u.employee_id <> ''
   AND u.dept_id IN (SELECT id FROM departments WHERE status = 'deleted');

-- 自检：下面应为 0（还剩多少成员挂在软删部门下且花名册能查到新部门）
--   SELECT count(*) FROM users u WHERE u.dept_id IN (SELECT id FROM departments WHERE status='deleted');

COMMIT;
