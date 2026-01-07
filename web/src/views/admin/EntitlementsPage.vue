<template>
  <div class="entitlements-page">
    <div class="page-header">
      <h2 class="page-title">模型组授权</h2>
      <p class="page-desc">
        管理各模型组的开放/访问模式，以及向工作室和部门授权。
      </p>
    </div>

    <div class="entitlements-placeholder">
      <el-empty
        description="授权管理功能正在建设中，请先在模型组管理页面使用授权入口。"
        :image-size="96"
      />
      <el-button type="primary" @click="$router.push('/admin/groups')">
        前往模型组管理
      </el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ElEmpty, ElButton } from 'element-plus'
</script>

<style scoped>
.entitlements-page {
  padding: 24px;
  min-height: 320px;
}

.page-header {
  margin-bottom: 16px;
}

.page-title {
  font-size: 18px;
  font-weight: 600;
  margin: 0 0 6px;
}

.page-desc {
  color: #909399;
  font-size: 13px;
  margin: 0;
}

.entitlements-placeholder {
  text-align: center;
  padding-top: 48px;
}
</style>
