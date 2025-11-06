<template>
  <div class="catalog-sync-page">
    <div class="stage-panel" data-testid="stage-panel">
      <div class="stage-summary">
        <p class="stage-kicker">你当前在做什么</p>
        <h3 class="stage-title">{{ stageTitle }}</h3>
        <p class="stage-description">{{ stageDescription }}</p>
      </div>
      <div class="stage-steps" aria-label="Catalog 使用流程">
        <div class="stage-step" :class="{ active: stageStep === 1 }">
          <span class="step-index">1</span>
          <div>
            <p class="step-title">同步目录</p>
            <p class="step-hint">从上游拉取 Catalog 条目</p>
          </div>
        </div>
        <div class="stage-step" :class="{ active: stageStep === 2 }">
          <span class="step-index">2</span>
          <div>
            <p class="step-title">绑定本地模型</p>
            <p class="step-hint">把本地 model 关联到 Catalog</p>
          </div>
        </div>
        <div class="stage-step" :class="{ active: stageStep === 3 }">
          <span class="step-index">3</span>
          <div>
            <p class="step-title">处理差异</p>
            <p class="step-hint">检查并应用价格变化</p>
          </div>
        </div>
      </div>
    </div>

    <div class="action-board" data-testid="action-board">
      <div class="action-card action-card-primary">
        <p class="action-kicker">当前状态</p>
        <h3 class="action-title">{{ currentStatusTitle }}</h3>
        <p class="action-description">{{ currentStatusDescription }}</p>
      </div>
      <div class="action-card">
        <p class="action-kicker">推荐动作</p>
        <h3 class="action-title">{{ recommendedActionTitle }}</h3>
        <p class="action-description">{{ recommendedActionDescription }}</p>
        <div class="action-buttons">
          <el-button
            v-if="recommendedActionKey === 'sync'"
            type="primary"
            :loading="refreshing || syncRunning"
            data-testid="recommended-action-sync"
            @click="handleManualRefresh"
          >
            {{ syncRunning ? '同步中…' : '立即同步目录' }}
          </el-button>
          <el-button
            v-else-if="recommendedActionKey === 'bind'"
            type="primary"
            :loading="autoBinding"
            data-testid="recommended-action-bind"
            @click="handleAutoBind"
          >
            去处理未关联模型
          </el-button>
          <el-button
            v-else-if="recommendedActionKey === 'review_diff'"
            type="primary"
            data-testid="recommended-action-diff"
            @click="scrollToDiffSection"
          >
            去查看差异列表
          </el-button>
          <el-button
            v-else
            data-testid="recommended-action-idle"
            @click="$router.push('/admin/models')"
          >
            去模型管理查看详情
          </el-button>
        </div>
      </div>
      <div class="action-card">
        <p class="action-kicker">可选动作</p>
        <ul class="action-list">
          <li>如果你怀疑目录数据旧了，可以随时点击“手动刷新目录”。</li>
          <li>如果自动绑定结果不符合预期，可以去“模型管理”手动调整绑定关系。</li>
          <li>如果差异很多，建议先筛选“已脱离目录”或“价格未设”的模型优先处理。</li>
        </ul>
      </div>
    </div>

    <!-- 顶部：同步元信息 + 操作按钮 -->
    <div class="page-header">
      <div>
        <h2 class="page-title">模型目录管理</h2>
        <p class="page-subtitle">
          当前目录已收录
          <strong>{{ catalogTotal }}</strong>
          个模型参考项
          <span v-if="syncedAtText" class="sync-meta">
            · 最近同步于 <span class="sync-time">{{ syncedAtText }}</span>
          </span>
          ·
          <strong>{{ diffRows.length }}</strong>
          个本地模型需要你进一步确认（当前筛选 {{ filteredRows.length }} 条）
        </p>
      </div>
      <div class="header-actions">
        <el-button
          type="primary"
          :loading="autoBinding"
          @click="handleAutoBind"
          title="把尚未关联 Catalog 的本地模型，按 name / upstream_model_id 自动匹配到目录条目；同名多个候选只列出候选，不会盲绑。"
          >处理未关联模型</el-button
        >
        <el-button :loading="refreshing || syncRunning" @click="handleManualRefresh">
          {{ syncRunning ? '同步中…' : '手动刷新目录' }}
        </el-button>
      </div>
    </div>

    <div class="tab-nav" data-testid="tab-nav">
      <button
        type="button"
        class="tab-nav-item"
        :class="{ active: currentTab === 'sync' }"
        @click="switchTab('sync')"
      >
        目录更新
      </button>
      <button
        type="button"
        class="tab-nav-item"
        :class="{ active: currentTab === 'bind' }"
        @click="switchTab('bind')"
      >
        模型对应
      </button>
      <button
        type="button"
        class="tab-nav-item"
        :class="{ active: currentTab === 'diff' }"
        @click="switchTab('diff')"
      >
        价格差异
      </button>
    </div>

    <div class="tab-intro" data-testid="tab-intro">
      <h3 class="section-title">{{ currentTabTitle }}</h3>
      <p class="section-description">{{ currentTabDescription }}</p>
    </div>

    <div class="workspace-grid" data-testid="workspace-grid">
      <section v-if="currentTab === 'sync'" class="workspace-card" :class="workspaceSyncStatus.panelClassName">
        <div class="workspace-head">
          <p class="workspace-kicker">任务区 1</p>
          <div class="workspace-title-row">
            <h4>同步 Catalog 目录</h4>
            <span class="workspace-status" :class="workspaceSyncStatus.className">{{ workspaceSyncStatus.label }}</span>
          </div>
        </div>
        <p class="workspace-text">
          先把平台提供的模型目录同步到本地。没有这份目录，后面的关联和价格对比都无法进行。
        </p>
        <p class="workspace-why">页面里的参考价格和状态说明，都来自这一步。</p>
        <div class="workspace-actions">
          <el-button :loading="refreshing || syncRunning" @click="handleManualRefresh">
            {{ syncRunning ? '同步中…' : '更新目录数据' }}
          </el-button>
        </div>
      </section>

      <section v-if="currentTab === 'bind'" class="workspace-card" :class="workspaceBindStatus.panelClassName">
        <div class="workspace-head">
          <p class="workspace-kicker">任务区 2</p>
          <div class="workspace-title-row">
            <h4>绑定本地模型</h4>
            <span class="workspace-status" :class="workspaceBindStatus.className">{{ workspaceBindStatus.label }}</span>
          </div>
        </div>
        <p class="workspace-text">
          把你已经接入的本地模型，对应到目录里的参考项。没完成对应的模型，不会进入差异检查。
        </p>
        <p class="workspace-why">很多“这里为什么没东西”的情况，其实只是模型还没对应到目录。</p>
        <div class="workspace-actions">
          <el-button
            type="primary"
            :loading="autoBinding"
            @click="handleAutoBind"
            title="把尚未关联 Catalog 的本地模型，按 name / upstream_model_id 自动匹配到目录条目；同名多个候选只列出候选，不会盲绑。"
          >
            处理未对应模型
          </el-button>
          <el-button @click="$router.push('/admin/models')">去模型列表手动处理</el-button>
        </div>
      </section>

      <section v-if="currentTab === 'diff'" class="workspace-card" :class="workspaceDiffStatus.panelClassName">
        <div class="workspace-head">
          <p class="workspace-kicker">任务区 3</p>
          <div class="workspace-title-row">
            <h4>处理价格差异</h4>
            <span class="workspace-status" :class="workspaceDiffStatus.className">{{ workspaceDiffStatus.label }}</span>
          </div>
        </div>
        <p class="workspace-text">
          这里只展示“已经完成对应”的模型里，价格需要确认、或者目录提示即将下线的项目。
        </p>
        <p class="workspace-why">这里为空，通常表示价格本来就一致，或者模型还没完成目录对应。</p>
        <div class="workspace-subtypes">
          <div class="workspace-subtype">
            <strong>价格未设</strong>
            <p>本地价格是 $0/$0，但目录里已经有参考价格。通常表示模型已经完成对应，但你还没决定是否直接采用参考价。</p>
          </div>
          <div class="workspace-subtype">
            <strong>已脱离目录</strong>
            <p>本地价格已经设置过，而且和目录里的参考价格不同。通常表示你曾手动改过价格，或者目录后来更新了。</p>
          </div>
        </div>
        <div class="workspace-actions">
          <el-button @click="scrollToDiffSection">查看差异列表</el-button>
        </div>
      </section>
    </div>

    <!--
      同步状态横幅（修复 B）：在用户视野最显眼的位置展示后端 sync-status 的"事实"。
      - running=true → 蓝色 info，提示"正在同步…"
      - 有 last_error → 红色 error，把上次失败原因明确摆出来
      - 成功且来源 bundled → 黄色 warning，提示走了回退链路（远程不可达）
      - 成功且来源 remote → 绿色 success，简要展示"同步了 N 条 · 耗时 Xs · X 前"

      为什么这是关键修复：之前用户点了"手动刷新"，前端只 setTimeout(4000) 然后
      重新拉 diff —— 一旦后端 sync 实际上失败了，用户既看不到错误，也无从知道
      为什么 catalog_total 没变。这个横幅把后端真实状态贴脸告诉用户。
    -->
    <el-alert
      v-if="currentTab === 'sync' && syncStatusBanner"
      :type="syncStatusBanner.type"
      :title="syncStatusBanner.title"
      :description="syncStatusBanner.description"
      :closable="syncStatusBanner.closable"
      show-icon
      class="sync-status-banner"
      data-testid="sync-status-banner"
    />

    <!-- 空状态：完全没有 diff（按 bindingStats 分四种语义） -->
    <el-empty
      v-if="currentTab === 'diff' && !loading && !diffRows.length"
      description=""
      class="empty-state"
      data-testid="empty-state"
    >
      <template #description>
        <div class="empty-desc">
          <!-- 情形 1：目录为空（同步未触发或失败）。bindingStats 取不到时也走这里兜底。 -->
          <template v-if="emptyKind === 'catalog_empty'">
            <p class="empty-title" data-testid="empty-title">
              ⚠️ 目录为空，同步尚未生效
            </p>
            <p class="empty-hint">
              目录里还没有任何上游模型条目。点下方按钮触发后台同步，
              系统会先尝试拉取远程 LiteLLM 清单，失败时自动回退到内置快照。
            </p>
            <div class="empty-actions">
              <el-button
                type="primary"
                :loading="refreshing"
                data-testid="empty-cta-sync"
                @click="handleManualRefresh"
              >
                立即同步目录
              </el-button>
            </div>
          </template>

          <!-- 情形 2：本地一个 model 都没注册 → 让用户先去 ModelListPage。 -->
          <template v-else-if="emptyKind === 'no_local_models'">
            <p class="empty-title" data-testid="empty-title">
              还没有注册任何本地模型
            </p>
            <p class="empty-hint">
              目录已收录 <strong>{{ catalogTotal }}</strong> 个上游模型，
              但本地一个 model 都还没有。请先到「模型管理」注册要使用的模型，
              或直接从目录条目一键创建。
            </p>
            <div class="empty-actions">
              <el-button
                type="primary"
                data-testid="empty-cta-models"
                @click="$router.push('/admin/models')"
              >
                前往模型管理
              </el-button>
            </div>
          </template>

          <!-- 情形 3：本地有 model 但都没绑定到目录 → 引导点「智能绑定」。 -->
          <template v-else-if="emptyKind === 'all_unbound'">
            <p class="empty-title" data-testid="empty-title">
              本地模型还没有绑定到目录
            </p>
            <p class="empty-hint">
              目录已收录 <strong>{{ catalogTotal }}</strong> 个上游模型，
              本地有 <strong>{{ bindingStats?.unbound_count }}</strong> 个 model 尚未绑定。
              绑定之后才能看到价格与能力差异、自动跟随目录更新。
            </p>
            <p class="empty-hint empty-next-step">
              下一步建议：先把这些未对应模型和目录关联起来，再回来处理价格差异。
            </p>
            <div class="empty-actions">
              <el-button
                type="primary"
                :loading="autoBinding"
                data-testid="empty-cta-autobind"
                @click="handleAutoBind"
              >
                去绑定这 {{ bindingStats?.unbound_count }} 个未关联模型
              </el-button>
              <el-button
                data-testid="empty-cta-manual-bind"
                @click="$router.push('/admin/models')"
              >
                去模型管理手动处理
              </el-button>
            </div>
          </template>

          <!-- 情形 4：部分绑定 + 部分未绑定，diff 没差异（已绑定的与目录一致）。 -->
          <template v-else-if="emptyKind === 'mixed_no_diff'">
            <p class="empty-title" data-testid="empty-title">
              已绑定模型与目录完全一致
            </p>
            <p class="empty-hint">
              <strong>{{ bindingStats?.bound_count }}</strong> 个已绑定 model
              的价格 / 上下文 / 能力位与目录当前快照吻合，没有需要处理的差异。
              另外还有 <strong>{{ bindingStats?.unbound_count }}</strong> 个未绑定 model，
              如果想纳入目录管理可点下方按钮。
            </p>
            <p class="empty-hint empty-next-step">
              你现在不需要处理差异；下一步更有价值的是，把剩余未对应模型纳入目录管理。
            </p>
            <div class="empty-actions">
              <el-button
                type="primary"
                :loading="autoBinding"
                data-testid="empty-cta-autobind"
                @click="handleAutoBind"
              >
                去绑定剩余 {{ bindingStats?.unbound_count }} 个未关联模型
              </el-button>
            </div>
          </template>

          <!-- 情形 5：所有 model 都已绑定且无差异 —— 真正的"一切安好"。 -->
          <template v-else>
            <p class="empty-title" data-testid="empty-title">
              一切就绪，无需处理
            </p>
            <p class="empty-hint">
              目录已收录 <strong>{{ catalogTotal }}</strong> 个上游模型；
              本地 <strong>{{ bindingStats?.bound_count || 0 }}</strong> 个 model 全部绑定到目录，
              且与目录当前快照的价格 / 上下文 / 能力位完全一致。
            </p>
          </template>
        </div>
      </template>
    </el-empty>

    <!-- 筛选区 -->
    <div v-if="currentTab === 'diff' && diffRows.length" class="filter-bar" data-testid="diff-section">
      <div class="filter-header">
        <h3 class="section-title">差异筛选</h3>
        <p class="section-description">在这里筛选“已经完成目录对应”的模型里，哪些需要你关注和处理。</p>
      </div>
      <el-input
        v-model="searchKeyword"
        placeholder="搜索本地模型名或目录上游 ID"
        clearable
        :prefix-icon="Search"
        class="search-input"
      />
      <el-select
        v-model="statusFilters"
        placeholder="按状态筛选"
        multiple
        collapse-tags
        collapse-tags-tooltip
        clearable
        class="status-select"
      >
        <el-option label="已脱离目录（override）" value="overridden" />
        <el-option label="已废弃（deprecation）" value="deprecated" />
        <el-option label="涨价（catalog &gt; local）" value="price_up" />
        <el-option label="降价（catalog &lt; local）" value="price_down" />
        <el-option label="本地价未设（local=0）" value="price_unset" />
      </el-select>
      <div class="threshold-control">
        <span class="threshold-label">|Δ| ≥</span>
        <el-input-number
          v-model="minAbsDelta"
          :min="0"
          :max="100"
          :step="5"
          size="default"
          controls-position="right"
        />
        <span class="threshold-label">%</span>
      </div>
      <el-select v-model="sortKey" class="sort-select">
        <el-option label="按 |Δ| 降序（变化大的先看）" value="delta_desc" />
        <el-option label="按 |Δ| 升序" value="delta_asc" />
        <el-option label="按本地模型名（A→Z）" value="name_asc" />
        <el-option label="按目录上游 ID（A→Z）" value="catalog_asc" />
        <el-option label="按目录价（高→低）" value="price_desc" />
      </el-select>
      <el-button @click="resetFilters" :disabled="!hasFilter">重置</el-button>
      <el-button @click="exportCSV" :icon="Download">导出 CSV</el-button>
    </div>

    <div v-if="currentTab === 'diff' && diffRows.length" class="diff-explainer" data-testid="diff-explainer">
      <div class="diff-explainer-main">
        <h3 class="section-title">你现在看到的是什么</h3>
        <p class="section-description">
          这里只显示“已经完成目录对应”的模型里，需要你进一步确认的项目：
          可能是本地价格还没填，也可能是本地价格已经和目录参考价不一致，或者目录提示该模型即将下线。
        </p>
      </div>
      <div v-if="deprecatedCount > 0" class="deprecation-banner" data-testid="deprecation-banner">
        <strong>优先关注：</strong>
        当前有 {{ deprecatedCount }} 个项目对应的目录条目标记了下线时间，建议优先确认是否需要切换模型。
      </div>
      <div class="diff-summary-grid">
        <div class="diff-summary-card">
          <p class="diff-summary-label">待补价格</p>
          <strong class="diff-summary-value">{{ priceUnsetCount }}</strong>
          <p class="diff-summary-hint">本地价格还是 $0/$0，但目录里已经有参考价格。</p>
        </div>
        <div class="diff-summary-card">
          <p class="diff-summary-label">已脱离目录</p>
          <strong class="diff-summary-value">{{ overriddenCount }}</strong>
          <p class="diff-summary-hint">本地价格已经填写过，但和目录里的参考价格不同。</p>
        </div>
        <div class="diff-summary-card">
          <p class="diff-summary-label">废弃提醒</p>
          <strong class="diff-summary-value">{{ deprecatedCount }}</strong>
          <p class="diff-summary-hint">目录提示该模型即将下线，建议优先检查。</p>
        </div>
      </div>
    </div>

    <!-- 差异表格 -->
    <div v-if="currentTab === 'diff' && diffRows.length" class="diff-groups">
      <section v-if="priceUnsetRows.length" class="diff-group" data-testid="group-price-unset">
        <div class="diff-group-header">
          <h3 class="section-title">待补价格</h3>
          <p class="section-description">这些模型已经完成目录对应，但本地价格还是 $0/$0。你现在要决定是否直接采用目录里的参考价格。</p>
          <div class="diff-group-actions">
            <el-button size="small" @click="selectPriceUnsetRows">选中这组 {{ priceUnsetRows.length }} 条</el-button>
          </div>
        </div>
        <el-table
          v-loading="loading"
          :data="pagedPriceUnsetRows"
          border
          stripe
          row-key="model_id"
          @selection-change="handleSelectionChange"
        >
          <el-table-column type="selection" width="45" :reserve-selection="true" />
          <el-table-column label="本地模型" prop="model_name" min-width="240">
            <template #default="{ row }">
              <span class="mono">{{ row.model_name }}</span>
              <el-tag type="warning" size="small" class="badge-override">价格未设</el-tag>
              <el-tag
                v-if="row.catalog_deprecation_date"
                type="warning"
                size="small"
                class="badge-deprecation"
                >⏰ {{ formatDate(row.catalog_deprecation_date) }} 废弃</el-tag
              >
            </template>
          </el-table-column>
          <el-table-column label="目录上游" min-width="220">
            <template #default="{ row }">
              <span class="mono dim">{{ row.catalog_source_model_id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="本地价 (in/out, $/M tok)" width="200" align="right">
            <template #default="{ row }">
              {{ formatPrice(row.local_input_price) }} /
              {{ formatPrice(row.local_output_price) }}
            </template>
          </el-table-column>
          <el-table-column label="目录价 (in/out, $/M tok)" width="200" align="right">
            <template #default="{ row }">
              {{ formatPrice(row.catalog_input_price) }} /
              {{ formatPrice(row.catalog_output_price) }}
            </template>
          </el-table-column>
          <el-table-column label="建议" width="180" align="center">
            <template #default>
              <span class="delta-flat">未设 → 跟随</span>
            </template>
          </el-table-column>
        </el-table>
      </section>

      <section v-if="followupRows.length" class="diff-group" data-testid="group-followup">
        <div class="diff-group-header">
          <h3 class="section-title">已偏离参考价 / 需要复核</h3>
          <p class="section-description">这些模型已经设置过本地价格，但和目录里的参考价格不同。你需要判断是继续保留当前设置，还是改回参考价。</p>
          <div class="diff-group-actions">
            <el-button size="small" @click="selectFollowupRows">选中这组 {{ followupRows.length }} 条</el-button>
          </div>
        </div>
        <el-table
          v-loading="loading"
          :data="pagedFollowupRows"
          border
          stripe
          row-key="model_id"
          @selection-change="handleSelectionChange"
        >
          <el-table-column type="selection" width="45" :reserve-selection="true" />
          <el-table-column label="本地模型" prop="model_name" min-width="240">
            <template #default="{ row }">
              <span class="mono">{{ row.model_name }}</span>
              <el-tag
                v-if="row.is_overridden"
                type="danger"
                size="small"
                class="badge-override"
                >已脱离目录</el-tag
              >
              <el-tag
                v-if="row.catalog_deprecation_date"
                type="warning"
                size="small"
                class="badge-deprecation"
                >⏰ {{ formatDate(row.catalog_deprecation_date) }} 废弃</el-tag
              >
            </template>
          </el-table-column>
          <el-table-column label="目录上游" min-width="220">
            <template #default="{ row }">
              <span class="mono dim">{{ row.catalog_source_model_id }}</span>
            </template>
          </el-table-column>
          <el-table-column label="本地价 (in/out, $/M tok)" width="200" align="right">
            <template #default="{ row }">
              {{ formatPrice(row.local_input_price) }} /
              {{ formatPrice(row.local_output_price) }}
            </template>
          </el-table-column>
          <el-table-column label="目录价 (in/out, $/M tok)" width="200" align="right">
            <template #default="{ row }">
              {{ formatPrice(row.catalog_input_price) }} /
              {{ formatPrice(row.catalog_output_price) }}
            </template>
          </el-table-column>
          <el-table-column label="变化%" width="180" align="center">
            <template #default="{ row }">
              <span :class="deltaClass(row.input_delta_pct)">
                {{ deltaArrow(row.input_delta_pct) }}
                {{ formatPct(row.input_delta_pct) }}
              </span>
              /
              <span :class="deltaClass(row.output_delta_pct)">
                {{ deltaArrow(row.output_delta_pct) }}
                {{ formatPct(row.output_delta_pct) }}
              </span>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </div>

    <!-- 底部：分页 + 操作 -->
    <div v-if="currentTab === 'diff' && diffRows.length" class="footer-bar">
      <div class="footer-intro">
        <h3 class="section-title">下一步动作</h3>
        <p class="section-description">勾选你确认要采用目录参考价格的模型，然后批量更新本地价格。</p>
        <p class="footer-risk-note">
          注意：执行后会直接把所选模型的本地价格改成目录里的参考价格。
        </p>
      </div>
      <el-pagination
        v-if="filteredRows.length > pageSize"
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="filteredRows.length"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        background
      />
      <div class="footer-actions">
        <el-button @click="selectAllFiltered" :disabled="!filteredRows.length"
          >全选当前筛选 ({{ filteredRows.length }})</el-button
        >
        <el-button @click="clearSelection" :disabled="!selectedIds.length"
          >清空选择</el-button
        >
        <el-button type="primary" :loading="applying" @click="handleApply">
          采用目录参考价格{{ selectedIds.length ? `(${selectedIds.length})` : '' }}
        </el-button>
      </div>
    </div>

    <!-- 智能绑定结果弹窗 -->
    <el-dialog
      v-model="autoBindDialogVisible"
      title="智能绑定结果"
      width="640px"
    >
      <div v-if="autoBindResult" class="auto-bind-summary">
        <p>
          <el-tag type="success" effect="dark"
            >已绑定 {{ autoBindResult.matched.length }} 条</el-tag
          >
          <el-tag type="warning" effect="dark" class="ml-8"
            >多候选待选 {{ autoBindResult.ambiguous.length }} 条</el-tag
          >
          <el-tag class="ml-8">未匹配 {{ autoBindResult.unmatched }} 条</el-tag>
        </p>

        <template v-if="autoBindResult.matched.length">
          <h4>已自动绑定</h4>
          <ul class="bind-list">
            <li
              v-for="m in autoBindResult.matched.slice(0, 30)"
              :key="m.model_id"
            >
              <span class="mono">{{ m.model_name }}</span>
              <span class="arrow">→</span>
              <span class="mono dim">{{ m.catalog_source_model_id }}</span>
            </li>
          </ul>
          <p v-if="autoBindResult.matched.length > 30" class="ellipsis">
            …等共 {{ autoBindResult.matched.length }} 条
          </p>
        </template>

        <template v-if="autoBindResult.ambiguous.length">
          <h4>多候选（请逐条选择目录条目，再点「保存已选」批量入库）</h4>
          <table class="ambiguous-table" data-testid="ambiguous-table">
            <thead>
              <tr>
                <th>本地模型</th>
                <th>目录条目</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="a in autoBindResult.ambiguous"
                :key="a.model_id"
                data-testid="ambiguous-row"
              >
                <td class="mono">{{ a.model_name }}</td>
                <td>
                  <el-select
                    v-model="ambiguousSelections[a.model_id]"
                    placeholder="─请选择─"
                    clearable
                    size="small"
                    style="width: 100%"
                    :data-testid="`ambiguous-select-${a.model_id}`"
                  >
                    <el-option
                      v-for="c in a.candidates"
                      :key="c.catalog_id"
                      :label="`${c.provider || '-'} / ${c.catalog_source_model_id}`"
                      :value="c.catalog_id"
                    />
                  </el-select>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="ambiguous-footer">
            <el-checkbox
              v-model="ambiguousResetFields"
              data-testid="ambiguous-reset-fields"
            >
              同时把价格、上下文长度、最大输出、上游模型 ID 重置为目录的当前值
            </el-checkbox>
          </div>
        </template>
      </div>
      <template #footer>
        <el-button @click="autoBindDialogVisible = false">关闭</el-button>
        <el-button
          v-if="autoBindResult && autoBindResult.ambiguous.length"
          type="primary"
          :loading="batchBinding"
          :disabled="ambiguousSelectedCount === 0"
          data-testid="ambiguous-save-btn"
          @click="handleAmbiguousSave"
        >
          保存已选 {{ ambiguousSelectedCount }} 条
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search, Download } from '@element-plus/icons-vue'
import {
  getCatalogDiff,
  applyCatalogDiff,
  triggerCatalogSync,
  searchCatalog,
  autoBindCatalog,
  batchBindModelCatalog,
  getCatalogBindingStats,
  getCatalogSyncStatus
} from '@/services/control-plane'
import type {
  PriceDiffEntry,
  AutoBindResult,
  CatalogBindingStats,
  CatalogSyncStatus
} from '@/types/catalog'

// ===================== 数据 / 状态 =====================
const diffRows = ref<PriceDiffEntry[]>([])
const refreshing = ref(false)
const applying = ref(false)
const loading = ref(false)
const catalogTotal = ref(0)
const lastSyncedAt = ref<string>('')
const autoBinding = ref(false)
const autoBindDialogVisible = ref(false)
const autoBindResult = ref<AutoBindResult | null>(null)
const currentTab = ref<'sync' | 'bind' | 'diff'>('sync')

// 目录绑定状态：catalog 总数 + 本地 model 已绑定 / 未绑定计数。
// 空状态区域用它来精准判定"diff 为空到底意味着啥"。
const bindingStats = ref<CatalogBindingStats | null>(null)

// 目录同步任务状态（来自 GET /api/v1/model-catalog/sync-status）。
// 修复 B：用它驱动顶部状态横幅，让用户能直接看到"上次同步成功/失败/在跑"。
const syncStatus = ref<CatalogSyncStatus | null>(null)
// CAT-2: ambiguous 段每个 model_id → 选中的 catalog_id（admin 显式选定）
const ambiguousSelections = ref<Record<string, string>>({})
const ambiguousResetFields = ref(false)
const batchBinding = ref(false)

const ambiguousSelectedCount = computed(() => {
  return Object.values(ambiguousSelections.value).filter((v) => !!v).length
})

// ===================== 筛选 / 排序 / 分页 =====================
type StatusFilter =
  | 'overridden'
  | 'deprecated'
  | 'price_up'
  | 'price_down'
  | 'price_unset'
type SortKey =
  | 'delta_desc'
  | 'delta_asc'
  | 'name_asc'
  | 'catalog_asc'
  | 'price_desc'

const searchKeyword = ref('')
const statusFilters = ref<StatusFilter[]>([])
const minAbsDelta = ref(0)
const sortKey = ref<SortKey>('delta_desc')
const currentPage = ref(1)
const pageSize = ref(20)

// 跨页保留的选中行（map<id, row>），表格本身的 reserve-selection 也会保留，
// 但我们也要在程序化全选/清空时手动管理。
const selectedIds = ref<string[]>([])

const hasFilter = computed(
  () =>
    searchKeyword.value !== '' ||
    statusFilters.value.length > 0 ||
    minAbsDelta.value > 0 ||
    sortKey.value !== 'delta_desc'
)

const syncedAtText = computed(() =>
  lastSyncedAt.value ? new Date(lastSyncedAt.value).toLocaleString() : ''
)

/**
 * 空状态语义判定。把"diff 为空"细分成 5 种 case，
 * 让模板能精准给出"该点哪个按钮"的指引。
 *
 * - 'catalog_empty'      目录里没东西 / stats 加载失败 → 引导同步
 * - 'no_local_models'    本地一个 model 都没有        → 引导去模型管理
 * - 'all_unbound'        本地有 model 但都没绑定      → 引导点智能绑定
 * - 'mixed_no_diff'      部分绑定 + 部分未绑定，已绑的一致 → 继续绑余下
 * - 'all_bound_no_diff'  全部绑定且无差异              → 一切安好
 */
type EmptyKind =
  | 'catalog_empty'
  | 'no_local_models'
  | 'all_unbound'
  | 'mixed_no_diff'
  | 'all_bound_no_diff'

const emptyKind = computed<EmptyKind>(() => {
  const s = bindingStats.value
  // stats 加载失败时按"目录空"兜底。即使后端真有数据，也促使用户去
  // 重试一次同步，比展示一段含糊文案要好。
  if (!s) return 'catalog_empty'
  if (s.catalog_total <= 0) return 'catalog_empty'
  if (s.model_total <= 0) return 'no_local_models'
  if (s.bound_count <= 0) return 'all_unbound'
  if (s.unbound_count > 0) return 'mixed_no_diff'
  return 'all_bound_no_diff'
})

// 同步状态便利访问：用于按钮 loading + "同步中" 显示
const syncRunning = computed(() => !!syncStatus.value?.running)

const preferredTab = computed<'sync' | 'bind' | 'diff'>(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) return 'sync'
  if (s.unbound_count > 0) return 'bind'
  return 'diff'
})

