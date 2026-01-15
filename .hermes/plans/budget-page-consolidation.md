# Spec: 预算管理页面整合

## 目标

将所有预算相关的**查看和配置操作**集中到 `BudgetManagePage.vue` 页面内完成，不再跳转到组织架构页（`OrgTreePage`）。

**核心原则**：
1. **配置用抽屉**：点击"调整"等按钮 → 右侧弹出抽屉完成配置
2. **抽屉内嵌套**：抽屉内容太多时，可在抽屉内再弹窗/卡片进入下一级
3. **所有操作不离开当前页面**：用户从进入预算页到完成所有配置，不需要跳转

## 涉及文件

| 文件 | 角色 |
|------|------|
| `web/src/views/admin/BudgetManagePage.vue` | **主文件**，需要大量修改 |
| `web/src/components/budget/BudgetScopePanel.vue` | **参考**，其 drawer/dialog 逻辑需迁移 |
| `web/src/views/admin/org/OrgTreePage.vue` | **参考**，了解原有配置流程 |
| `web/src/api/budget.ts` | API 层，无需改动 |

## 当前问题清单

### P0：所有"调整"按钮都跳到 OrgTreePage

当前 BudgetManagePage 中以下操作全部 `router.push('/admin/org?...')`，需改为**原地抽屉配置**：

| 操作按钮 | 位置 | 当前行为 |
|----------|------|----------|
| 表格行"调整" | admin 工作室表格 | `goOrg(row)` → 跳转 |
| 表格行"调整" | studio_admin 部门表格 | `goOrg(row)` → 跳转 |
| 表格行"调整配额" | dept_admin 成员表格 ×2 | `goOrg(row)` → 跳转 |
| Header"进入组织治理" | 页面顶部 | `goOrg()` → 跳转 |
| 抽屉内"去调整 →" | admin 下钻抽屉 header | `goOrgFromDrawer()` → 跳转 |
| 抽屉内"去调整 →" | studio_admin 成员抽屉 header | `goOrgFromDeptDrawer()` → 跳转 |

### P1：缺少内联配置能力

BudgetScopePanel 中有但 BudgetManagePage 没有的：
- **调整工作室总额度**（drawer 表单）
- **调整部门池额度**（日常池/服务池，drawer 表单）
- **调整成员单独额度**（drawer 表单）
- **添加成员配额**（dialog 表单，搜索成员 + 设置额度）
- **还原成员到共享池**（confirm + API delete）

## 改造方案

### 1. 统一额度调整抽屉

**新增一个通用抽屉组件** `<BudgetEditDrawer>`，替代所有跳到 OrgTreePage 的入口：

```
触发：点击任意"调整"/"设置额度"按钮
抽屉内容：
├── 标题：显示 scope 名称（如"日常预算 · 产品部"）
├── 摘要卡片：已用/总额/使用率（只读）
├── 表单：月额度、周额度、日额度（el-input-number）
└── 保存按钮 → setBudgetBatch API
```

**适用范围**：
- 公司预算池（company）
- 工作室总额度（studio）
- 部门日常池（dept_daily）
- 部门服务池（dept_service）
- 成员单独额度（member）

### 2. 各角色的改造点

#### Admin 角色

**工作室表格操作列**：
- "查看部门"（已有抽屉，保留）
- ~~"调整"~~→ 改为"调整额度"，点击弹出 BudgetEditDrawer（scope_type=studio）

**下钻抽屉内**：
- ~~"去调整 →"~~→ 改为"调整额度"，点击弹出 BudgetEditDrawer（scope_type=studio）
- 部门行"调整" → 弹出 BudgetEditDrawer（scope_type=dept_daily 或 dept_service）
- 成员行 → 可展开查看，无需跳走

**侧边栏**：
- ~~"调整工作室预算"~~ → 改为"调整额度"，弹 drawer
- ~~"调整部门预算"~~ → 改为"调整额度"，弹 drawer

#### Studio Admin 角色

**部门表格操作列**：
- "查看成员"（已有抽屉，保留）
- ~~"调整"~~→ 改为"调整额度"，弹 BudgetEditDrawer（scope_type=dept_daily）

