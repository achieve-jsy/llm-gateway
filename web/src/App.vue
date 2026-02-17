<template>
  <div class="layout-sidebar" :class="{ 'layout-sidebar--mobile': isMobile }">
    <!-- 侧边栏：已登录时始终显示（/403 等兜底页也保留菜单） -->
    <aside v-if="authStore.isLoggedIn" class="sidebar" :class="{ 'sidebar--collapsed': isCollapsed, 'sidebar--mobile-open': isMobile && showMobileSidebar }">
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
        <el-menu-item index="/access/unified-entry">
          <el-icon><Connection /></el-icon>
          <template #title>首页</template>
        </el-menu-item>

        <el-divider class="menu-divider" />

        <div class="menu-group-label">员工</div>
        <el-menu-item index="/keys">
          <el-icon><Key /></el-icon>
          <template #title>我的 Key</template>
        </el-menu-item>
        <el-menu-item index="/my/usage">
          <el-icon><DataLine /></el-icon>
          <template #title>我的用量</template>
        </el-menu-item>
        <el-menu-item index="/model-square">
          <el-icon><Collection /></el-icon>
          <template #title>模型广场</template>
        </el-menu-item>
        <el-menu-item index="/my/budget">
          <el-icon><Wallet /></el-icon>
          <template #title>
            我的预算
            <span v-if="budgetAlert" class="budget-alert-dot" />
          </template>
        </el-menu-item>
        <el-menu-item index="/admin/group-health-monitor">
          <el-icon><Histogram /></el-icon>
          <template #title>模型组健康</template>
        </el-menu-item>
        <el-menu-item v-if="authStore.isManager" index="/admin/expansion-requests">
          <el-icon><TrendCharts /></el-icon>
          <template #title>扩容申请</template>
        </el-menu-item>

        <!-- 管理（管理角色可见）-->
        <template v-if="authStore.isManager">
          <el-divider class="menu-divider" />
          <div class="menu-group-label">管理</div>
          <!-- 组织架构：admin 看全树，studio/dept admin 看自己管辖范围 -->
          <el-menu-item index="/admin/org">
            <el-icon><OfficeBuilding /></el-icon>
            <template #title>
              组织架构
              <span v-if="!authStore.isAdmin" class="menu-scope-tag">我的范围</span>
            </template>
          </el-menu-item>
          <el-menu-item index="/admin/usage/dashboard">
            <el-icon><Histogram /></el-icon>
            <template #title>用量看板</template>
          </el-menu-item>
          <el-menu-item index="/admin/budget">
            <el-icon><Wallet /></el-icon>
            <template #title>预算管理</template>
          </el-menu-item>
          <el-menu-item index="/admin/service-keys">
            <el-icon><Key /></el-icon>
            <template #title>Service Key</template>
          </el-menu-item>
          <el-menu-item index="/admin/entitlements">
            <el-icon><Lock /></el-icon>
            <template #title>模型组授权</template>
          </el-menu-item>
          <!-- 角色管理仅 admin 可见 -->
          <el-menu-item v-if="authStore.isAdmin" index="/admin/org/roles">
            <el-icon><UserFilled /></el-icon>
            <template #title>角色管理</template>
          </el-menu-item>
          <el-menu-item index="/admin/audit-logs">
            <el-icon><Document /></el-icon>
            <template #title>审计日志</template>
          </el-menu-item>
        </template>

        <!-- 平台（仅 admin）-->
        <template v-if="authStore.isAdmin">
          <el-divider class="menu-divider" />
          <div class="menu-group-label">平台</div>
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
          <el-menu-item index="/admin/ops">
            <el-icon><DataBoard /></el-icon>
            <template #title>运维监控</template>
          </el-menu-item>
          <el-menu-item index="/admin/usage-cost">
            <el-icon><Histogram /></el-icon>
            <template #title>用量与成本</template>
          </el-menu-item>
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
          <el-menu-item index="/admin/announcements">
            <el-icon><Bell /></el-icon>
            <template #title>公告管理</template>
          </el-menu-item>
        </template>
      </el-menu>
    </aside>

    <!-- 主内容区 -->
    <div class="main-content">
      <!-- 顶部工具栏 -->
      <div v-if="authStore.isLoggedIn" class="top-bar">        <button v-if="isMobile" class="hamburger-btn" @click="showMobileSidebar = !showMobileSidebar">
          <el-icon :size="20"><Menu /></el-icon>
        </button>
        <button v-if="!isMobile" class="collapse-btn" @click="toggleCollapse" :title="isCollapsed ? '展开菜单' : '收起菜单'">
          <el-icon :size="18"><Fold v-if="!isCollapsed" /><Expand v-else /></el-icon>
        </button>
        <div class="top-bar-spacer"></div>
        <AnnouncementBanner v-if="authStore.isLoggedIn" ref="bannerRef" />
        <NotificationBell v-if="authStore.isLoggedIn" ref="notificationBellRef" />
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

      <!-- 部门状态警告 Banner（刷新后依然可见，直到用户手动关闭） -->
      <transition name="dept-banner">
        <div
          v-if="authStore.isLoggedIn && showDeptBanner && deptBannerInfo"
          class="dept-status-banner"
          :class="`dept-status-banner--${deptBannerInfo.type}`"
        >
          <el-icon class="dept-banner__icon"><Warning /></el-icon>
          <span class="dept-banner__text">{{ deptBannerInfo.message }}</span>
          <el-button link size="small" class="dept-banner__close" @click="dismissDeptBanner">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </transition>

      <!-- 页面内容 -->
      <div class="page-container" :class="{ 'page-container--public': !authStore.isLoggedIn }">
        <router-view />
      </div>
    </div>

    <div v-if="isMobile && showMobileSidebar" class="sidebar-backdrop" @click="showMobileSidebar = false" />

    <AnnouncementPopup
      v-if="authStore.isLoggedIn"
      :visible="showPopup"
      @close="showPopup = false"
      @all-read="onPopupAllRead"
    />
  </div>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { onMounted, computed, ref, watch, onUnmounted } from 'vue'