const stageStep = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) return 1
  if (s.unbound_count > 0) return 2
  return 3
})

const stageTitle = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) return '第 1 步：先同步 Catalog 目录'
  if (s.model_total <= 0) return '第 2 步：先注册本地模型，再绑定到 Catalog'
  if (s.unbound_count > 0) return `第 2 步：还有 ${s.unbound_count} 个本地模型未关联 Catalog`
  if (diffRows.value.length > 0) return `第 3 步：当前有 ${diffRows.value.length} 个已绑定模型存在差异`
  return '第 3 步已完成：已绑定模型当前与 Catalog 一致'
})

const stageDescription = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) {
    return '先把上游 Catalog 同步到本地；没有目录数据时，后面的绑定和差异对比都无从进行。'
  }
  if (s.model_total <= 0) {
    return '目录已经有数据，但你还没有注册任何本地模型；先去模型管理创建或导入模型。'
  }
  if (s.unbound_count > 0) {
    return '当前页面推荐你先处理“未关联模型”。没绑定到 Catalog 的模型不会参与价格差异对比，也不会自动跟随目录更新。'
  }
  if (diffRows.value.length > 0) {
    return '现在可以专注处理差异：这些模型已经绑定了 Catalog，但本地价格与目录价格不一致，或者目录条目已废弃。'
  }
  return '当前不需要处理差异；目录、绑定关系和已绑定模型价格都处于一致状态。'
})

