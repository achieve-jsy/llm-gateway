-- EHR 工作室一次性回填：**人工确认后**执行，执行前必须已跑 dry-run 并对齐改名映射。
-- 前置：atlas-migrations/20261104090000_studios_ehr_identity.sql 已 apply。
\pset pager off
BEGIN;

-- ① 精确同名认领：把 EHR 事业部 ID 回填到老行上（保住 studios.id → Key/角色/账单快照不乱）
UPDATE studios s
   SET ehr_department_id = a.department_id,
       source            = 'ehr',
       leader_name       = COALESCE(a.leader_name, ''),
       leader_passport   = COALESCE(a.leader_passport, ''),
       leader_employee_id= COALESCE(a.leader_employee_id, '')
  FROM ai_app_studios a
 WHERE a.name = s.name
   AND s.ehr_department_id IS NULL;

-- ② 改名认领（逐条确认后取消注释；示例：XLab 是 AI-Lab 的旧名）
-- UPDATE studios SET ehr_department_id = '<EHR事业部ID>', source = 'ehr'
--  WHERE id = '<老工作室uuid>' AND ehr_department_id IS NULL;

-- ③ EHR 里存在但人工隐藏：source 仍是 'ehr'（同步照常更新 name/负责人），只是不显示
UPDATE studios SET visible = false WHERE name IN ('元气工作室', '公共关系事业部');

-- ④ 老表独有的手工残留：不再属于任何同步来源，隐藏但不删行（快照还要 join）
UPDATE studios SET visible = false, source = 'manual'
 WHERE name IN ('test', '测试工作室AI', '互娱事业部', '创新事业部', '战略发展中心', '数字内容创新工作室')
   AND ehr_department_id IS NULL;

-- ⑤ 收尾自检：以下查询必须返回 0 行
--   SELECT * FROM studios WHERE source = 'ehr' AND ehr_department_id IS NULL;
COMMIT;
