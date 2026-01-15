# 模型广场筛选功能设计

## 背景

当前模型广场（`/model-square`）只有文本搜索和模式过滤，200+ 模型组一次性加载，用户难以按能力、价格等维度筛选定位到满足需求的模型。

## 目标

增加高级筛选面板，支持：
1. **能力筛选**：视觉理解、工具调用、长上下文
2. **价格区间**：输入/输出价格范围
3. **排序**：名称、价格、上下文长度

## 现状分析

### 文件结构

| 文件 | 职责 |
|------|------|
| `web/src/views/ModelSquarePage.vue` | 页面入口 |
| `web/src/components/access/ModelDiscoverySection.vue` | 核心筛选组件（728行） |
| `web/src/api/groups.ts` | API 层 |
| `web/src/types/group.ts` | 类型定义 |

### 当前筛选逻辑

```typescript
const filteredGroups = computed(() => {
  let list = groups.value.filter(g => g.enabled && isGroupAvailable(g))
  if (modeFilter.value) list = list.filter(g => caps.value[g.name]?.mode === modeFilter.value)
  const q = searchQuery.value.toLowerCase().trim()
  if (q) list = list.filter(g => g.name.toLowerCase().includes(q) || (g.description || '').toLowerCase().includes(q))
  return list
})
```

### 可用数据字段（来自 `getGroupCapabilities()`）

```typescript
interface GroupCapability {
  mode: string                    // chat, embedding, image_generation...
  max_input_tokens: number        // 上下文窗口
  max_output_tokens: number       // 最大输出
  supports_vision: boolean        // 视觉理解
  supports_function_calling: boolean  // 工具调用
  supports_tool_choice: boolean   // tool_choice 支持
  input_price_per_million: number     // 输入价格 $/M tokens
  output_price_per_million: number    // 输出价格 $/M tokens
}
```

## 设计方案

### UI 交互

在现有搜索栏右侧增加 **"高级筛选"** 折叠按钮，点击展开筛选面板：

```
[🔍 搜索模型组...] [全部] [对话] [向量] [图像生成] [🔽 高级筛选]

┌─────────────────────────────────────────────────────────────┐
│  能力: ☐ 视觉理解  ☐ 工具调用  ☐ 长上下文 (>32K)          │
│  价格: [$0] - [$10] / 百万 tokens                            │
│  排序: [名称 ▼] [价格 ▼] [上下文 ▼]                         │
└─────────────────────────────────────────────────────────────┘
```

### 筛选逻辑

```typescript
// 新增响应式状态
const advancedFilters = ref({
  vision: false,
  functionCalling: false,
  longContext: false,  // > 32K
  priceMin: 0,
  priceMax: 10,  // $/M tokens
  sortBy: 'name',  // name | price | context
  sortDesc: false
})

const filteredGroups = computed(() => {
  let list = groups.value.filter(g => g.enabled && isGroupAvailable(g))
  
  // 模式过滤（现有）
  if (modeFilter.value) {
    list = list.filter(g => caps.value[g.name]?.mode === modeFilter.value)
  }
  
  // 文本搜索（现有）
  const q = searchQuery.value.toLowerCase().trim()
  if (q) {
    list = list.filter(g => 
      g.name.toLowerCase().includes(q) || 
      (g.description || '').toLowerCase().includes(q)
    )
  }
  
  // 能力筛选（新增）
  if (advancedFilters.value.vision) {
    list = list.filter(g => caps.value[g.name]?.supports_vision)
  }
  if (advancedFilters.value.functionCalling) {
    list = list.filter(g => 
      caps.value[g.name]?.supports_function_calling || 
      caps.value[g.name]?.supports_tool_choice
    )
  }
  if (advancedFilters.value.longContext) {
    list = list.filter(g => (caps.value[g.name]?.max_input_tokens || 0) > 32000)
  }
  
  // 价格筛选（新增）
  const { priceMin, priceMax } = advancedFilters.value
  list = list.filter(g => {
    const cap = caps.value[g.name]
    if (!cap) return true
    const inputPrice = cap.input_price_per_million || 0
    const outputPrice = cap.output_price_per_million || 0
    const avgPrice = (inputPrice + outputPrice) / 2
    return avgPrice >= priceMin && avgPrice <= priceMax
  })
  
  // 排序（新增）
  const { sortBy, sortDesc } = advancedFilters.value
  list.sort((a, b) => {
    const capA = caps.value[a.name]
    const capB = caps.value[b.name]
    if (!capA || !capB) return 0
    
    let valA, valB
    switch (sortBy) {
      case 'price':
        valA = ((capA.input_price_per_million || 0) + (capA.output_price_per_million || 0)) / 2
        valB = ((capB.input_price_per_million || 0) + (capB.output_price_per_million || 0)) / 2
        break
      case 'context':
        valA = capA.max_input_tokens || 0
        valB = capB.max_input_tokens || 0
        break
      default: // name
        valA = a.name.toLowerCase()
        valB = b.name.toLowerCase()
        return sortDesc ? valB.localeCompare(valA) : valA.localeCompare(valB)
    }
    return sortDesc ? valB - valA : valA - valB
  })
  
  return list
})
```

