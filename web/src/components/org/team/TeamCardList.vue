<template>
  <!-- 选中工作室下的下属团队卡（原型图3 左栏）：团队名 / 人数 / EHR 负责人 / 团队管理员 -->
  <div class="team-cards">
    <div class="team-cards__title">
      下属团队
      <span class="team-cards__count">{{ teams.length }}</span>
    </div>

    <div
      v-for="t in teams"
      :key="t.dept_id"
      :class="['team-card', { 'is-active': t.dept_id === activeDeptId }]"
      :data-test="`team-card-${t.dept_id}`"
      @click="$emit('select', t)"
    >
      <div class="team-card__head">
        <span class="team-card__name" :title="t.dept_name">{{ t.dept_name }}</span>
        <span class="team-card__members">{{ t.member_count }} 人</span>
      </div>

      <div class="team-card__row">
        <span class="team-card__label">负责人</span>
        <span class="team-card__value" :title="leadersText(t.leaders)">{{ leadersText(t.leaders) }}</span>
        <el-button
          link type="primary" size="small" class="team-card__add"
          title="加负责人（花名册工号/姓名）"
          @click.stop="$emit('add-leader', t)"
        >+</el-button>
      </div>

      <div class="team-card__row">
        <span class="team-card__label">管理员</span>
        <span class="team-card__value" :title="adminNames(t)">
          {{ t.admins.length ? adminNames(t) : '未设置' }}
        </span>
        <el-button
          v-if="canManage" link type="primary" size="small" class="team-card__add"
          @click.stop="$emit('set-admin', t)"
        >设置</el-button>
      </div>
    </div>

    <div v-if="teams.length === 0" class="team-cards__empty">该工作室暂无团队</div>
  </div>
</template>

<script setup lang="ts">
import type { TeamCard } from '@/types/org'
import { leadersText } from './memberFormat'

defineProps<{
  teams: TeamCard[]
  activeDeptId?: string
  canManage?: boolean
}>()

defineEmits<{
  (e: 'select', team: TeamCard): void
  (e: 'set-admin', team: TeamCard): void
  (e: 'add-leader', team: TeamCard): void
}>()

function adminNames(t: TeamCard): string {
  return t.admins.map(a => a.name).join('、')
}
</script>

<style scoped>
.team-cards {
  padding: 8px 8px 4px;
  border-top: 1px solid var(--el-border-color-lighter);
  margin-top: 6px;
}
.team-cards__title {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 6px 8px;
  font-size: 12px;
  font-weight: 700;
  color: var(--el-text-color-secondary);
  letter-spacing: .3px;
}
.team-cards__count {
  font-weight: 500;
  color: var(--el-text-color-placeholder);
}
.team-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  margin-bottom: 6px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  cursor: pointer;
  transition: border-color .15s, background .15s;
}
.team-card:hover { background: var(--el-fill-color-light); }
.team-card.is-active {
  border-color: var(--el-color-primary-light-5);
  background: var(--el-color-primary-light-9);
}
.team-card__head { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.team-card__name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.team-card__members { font-size: 11px; color: var(--el-text-color-placeholder); flex-shrink: 0; }
.team-card__row { display: flex; align-items: center; gap: 4px; min-width: 0; }
.team-card__label { font-size: 11px; color: var(--el-text-color-placeholder); flex-shrink: 0; }
.team-card__value {
  font-size: 11px;
  color: var(--el-text-color-regular);
  flex: 1;
  min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.team-card__add { padding: 0 2px; font-size: 11px; }
.team-cards__empty {
  padding: 12px 6px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  text-align: center;
}
</style>
