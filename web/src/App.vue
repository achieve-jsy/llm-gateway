<template>
  <div class="layout-sidebar">
    <!-- 侧边栏 -->
    <aside v-if="!isPublicRoute" class="sidebar" :class="{ 'sidebar--collapsed': isCollapsed }">
      <div class="sidebar-header">
        <img :src="sidebarLogoSrc" class="sidebar-logo" alt="logo" />
        <span v-show="!isCollapsed" class="sidebar-title">{{ siteTitle }}</span>
      </div>
      <el-menu
        class="sidebar-menu"
        :default-active="activeMenu"
        :collapse="isCollapsed"
        @select="handleMenuSelect"
      >
        <div class="menu-group-label">接入</div>
        <el-menu-item index="/access/unified-entry">
          <el-icon><Connection /></el-icon>
          <template #title>统一接入</template>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">员工</div>
        <el-menu-item index="/home">
          <el-icon><HomeFilled /></el-icon>
          <template #title>首页</template>
        </el-menu-item>
        <el-menu-item index="/keys">
          <el-icon><Key /></el-icon>
          <template #title>我的 Key</template>
        </el-menu-item>
        <el-menu-item index="/admin/group-health-monitor">
          <el-icon><Histogram /></el-icon>
          <template #title>模型组健康</template>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">管理</div>
        <el-menu-item index="/admin/providers">
          <el-icon><Operation /></el-icon>
          <template #title>渠道管理</template>
        </el-menu-item>
        <el-menu-item index="/admin/models">
          <el-icon><Cpu /></el-icon>
          <template #title>模型管理</template>
        </el-menu-item>
        <el-menu-item index="/admin/groups">
          <el-icon><Collection /></el-icon>
          <template #title>模型组</template>
        </el-menu-item>
        <el-menu-item index="/admin/tags">
          <el-icon><PriceTag /></el-icon>
          <template #title>标签管理</template>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">运维</div>
        <el-menu-item index="/admin/health-monitor">
          <el-icon><Monitor /></el-icon>
          <template #title>模型健康监控</template>
        </el-menu-item>
        <el-menu-item index="/admin/catalog-sync">
          <el-icon><Refresh /></el-icon>
          <template #title>Catalog 同步</template>
        </el-menu-item>
        <el-menu-item index="/admin/chat-playground">
          <el-icon><ChatDotRound /></el-icon>
          <template #title>对话测试</template>
        </el-menu-item>
        <el-menu-item index="/admin/settings">
          <el-icon><Setting /></el-icon>
          <template #title>系统设置</template>
        </el-menu-item>
      </el-menu>
    </aside>

    <!-- 主内容区 -->
    <div class="main-content">
      <!-- 顶部工具栏 -->
      <div v-if="!isPublicRoute" class="top-bar">
        <button class="collapse-btn" @click="toggleCollapse" :title="isCollapsed ? '展开菜单' : '收起菜单'">
          <el-icon :size="18"><Fold v-if="!isCollapsed" /><Expand v-else /></el-icon>
        </button>
        <div class="top-bar-spacer"></div>
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
      <div class="page-container" :class="{ 'page-container--public': isPublicRoute }">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { onMounted, computed, ref, watch } from 'vue'
import { isDark, toggleTheme } from './composables/useTheme'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { Connection, Operation, Cpu, PriceTag, Refresh, ChatDotRound, User, HomeFilled, Key, Monitor, Setting, Fold, Expand, Histogram, Collection } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const settingsStore = useSettingsStore()

const isCollapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')
const activeMenu = ref(route.path)

watch(() => route.path, (newPath) => {
  activeMenu.value = newPath
})

const isPublicRoute = computed(() => route.meta.public as boolean | undefined)

const sidebarLogoSrc = computed(() => {
  return settingsStore.publicSettings?.site_logo || '/images/logo_example.png'
})

const siteTitle = computed(() => {
  return settingsStore.publicSettings?.site_name || '灵锋驿'
})

onMounted(() => {
  settingsStore.loadPublicSettings()
})

function handleMenuSelect(index: string) {
  if (index && index !== route.path) {
    router.push(index)
  }
}

function toggleCollapse() {
  isCollapsed.value = !isCollapsed.value
  localStorage.setItem('sidebar-collapsed', String(isCollapsed.value))
}

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
  overflow: hidden;
}

.collapse-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  color: var(--el-text-color-secondary);
  transition: background-color 0.15s, color 0.15s;
  flex-shrink: 0;
}

.collapse-btn:hover {
  background-color: var(--el-fill-color);
  color: #2563EB;
}

.sidebar-logo {
  height: 34px;
  width: 34px;
  object-fit: contain;
  flex-shrink: 0;
}

.sidebar-title {
  font-family: 'Plus Jakarta Sans', sans-serif;
  font-size: 17px;
  font-weight: 800;
  color: #2563EB;
  letter-spacing: 0.01em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
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

.page-container--public {
  padding: 0;
  height: 100%;
}
</style>