const currentStatusTitle = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) return 'Catalog 目录还没有准备好'
  if (s.model_total <= 0) return '目录有数据，但本地还没有模型'
  if (s.unbound_count > 0 && s.bound_count <= 0) return `当前有 ${s.unbound_count} 个本地模型完全未关联 Catalog`
  if (s.unbound_count > 0) return `当前还有 ${s.unbound_count} 个本地模型未关联 Catalog`
  if (diffRows.value.length > 0) return `当前有 ${diffRows.value.length} 个已绑定模型存在差异`
  return '当前已绑定模型与 Catalog 保持一致'
})

const currentStatusDescription = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) {
    return '目录里还没有可用的模型目录数据。'
  }
  if (s.model_total <= 0) {
    return '目录已有数据，但你还没有本地模型。'
  }
  if (s.unbound_count > 0 && s.bound_count <= 0) {
    return '这些模型还没对应到目录，所以系统暂时无法判断它们和目录之间的价格差异。'
  }
  if (s.unbound_count > 0) {
    return '已完成对应的模型目前没有差异，但剩余模型还没纳入目录管理。'
  }
  if (diffRows.value.length > 0) {
    return '这些模型已经完成目录对应，但本地价格和目录参考价不同，或目录提示该模型即将下线。'
  }
  return '你当前已经完成目录同步、模型对应和差异处理。'
})

