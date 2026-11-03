<template>
  <el-table
    :data="members"
    row-key="id"
    stripe
    size="small"
    v-loading="loading"
  >
    <!-- 行展开：成员配置三卡 -->
    <el-table-column type="expand" width="34">
      <template #default="{ row }">
        <MemberConfigPanel
          :quota="quotaOf(row)"
          :budget="configOf(row).budget"
          :entitlement="configOf(row).entitlement"
          @edit-quota="$emit('edit-quota', row)"
          @manage-budget="$emit('manage-budget', row)"
          @manage-entitlement="$emit('manage-entitlement', row)"
        />
      </template>
    </el-table-column>

    <!-- 姓名 + 角色徽标 -->
    <el-table-column label="姓名" min-width="190">
      <template #default="{ row }">
        <div class="member-cell">
          <el-badge :is-dot="row.has_conflict" :offset="[-2, 2]" type="danger">
            <div class="member-avatar">{{ row.name?.charAt(0) || '?' }}</div>
          </el-badge>
          <span class="member-name">{{ row.name }}</span>
          <el-tag v-if="row.is_dept_admin" size="small" type="warning" effect="plain">团队管理员</el-tag>
          <el-tag v-else size="small" type="info" effect="plain">员工</el-tag>
        </div>
      </template>
    </el-table-column>

    <el-table-column label="岗位" min-width="110">
      <template #default="{ row }">
        <span v-if="row.job_title">{{ row.job_title }}</span>
        <span v-else class="text-placeholder">—</span>
      </template>
    </el-table-column>

    <el-table-column label="配额" width="180">
      <template #default="{ row }">
        <span :class="isQuotaOverride(quotaOf(row)) ? 'cell-strong' : 'text-placeholder'">
          {{ quotaText(quotaOf(row)) }}
        </span>
      </template>
    </el-table-column>

    <el-table-column label="预算规则" min-width="170">
      <template #default="{ row }">
        <span :class="configOf(row).budget && configOf(row).budget!.source === 'member' ? 'cell-strong' : 'text-placeholder'">
          {{ budgetText(configOf(row).budget) }}
        </span>
      </template>
    </el-table-column>

    <el-table-column label="模型授权" width="130">
      <template #default="{ row }">
        <span :class="configOf(row).entitlement && configOf(row).entitlement!.group_count > 0 ? 'cell-strong' : 'text-placeholder'">
          {{ entitlementText(configOf(row).entitlement) }}
        </span>
      </template>
    </el-table-column>

    <el-table-column v-if="canManage" label="操作" width="140" align="center">
      <template #default="{ row }">
        <el-button link type="primary" size="small" @click.stop="$emit('edit-quota', row)">配额</el-button>
        <el-button link type="primary" size="small" @click.stop="$emit('view-usage', row)">查看用量</el-button>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import MemberConfigPanel from './MemberConfigPanel.vue'
import { budgetText, configOf, entitlementText, isQuotaOverride, quotaText } from './memberFormat'
import type { MemberRow, QuotaLike } from './memberFormat'

const props = defineProps<{
  members: MemberRow[]
  // 现有 api/quota.ts 的 getUserQuota 结果，优先于 members[].config.quota
  quotaMap?: Record<string, QuotaLike>
  loading?: boolean
  canManage?: boolean
}>()

defineEmits<{
  (e: 'edit-quota', member: MemberRow): void
  (e: 'view-usage', member: MemberRow): void
  (e: 'manage-budget', member: MemberRow): void
  (e: 'manage-entitlement', member: MemberRow): void
}>()

function quotaOf(row: MemberRow): QuotaLike | undefined {
  return props.quotaMap?.[row.id] ?? configOf(row).quota ?? undefined
}
</script>

<style scoped>
.member-cell { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.member-avatar {
  width: 26px; height: 26px; border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; flex-shrink: 0;
}
.member-name { font-size: 13px; }
.cell-strong { font-size: 13px; color: var(--el-text-color-primary); }
.text-placeholder { color: var(--el-text-color-placeholder); font-size: 13px; }
</style>
