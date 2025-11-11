<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑标签' : '新建标签'"
    width="460px"
    destroy-on-close
    @close="emit('close')"
  >
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-width="90px"
      label-position="left"
    >
      <el-form-item label="标签名称" prop="name">
        <el-input v-model="formData.name" placeholder="请输入标签名称" />
      </el-form-item>

      <el-form-item label="标签颜色" prop="color">
        <el-color-picker v-model="formData.color" :show-alpha="false" />
        <span class="color-preview" :style="{ backgroundColor: formData.color }"></span>
        <span class="form-hint inline">{{ formData.color }}</span>
      </el-form-item>

      <el-form-item label="描述">
        <el-input
          v-model="formData.description"
          type="textarea"
          :rows="2"
          placeholder="标签描述（可选）"
        />
      </el-form-item>

      <el-form-item label="排序">
        <el-input-number
          v-model="formData.sort_order"
          :min="0"
          :max="9999"
          style="width: 160px"
        />
        <span class="form-hint inline">数值越小越靠前</span>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('close')">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        确定
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { createModelTag, updateModelTag } from '@/services/control-plane'
import type { ModelTag, ModelTagPayload } from '@/types/model-tag'

interface FormState {
  name: string
  color: string
  description: string
  sort_order: number
}

const props = defineProps<{
  visible: boolean
  tag: ModelTag | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'update:visible': [val: boolean]
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)

const isEdit = computed(() => !!props.tag)

const formData = ref<FormState>({
  name: '',
  color: '#409EFF',
  description: '',
  sort_order: 0
})

watch(
  () => props.tag,
  (val) => {
    if (val) {
      formData.value = {
        name: val.name,
        color: val.color || '#409EFF',
        description: val.description || '',
        sort_order: val.sort_order || 0
      }
    } else {
      formData.value = {
        name: '',
        color: '#409EFF',
        description: '',
        sort_order: 0
      }
    }
  },
  { immediate: true }
)

const rules: FormRules = {
  name: [{ required: true, message: '请输入标签名称', trigger: 'blur' }],
  color: [{ required: true, message: '请选择标签颜色', trigger: 'change' }]
}

async function handleSubmit() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload: ModelTagPayload = {
      name: formData.value.name,
      color: formData.value.color,
      description: formData.value.description || undefined,
      sort_order: formData.value.sort_order
    }

    if (isEdit.value && props.tag) {
      await updateModelTag(props.tag.id, payload)
      ElMessage.success('更新成功')
    } else {
      await createModelTag(payload)
      ElMessage.success('创建成功')
    }
    emit('saved')
  } catch {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.form-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

.form-hint.inline {
  margin-top: 0;
  margin-left: 8px;
}

.color-preview {
  display: inline-block;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  margin-left: 12px;
  vertical-align: middle;
  border: 1px solid var(--el-border-color);
}
</style>
