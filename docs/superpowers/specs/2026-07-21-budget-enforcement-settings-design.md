# 预算拦截开关进系统设置（设计）

日期：2026-07-21

## 目标

平台管理员可在 `/admin/settings`「预算拦截」tab 热切换拦截开关，写入 `system_configs` 后立即重发 Redis budget tier snapshot，gateway 热加载，无需重启。

## 范围

可配置四项：

- `positive_enforcement_enabled` 正向拦截
- `zero_block_enabled` 零额度拦截
- `shadow_enabled` 影子模式
- `v2_total_enforcement_enabled` v2 总额拦截

非目标：UI 不暴露 master / accounting / legacy_total_write；不进 `/settings/public`。

## 行为

1. 权威源：DB；首次无记录时用部署 cfg/env 种子写入。
2. 保存：校验影子 ⊕ 正向互斥；若开正向/影子且 `master_enabled=false` 则 400；开正向/影子时 snapshot 内自动打开 `accounting_enabled`。
3. 保存成功后 `UpdateEnforcement` + `MarkDirtyAndPublish`。

## UI

设置页新 tab，四个 `el-switch`；开正向拦截二次确认。