import { isDark, toggleTheme } from './composables/useTheme'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { Connection, Operation, Cpu, PriceTag, Refresh, ChatDotRound, User, Key, Monitor, Setting, Fold, Expand, Histogram, Collection, Bell, DataLine, OfficeBuilding, UserFilled, Warning, Close, Menu, Wallet, Lock, TrendCharts, Document, DataBoard } from '@element-plus/icons-vue'
import NotificationBell from '@/components/common/NotificationBell.vue'
import AnnouncementBanner from '@/components/common/AnnouncementBanner.vue'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { getUnreadAnnouncements } from '@/services/control-plane'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const settingsStore = useSettingsStore()

const isCollapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')
const isMobile = ref(window.innerWidth < 768)
const showMobileSidebar = ref(false)
const activeMenu = ref(route.path)
const showPopup = ref(false)
const budgetAlert = ref(false)

// 预算告警检查：任一层 ≥80% 显示红点
async function checkBudgetAlert() {
  if (!authStore.isLoggedIn) return
  try {
    const { getMyBudget } = await import('@/services/control-plane')
    const chain = await getMyBudget()
    budgetAlert.value = chain.some(item => item.amount_usd > 0 && (item.used_ratio ?? 0) >= 0.8)
  } catch { budgetAlert.value = false }
}

watch(() => route.path, (newPath) => {
  activeMenu.value = newPath
})

const sidebarLogoSrc = computed(() => {
  return settingsStore.publicSettings?.site_logo || '/images/logo_example.png'
})

const siteTitle = computed(() => {
  return settingsStore.publicSettings?.site_name || '灵锋驿'
})

// ── 部门状态 Banner ────────────────────────────────────────────────────────
const DEPT_BANNER_DISMISS_KEY = 'dept_banner_dismissed'
const showDeptBanner = ref(sessionStorage.getItem(DEPT_BANNER_DISMISS_KEY) !== '1')

