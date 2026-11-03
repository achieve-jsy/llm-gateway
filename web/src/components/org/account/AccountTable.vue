<template>
  <el-table :data="accounts" v-loading="loading" stripe row-key="user_id" size="default">
    <el-table-column label="姓名" min-width="190">
      <template #default="{ row }">
        <div class="name-cell">
          <div class="avatar">{{ row.name?.charAt(0) || '?' }}</div>
          <div class="name-main">
            <div class="name-line">
              <span class="name">{{ row.name || row.user_id }}</span>
              <RoleBadge :role="row.primary_role" />
              <!-- 一人多角色：主徽标取最高权限，其余折叠进 tooltip -->
              <el-tooltip v-if="otherRoles(row).length > 0" placement="top" :show-after="100">
                <template #content>
                  <div v-for="r in otherRoles(row)" :key="r.role + r.scope_id" class="tip-line">
                    {{ roleLabel(r.role) }} · {{ r.scope_name || '全公司' }}
                  </div>
                </template>
                <el-tag size="small" type="info" effect="plain" class="more-roles">
                  +{{ otherRoles(row).length }}
                </el-tag>
              </el-tooltip>
            </div>
          </div>
        </div>
      </template>
    </el-table-column>

    <el-table-column label="工号" width="110">
      <template #default="{ row }">
        <span class="mono">{{ row.employee_id || '—' }}</span>
      </template>
    </el-table-column>

    <el-table-column label="岗位" min-width="130">
      <template #default="{ row }">
        <span>{{ row.job_title || '—' }}</span>
      </template>
    </el-table-column>

    <el-table-column label="所属团队" min-width="150">
      <template #default="{ row }">
        <div class="team-cell">
          <span>{{ row.dept_name || '—' }}</span>
          <span v-if="row.studio_name" class="sub">{{ row.studio_name }}</span>
        </div>
      </template>
    </el-table-column>

    <el-table-column label="邮箱" min-width="180">
      <template #default="{ row }">
        <span class="sub">{{ row.email || '—' }}</span>
      </template>
    </el-table-column>

    <el-table-column label="创建时间" width="120">
      <template #default="{ row }">
        <span class="sub">{{ (row.created_at || '').slice(0, 10) || '—' }}</span>
      </template>
    </el-table-column>

    <el-table-column label="操作" width="100" align="center" fixed="right">
      <template #default="{ row }">
        <el-button link type="primary" size="small" data-test="set-role" @click="emit('set-role', row)">
          设置角色
        </el-button>
      </template>
    </el-table-column>

    <template #empty>
      <el-empty :description="emptyText" :image-size="70" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { AccountRow, RoleGrantItem } from '@/types/org'
import RoleBadge from './RoleBadge.vue'
import { roleLabel } from './roles'

withDefaults(defineProps<{
  accounts: AccountRow[]
  loading?: boolean
  emptyText?: string
}>(), { loading: false, emptyText: '暂无账号' })

const emit = defineEmits<{ (e: 'set-role', row: AccountRow): void }>()

// 主角色之外的全部授权（含同角色不同范围），用于折叠展示
function otherRoles(row: AccountRow): RoleGrantItem[] {
  const list = row.roles || []
  const idx = list.findIndex(r => r.role === row.primary_role)
  return idx >= 0 ? [...list.slice(0, idx), ...list.slice(idx + 1)] : list
}
</script>

<style scoped>
.name-cell { display: flex; align-items: center; gap: 10px; }
.avatar {
  width: 30px; height: 30px; border-radius: 50%;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary-dark-2);
  display: flex; align-items: center; justify-content: center;
  font-size: 13px; font-weight: 600; flex-shrink: 0;
}
.name-main { min-width: 0; }
.name-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.name { font-size: 14px; font-weight: 500; }
.more-roles { cursor: default; }
.tip-line { line-height: 1.7; }
.team-cell { display: flex; flex-direction: column; }
.sub { font-size: 12px; color: var(--el-text-color-secondary); }
.mono { font-family: var(--el-font-family-monospace, monospace); font-size: 12px; }
</style>
