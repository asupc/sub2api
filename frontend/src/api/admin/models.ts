/**
 * Admin Model Manifest API
 * 模型清单与账号供给透视（模型 ⟷ 账号倒排查询）
 */

import { apiClient } from '../client'

/** 全盘供给核心指标（全局口径，不受查询过滤影响） */
export interface ModelManifestSummary {
  total_models: number
  healthy_models: number
  danger_models: number
  total_accounts: number
}

/** 模型下挂载账号的明细 */
export interface ModelManifestAccountItem {
  id: number
  name: string
  platform: string
  type: string
  status: string
  schedulable: boolean
  rate_limited: boolean
  concurrency: number
  priority: number
  groups: string[]
  /** 映射目标；空字符串表示原生直通 */
  target: string
}

/** 单个模型的供给画像 */
export interface ModelManifestItem {
  id: string
  display_name: string
  platforms: string[]
  total_accounts: number
  active_accounts: number
  total_concurrency: number
  /** true = 0 可调度账号（断供告警） */
  has_warning: boolean
  accounts: ModelManifestAccountItem[]
  /** 最近一次模型能力测评的能力分；null 表示尚未测评 */
  benchmark_score?: number | null
  /** 该分数所属的套件版本（跨版本分数不完全可比） */
  benchmark_suite_version?: string
  benchmark_scored_at?: string | null
}

export interface ModelManifestResponse {
  summary: ModelManifestSummary
  models: ModelManifestItem[]
}

export interface ModelManifestParams {
  platform?: string
  status?: 'all' | 'healthy' | 'danger'
  search?: string
}

/**
 * 获取模型清单与账号供给透视
 * @param params - 过滤参数（platform / status / search）
 */
export async function getModelManifest(params?: ModelManifestParams): Promise<ModelManifestResponse> {
  const { data } = await apiClient.get<ModelManifestResponse>('/admin/models/manifest', { params })
  return data
}

export const modelsAPI = {
  getModelManifest
}

export default modelsAPI
