<template>
  <div class="layout-sidebar">
    <!-- 侧边栏 -->
    <aside class="sidebar">
      <div class="sidebar-header">LLM Gateway</div>
      <el-menu
        class="sidebar-menu"
        :default-active="route.path"
        router
      >
        <el-menu-item index="/access/unified-entry">
          <el-icon><Connection /></el-icon>
          <span>统一接入</span>
        </el-menu-item>
        <el-menu-item index="/admin/providers">
          <el-icon><Operation /></el-icon>
          <span>渠道管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/models">
          <el-icon><Cpu /></el-icon>
          <span>模型管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/tags">
          <el-icon><PriceTag /></el-icon>
          <span>标签管理</span>
        </el-menu-item>
        <el-menu-item index="/admin/catalog-sync">
          <el-icon><Refresh /></el-icon>
          <span>Catalog 同步</span>
        </el-menu-item>
        <el-menu-item index="/admin/chat-playground">
          <el-icon><ChatDotRound /></el-icon>
          <span>对话测试</span>
        </el-menu-item>
      </el-menu>
    </aside>

    <!-- 主内容区 -->
    <div class="main-content">
      <!-- 顶部工具栏 -->
      <div class="top-bar">
        <span class="theme-toggle" @click="toggleTheme" :title="isDark ? '切换到浅色模式' : '切换到深色模式'">
          {{ isDark ? '☀️' : '🌙' }}
        </span>
      </div>

      <!-- 页面内容 -->
      <div class="page-container">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'
import { isDark, toggleTheme } from './composables/useTheme'
import { Connection, Operation, Cpu, PriceTag, Refresh, ChatDotRound } from '@element-plus/icons-vue'

const route = useRoute()
</script>

<style scoped>
.sidebar-header {
  font-family: var(--font-family);
}
</style>