### UI 组件

```vue
<template>
  <div class="discovery-toolbar">
    <!-- 现有搜索栏 -->
    <el-input v-model="searchQuery" placeholder="搜索模型组..." clearable />
    
    <!-- 现有模式按钮 -->
    <div class="mode-buttons">...</div>
    
    <!-- 高级筛选按钮 -->
    <el-button @click="showAdvanced = !showAdvanced">
      <el-icon><Filter /></el-icon>
      高级筛选
    </el-button>
  </div>
  
  <!-- 高级筛选面板 -->
  <el-collapse-transition>
    <div v-if="showAdvanced" class="advanced-filters">
      <!-- 能力筛选 -->
      <div class="filter-group">
        <label>能力:</label>
        <el-checkbox v-model="advancedFilters.vision">视觉理解</el-checkbox>
        <el-checkbox v-model="advancedFilters.functionCalling">工具调用</el-checkbox>
        <el-checkbox v-model="advancedFilters.longContext">长上下文 (>32K)</el-checkbox>
      </div>
      
      <!-- 价格筛选 -->
      <div class="filter-group">
        <label>价格:</label>
        <el-input-number v-model="advancedFilters.priceMin" :min="0" :step="0.1" />
        <span>-</span>
        <el-input-number v-model="advancedFilters.priceMax" :min="0" :step="0.1" />
        <span>/ 百万 tokens</span>
      </div>
      
      <!-- 排序 -->
      <div class="filter-group">
        <label>排序:</label>
        <el-select v-model="advancedFilters.sortBy">
          <el-option label="名称" value="name" />
          <el-option label="价格" value="price" />
          <el-option label="上下文" value="context" />
        </el-select>
        <el-button @click="advancedFilters.sortDesc = !advancedFilters.sortDesc">
          {{ advancedFilters.sortDesc ? '↓' : '↑' }}
        </el-button>
      </div>
    </div>
  </el-collapse-transition>
</template>

<style scoped>
.advanced-filters {
  margin-top: 12px;
  padding: 16px;
  background: var(--el-bg-color-page);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.filter-group {
  display: flex;
  align-items: center;
  gap: 8px;
}

.filter-group label {
  min-width: 60px;
  font-weight: 500;
  color: var(--el-text-color-regular);
}
</style>
```

## 验收标准

1. 高级筛选面板可折叠/展开
2. 能力筛选：勾选后只显示具备对应能力的模型
3. 价格筛选：输入范围后只显示平均价格在范围内的模型
4. 排序：可按名称/价格/上下文升序或降序排列
5. 多个筛选条件可组合使用
6. 移动端响应式适配（筛选面板纵向堆叠）

## 技术要点

- 纯前端改造，后端 API 无需变更
- `getGroupCapabilities()` 已返回全部所需字段
- 筛选和排序在 `filteredGroups` computed 中完成
- 保持现有搜索和模式过滤功能不变