const recommendedActionKey = computed<'sync' | 'bind' | 'review_diff' | 'idle'>(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) return 'sync'
  if (s.model_total <= 0) return 'idle'
  if (s.unbound_count > 0) return 'bind'
  if (diffRows.value.length > 0) return 'review_diff'
  return 'idle'
})

const recommendedActionTitle = computed(() => {
  switch (recommendedActionKey.value) {
    case 'sync':
      return '先把目录同步下来'
    case 'bind':
      return '先处理未关联模型'
    case 'review_diff':
      return '先确认并处理差异'
    default:
      return '当前没有必须立刻执行的动作'
  }
})

const recommendedActionDescription = computed(() => {
  switch (recommendedActionKey.value) {
    case 'sync':
      return '先把模型目录拉下来，后面的对应和差异判断才能成立。'
    case 'bind':
      return '没对应到目录的模型，不会进入差异列表。'
    case 'review_diff':
      return '确认哪些模型采用目录参考价，哪些继续保留当前价格。'
    default:
      return '如果只是想核对细节，可以去模型列表查看单个模型状态。'
  }
})

const currentTabTitle = computed(() => {
  switch (currentTab.value) {
    case 'sync':
      return '目录更新'
    case 'bind':
      return '模型对应'
    default:
      return '价格差异'
  }
})

