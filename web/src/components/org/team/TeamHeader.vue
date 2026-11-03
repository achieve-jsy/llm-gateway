<template>
  <!-- 工作室工作台头部：工作室管理员（RBAC，可授可撤）+ 负责人（EHR，只读）+ 当前团队一行 -->
  <div class="wb-header">
    <div class="wb-header__breadcrumb">
      公司 <el-icon class="crumb-arrow"><ArrowRight /></el-icon> {{ studioName }}
      <template v-if="team">
        <el-icon class="crumb-arrow"><ArrowRight /></el-icon>
        {{ team.name }}
      </template>
    </div>

    <div class="wb-header__title">
      <el-icon class="title-icon"><OfficeBuilding /></el-icon>
      <span class="wb-header__name">{{ studioName }}</span>
      <span v-if="memberCount != null" class="wb-header__count">{{ memberCount }} 人</span>
      <el-tag v-if="team && team.status && team.status !== 'active'" type="warning" size="small">待归属</el-tag>
      <code v-if="team" class="wb-header__dept-id">{{ team.deptId }}</code>
    </div>

    <!-- 工作室管理员 -->
    <div class="wb-row">
      <span class="wb-row__label">工作室管理员</span>
      <template v-if="admins.length">
        <span v-for="a in admins" :key="a.user_id" class="wb-chip">
          {{ a.name }}
          <el-popconfirm
            v-if="canManage"
            :title="`确认卸任「${a.name}」的工作室管理员？`"
            confirm-button-text="卸任"
            confirm-button-type="danger"
            @confirm="$emit('revoke-studio-admin', a)"
          >
            <template #reference>
              <el-button link type="danger" size="small">卸任</el-button>
            </template>
          </el-popconfirm>
        </span>
      </template>
      <span v-else class="wb-row__muted">未设置管理员</span>
      <el-button v-if="canManage" link type="primary" size="small" @click="$emit('set-studio-admin')">
        设置工作室管理员
      </el-button>
    </div>

    <!-- 工作室负责人：来自 EHR，只读、多值、只增不删 -->
    <div class="wb-row">
      <span class="wb-row__label">负责人<span class="wb-row__hint">（EHR 同步）</span></span>
      <span class="wb-row__value" :title="leadersText(leaders)">{{ leadersText(leaders) }}</span>
      <el-button
        v-if="canManage" link type="primary" size="small"
        title="加负责人（花名册工号/姓名）" @click="$emit('add-studio-leader')"
      >+</el-button>
    </div>

    <!-- 当前团队：名称 / 人数 / 团队管理员 / 负责人 -->
    <div v-if="team" class="wb-team">
      <div class="wb-team__head">
        <el-icon class="title-icon"><Folder /></el-icon>
        <span class="wb-team__name">{{ team.name }}</span>
        <span class="wb-team__count">{{ team.memberCount ?? 0 }} 人</span>
        <el-button
          v-if="canManageTeam" link type="primary" size="small"
          @click="$emit('set-team-admin')"
        >设置团队管理员</el-button>
      </div>

      <div class="wb-row">
        <span class="wb-row__label">团队管理员</span>
        <template v-if="team.admins.length">
          <span v-for="a in team.admins" :key="a.user_id" class="wb-chip">
            {{ a.name }}
            <el-popconfirm
              v-if="canManageTeam"
              :title="`确认卸任「${a.name}」的团队管理员？`"
              confirm-button-text="卸任"
              confirm-button-type="danger"
              @confirm="$emit('revoke-team-admin', a)"
            >
              <template #reference>
                <el-button link type="danger" size="small">卸任</el-button>
              </template>
            </el-popconfirm>
          </span>
        </template>
        <span v-else class="wb-row__muted">未设置管理员</span>
      </div>

      <div class="wb-row">
        <span class="wb-row__label">负责人<span class="wb-row__hint">（EHR 同步）</span></span>
        <span class="wb-row__value" :title="leadersText(team.leaders)">{{ leadersText(team.leaders) }}</span>
        <el-button
          v-if="canManageTeam" link type="primary" size="small"
          title="加负责人（花名册工号/姓名）" @click="$emit('add-team-leader')"
        >+</el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArrowRight, Folder, OfficeBuilding } from '@element-plus/icons-vue'
import type { AccountBrief, OrgLeaderBrief } from '@/types/org'
import { leadersText } from './memberFormat'
import type { TeamHeaderTeam } from './memberFormat'

defineProps<{
  studioName: string
  memberCount?: number | null
  admins: AccountBrief[]
  leaders: OrgLeaderBrief[]
  canManage?: boolean
  team?: TeamHeaderTeam | null
  canManageTeam?: boolean
}>()

defineEmits<{
  (e: 'set-studio-admin'): void
  (e: 'revoke-studio-admin', admin: AccountBrief): void
  (e: 'add-studio-leader'): void
  (e: 'set-team-admin'): void
  (e: 'revoke-team-admin', admin: AccountBrief): void
  (e: 'add-team-leader'): void
}>()
</script>

<style scoped>
.wb-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 20px 24px 0;
  flex-shrink: 0;
}
.wb-header__breadcrumb {
  display: flex; align-items: center; gap: 4px; flex-wrap: wrap;
  font-size: 12px; color: var(--el-text-color-secondary);
}
.crumb-arrow { font-size: 10px; }
.wb-header__title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 18px; font-weight: 700; color: var(--el-text-color-primary);
}
.title-icon { color: var(--el-color-primary); font-size: 16px; }
.wb-header__count {
  font-size: 12px; font-weight: 500;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border-radius: 10px;
  padding: 1px 8px;
}
.wb-header__dept-id {
  font-family: monospace; font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  padding: 1px 6px; border-radius: 4px;
}

.wb-row {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13px; min-height: 24px;
}
.wb-row__label { color: var(--el-text-color-secondary); font-size: 12px; }
.wb-row__hint { color: var(--el-text-color-placeholder); font-size: 11px; }
.wb-row__value { color: var(--el-text-color-primary); font-size: 13px; }
.wb-row__muted { color: var(--el-text-color-placeholder); font-size: 12px; }

.wb-chip {
  display: inline-flex; align-items: center; gap: 2px;
  padding: 1px 8px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  font-size: 12px;
}

.wb-team {
  display: flex; flex-direction: column; gap: 6px;
  margin-top: 4px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-left: 3px solid var(--el-color-primary);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
}
.wb-team__head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.wb-team__name { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.wb-team__count { font-size: 12px; color: var(--el-text-color-secondary); }

@media (max-width: 900px) {
  .wb-header { padding: 16px 16px 0; }
  .wb-header__title { font-size: 16px; }
}
</style>
