<template>
  <div class="urp">
    <!-- 页头：层级 + 周期 + 刷新 -->
    <div class="urp__bar">
      <el-radio-group v-model="level" @change="load">
        <el-radio-button value="personal">个人</el-radio-button>
        <el-radio-button value="department">部门</el-radio-button>
        <el-radio-button value="studio">工作室</el-radio-button>
      </el-radio-group>

      <el-select v-model="periodType" style="width: 90px" @change="load">
        <el-option label="周" value="week" />
        <el-option label="月" value="month" />
        <el-option label="日" value="day" />
      </el-select>

      <el-date-picker
        v-model="periodStart"
        type="date"
        value-format="YYYY-MM-DD"
        placeholder="周期起始"
        style="width: 160px"
        :disabled="loading"
        @change="load"
      />

      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>

    <!-- 三态强制：加载骨架 -->
    <div v-if="loading" class="urp__skeleton">
      <el-skeleton :rows="6" animated />
    </div>

    <!-- 三态强制：错误重试 -->
    <el-empty v-else-if="error" description="报告加载失败">
      <template #description>
        <span class="urp__err">{{ error }}</span>
      </template>
      <el-button type="primary" @click="load">重试</el-button>
    </el-empty>

    <!-- 三态强制：空态 -->
    <el-empty v-else-if="!report" description="暂无报告数据" />

    <!-- 报告内容 -->
    <template v-else>
      <ReportShell :title="reportTitle">
        <template #actions>
          <span class="urp__meta">
            {{ report.meta.scope.name }} · {{ report.meta.period.from }} ~ {{ report.meta.period.to }}
            <el-tag size="small" effect="plain">规则 {{ report.meta.rule_version }}</el-tag>
          </span>
        </template>

        <ReportDataQuality :data-quality="report.data_quality" />
      </ReportShell>

      <!-- ── 个人报告 ── -->
      <template v-if="level === 'personal'">
        <ReportShell v-if="report.safety_anomaly.hit" title="严重异常">
          <SafetyAnomalyPanel
            :anomaly="report.safety_anomaly"
            :currency="reportCurrency"
            @feedback="onPersonalFeedback"
          />
        </ReportShell>

        <ReportShell title="成本构成">
          <CostBucketBar :cost-breakdown="report.cost_breakdown" :currency="reportCurrency" />
        </ReportShell>

        <ReportShell title="效率与情况">
          <div v-if="report.usage_issues?.length" class="urp__cards">
            <IssueDiagnosisCard
              v-for="issue in report.usage_issues"
              :key="issue.type"
              :issue="issue"
              :currency="reportCurrency"
            />
          </div>
          <el-empty v-else description="本期未命中任何异常情况" :image-size="72" />
        </ReportShell>

        <ReportShell title="本周结论">
          <div class="urp-concl">
            <p class="urp-concl__note">{{ report.conclusion.note }}</p>
            <div class="urp-concl__chips">
              <span class="urp-concl__chip">成本主桶：{{ costBucketLabel(report.conclusion.cost_main_bucket) }}（{{ (costMainBucketShare * 100).toFixed(1) }}%）</span>
              <span class="urp-concl__chip">问题占比：{{ pct(report.conclusion.issue_cost_ratio) }}</span>
              <span class="urp-concl__chip">主导问题：{{ dominantLabel(report.conclusion.dominant_issue) }}</span>
            </div>
          </div>
        </ReportShell>
      </template>

      <!-- ── 部门 / 工作室报告 ── -->
      <template v-else>
        <ReportShell v-if="report.safety_anomaly.hit" title="严重异常">
          <SafetyAnomalyPanel
            :anomaly="report.safety_anomaly"
            :currency="reportCurrency"
            @feedback="onGroupFeedback"
          />
        </ReportShell>

        <template v-if="memberEvaluation">
          <ReportShell title="成员整合评估">
            <MemberQuadrantChart
              :members="memberEvaluation.members"
              :cost-median="memberEvaluation.cost_median_usd"
              :issue-ratio-median="memberEvaluation.issue_ratio_median"
              :loading="loading"
              @select="onMemberSelect"
            />
          </ReportShell>

          <ReportShell title="分群">
            <MemberClusterChart
              :members="memberEvaluation.members"
              :clusters="memberEvaluation.clusters"
              :loading="loading"
            />
          </ReportShell>

          <ReportShell title="存在问题的人">
            <ProblemMemberList :members="memberEvaluation.members" :currency="reportCurrency" />
          </ReportShell>

          <ReportShell title="正常使用的人">
            <NormalMemberList :members="memberEvaluation.members" :currency="reportCurrency" />
          </ReportShell>

          <div v-if="selectedMember" class="urp__member-card">
            <ReportShell :title="`成员详情：${selectedMember.name}`">
              <p class="urp__member-line">
                象限：{{ quadrantLabel(selectedMember.quadrant) }} · 成本 {{ fmtCost(selectedMember.cost_usd) }}
                · 问题占比 {{ pct(selectedMember.issue_cost_ratio) }}
              </p>
              <div v-if="selectedMember.usage_issues?.length" class="urp__member-issues">
                <IssueDiagnosisCard
                  v-for="issue in selectedMember.usage_issues"
                  :key="issue.type"
                  :issue="issue"
                  :currency="reportCurrency"
                />
              </div>
            </ReportShell>
          </div>
        </template>
        <el-empty v-else description="该范围暂无成员评估数据" :image-size="72" />

        <ReportShell title="本周结论">
          <div class="urp-concl">
            <p class="urp-concl__note">{{ report.conclusion.note }}</p>
            <div class="urp-concl__chips">
              <span class="urp-concl__chip">成本主桶：{{ costBucketLabel(report.conclusion.cost_main_bucket) }}（{{ (costMainBucketShare * 100).toFixed(1) }}%）</span>
              <span class="urp-concl__chip">问题占比：{{ pct(report.conclusion.issue_cost_ratio) }}</span>
              <span class="urp-concl__chip">主导问题：{{ dominantLabel(report.conclusion.dominant_issue) }}</span>
            </div>
          </div>
        </ReportShell>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'
