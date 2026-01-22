<template>
  <div class="group-detail-page">
    <!-- 顶部面包屑 + 标题 -->
    <div class="page-header">
      <div class="header-breadcrumb">
        <el-button link @click="router.push('/admin/groups')">
          <el-icon><ArrowLeft /></el-icon> 模型组
        </el-button>
        <span class="bc-sep">/</span>
        <span class="bc-current">{{ group?.name || '加载中...' }}</span>
      </div>
      <div class="header-meta" v-if="group">
        <el-tag :type="group.enabled ? 'success' : 'info'" size="small">{{ group.enabled ? '启用' : '禁用' }}</el-tag>
        <el-tag type="info" size="small">{{ strategyLabel(group.strategy) }}</el-tag>
      </div>
    </div>

    <div v-if="pageLoading" v-loading="true" style="height: 200px" />
    <div v-else-if="!group" class="state-error">
      <p>模型组不存在或无权访问</p>
      <el-button @click="router.push('/admin/groups')">返回列表</el-button>
    </div>

    <el-tabs v-else v-model="activeTab" class="detail-tabs">
      <!-- Tab 1: 基本信息 -->
      <el-tab-pane label="基本信息" name="info">
        <el-descriptions :column="2" border size="small" class="info-desc">
          <el-descriptions-item label="名称">{{ group.name }}</el-descriptions-item>
          <el-descriptions-item label="策略">{{ strategyLabel(group.strategy) }}</el-descriptions-item>
          <el-descriptions-item label="模型数量">{{ group.model_count }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ group.enabled ? '启用' : '禁用' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间" :span="2">{{ group.created_at }}</el-descriptions-item>
        </el-descriptions>
      </el-tab-pane>

      <!-- Tab 2: 模型成员 -->
      <el-tab-pane label="模型成员" name="models">
        <div v-loading="modelsLoading">
          <el-table :data="models" stripe size="small" empty-text="暂无模型成员">
            <el-table-column label="模型名称" prop="model_name" min-width="200" />
            <el-table-column label="厂商" prop="provider_name" width="120" />
            <el-table-column label="权重" prop="weight" width="80" align="center" />
            <el-table-column label="优先级" prop="priority" width="80" align="center" />
            <el-table-column label="复杂度档位" prop="complexity_tier" width="110" align="center">
              <template #default="scope">
                <el-tag v-if="scope.row.complexity_tier" size="small">{{ scope.row.complexity_tier }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <!-- Tab 3: 授权管理 -->
      <el-tab-pane label="授权管理" name="entitlements">
        <div class="entitlement-section">
          <!-- 开放模式切换 -->
          <div class="open-mode-row">
            <div>
              <div class="om-title">开放模式</div>
              <div class="om-desc">公开：所有部门默认可用；受控：需单独授权</div>
            </div>
            <el-switch
              v-model="isPublic"
              active-text="公开"
              inactive-text="受控"
              :loading="modeSwitching"
              @change="onModeChange"
            />
          </div>

          <template v-if="!isPublic">
            <!-- 工作室授权 -->
            <div class="entitle-block">
              <div class="entitle-block-head">
                <span>工作室授权</span>
                <el-button size="small" type="primary" plain @click="openGrant('studio')">+ 添加</el-button>
              </div>
              <el-table :data="studioEntitlements" stripe size="small" empty-text="暂无工作室授权">
                <el-table-column label="工作室 ID" prop="scope_id" min-width="160" />
                <el-table-column label="添加时间" prop="created_at" width="160" />
                <el-table-column label="操作" width="80" align="center">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="handleRevoke(row)">撤销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <!-- 部门授权 -->
            <div class="entitle-block">
              <div class="entitle-block-head">
                <span>部门开通</span>
                <el-button size="small" type="primary" plain @click="openGrant('dept')">+ 添加</el-button>
              </div>
              <el-table :data="deptEntitlements" stripe size="small" empty-text="暂无部门开通">
                <el-table-column label="部门 ID" prop="scope_id" min-width="160" />
                <el-table-column label="添加时间" prop="created_at" width="160" />
                <el-table-column label="操作" width="80" align="center">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="handleRevoke(row)">撤销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </template>
          <div v-else class="public-hint">
            <el-icon><InfoFilled /></el-icon>
            公开模式下，所有部门默认可见可用，无需单独授权。
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 授权弹窗 -->
    <el-dialog v-model="grantVisible" title="添加授权" width="380px">
      <el-form label-width="80px">
        <el-form-item label="类型">
          <el-tag>{{ grantForm.scope_type === 'studio' ? '工作室' : '部门' }}</el-tag>
        </el-form-item>
        <el-form-item label="ID">
          <el-input v-model="grantForm.scope_id" :placeholder="`输入${grantForm.scope_type === 'studio' ? '工作室' : '部门'} ID`" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="grantVisible = false">取消</el-button>
        <el-button type="primary" :loading="grantSaving" @click="handleGrant">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, InfoFilled } from '@element-plus/icons-vue'
import { groupsApi } from '@/api/groups'
import { getGroupEntitlements, grantEntitlement, revokeEntitlement, setGroupOpenMode } from '@/services/control-plane'
import type { GroupEntitlement } from '@/types/entitlement'

const route = useRoute()
const router = useRouter()
const groupId = computed(() => Number(route.params.id))

const activeTab = ref('info')
const pageLoading = ref(false)
const modelsLoading = ref(false)
const modeSwitching = ref(false)
const grantVisible = ref(false)
const grantSaving = ref(false)
const grantForm = ref<{ scope_type: 'studio' | 'dept'; scope_id: string }>({ scope_type: 'studio', scope_id: '' })

const group = ref<any>(null)
const models = ref<any[]>([])
const entitlements = ref<GroupEntitlement[]>([])

const isPublic = ref(false)
const studioEntitlements = computed(() => entitlements.value.filter(e => e.scope_type === 'studio'))
const deptEntitlements = computed(() => entitlements.value.filter(e => e.scope_type === 'dept'))

function strategyLabel(s: string): string {
  const map: Record<string, string> = { round_robin: '轮询', priority: '优先级', weighted: '加权', auto: '自动' }
  return map[s] || s
}

async function loadGroup() {
  pageLoading.value = true
  try {
    const res = await groupsApi.getGroup(groupId.value)
    group.value = res.data?.data || res.data
    isPublic.value = (group.value?.open_mode || 'controlled') === 'public'
  } catch { group.value = null }
  finally { pageLoading.value = false }
}

async function loadModels() {
  modelsLoading.value = true
  try {
    const res = await groupsApi.listGroupModels(groupId.value)
    models.value = res.data?.data || []
  } catch { models.value = [] }
  finally { modelsLoading.value = false }
}

async function loadEntitlements() {
  try {
    entitlements.value = await getGroupEntitlements(groupId.value)
  } catch { entitlements.value = [] }
}

async function onModeChange(val: boolean) {
  modeSwitching.value = true
  try {
    await setGroupOpenMode(groupId.value, val ? 'public' : 'controlled')
    ElMessage.success('已更新，约 30 秒后对用户生效')
  } catch (e: any) {
    isPublic.value = !val
    ElMessage.error(e?.message || '更新失败')
  } finally { modeSwitching.value = false }
}

function openGrant(type: 'studio' | 'dept') {
  grantForm.value = { scope_type: type, scope_id: '' }
  grantVisible.value = true
}

async function handleGrant() {
  if (!grantForm.value.scope_id) { ElMessage.warning('请填写 ID'); return }
  grantSaving.value = true
  try {
    await grantEntitlement({ group_id: groupId.value, scope_type: grantForm.value.scope_type, scope_id: grantForm.value.scope_id })
    ElMessage.success('授权成功')
    grantVisible.value = false
    await loadEntitlements()
  } catch (e: any) { ElMessage.error(e?.message || '授权失败') }
  finally { grantSaving.value = false }
}

async function handleRevoke(row: GroupEntitlement) {
  try {
    await revokeEntitlement({ group_id: groupId.value, scope_type: row.scope_type, scope_id: row.scope_id })
    ElMessage.success('已撤销')
    await loadEntitlements()
  } catch (e: any) { ElMessage.error(e?.message || '撤销失败') }
}

onMounted(async () => {
  await loadGroup()
  await Promise.all([loadModels(), loadEntitlements()])
})
</script>

<style scoped>
.group-detail-page { padding: 24px; max-width: 960px; margin: 0 auto; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.header-breadcrumb { display: flex; align-items: center; gap: 6px; font-size: 14px; }
.bc-sep { color: var(--el-text-color-placeholder); }
.bc-current { font-weight: 600; color: var(--el-text-color-primary); }
.header-meta { display: flex; gap: 8px; }
.state-error { text-align: center; padding: 60px; color: var(--el-text-color-secondary); }
.detail-tabs { }
.info-desc { margin-top: 16px; }
.entitlement-section { display: flex; flex-direction: column; gap: 20px; padding-top: 16px; }
.open-mode-row { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border: 1px solid var(--el-border-color-light); border-radius: 10px; }
.om-title { font-weight: 600; font-size: 14px; color: var(--el-text-color-primary); }
.om-desc { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.entitle-block { }
.entitle-block-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; font-size: 14px; font-weight: 500; color: var(--el-text-color-regular); }
.public-hint { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--el-text-color-secondary); padding: 12px 16px; background: var(--el-fill-color-light); border-radius: 8px; }
</style>