const currentTabDescription = computed(() => {
  switch (currentTab.value) {
    case 'sync':
      return '这里只处理目录数据本身：是否同步成功、同步了多少、最近一次更新时间和失败原因。'
    case 'bind':
      return '这里只处理“本地模型如何对应到目录参考项”。没完成对应的模型，不会进入价格差异检查。'
    default:
      return '这里只处理“已经完成目录对应”的模型价格问题，包括待补价格、偏离参考价和下线提醒。'
  }
})

const priceUnsetCount = computed(() => diffRows.value.filter((row) => isPriceUnset(row)).length)
const overriddenCount = computed(() => diffRows.value.filter((row) => row.is_overridden).length)
const deprecatedCount = computed(() => diffRows.value.filter((row) => !!row.catalog_deprecation_date).length)
const priceUnsetRows = computed(() => sortedRows.value.filter((row) => isPriceUnset(row)))
const followupRows = computed(() => sortedRows.value.filter((row) => !isPriceUnset(row)))
const pagedPriceUnsetRows = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return priceUnsetRows.value.slice(start, start + pageSize.value)
})
const pagedFollowupRows = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return followupRows.value.slice(start, start + pageSize.value)
})

const workspaceSyncStatus = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) {
    return { label: '阻塞', className: 'status-blocked', panelClassName: 'panel-blocked' }
  }
  if (syncRunning.value) {
    return { label: '进行中', className: 'status-pending', panelClassName: 'panel-pending' }
  }
  return { label: '已完成', className: 'status-done', panelClassName: 'panel-done' }
})

const workspaceBindStatus = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) {
    return { label: '等待上一步', className: 'status-blocked', panelClassName: 'panel-blocked' }
  }
  if (s.model_total <= 0) {
    return { label: '待处理', className: 'status-pending', panelClassName: 'panel-pending' }
  }
  if (s.unbound_count > 0) {
    return { label: '待处理', className: 'status-pending', panelClassName: 'panel-pending' }
  }
  return { label: '已完成', className: 'status-done', panelClassName: 'panel-done' }
})

const workspaceDiffStatus = computed(() => {
  const s = bindingStats.value
  if (!s || s.catalog_total <= 0) {
    return { label: '等待上一步', className: 'status-blocked', panelClassName: 'panel-blocked' }
  }
  if (s.bound_count <= 0) {
    return { label: '等待绑定', className: 'status-blocked', panelClassName: 'panel-blocked' }
  }
  if (diffRows.value.length > 0) {
    return { label: '待处理', className: 'status-pending', panelClassName: 'panel-pending' }
  }
  return { label: '已完成', className: 'status-done', panelClassName: 'panel-done' }
})

/**
 * 同步状态横幅配置。null 表示不显示横幅（从未跑过 + 不在跑 + 没历史错误）。
 *
 * 设计原则：
 *  - 不在跑、没历史 → 不显示（避免横幅噪音占地方）
 *  - 在跑 → 蓝色 info，提示用户等待
 *  - 有 last_error → 红色 error，把上次失败原因显出来（最重要）
 *  - 成功 bundled → 黄色 warning，提示走了回退（远程不可达，admin 该关注）
 *  - 成功 remote → 绿色 success，简要回执，可关闭
 *
 * 时间字段用了 Date.toLocaleString()，浏览器自动按本地时区显示。
 */
type BannerType = 'success' | 'info' | 'warning' | 'error'
const syncStatusBanner = computed<
  { type: BannerType; title: string; description: string; closable: boolean } | null
>(() => {
  const s = syncStatus.value
  if (!s) return null

  if (s.running) {
    return {
      type: 'info',
      title: '正在同步目录…',
      description: s.last_started_at
        ? `本次开始于 ${new Date(s.last_started_at).toLocaleString()}，请稍候。`
        : '请稍候，结果将自动刷新。',
      closable: false
    }
  }

  // 不在跑：根据 last_error / last_source 决定横幅类型
  if (s.last_error) {
    return {
      type: 'error',
      title: '上次同步失败',
      description: `错误：${s.last_error}` +
        (s.last_finished_at
          ? `（${new Date(s.last_finished_at).toLocaleString()}）`
          : ''),
      closable: true
    }
  }

  // 没有错误且也没跑过 → 不显示横幅
  if (!s.last_finished_at) return null

  const durSec = (s.last_duration_ms / 1000).toFixed(1)
  const finishedText = new Date(s.last_finished_at).toLocaleString()

  if (s.last_source === 'bundled') {
    return {
      type: 'warning',
      title: '已用内置兜底数据完成同步',
      description: `远程目录拉取失败，已回退到内置快照（${s.last_count} 条 · 耗时 ${durSec}s · ${finishedText}）。建议检查 GitHub raw 链路是否可达。`,
      closable: true
    }
  }

  if (s.last_source === 'remote') {
    return {
      type: 'success',
      title: '目录已同步',
      description: `从远程拉取 ${s.last_count} 条 · 耗时 ${durSec}s · ${finishedText}`,
      closable: true
    }
  }

  // last_source 为空但又有 last_finished_at —— 理论上不应发生，
  // 保留兜底文案防止 UI 留白让人困惑。
  return {
    type: 'info',
    title: '目录同步已完成',
    description: `${finishedText} · 写入 ${s.last_count} 条`,
    closable: true
  }
})

