<template>
  <el-dialog
    :model-value="visible"
    title="批量设置标签"
    width="460px"
    destroy-on-close
    @close="emit('close')"
  >
    <div class="batch-tag-info">
      已选择 <strong>{{ modelIds.length }}</strong> 个模型
    </div>

    <el-form label-width="90px" label-position="left">
      <el-form-item label="选择标签">
        <el-select
          v-model="selectedTagIds"
          multiple
          placeholder="请选择要设置的标签"
          style="width: 100%"
          :loading="loadingTags"
        >
          <el-option
            v-for="tag in tagOptions"
            :key="tag.id"
            :label="tag.name"
            :value="tag.id"
          />
        </el-select>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="warning" :loading="submitting" @click="handleConfirm">
        确认设置
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getModelTags, batchSetModelTags } from '@/services/control-plane'
import type { ModelTag } from '@/types/model-tag'

const props = defineProps<{
  visible: boolean
  modelIds: string[]
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const selectedTagIds = ref<string[]>([])
const tagOptions = ref<ModelTag[]>([])
const loadingTags = ref(false)
const submitting = ref(false)

watch(
  () => props.visible,
  async (val) => {
    if (val) {
      selectedTagIds.value = []
      loadingTags.value = true
      try {
        tagOptions.value = await getModelTags()
      } catch {
        tagOptions.value = []
      } finally {
        loadingTags.value = false
      }
    }
  }
)

async function handleConfirm() {
  if (selectedTagIds.value.length === 0) {
    ElMessage.warning('请至少选择一个标签')
    return
  }

  submitting.value = true
  try {
    const result = await batchSetModelTags(props.modelIds, selectedTagIds.value)
    ElMessage.success(`成功为 ${result.updated} 个模型设置标签`)
    emit('saved')
  } catch {
    ElMessage.error('批量设置标签失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.batch-tag-info {
  margin-bottom: 16px;
  padding: 8px 12px;
  background-color: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

@media (max-width: 768px) {
  :deep(.el-dialog) {
    width: 95% !important;
    max-width: 95vw !important;
  }
}
</style>