import ReportShell from '@/components/usage-report/ReportShell.vue'
import ReportDataQuality from '@/components/usage-report/ReportDataQuality.vue'
import CostBucketBar from '@/components/usage-report/CostBucketBar.vue'
import IssueDiagnosisCard from '@/components/usage-report/IssueDiagnosisCard.vue'
import SafetyAnomalyPanel from '@/components/usage-report/SafetyAnomalyPanel.vue'
import MemberQuadrantChart from '@/components/usage-report/MemberQuadrantChart.vue'
import MemberClusterChart from '@/components/usage-report/MemberClusterChart.vue'
import ProblemMemberList from './components/ProblemMemberList.vue'
import NormalMemberList from './components/NormalMemberList.vue'
import { reportApi } from '@/api/usage-report'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import type {
  Report,
  MemberPoint,
  DominantIssue,
  CostBucket,
  Quadrant,
  ReportLevel,
} from '@/types/usage-report'

const route = useRoute()
const settingsStore = useSettingsStore()

const cc = computed<CurrencyConfig>(() => ({
  currency: reportCurrency.value,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const level = ref<ReportLevel>('department')
const periodType = ref<'week' | 'month' | 'day'>('week')
const periodStart = ref<string>(mondayOfNow())

const loading = ref(false)
const error = ref('')
const report = ref<Report | null>(null)

const LEVEL_LABEL: Record<ReportLevel, string> = {
  personal: '个人',
  department: '部门',
  studio: '工作室',
}
const DOMINANT_LABEL: Record<DominantIssue, string> = {
  normal: '正常',
  prefix_pollution: '前缀污染',
  toothpaste: '挤牙膏',
  multi_task_session: '多任务',
  output_runaway: '输出失控',
  over_model: '杀鸡用牛刀',
}
const COST_BUCKET_LABEL: Record<CostBucket, string> = {
  uncached_input: '非缓存输入',
  cache: '缓存',
  output: '输出',
}
const QUADRANT_LABEL: Record<Quadrant, string> = {
  priority_improve: '右上·优先改善',
  coach: '左上·习惯辅导',
  high_cost_normal: '右下·高成本正常',
  routine: '左下·常规',
}

const reportTitle = computed(() => `使用分析报告 · ${LEVEL_LABEL[level.value]}`)
const reportCurrency = computed<Currency>(() =>
  ((report.value?.meta.currency?.report as Currency) || 'CNY'),
)
const memberEvaluation = computed(() => report.value?.member_evaluation)
const selectedMember = ref<MemberPoint | null>(null)

function fmtCost(v: number): string {
  return formatCost(Number(v || 0), cc.value)
}
function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}
function quadrantLabel(q: Quadrant): string {
  return QUADRANT_LABEL[q as Quadrant] ?? q
}

// ── 结论计算 ──
const costTotal = computed(() => {
  const cb = report.value?.cost_breakdown
  if (!cb) return 0
  return (cb.uncached_input_cost_usd || 0) + (cb.cache_cost_usd || 0) + (cb.output_cost_usd || 0)
})
const costMainBucketShare = computed(() => {
  const c = report.value?.conclusion
  const cb = report.value?.cost_breakdown
  if (!c || !cb || costTotal.value <= 0) return 0
  const b = c.cost_main_bucket as CostBucket
  const v = {
    uncached_input: cb.uncached_input_cost_usd || 0,
    cache: cb.cache_cost_usd || 0,
    output: cb.output_cost_usd || 0,
  }[b] ?? 0
  return v / costTotal.value
})

async function load() {
  loading.value = true
  error.value = ''
  selectedMember.value = null
  try {
    const res = await reportApi.getReport(level.value, undefined, periodStart.value, periodType.value)
    report.value = res.data
  } catch (e: any) {
    report.value = null
    error.value = e?.response?.data?.error?.details || e?.response?.data?.error?.message || e?.message || '报告加载失败'
  } finally {
    loading.value = false
  }
}

function onMemberSelect(payload: { user_id: string; name: string; quadrant: string }) {
  const m = memberEvaluation.value?.members?.find(x => x.user_id === payload.user_id)
  selectedMember.value = m ?? null
}

async function onPersonalFeedback(status: 'confirmed_ok' | 'need_check' | 'false_positive') {
  const userID = report.value?.meta.scope.id
  if (!userID) return
  try {
    await reportApi.postAnomalyFeedback(userID, status, periodStart.value)
    ElMessage.success('严重异常反馈已提交')
    await load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error?.message || '反馈提交失败')
  }
}

function onGroupFeedback() {
  ElMessage.info('部门/工作室报告请到成员视角反馈')
}

function mondayOfNow(): string {
  const d = new Date()
  const day = d.getDay() // 0=Sun..6=Sat
  const diff = day === 0 ? 6 : day - 1
  d.setDate(d.getDate() - diff)
  return d.toISOString().slice(0, 10)
}

onMounted(() => {
  const { level: lv, period_start, period_type } = route.query
  if (lv === 'personal' || lv === 'department' || lv === 'studio') level.value = lv
  if (period_type === 'week' || period_type === 'month' || period_type === 'day') periodType.value = period_type
  if (period_start) periodStart.value = String(period_start)
  load()
})

function costBucketLabel(b: CostBucket): string {
  return COST_BUCKET_LABEL[b as CostBucket] ?? b
}
function dominantLabel(d: DominantIssue): string {
  return DOMINANT_LABEL[d as DominantIssue] ?? d
}
</script>

<style scoped>
.urp {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.urp__bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.urp__skeleton {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  padding: 16px;
}
.urp__err {
  font-size: 13px;
  color: var(--el-color-danger);
}
.urp__meta {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.urp__cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 10px;
}
.urp__member-card { }
.urp__member-line {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.urp__member-issues {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 10px;
}
</style>

<!-- 结论块样式（非 scoped，供内联 template 组件使用） -->
<style>
.urp-concl {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.urp-concl__note {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
}
.urp-concl__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.urp-concl__chip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  border-radius: 6px;
  padding: 4px 8px;
}
</style>
