-- A5: Clean call_logs.dept_id and usage_daily.dept_id
-- Legacy format "dept:xxx" → pure UUID "xxx"
-- Empty strings remain empty (indicates no department)

-- Step 1: Clean call_logs.dept_id — remove "dept:" prefix
UPDATE call_logs
SET dept_id = SUBSTRING(dept_id FROM 6)
WHERE dept_id LIKE 'dept:%';

-- Step 2: Clean usage_daily.dept_id — same format issue
UPDATE usage_daily
SET dept_id = SUBSTRING(dept_id FROM 6)
WHERE dept_id LIKE 'dept:%';
