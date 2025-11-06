-- 007_create_model_tags.sql
-- 模型标签功能：独立标签表 + 多对多关联表

-- 标签定义表
CREATE TABLE IF NOT EXISTS model_tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL,
    color VARCHAR(7) NOT NULL DEFAULT '#2BA8A2',
    description VARCHAR(200) DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_model_tags_name_unique ON model_tags (name);
CREATE INDEX IF NOT EXISTS idx_model_tags_sort ON model_tags (sort_order DESC);

COMMENT ON TABLE model_tags IS '模型标签定义表，管理员自由创建，用于模型分类和筛选';
COMMENT ON COLUMN model_tags.name IS '标签名称（唯一）';
COMMENT ON COLUMN model_tags.color IS '标签颜色（十六进制，前端展示）';
COMMENT ON COLUMN model_tags.sort_order IS '排序权重（越大越靠前）';

-- 模型-标签关联表（多对多）
CREATE TABLE IF NOT EXISTS model_tag_relations (
    model_id UUID NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES model_tags(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (model_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_model_tag_relations_model ON model_tag_relations (model_id);
CREATE INDEX IF NOT EXISTS idx_model_tag_relations_tag ON model_tag_relations (tag_id);

COMMENT ON TABLE model_tag_relations IS '模型与标签的多对多关联表';

-- 预置一些常用标签
INSERT INTO model_tags (name, color, description, sort_order) VALUES
('文本', '#2BA8A2', '支持纯文本对话', 100),
('视觉', '#5DADE2', '支持图片输入（多模态视觉）', 90),
('音频', '#9B59B6', '支持音频输入/输出', 80),
('代码', '#1E8C86', '擅长代码生成与理解', 70),
('推理', '#FFD23F', '支持深度推理/思维链', 60),
('联网', '#27AE60', '支持实时联网搜索', 50),
('长文本', '#E67E22', '支持超长上下文（>32K）', 40),
('函数调用', '#EF6C4A', '支持 Function Calling / Tool Use', 30),
('多模态', '#8E44AD', '同时支持多种输入输出模态', 20)
ON CONFLICT (name) DO NOTHING;
