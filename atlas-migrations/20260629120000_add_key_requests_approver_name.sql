-- 冗余审批人姓名，供 Key 申请列表直接展示（与 requester_name 同语义）
ALTER TABLE key_requests ADD COLUMN IF NOT EXISTS approver_name VARCHAR(128);

COMMENT ON COLUMN key_requests.approver_name IS '审批人姓名冗余（供列表直接展示）';