**成员抽屉内**：
- ~~"去调整 →"~~→ 改为"调整额度"，弹 drawer
- 每个成员卡片增加"调额"按钮 → 弹 BudgetEditDrawer（scope_type=member）

#### Dept Admin 角色

**成员表格**：
- "调整配额"列 → 弹 BudgetEditDrawer（scope_type=member）
- 新增"+ 设置额度"按钮 → 弹成员选择 Dialog（搜索成员 + 设额度）
- 新增"还原"操作 → confirm 弹窗 → deleteMemberBudget API

### 3. 抽屉内嵌套交互

当抽屉内容太多时（如 admin 下钻抽屉 → 部门列表 → 成员列表），采用**抽屉内卡片展开**的方式：

```
下钻抽屉（480px）：
├── 部门列表（卡片式）
│   ├── 部门A 卡片（可点击展开）
│   │   ├── 日常池 + 服务池 摘要（内嵌卡片）
│   │   │   └── 各自带"调额"按钮 → 弹 BudgetEditDrawer
│   │   └── 成员列表（展开后懒加载）
│   │       ├── 成员1：额度 + "调额"
│   │       ├── 成员2：额度 + "调额"
│   │       └── ...
│   └── 部门B 卡片 ...
```

**关键**：不再从抽屉里跳转到 OrgTreePage，而是在抽屉内完成所有下钻。

### 4. 表格列宽优化

#### Admin 工作室表格
```
列名         | min-width | max-width | 对齐    | 说明
-------------|-----------|-----------|---------|------
工作室       | 160px     | 220px     | left    | 名称 + scope_type 色点
已分配额度   | 100px     | 130px     | right   | 紧凑显示
已使用       | 160px     | 200px     | left    | 金额 + 进度条 + 百分比
日限/周限    | 120px     | 160px     | center  | 双行紧凑
操作         | 150px     | 150px     | center  | 两个按钮水平排列
```

#### Studio Admin 部门表格
```
列名         | min-width | max-width | 对齐    | 说明
-------------|-----------|-----------|---------|------
部门         | 140px     | 180px     | left    | 风险指示 + 名称
日常预算     | 180px     | 220px     | left    | 金额+进度+日/周限子行
服务预算     | 180px     | 220px     | left    | 同上
操作         | 150px     | 150px     | center  | 查看成员 + 调整额度
```

#### Dept Admin 成员表格
```
列名         | min-width | max-width | 对齐    | 说明
-------------|-----------|-----------|---------|------
成员         | 140px     | 180px     | left    | 风险点+名称+部门标签
月配额       | 80px      | 100px     | right   | 纯数字
月额度进度   | 160px     | 200px     | left    | 金额+进度条
近7天消耗    | 100px     | 120px     | right   | 纯数字
7天日均/日限 | 140px     | 180px     | left    | 双行+进度条
操作         | 120px     | 120px     | center  | 调额按钮
```

### 5. 新增组件

#### BudgetEditDrawer.vue（新建）

```
路径：web/src/components/budget/BudgetEditDrawer.vue

Props：
- visible: boolean (v-model)
- scopeType: 'company' | 'studio' | 'dept_daily' | 'dept_service' | 'member'
- scopeId: string
- title: string
- currentData?: { amount, weekly, daily, used } (预填表单)
- canEdit: boolean (false 时显示"申请扩容"按钮)

Events：
- update:visible
- saved (保存成功后触发，父组件刷新数据)

内部结构：
- el-drawer (size=380px, direction=rtl)
  - 标题：{{ title }}
  - 摘要区：已用/总额 + 使用率进度条
  - el-form (label-width=90px)
    - 月额度: el-input-number (min=0, precision=2)
    - 周额度: el-input-number (min=-1, precision=2, -1=不限)
    - 日额度: el-input-number (min=-1, precision=2, -1=不限)
  - Footer：取消 + 保存（type=primary, loading）

API: setBudgetBatch([{ scope_type, scope_id, amount_usd, weekly_amount_usd, daily_amount_usd }])
成功后: ElMessage.success('预算已更新') → emit('saved') → close
```

#### MemberQuotaDialog.vue（新建）