// ===================== 过滤 + 排序 + 分页 =====================
function isPriceUnset(row: PriceDiffEntry): boolean {
  // 本地 in/out 都为 0 但目录至少一个 > 0 → 视为"未设置，建议跟随目录"
  return (
    Number(row.local_input_price) === 0 &&
    Number(row.local_output_price) === 0 &&
    (Number(row.catalog_input_price) > 0 ||
      Number(row.catalog_output_price) > 0)
  )
}

function maxAbsDelta(row: PriceDiffEntry): number {
  return Math.max(
    Math.abs(Number(row.input_delta_pct) || 0),
    Math.abs(Number(row.output_delta_pct) || 0)
  )
}

const filteredRows = computed<PriceDiffEntry[]>(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  const flags = new Set(statusFilters.value)
  const threshold = minAbsDelta.value

  return diffRows.value.filter((r) => {
    if (kw) {
      const a = (r.model_name || '').toLowerCase()
      const b = (r.catalog_source_model_id || '').toLowerCase()
      if (!a.includes(kw) && !b.includes(kw)) return false
    }
    if (flags.size > 0) {
      const matchAny =
        (flags.has('overridden') && r.is_overridden) ||
        (flags.has('deprecated') && r.catalog_deprecation_date) ||
        (flags.has('price_unset') && isPriceUnset(r)) ||
        (flags.has('price_up') &&
          (Number(r.input_delta_pct) > 0 ||
            Number(r.output_delta_pct) > 0)) ||
        (flags.has('price_down') &&
          (Number(r.input_delta_pct) < 0 ||
            Number(r.output_delta_pct) < 0))
      if (!matchAny) return false
    }
    // 价格未设的不参与阈值过滤（它的 delta 计算后是 0，但应该出现在 price_unset 这条规则里）
    if (threshold > 0 && !isPriceUnset(r) && maxAbsDelta(r) < threshold) {
      return false
    }
    return true
  })
})

const sortedRows = computed<PriceDiffEntry[]>(() => {
  const arr = filteredRows.value.slice()
  switch (sortKey.value) {
    case 'delta_desc':
      arr.sort((a, b) => maxAbsDelta(b) - maxAbsDelta(a))
      break
    case 'delta_asc':
      arr.sort((a, b) => maxAbsDelta(a) - maxAbsDelta(b))
      break
    case 'name_asc':
      arr.sort((a, b) => (a.model_name || '').localeCompare(b.model_name || ''))
      break
    case 'catalog_asc':
      arr.sort((a, b) =>
        (a.catalog_source_model_id || '').localeCompare(
          b.catalog_source_model_id || ''
        )
      )
      break
    case 'price_desc':
      arr.sort(
        (a, b) =>
          Number(b.catalog_input_price || 0) -
          Number(a.catalog_input_price || 0)
      )
      break
  }
  return arr
})

const pagedRows = computed<PriceDiffEntry[]>(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return sortedRows.value.slice(start, start + pageSize.value)
})

// 改任何筛选条件都回到第 1 页
watch([searchKeyword, statusFilters, minAbsDelta, sortKey], () => {
  currentPage.value = 1
})

// ===================== 工具函数 =====================
function formatDate(iso: string): string {
  return iso.slice(0, 10)
}

function formatPrice(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(Number(v))) return '—'
  const n = Number(v)
  if (n === 0) return '$0'
  return n < 0.1 ? `$${n.toFixed(4)}` : `$${n.toFixed(2)}`
}

function formatPct(pct: number | null | undefined): string {
  if (pct === null || pct === undefined || Number.isNaN(Number(pct))) return '—'
  return `${Math.abs(Number(pct)).toFixed(1)}%`
}

function deltaArrow(pct: number): string {
  if (!Number.isFinite(pct) || pct === 0) return '='
  return pct > 0 ? '↑' : '↓'
}

function deltaClass(pct: number): string {
  if (!Number.isFinite(pct) || pct === 0) return 'delta-flat'
  return pct > 0 ? 'delta-up' : 'delta-down'
}

function resetFilters() {
  searchKeyword.value = ''
  statusFilters.value = []
  minAbsDelta.value = 0
  sortKey.value = 'delta_desc'
}

function switchTab(tab: 'sync' | 'bind' | 'diff') {
  currentTab.value = tab
}

