<template>
  <div class="layout-sidebar">
    <!-- 侧边栏 -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <img src="/images/logo_example.png" class="sidebar-logo" alt="logo" />
        <span class="sidebar-title">灵锋驿</span>
      </div>
      <el-menu
        class="sidebar-menu"
        :default-active="route.path"
        router
      >
        <div class="menu-group-label">接入</div>
        <el-menu-item index="/access/unified-entry">
          <el-icon><Connection /></el-icon>
          <span>统一接入</span>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">员工</div>
        <el-menu-item index="/home">
          <el-icon><HomeFilled /></el-icon>
          <span>首页</span>
        </el-menu-item>
        <el-menu-item index="/keys">
          <el-icon><Key /></el-icon>
          <span>我的 Key</span>
        </el-menu-item>
        <el-menu-item index="/models">
          <el-icon><Monitor /></el-icon>
          <span>可用模型</span>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">管理</div>
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

        <el-divider class="menu-divider" />

        <div class="menu-group-label">运维</div>
        <el-menu-item index="/admin/catalog-sync">
          <el-icon><Refresh /></el-icon>
          <span>Catalog 同步</span>
        </el-menu-item>
        <el-menu-item index="/admin/chat-playground">
          <el-icon><ChatDotRound /></el-icon>
          <span>对话测试</span>
        </el-menu-item>
        <el-menu-item index="/admin/settings">
          <el-icon><Setting /></el-icon>
          <span>系统设置</span>
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
        <el-dropdown v-if="authStore.isLoggedIn" trigger="click" @command="handleUserCommand">
          <div class="user-info">
            <el-icon class="user-icon" size="22"><User /></el-icon>
            <span class="user-name">{{ authStore.userName }}</span>
            <span v-if="authStore.departmentName" class="user-dept">{{ authStore.departmentName }}</span>
          </div>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-icon v-else class="user-icon" size="24"><User /></el-icon>
      </div>

      <!-- 页面内容 -->
      <div class="page-container">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { onMounted } from 'vue'
import { isDark, toggleTheme } from './composables/useTheme'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { Connection, Operation, Cpu, PriceTag, Refresh, ChatDotRound, User, HomeFilled, Key, Monitor, Setting } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

onMounted(() => {
  useSettingsStore().loadPublicSettings()
})

function handleUserCommand(command: string) {
  if (command === 'logout') {
    authStore.handleLogout()
    router.push('/login')
  }
}
</script>

<style scoped>
.sidebar-header {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-family);
}

.sidebar-logo {
  height: 34px;
  width: 34px;
  object-fit: contain;
}

.sidebar-title {
  font-size: 18px;
  font-weight: 800;
  color: #1E8C86;
  letter-spacing: 1px;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 8px;
  transition: background-color 0.15s;
}

.user-info:hover {
  background-color: var(--el-fill-color);
}

.user-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.user-dept {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
  background-color: var(--el-fill-color);
}
</style>