```
路径：web/src/components/budget/MemberQuotaDialog.vue

Props：
- visible: boolean (v-model)
- scopeId: string (部门ID)

Events：
- update:visible
- saved

内部结构：
- el-dialog (width=460px, close-on-click-modal=false)
  - el-form
    - 选择成员: el-select (remote, filterable) → orgStore.searchUsers()
    - 月限额: el-input-number (min=0, precision=2)
    - 周限额: el-input-number (min=-1, precision=2)
    - 日限额: el-input-number (min=-1, precision=2)
  - Footer: 取消 + 确认

API: setBudgetBatch([{ scope_type:'member', scope_id: userId, ... }])
```

### 6. BudgetManagePage 改造细节

#### Header 区域
```diff
- <el-button type="primary" @click="goOrg()">进入组织治理</el-button>
+ <!-- 删除跳转按钮，预算操作全部在当前页面完成 -->
+ <!-- 保留刷新按钮 -->
```

#### 所有 goOrg() 调用替换
```diff
- goOrg(row)  →  打开 BudgetEditDrawer
- goOrgFromDrawer()  →  打开 BudgetEditDrawer（不关闭下钻抽屉）
- goOrgFromDeptDrawer()  →  打开 BudgetEditDrawer（不关闭成员抽屉）
```

#### 新增响应式数据
```typescript
// 额度调整抽屉
const editDrawerVisible = ref(false)
const editDrawerScope = ref<{ type: string; id: string; title: string; data?: any } | null>(null)

// 成员配额弹窗
const memberQuotaDialogVisible = ref(false)
const memberQuotaDeptId = ref('')
```

#### Dept Admin 成员表格操作列改造
```diff
  <el-table-column label="操作" width="120" align="center">
    <template #default="{ row }">
-     <el-button type="primary" plain @click="goOrg(row)">调整配额</el-button>
+     <el-button type="primary" plain size="small" @click="openEditDrawer('member', row.scope_id, row.label, row)">调额</el-button>
+     <el-button v-if="row.amount_usd > 0" type="danger" plain size="small" @click="resetMemberToPool(row)">还原</el-button>
    </template>
  </el-table-column>
```

#### 新增"设置额度"入口（Dept Admin 成员表格上方）
```html
<el-button type="primary" @click="openMemberQuotaDialog(selectedDeptId)">
  + 设置额度
</el-button>
```

### 7. "还原到池"功能

```typescript
async function resetMemberToPool(row: any) {
  try {
    await ElMessageBox.confirm(
      `确定将 ${row.label} 的额度还原为共享池？还原后该成员将使用部门共享额度。`,
      '还原确认',
      { type: 'warning' }
    )
    await http.delete('/api/v1/budgets', {
      params: { scope_type: 'member', scope_id: row.scope_id }
    })
    ElMessage.success('已还原到共享池')
    await load()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
  }
}
```

## 不做的事情

1. **不修改 OrgTreePage**：保留原样，只是 BudgetManagePage 不再跳过去
2. **不修改 BudgetScopePanel**：它是 OrgTreePage 的子组件，不删除
3. **不改路由**：`/admin/org` 路由保留，只是预算页不再依赖它
4. **不改后端 API**：所有需要的 API 已经存在
5. **不改变已有抽屉的逻辑**：admin 下钻抽屉和 studio_admin 成员抽屉的展开/折叠逻辑保持不变，只是把其中的跳转按钮改为弹配置抽屉

## 验证标准

1. ✅ Admin：从工作室表格点击"调整额度" → 弹抽屉 → 保存成功 → 数据刷新
2. ✅ Admin：下钻抽屉内部门行"调额" → 弹抽屉 → 保存成功
3. ✅ Studio Admin：部门表格"调整额度" → 弹抽屉 → 保存成功
4. ✅ Studio Admin：成员抽屉内成员"调额" → 弹抽屉 → 保存成功
5. ✅ Dept Admin：成员表格"调额" → 弹抽屉 → 保存成功
6. ✅ Dept Admin："+ 设置额度" → 弹弹窗 → 搜索成员 → 保存成功
7. ✅ Dept Admin：成员"还原" → 确认 → 删除成功
8. ✅ 所有操作完成后不离开 BudgetManagePage
9. ✅ 表格列宽合理，无文字截断
10. ✅ 移动端/小屏适配正常
