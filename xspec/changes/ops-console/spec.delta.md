# ops-console 需求变更 — CHG-20260603-001

> 基线：xspec/modules/ops-console/spec.md (v1.0)

## 变更摘要

新增「个人用量页」和「全局用量看板」两个页面，让员工和管理员直观查看用量与成本估算（含分桶成本和延迟指标）；同时简化 Catalog 管理 UI——删除「价格差异」tab，模型编辑增加 price_mode 开关和多桶定价输入。

## 新增需求

### US-NEW-01：个人用量页（P0）

作为员工，我想在管理后台查看自己的用量详情（总量/按模型拆分/调用次数/成本估算）和用量趋势（按日/周/月切换），以便了解自己的模型使用情况和花费。

- 总量概览卡片：总请求数、总 Token 数、总估算成本
- 按模型拆分表格：各模型的请求数 / prompt tokens / completion tokens / reasoning tokens / 缓存命中 / 成本估算（含分桶子项）
- 延迟指标卡片：平均 TTFT / P95 TTFT / 平均端到端延迟 / P95 延迟
- 用量趋势图：支持按日/周/月粒度切换，展示 Token 消耗和成本走势
- 延迟趋势图：展示 TTFT / 端到端延迟随时间的变化趋势
- 高消耗定位：高亮消耗最大的模型或时段
- 数据范围：仅展示当前用户自己的数据（调用 GET /v1/usage + user_id 过滤）

### US-NEW-02：全局用量看板（P1）

作为管理员，我想查看全公司的用量概览（雏形），以便掌握资源消耗全局情况。

- 全局概览卡片：全公司总请求数、总 Token 数、总估算成本
- 按模型维度排行：各模型的消耗占比
- 按部门维度排行：各部门的消耗占比（dept_id 维度）
- 时间范围选择：支持选择日期区间
- 数据范围：管理员可见全量数据

### US-NEW-03：Catalog UI 简化——删除价格差异 Tab（P1）

作为管理员，Catalog 管理页面移除「价格差异」tab（含待补价、已偏离、apply-diff 操作），简化为仅保留「目录同步」和「模型对应」两个步骤。

- 删除：PriceDiff / ApplyDiff 相关页面组件
- 保留：同步按钮 + 同步状态展示
- 保留：auto-bind / batch-bind 操作

### US-NEW-04：模型编辑增加 price_mode 开关（P1）

作为管理员，在模型创建/编辑对话框中，增加「定价模式」选项：跟随目录（默认）/ 手动填写。选择手动时展示多桶定价输入框。

- 默认选中「跟随目录」，此时价格输入区隐藏或只读展示 catalog 提供的多桶价格
- 切换为「手动」时展示四桶定价输入：Prompt 价 / Completion 价 / Reasoning 价（可选）/ Cached 价（可选）
- Prompt 价和 Completion 价必填，Reasoning 价和 Cached 价选填（留空则降级使用 Completion 价）
- 价格单位标注为 USD / 1M tokens

## 修改需求

- 原 spec §5「页面流与路由结构」→ 新增路由：
  - `/admin/usage` — 个人用量页
  - `/admin/usage/global` — 全局用量看板（管理员）
- 原 spec §6.4「Key 管理」中 US-05「查看自己的用量统计」→ 用量入口从 Key 详情页独立为顶级页面「个人用量页」，Key 详情页保留简化的用量摘要并链接到完整页面

## 新增验收标准

| 编号 | Given（前置状态） | When（触发动作） | Then（预期结果） | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| AC-NEW-01 | Given 员工有过去 7 天的调用记录 | When 访问个人用量页 | Then 展示总量卡片（请求数/Token/成本）和按模型拆分表格 | P0 |
| AC-NEW-02 | Given 员工在个人用量页 | When 切换趋势图粒度为「按周」 | Then 趋势图按周聚合展示 Token 和成本走势 | P0 |
| AC-NEW-03 | Given 员工在个人用量页 | When 数据加载中 | Then 展示 loading 状态；加载完成后即时渲染 | P0 |
| AC-NEW-04 | Given 管理员访问全局看板 | When 选择日期范围为本月 | Then 展示全公司该月总量 + 按模型/部门维度排行 | P1 |
| AC-NEW-05 | Given 普通员工尝试访问全局看板路由 | When 路由守卫检查 | Then 跳转到个人用量页（无权限提示） | P1 |
| AC-NEW-06 | Given 管理员在 Catalog 页面 | When 查看 tab 列表 | Then 不再有「价格差异」tab | P1 |
| AC-NEW-07 | Given 管理员编辑模型 | When 切换 price_mode 为「手动」 | Then 展示 input_price / output_price 输入框，必填校验 | P1 |
| AC-NEW-08 | Given 个人用量页无数据 | When 首次访问 | Then 展示空状态引导（非报错） | P0 |
| AC-NEW-09 | Given 员工在个人用量页 | When 查看延迟指标卡片 | Then 展示 avg_ttft / p95_ttft / avg_duration / p95_duration | P0 |
| AC-NEW-10 | Given 员工在个人用量页 | When 切换到延迟趋势图 | Then 展示 TTFT 和端到端延迟的时间走势 | P1 |
| AC-NEW-11 | Given 管理员编辑模型并切换到 manual 模式 | When 仅填写 Prompt/Completion 价，Reasoning 价留空 | Then 保存成功，Reasoning 价在取价时标记降级 | P1 |

## 修改验收标准

- 原 AC-06（员工在 Key 用量页查看统计图表）→ 用量入口升级为独立的个人用量页，Key 详情页保留简化摘要 + 跳转链接
