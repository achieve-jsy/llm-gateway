-- EHR 工作室对账 dry-run：上线前人工过一遍，四分类输出。
-- 依赖：ai_app_studios（EHR 同步产物）与 studios（老表）。
--
-- 只用老列（name 做反连接），**不依赖**迁移 1 新增的 ehr_department_id/source/visible ——
-- 这样迁移还没 apply 时也能先跑对账。回填完成后再跑一次，① 应返回 0 行。
\pset pager off

-- ① 精确同名（预计 11 条）：可直接认领，回填 ehr_department_id 即可
SELECT s.id AS studio_id, s.name, a.department_id AS ehr_department_id
  FROM studios s
  JOIN ai_app_studios a ON a.name = s.name
 ORDER BY s.name;

-- ② 老表独有（预计 7 条）：EHR 里没有同名工作室，含疑似改名（XLab ↔ AI-Lab），需人工判定
SELECT s.id AS studio_id, s.name, s.created_at
  FROM studios s
 WHERE NOT EXISTS (SELECT 1 FROM ai_app_studios a WHERE a.name = s.name)
 ORDER BY s.name;

-- ③ EHR 独有（预计 4 条）：同步会新建这些工作室（新 id）
SELECT a.department_id, a.name, a.visible
  FROM ai_app_studios a
 WHERE NOT EXISTS (SELECT 1 FROM studios s WHERE s.name = a.name)
 ORDER BY a.name;

-- ④ 引用检查：这些工作室一旦隐藏，仍被角色/Key 引用（权限与快照不会断，但页面看不到）
SELECT s.name,
       count(DISTINCT ur.id) AS roles,
       count(DISTINCT k.id)  AS keys
  FROM studios s
  LEFT JOIN user_roles ur ON ur.scope_type = 'studio' AND ur.scope_id = s.id::text
  LEFT JOIN api_keys  k  ON k.studio_id = s.id::text
 GROUP BY s.name
HAVING count(DISTINCT ur.id) + count(DISTINCT k.id) > 0
 ORDER BY (count(DISTINCT ur.id) + count(DISTINCT k.id)) DESC;