const deptBannerInfo = computed<{ message: string; type: 'warning' | 'error' } | null>(() => {
  const ds = authStore.deptStatus
  if (ds === 'none') {
    return { type: 'error', message: '你的账号暂未关联部门，部分功能可能受限，请联系平台管理员。' }
  }
  if (ds === 'pending_studio') {
    return { type: 'warning', message: '你所在的部门尚未归入任何工作室，数据统计可能不完整，请联系管理员完成映射配置。' }
  }
  return null
})

function dismissDeptBanner() {
  showDeptBanner.value = false
  sessionStorage.setItem(DEPT_BANNER_DISMISS_KEY, '1')
}

// 当 dept_status 变为 active（映射完成后），自动隐藏 banner
watch(() => authStore.deptStatus, (s) => {
  if (s === 'active' || s == null) {
    showDeptBanner.value = false
    sessionStorage.removeItem(DEPT_BANNER_DISMISS_KEY)
  } else {
    // 新的问题状态出现时重置 dismissed，确保下次登录看到
    showDeptBanner.value = true
  }
})

const notificationBellRef = ref<{ refresh: () => void } | null>(null)
let bellRefreshTimer: ReturnType<typeof setInterval> | null = null

const handleResize = () => { isMobile.value = window.innerWidth < 768; if (!isMobile.value) showMobileSidebar.value = false }

onMounted(() => {
  settingsStore.loadPublicSettings()
  checkPopupAnnouncements()
  checkBudgetAlert()
  window.addEventListener('resize', handleResize)
  // 每 30s 刷新一次铃铛未读数（含告警定向推送）
  bellRefreshTimer = setInterval(() => {
    if (authStore.isLoggedIn) notificationBellRef.value?.refresh()
  }, 30000)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
  if (bellRefreshTimer) clearInterval(bellRefreshTimer)
})

watch(() => authStore.isLoggedIn, (loggedIn) => {
  if (loggedIn) checkPopupAnnouncements()
})

function handleMenuSelect(index: string) {
  if (isMobile.value) showMobileSidebar.value = false
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
    sessionStorage.removeItem('popup_shown')
    router.push('/login')
  }
}

async function checkPopupAnnouncements() {
  if (sessionStorage.getItem('popup_shown')) return
  if (!localStorage.getItem('access_token')) return
  try {
    const items = await getUnreadAnnouncements()
    if ((items || []).some(a => a.display_mode === 'popup')) {
      showPopup.value = true
      sessionStorage.setItem('popup_shown', '1')
    }
  } catch {}
}

function onPopupAllRead() {
  showPopup.value = false
}
</script>

<style scoped>
.sidebar-header {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-family);
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
  transition: background-color 0.2s;
  flex-shrink: 0;
}

.collapse-btn:hover {
  background-color: var(--el-fill-color);
  color: #1F8A3B;
}

.sidebar-logo {
  height: 34px;
  width: 34px;
  object-fit: contain;
}

.sidebar-title {
  font-size: 18px;
  font-weight: 800;
  color: #1F8A3B;
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

.page-container--public {
  padding: 0;
  height: 100%;
}

/* org-tree 菜单项中"我的范围"小标签 */
.menu-scope-tag {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 5px;
  font-size: 10px;
  line-height: 1.4;
  border-radius: 3px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  vertical-align: middle;
}

.sidebar-backdrop {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.4);
  z-index: 998;
}

.hamburger-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  color: var(--el-text-color-secondary);
  transition: background-color 0.2s;
  flex-shrink: 0;
}

.hamburger-btn:hover {
  background-color: var(--el-fill-color);
  color: #1F8A3B;
}

/* Mobile sidebar drawer mode */
.layout-sidebar--mobile .sidebar {
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  z-index: 999;
  transform: translateX(-100%);
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  width: 260px !important;
}

.layout-sidebar--mobile .sidebar.sidebar--mobile-open {
  transform: translateX(0);
  box-shadow: 4px 0 24px rgba(0, 0, 0, 0.15);
}

.layout-sidebar--mobile .main-content {
  width: 100%;
}

.budget-alert-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-danger);
  margin-left: 4px;
  vertical-align: middle;
  animation: badge-pulse 1.5s infinite;
}
@keyframes badge-pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }

</style>
