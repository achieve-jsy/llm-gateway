<template>
  <div class="ip-allowlist-section">
    <div class="section-header">
      <span class="section-title">
        <el-icon><Lock /></el-icon>
        IP 白名单
      </span>
      <el-button
        type="primary"
        size="small"
        link
        @click="openDialog"
      >
        {{ ipAllowlist.length > 0 ? '编辑' : '配置' }}
      </el-button>
    </div>

    <div v-if="ipAllowlist.length === 0" class="empty-state">
      <el-text type="info" size="small">未配置（允许所有 IP）</el-text>
    </div>

    <div v-else class="ip-tags">
      <el-tag
        v-for="(ip, index) in ipAllowlist"
        :key="index"
        size="small"
        type="info"
        effect="plain"
        closable
        @close="removeIP(index)"
      >
        {{ ip }}
      </el-tag>
    </div>

    <!-- 编辑弹窗 -->
    <el-dialog
      v-model="dialogVisible"
      title="IP 白名单配置"
      width="500px"
      :close-on-click-modal="false"
    >
      <el-form label-width="100px">
        <el-form-item label="IP 地址">
          <div class="input-with-add">
            <el-input
              v-model="newIP"
              placeholder="例如: 192.0.2.10 或 10.0.0.0/24"
              clearable
              @keyup.enter="addIP"
            />
            <el-button type="primary" @click="addIP" :disabled="!newIP.trim()">
              添加
            </el-button>
          </div>
        </el-form-item>

        <el-form-item label="已配置 IP">
          <div v-if="editList.length === 0" class="edit-empty">
            <el-text type="info" size="small">暂无 IP 地址</el-text>
          </div>
          <div v-else class="edit-list">
            <div
              v-for="(ip, index) in editList"
              :key="index"
              class="edit-item"
            >
              <span class="edit-ip">{{ ip }}</span>
              <el-button
                type="danger"
                size="small"
                link
                @click="removeEditIP(index)"
              >
                删除
              </el-button>
            </div>
          </div>
        </el-form-item>

        <el-form-item>
          <el-text type="warning" size="small">
            提示：留空将允许所有 IP 访问
          </el-text>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveIPAllowlist" :loading="saving">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Lock } from '@element-plus/icons-vue'

const props = defineProps<{
  modelValue: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
  (e: 'save', value: string[]): void
}>()

const ipAllowlist = ref<string[]>([...props.modelValue])
const dialogVisible = ref(false)
const editList = ref<string[]>([])
const newIP = ref('')
const saving = ref(false)

watch(() => props.modelValue, (val) => {
  ipAllowlist.value = [...val]
})

function openDialog() {
  editList.value = [...ipAllowlist.value]
  newIP.value = ''
  dialogVisible.value = true
}

function addIP() {
  const ip = newIP.value.trim()
  if (!ip) return

  // 简单的 IP 格式验证
  const ipPattern = /^(\d{1,3}\.){3}\d{1,3}(\/\d{1,2})?$/
  if (!ipPattern.test(ip)) {
    ElMessage.warning('请输入有效的 IP 地址或 CIDR')
    return
  }

  if (editList.value.includes(ip)) {
    ElMessage.warning('该 IP 已存在')
    return
  }

  editList.value.push(ip)
  newIP.value = ''
}

function removeEditIP(index: number) {
  editList.value.splice(index, 1)
}

function removeIP(index: number) {
  ipAllowlist.value.splice(index, 1)
  emit('update:modelValue', ipAllowlist.value)
}

async function saveIPAllowlist() {
  saving.value = true
  try {
    ipAllowlist.value = [...editList.value]
    emit('update:modelValue', ipAllowlist.value)
    emit('save', ipAllowlist.value)
    ElMessage.success('保存成功')
    dialogVisible.value = false
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.error || err?.message || '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.ip-allowlist-section {
  margin-top: 12px;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.empty-state {
  padding: 8px 0;
}

.ip-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.input-with-add {
  display: flex;
  gap: 8px;
  width: 100%;
}

.input-with-add .el-input {
  flex: 1;
}

.edit-empty {
  padding: 12px 0;
  text-align: center;
  width: 100%;
}

.edit-list {
  width: 100%;
  max-height: 200px;
  overflow-y: auto;
}

.edit-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  margin-bottom: 8px;
}

.edit-item:last-child {
  margin-bottom: 0;
}

.edit-ip {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--el-text-color-primary);
}
</style>
