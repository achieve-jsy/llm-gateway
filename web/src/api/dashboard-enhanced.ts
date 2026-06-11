/**
 * dashboard-enhanced.ts — Phase B 增强 API 服务层
 *
 * 封装 4 个增强/新增端点，返回强类型响应。
 * 所有日期参数格式: YYYY-MM-DD
 */
import http from '@/services/http'
import type {
  UsersApiResponse,
  ModelsApiResponse,
  UserDetailResponse,
  ModelDetailResponse,
} from '@/types/dashboard-enhanced'

/** 增强版用户排行 GET /api/v1/usage/dashboard/users */
export async function fetchUsersRanking(
  params: {
    from: string
    to: string
    studio_id?: string
    dept_id?: string
    model_name?: string
    page?: number
    page_size?: number
  },
  opts?: { timeout?: number; signal?: AbortSignal },
): Promise<UsersApiResponse> {
  const res = await http.get('/api/v1/usage/dashboard/users', { params, ...opts })
  return res.data as UsersApiResponse
}

/** 增强版模型排行 GET /api/v1/usage/dashboard/models */
export async function fetchModelsRanking(params: {
  start: string
  end: string
  limit?: number
  user_id?: string
  status?: string
}): Promise<ModelsApiResponse> {
  const res = await http.get('/api/v1/usage/dashboard/models', { params })
  return res.data as ModelsApiResponse
}

/** 用户详情 GET /api/v1/usage/dashboard/user-detail */
export async function fetchUserDetail(params: {
  user_id: string
  from?: string
  to?: string
}): Promise<UserDetailResponse> {
  const res = await http.get('/api/v1/usage/dashboard/user-detail', { params })
  return res.data as UserDetailResponse
}

/** 模型详情 GET /api/v1/usage/dashboard/model-detail */
export async function fetchModelDetail(params: {
  model: string
  from?: string
  to?: string
}): Promise<ModelDetailResponse> {
  const res = await http.get('/api/v1/usage/dashboard/model-detail', { params })
  return res.data as ModelDetailResponse
}
