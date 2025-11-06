# [PROJECT_NAME] 项目宪章

## 项目配置

<!--
  项目级元信息，是 xflow 各 /xf/* 命令的行为分支开关。
  一个仓库只能有一个 profile。此字段由 xf-profile.mjs 解析，所有命令读取此字段决定行为。

  取值：
    - general          通用模式（默认），多模块协作的 SDD 流程
    - xuanwu-activity  玄武游戏活动模式，单模块 activity 工程，/xf/split 跳过、/xf/product 简化

  修改此字段视为重大配置变更，会影响所有新发起的变更流程；已有模块不会自动迁移。
-->

- `project_profile`: general

---

## 核心原则

### 原则一：[PRINCIPLE_1_NAME]

[PRINCIPLE_1_DESCRIPTION]

**约束条件**：
- [具体的不可违反的规则]

### 原则二：[PRINCIPLE_2_NAME]

[PRINCIPLE_2_DESCRIPTION]

**约束条件**：
- [具体的不可违反的规则]

<!-- 可根据项目需要增减原则数量，建议 2-5 条 -->

---

## 技术方向

<!-- 只记录已确定的技术方向，不需要覆盖所有领域。未确定的在后续 /xf/product 和 /xf/design 阶段决定 -->

| 领域 | 选择 | 说明 |
| :--- | :--- | :--- |
| [已确定的领域] | [选择] | [理由或来源] |

---

## 治理

- 本宪章是项目的最高准则，所有需求设计、技术方案和代码实现必须遵守
- 宪章修订需通过 `/xf/constitution` 命令执行，记录修订原因
- 修订采用语义化版本：MAJOR（原则重大变更）、MINOR（新增板块）、PATCH（措辞修正）
- 与宪章冲突的设计/实现必须先修订宪章或获得明确豁免

**版本**：[VERSION] | **创建日期**：[CREATED_DATE] | **最后修订**：[LAST_AMENDED_DATE]
