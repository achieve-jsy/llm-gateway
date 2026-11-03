-- 组织负责人多值列表：同步只追加/更新属性，永不删除（增量覆盖与保留，见 spec 2026-11-03）。
CREATE TABLE org_node_leaders (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_type   varchar(8)  NOT NULL,
    node_id     uuid        NOT NULL,
    name        varchar(64) NOT NULL,
    passport    varchar(64) NOT NULL DEFAULT '',
    employee_id varchar(64) NOT NULL DEFAULT '',
    source      varchar(8)  NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 同一节点同一人只留一行：身份 = employee_id > passport > name。
CREATE UNIQUE INDEX uniq_org_node_leader ON org_node_leaders (
    node_type, node_id,
    (CASE WHEN employee_id <> '' THEN employee_id WHEN passport <> '' THEN passport ELSE name END)
);
CREATE INDEX idx_org_node_leaders_node ON org_node_leaders (node_type, node_id);