function scrollToDiffSection() {
  currentTab.value = 'diff'
  const el = document.querySelector('[data-testid="diff-section"]')
  if (el && 'scrollIntoView' in el) {
    ;(el as HTMLElement).scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
}

// ===================== 数据加载 =====================
async function loadCatalogMeta() {
  const [statsRes, searchRes, statusRes] = await Promise.allSettled([
    getCatalogBindingStats(),
    searchCatalog({ limit: 1 }),
    getCatalogSyncStatus(),
  ])
  if (statsRes.status === 'fulfilled') {
    bindingStats.value = statsRes.value
    catalogTotal.value = statsRes.value.catalog_total
  } else {
    bindingStats.value = null
  }
  if (searchRes.status === 'fulfilled' && searchRes.value.data.length > 0) {
    lastSyncedAt.value = searchRes.value.data[0].synced_at
  }
  if (statusRes.status === 'fulfilled') {
    syncStatus.value = statusRes.value
  } else {
    syncStatus.value = null
  }

  currentTab.value = preferredTab.value
}

async function loadDiff() {
  loading.value = true
  try {
    const result = await getCatalogDiff()
    diffRows.value = result.data
    selectedIds.value = []
  } catch {
    ElMessage.error('加载 Catalog 差异失败')
  } finally {
    loading.value = false
  }
}

/**
 * 改了绑定关系（autoBind / ambiguousSave / applyDiff）之后调用，
 * 同时刷新 diff 表和 bindingStats，让空状态文案立即跟上真实情况。
 */
async function reloadDiffAndStats() {
  await Promise.all([loadDiff(), loadCatalogMeta()])
}

// ===================== 选择 / 应用 =====================
function handleSelectionChange(rows: PriceDiffEntry[]) {
  selectedIds.value = rows.map((r) => r.model_id)
}

function selectAllFiltered() {
  selectedIds.value = filteredRows.value.map((r) => r.model_id)
  ElMessage.info(`已选中筛选范围内全部 ${selectedIds.value.length} 条`)
}

function selectPriceUnsetRows() {
  selectedIds.value = priceUnsetRows.value.map((r) => r.model_id)
  ElMessage.info(`已选中待补价格分组 ${selectedIds.value.length} 条`)
}

function selectFollowupRows() {
  selectedIds.value = followupRows.value.map((r) => r.model_id)
  ElMessage.info(`已选中已脱离目录分组 ${selectedIds.value.length} 条`)
}

function clearSelection() {
  selectedIds.value = []
}

async function handleApply() {
  if (!selectedIds.value.length) {
    ElMessage.warning('请先勾选要应用的差异行')
    return
  }
  applying.value = true
  try {
    const result = await applyCatalogDiff(selectedIds.value)
    const msg =
      `已更新 ${result.updated} 项 · 跳过 ${result.skipped} 项` +
      (result.failed_ids.length ? ` · 失败 ${result.failed_ids.length} 项` : '')
    ElMessage.success(msg)
    selectedIds.value = []
    await reloadDiffAndStats()
  } catch {
    ElMessage.error('应用差异失败')
  } finally {
    applying.value = false
  }
}

// ===================== 导出 CSV =====================
function exportCSV() {
  const rows = sortedRows.value
  if (!rows.length) {
    ElMessage.warning('当前筛选下没有数据可导出')
    return
  }
  const header = [
    'model_id',
    'model_name',
    'catalog_source_model_id',
    'is_overridden',
    'local_input_price',
    'local_output_price',
    'catalog_input_price',
    'catalog_output_price',
    'input_delta_pct',
    'output_delta_pct',
    'catalog_deprecation_date'
  ]
  const escape = (v: unknown) => {
    if (v === null || v === undefined) return ''
    const s = String(v)
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const csv = [
    header.join(','),
    ...rows.map((r) =>
      [
        r.model_id,
        r.model_name,
        r.catalog_source_model_id,
        r.is_overridden,
        r.local_input_price,
        r.local_output_price,
        r.catalog_input_price,
        r.catalog_output_price,
        r.input_delta_pct,
        r.output_delta_pct,
        r.catalog_deprecation_date ?? ''
      ]
        .map(escape)
        .join(',')
    )
  ].join('\n')
  // BOM + UTF-8，避免 Excel 中文乱码
  const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const ts = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
  link.href = url
  link.download = `catalog-diff-${ts}.csv`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

// ===================== 智能绑定 / 手动刷新 =====================
async function handleAutoBind() {
  autoBinding.value = true
  try {
    const res = await autoBindCatalog()
    autoBindResult.value = res
    // 每次重新打开弹窗都清空人工选择，避免上一轮残留
    ambiguousSelections.value = {}
    ambiguousResetFields.value = false
    autoBindDialogVisible.value = true
    if (res.matched.length > 0) {
      ElMessage.success(`已自动绑定 ${res.matched.length} 条本地模型到目录`)
      await reloadDiffAndStats()
    } else if (res.ambiguous.length > 0) {
      ElMessage.warning(
        `${res.ambiguous.length} 条本地模型有多个目录候选，需要人工选择`
      )
    } else {
      ElMessage.info('当前没有可自动绑定的本地模型（或全部已绑定）')
    }
  } catch {
    ElMessage.error('智能绑定失败，请重试或检查后端日志')
  } finally {
    autoBinding.value = false
  }
}

/**
 * CAT-2：把 ambiguous 段中 admin 已选的条目批量绑定。
 * 单条失败不阻塞，最终把成功项从 ambiguous 列表里抹去；剩下的让 admin 继续处理。
 */
async function handleAmbiguousSave() {
  if (!autoBindResult.value) return
  const bindings = Object.entries(ambiguousSelections.value)
    .filter(([, cid]) => !!cid)
    .map(([modelId, catalogId]) => ({
      model_id: modelId,
      catalog_id: catalogId,
      reset_fields: ambiguousResetFields.value
    }))
  if (!bindings.length) {
    ElMessage.info('请先为至少一行选择目录条目')
    return
  }
  batchBinding.value = true
  try {
    const res = await batchBindModelCatalog(bindings)
    if (res.applied > 0) {
      ElMessage.success(`已成功绑定 ${res.applied} 条本地模型到目录`)
    }
    const failedCount = Object.keys(res.failed_ids).length
    if (failedCount > 0) {
      ElMessage.warning(`${failedCount} 条绑定失败，请查看后端日志`)
    }
    // 把已成功的从 ambiguous 列表抹去 + 把对应的 model_id 移入 matched
    const succeededIds = new Set(
      bindings
        .map((b) => b.model_id)
        .filter((id) => !res.failed_ids[id])
    )
    if (autoBindResult.value && succeededIds.size > 0) {
      const remainingAmbiguous = autoBindResult.value.ambiguous.filter(
        (a) => !succeededIds.has(a.model_id)
      )
      const newlyMatched = autoBindResult.value.ambiguous
        .filter((a) => succeededIds.has(a.model_id))
        .map((a) => {
          const cid = ambiguousSelections.value[a.model_id]
          const cand = a.candidates.find((c) => c.catalog_id === cid)
          return {
            model_id: a.model_id,
            model_name: a.model_name,
            catalog_id: cid,
            catalog_source_model_id: cand?.catalog_source_model_id || ''
          }
        })
      autoBindResult.value = {
        ...autoBindResult.value,
        matched: [...autoBindResult.value.matched, ...newlyMatched],
        ambiguous: remainingAmbiguous
      }
      // 清掉已成功项的选择
      for (const id of succeededIds) {
        delete ambiguousSelections.value[id]
      }
    }
    await reloadDiffAndStats()
  } catch {
    ElMessage.error('批量绑定失败，请重试或检查后端日志')
  } finally {
    batchBinding.value = false
  }
}

/**
 * 修复 B + C：轮询 sync-status 等同步真正结束（替换原来的 setTimeout(4000)）。
 *
 * 旧实现的问题：
 *   - 同步可能 < 1s 也可能 > 10s，固定 4s 既等不及也浪费等
 *   - 同步失败了用户看不到任何反馈
 *
 * 新实现：触发 sync 后立刻轮询 sync-status，running 翻 false 时
 *   - 有 last_error → toast 错误 + 横幅红色显示原因
 *   - 成功         → toast "同步了 N 条"，再 reload diff
 *
 * 轮询参数：每 1.5s 一次，最长 60s（remote 路径慢起来 ~ 15s）。
 */
const SYNC_POLL_INTERVAL_MS = 1500
const SYNC_POLL_TIMEOUT_MS = 60_000

async function pollSyncStatusUntilDone(): Promise<CatalogSyncStatus | null> {
  const deadline = Date.now() + SYNC_POLL_TIMEOUT_MS
  while (Date.now() < deadline) {
    let st: CatalogSyncStatus | null = null
    try {
      st = await getCatalogSyncStatus()
      syncStatus.value = st
    } catch {
      // 单次失败不致命，继续轮询
    }
    if (st && !st.running) return st
    await new Promise((resolve) => setTimeout(resolve, SYNC_POLL_INTERVAL_MS))
  }
  return syncStatus.value
}

async function handleManualRefresh() {
  refreshing.value = true
  try {
    await triggerCatalogSync()
    ElMessage.info('已触发同步任务，正在等待结果…')

    const finalStatus = await pollSyncStatusUntilDone()

    if (!finalStatus) {
      ElMessage.warning('同步状态轮询超时，请稍后手动刷新页面查看结果')
    } else if (finalStatus.last_error) {
      ElMessage.error(`同步失败：${finalStatus.last_error}`)
    } else {
      const sourceLabel = finalStatus.last_source === 'bundled' ? '内置兜底' : '远程'
      ElMessage.success(
        `同步完成：${sourceLabel}写入 ${finalStatus.last_count} 条 · 耗时 ${(finalStatus.last_duration_ms / 1000).toFixed(1)}s`
      )
    }
    // 不管成功失败，都把元信息 / diff 拉一次：成功要让 catalog_total 上来，
    // 失败要让横幅展示最新错误。
    await reloadDiffAndStats()
  } catch {
    ElMessage.error('触发同步任务失败')
  } finally {
    refreshing.value = false
  }
}

onMounted(() => {
  loadCatalogMeta()
  loadDiff()
})

defineExpose({ selectedIds, loadDiff, filteredRows, sortedRows, pagedRows })
</script>

<style scoped>
.catalog-sync-page {
  padding: 24px;
}

.stage-panel {
  display: grid;
  grid-template-columns: minmax(320px, 1.2fr) minmax(420px, 1fr);
  gap: 16px;
  padding: 16px 18px;
  margin-bottom: 16px;
  background: linear-gradient(135deg, rgba(64, 158, 255, 0.08), rgba(103, 194, 58, 0.06));
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.action-board {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.action-card {
  padding: 16px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.action-card-primary {
  background: linear-gradient(180deg, rgba(64, 158, 255, 0.08), rgba(64, 158, 255, 0.02));
}

.action-kicker {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.action-title {
  margin: 6px 0 8px;
  color: var(--el-text-color-primary);
  font-size: 18px;
  font-weight: 600;
}

.action-description {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
}

.action-buttons {
  margin-top: 14px;
}

.action-list {
  margin: 10px 0 0;
  padding-left: 18px;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.8;
}

.stage-kicker,
.step-hint,
.section-description {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.stage-title,
.section-title {
  margin: 4px 0 8px;
  color: var(--el-text-color-primary);
  font-size: 18px;
  font-weight: 600;
}

.stage-description {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
}

.stage-steps {
  display: grid;
  gap: 10px;
}

.stage-step {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 10px 12px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}

.stage-step.active {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px rgba(64, 158, 255, 0.15) inset;
}

.step-index {
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  font-size: 12px;
  font-weight: 700;
  flex-shrink: 0;
}

.stage-step.active .step-index {
  background: var(--el-color-primary);
  color: #fff;
}

.step-title {
  margin: 0 0 4px;
  color: var(--el-text-color-primary);
  font-size: 14px;
  font-weight: 600;
}

.section-intro {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.workspace-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.workspace-card {
  padding: 16px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.03);
}

.workspace-card.panel-done {
  border-color: rgba(103, 194, 58, 0.35);
  background: linear-gradient(180deg, rgba(103, 194, 58, 0.07), rgba(103, 194, 58, 0.02));
}

.workspace-card.panel-pending {
  border-color: rgba(230, 162, 60, 0.35);
  background: linear-gradient(180deg, rgba(230, 162, 60, 0.08), rgba(230, 162, 60, 0.02));
}

.workspace-card.panel-blocked {
  border-color: rgba(245, 108, 108, 0.35);
  background: linear-gradient(180deg, rgba(245, 108, 108, 0.08), rgba(245, 108, 108, 0.02));
}

.workspace-head h4 {
  margin: 4px 0 10px;
  color: var(--el-text-color-primary);
  font-size: 16px;
  font-weight: 600;
}

.workspace-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.workspace-status {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
}

.workspace-status.status-done {
  background: rgba(103, 194, 58, 0.12);
  color: #3f8f22;
}

.workspace-status.status-pending {
  background: rgba(230, 162, 60, 0.12);
  color: #b67518;
}

.workspace-status.status-blocked {
  background: rgba(245, 108, 108, 0.12);
  color: #c45656;
}

.workspace-kicker {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.workspace-text,
.workspace-why {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
}

.workspace-why {
  margin-top: 8px;
  padding-left: 10px;
  border-left: 3px solid var(--el-color-primary-light-5);
}

.workspace-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 14px;
}

.workspace-subtypes {
  display: grid;
  gap: 10px;
  margin-top: 14px;
}

.workspace-subtype {
  padding: 10px 12px;
  background: var(--el-fill-color-extra-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}

.workspace-subtype strong {
  display: inline-block;
  margin-bottom: 6px;
  color: var(--el-text-color-primary);
  font-size: 13px;
}

.workspace-subtype p {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 1.7;
}

.section-card {
  padding: 14px 16px;
  background: var(--el-fill-color-extra-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}

.section-card h4 {
  margin: 0 0 8px;
  color: var(--el-text-color-primary);
  font-size: 14px;
  font-weight: 600;
}

.section-card p {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 1.7;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  gap: 12px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size);
  font-weight: var(--font-title-weight);
  margin: 0 0 6px;
}

.page-subtitle {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.6;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
  flex-wrap: wrap;
}

.sync-meta {
  color: var(--el-text-color-secondary);
}

.sync-time {
  font-family: monospace;
}

.tab-nav {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.tab-nav-item {
  border: 1px solid var(--el-border-color-light);
  background: var(--el-fill-color-blank);
  color: var(--el-text-color-regular);
  border-radius: 999px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.tab-nav-item.active {
  background: var(--el-color-primary);
  border-color: var(--el-color-primary);
  color: #fff;
}

.tab-intro {
  padding: 12px 14px;
  margin-bottom: 14px;
  background: var(--el-fill-color-extra-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 14px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  margin-bottom: 12px;
}

.filter-header,
.footer-intro {
  width: 100%;
  margin-bottom: 4px;
}

.diff-explainer {
  display: grid;
  gap: 12px;
  padding: 14px 16px;
  margin-bottom: 12px;
  background: var(--el-fill-color-extra-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
}

.diff-summary-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.diff-summary-card {
  padding: 12px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}

.diff-summary-label {
  margin: 0 0 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.diff-summary-value {
  display: inline-block;
  margin-bottom: 6px;
  color: var(--el-text-color-primary);
  font-size: 24px;
  line-height: 1;
}

.diff-summary-hint {
  margin: 0;
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 1.6;
}

.deprecation-banner {
  padding: 10px 12px;
  background: rgba(230, 162, 60, 0.12);
  border: 1px solid rgba(230, 162, 60, 0.24);
  border-radius: 8px;
  color: var(--el-color-warning-dark-2);
  font-size: 13px;
  line-height: 1.7;
}

.diff-groups {
  display: grid;
  gap: 14px;
}

.diff-group {
  display: grid;
  gap: 8px;
}

.diff-group-header {
  padding: 10px 12px 0;
}

.diff-group-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.search-input {
  width: 280px;
}

.status-select {
  width: 240px;
}

.threshold-control {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.threshold-label {
  flex-shrink: 0;
}

.sort-select {
  width: 240px;
}

.mono {
  font-family: monospace;
  margin-right: 6px;
}

.dim {
  color: var(--el-text-color-secondary);
}

.badge-override {
  margin-left: 6px;
}

.badge-deprecation {
  margin-left: 6px;
}

.delta-up {
  color: #e74c3c;
  font-weight: 600;
}

.delta-down {
  color: #27ae60;
  font-weight: 600;
}

.delta-flat {
  color: #98989d;
}

.footer-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
  gap: 12px;
  flex-wrap: wrap;
}

.footer-actions {
  display: flex;
  gap: 10px;
  margin-left: auto;
}

.footer-risk-note {
  margin: 8px 0 0;
  color: var(--el-color-warning-dark-2);
  font-size: 12px;
  line-height: 1.6;
}

.empty-state {
  margin: 40px auto;
}

.empty-desc {
  max-width: 560px;
  text-align: left;
  margin: 0 auto;
}

.empty-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 12px;
  text-align: center;
}

.empty-hint {
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.7;
  margin: 6px 0;
}

.empty-next-step {
  padding-left: 10px;
  border-left: 3px solid var(--el-color-primary-light-5);
}

.empty-actions {
  display: flex;
  justify-content: center;
  gap: 10px;
  margin-top: 18px;
}

@media (max-width: 900px) {
  .page-header {
    flex-direction: column;
  }

  .header-actions {
    width: 100%;
  }
}

.sync-status-banner {
  margin: 0 0 16px 0;
}

.empty-hint code {
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 12px;
}

.empty-hint a {
  color: var(--el-color-primary);
}

.auto-bind-summary h4 {
  margin: 16px 0 8px;
  font-size: 13px;
  color: var(--el-text-color-primary);
}

.bind-list {
  margin: 0;
  padding-left: 0;
  list-style: none;
  max-height: 240px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
}

.bind-list li {
  padding: 6px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.bind-list li:last-child {
  border-bottom: none;
}

.bind-list .arrow {
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

.ellipsis {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-align: right;
}

.ml-8 {
  margin-left: 8px;
}

.ambiguous-table {
  width: 100%;
  margin-top: 8px;
  border-collapse: collapse;
  font-size: 12px;
  table-layout: fixed;
}

.ambiguous-table thead th {
  text-align: left;
  padding: 6px 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  font-weight: normal;
  font-size: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.ambiguous-table thead th:first-child {
  width: 38%;
}

.ambiguous-table tbody td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  vertical-align: middle;
  word-break: break-all;
}

.ambiguous-footer {
  margin-top: 12px;
  padding-top: 8px;
  border-top: 1px dashed var(--el-border-color-lighter);
}

@media (max-width: 960px) {
  .stage-panel,
  .section-intro,
  .action-board,
  .workspace-grid {
    grid-template-columns: 1fr;
  }

  .diff-summary-grid {
    grid-template-columns: 1fr;
  }

  .page-header,
  .footer-bar {
    align-items: stretch;
  }

  .header-actions,
  .workspace-actions,
  .diff-group-actions,
  .footer-actions,
  .empty-actions {
    width: 100%;
    flex-direction: column;
  }

  .search-input,
  .status-select,
  .sort-select {
    width: 100%;
  }

  .stage-panel,
  .action-card,
  .workspace-card,
  .diff-explainer,
  .filter-bar {
    padding-left: 14px;
    padding-right: 14px;
  }
}
</style>
