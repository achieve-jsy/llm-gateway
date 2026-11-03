import http from '@/services/http'
import type {
  Report,
  MemberEvaluation,
  ReportLevel,
  PeriodType,
  SafetyAnomalyStatus,
} from '@/types/usage-report'

// ⚠️ 响应形状说明：这三个报告端点与后台常见 `{code, data}` 包裹不同，
// 后端直接返回裸对象（Report / { member_evaluation } / { user_id, status }）。
// 因此调用方通过 res.data 取到直接产物，不要用 res.data.data。

/** GET /api/v1/usage/report/members 层级（仅 department|studio）。 */
export type ReportMembersLevel = 'department' | 'studio'

/** GET /api/v1/usage/report/members 响应：{ member_evaluation: MemberEvaluation }。 */
export interface ReportMembersResponse {
  member_evaluation: MemberEvaluation
}

/** 负责人可回写的严重异常待核查状态（不含 pending_review，初始值只能是 pending_review）。 */
export type AnomalyFeedbackStatus = Exclude<SafetyAnomalyStatus, 'pending_review'>

/** 严重异常待核查反馈响应。 */
export interface AnomalyFeedbackResponse {
  user_id: string
  status: string
}

export const reportApi = {
  /**
   * GET /api/v1/usage/report — 返回 Report JSON（个人/部门/工作室，report_snapshot 缓存命中直返）。
   * 成功后 res.data 即 Report。
   * @param id 个人=user_id / 部门=dept_id / 工作室=studio_id（可空，交由后端 EffectiveScope 收敛）
   * @param periodStart 周期起始日 YYYY-MM-DD（必填）
   * @param periodType 周期类型 week|month|day，默认 week
   */
  getReport(level: ReportLevel, id: string | undefined, periodStart: string, periodType?: PeriodType) {
    return http.get<Report>('/api/v1/usage/report', {
      params: { level, id, period_start: periodStart, period_type: periodType },
    })
  },

  /**
   * GET /api/v1/usage/report/members — 返回 { member_evaluation: MemberEvaluation }（仅 department|studio）。
   * 成功后 res.data.member_evaluation 即 MemberEvaluation。
   */
  getReportMembers(level: ReportMembersLevel, id: string | undefined, periodStart: string, periodType?: PeriodType) {
    return http.get<ReportMembersResponse>('/api/v1/usage/report/members', {
      params: { level, id, period_start: periodStart, period_type: periodType },
    })
  },

  /**
   * POST /api/v1/usage/report/anomaly/:user_id/feedback — 负责人回写 3.6 待核查状态。
   * @param status confirmed_ok | need_check | false_positive（非 pending_review，见 AnomalyFeedbackStatus）
   * @param periodStart 周期起始日 YYYY-MM-DD（与 anomaly_review 记录匹配）
   */
  postAnomalyFeedback(userId: string, status: AnomalyFeedbackStatus, periodStart: string) {
    return http.post<AnomalyFeedbackResponse>(`/api/v1/usage/report/anomaly/${userId}/feedback`, {
      status,
      period_start: periodStart,
    })
  },
}
