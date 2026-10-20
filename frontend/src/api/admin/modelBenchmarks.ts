/**
 * Admin Model Capability Benchmark API
 * 高难度模型测评、资源效率指标与历史曲线
 */

import { apiClient } from '../client'
import type { ModelManifestItem } from './models'

export interface ModelBenchmarkSuite {
  id: string
  version: string
  scoring_version: string
  item_count: number
  family_count: number
  min_difficulty: number
  max_difficulty: number
  category_weights: Record<string, number>
}

export interface ModelBenchmarkPlan {
  comparison_key: string
  id: number
  name: string
  suite_id: string
  suite_version: string
  scoring_version: string
  model_id: string
  cron_expression: string
  enabled: boolean
  sample_count: number
  max_output_tokens: number
  timeout_seconds: number
  reasoning_effort?: string
  reasoning_level?: string
  max_results: number
  running_status: 'idle' | 'running'
  claim_expires_at?: string | null
  /** 非空表示存在一次性手动排队请求（停用计划亦可排队） */
  manual_trigger_at?: string | null
  last_run_at?: string | null
  next_run_at?: string | null
  created_at: string
  updated_at: string
}

export interface ModelBenchmarkPlanRequest {
  comparison_key?: string
  name: string
  suite_id?: string
  model_id: string
  cron_expression: string
  enabled?: boolean
  sample_count?: number
  max_output_tokens?: number
  timeout_seconds?: number
  reasoning_effort?: string
  reasoning_level?: string
  max_results?: number
}

export interface ModelBenchmarkQuality {
  completion_rate: number
  format_compliance_rate: number
  format_error_count: number
  budget_exhausted_count: number
  ranking_eligible: boolean
  ranking_reason: string
  category_coverage: Record<string, number>
}

export interface ModelBenchmarkRun {
  comparison_key: string
  max_output_tokens: number
  timeout_seconds: number
  reasoning_effort?: string
  reasoning_level?: string
  sample_count: number
  quality?: ModelBenchmarkQuality
  id: number
  plan_id: number
  suite_id: string
  suite_version: string
  scoring_version: string
  model_id: string
  upstream_model_id: string
  status: 'running' | 'success' | 'partial' | 'failed' | 'canceled'
  intelligence_score?: number | null
  efficiency_score?: number | null
  composite_score?: number | null
  category_scores: Record<string, number>
  item_count: number
  answered_count: number
  correct_count: number
  input_tokens?: number | null
  output_tokens?: number | null
  total_tokens?: number | null
  estimated_cost?: number | null
  avg_latency_ms?: number | null
  p95_latency_ms?: number | null
  usage_source: 'upstream' | 'missing'
  usage_field_coverage: Record<string, boolean>
  error_message: string
  started_at: string
  finished_at?: string | null
  created_at: string
}

export interface ModelBenchmarkRunItem {
  id: number
  run_id: number
  item_id: string
  family_id: string
  category: string
  difficulty: number
  weight: number
  prompt_hash: string
  prompt_snapshot: string
  response_text: string
  status: 'correct' | 'partial' | 'incorrect' | 'format_error' | 'execution_failed'
  score: number
  earned_points: number
  possible_points: number
  checker_type: string
  failure_reason: string
  input_tokens?: number | null
  output_tokens?: number | null
  reasoning_tokens?: number | null
  cache_read_tokens?: number | null
  cache_write_tokens?: number | null
  total_tokens?: number | null
  estimated_cost?: number | null
  pricing_source: 'pricing' | 'missing'
  latency_ms?: number | null
  first_token_ms?: number | null
  error_message: string
  created_at: string
}

export interface ModelBenchmarkHistoryPoint {
  comparison_key: string
  max_output_tokens: number
  timeout_seconds: number
  sample_count: number
  run_id: number
  model_id: string
  created_at: string
  status: string
  intelligence_score?: number | null
  efficiency_score?: number | null
  composite_score?: number | null
  output_tokens?: number | null
  total_tokens?: number | null
  avg_latency_ms?: number | null
  p95_latency_ms?: number | null
  estimated_cost?: number | null
}

export interface ModelBenchmarkRanking {
  comparison_key: string
  max_output_tokens: number
  timeout_seconds: number
  reasoning_effort?: string
  reasoning_level?: string
  sample_count: number
  model_id: string
  suite_id: string
  suite_version: string
  scoring_version: string
  run_count: number
  intelligence_score?: number | null
  efficiency_score?: number | null
  composite_score?: number | null
  category_scores?: Record<string, number>
  output_tokens?: number | null
  avg_latency_ms?: number | null
  estimated_cost?: number | null
  last_run_at: string
}

export interface ModelBenchmarkModelOverview {
  model_id: string
  ranking?: ModelBenchmarkRanking
  latest_run?: ModelBenchmarkRun
  plans: ModelBenchmarkPlan[]
  recent_runs: ModelBenchmarkRun[]
  history: ModelBenchmarkHistoryPoint[]
}

/** 存在可排名执行的题库版本组合，供排行页切换历史题库 */
export interface ModelBenchmarkRankingSuite {
  suite_id: string
  suite_version: string
  scoring_version: string
  run_count: number
  last_run_at: string
}

/** 测评执行配置：回环网关请求使用的 API Key（0 = 未配置）与全局统一的采样次数、单题输出预算 */
export interface ModelBenchmarkSettings {
  api_key_id: number
  sample_count: number
  max_output_tokens: number
}

export async function listSuites(): Promise<ModelBenchmarkSuite[]> {
  const { data } = await apiClient.get<ModelBenchmarkSuite[]>('/admin/model-benchmarks/suites')
  return data ?? []
}

export async function listTargets(): Promise<ModelManifestItem[]> {
  const { data } = await apiClient.get<ModelManifestItem[]>('/admin/model-benchmarks/targets')
  return data ?? []
}

export async function listPlans(): Promise<ModelBenchmarkPlan[]> {
  const { data } = await apiClient.get<ModelBenchmarkPlan[]>('/admin/model-benchmarks/plans')
  return data ?? []
}

export async function createPlan(req: ModelBenchmarkPlanRequest): Promise<ModelBenchmarkPlan> {
  const { data } = await apiClient.post<ModelBenchmarkPlan>('/admin/model-benchmarks/plans', req)
  return data
}

export async function updatePlan(id: number, req: ModelBenchmarkPlanRequest): Promise<ModelBenchmarkPlan> {
  const { data } = await apiClient.put<ModelBenchmarkPlan>(`/admin/model-benchmarks/plans/${id}`, req)
  return data
}

export async function deletePlan(id: number): Promise<void> {
  await apiClient.delete(`/admin/model-benchmarks/plans/${id}`)
}

/** 手动排队一次执行；停用计划同样可排队（跑完仍保持停用） */
export async function queueRun(id: number): Promise<ModelBenchmarkPlan> {
  const { data } = await apiClient.post<ModelBenchmarkPlan>(`/admin/model-benchmarks/plans/${id}/run`)
  return data
}

export async function listRuns(params?: Record<string, string | number>): Promise<ModelBenchmarkRun[]> {
  const { data } = await apiClient.get<ModelBenchmarkRun[]>('/admin/model-benchmarks/runs', { params })
  return data ?? []
}

export async function getRun(id: number): Promise<ModelBenchmarkRun> {
  const { data } = await apiClient.get<ModelBenchmarkRun>(`/admin/model-benchmarks/runs/${id}`)
  return data
}

export async function listRunItems(id: number): Promise<ModelBenchmarkRunItem[]> {
  const { data } = await apiClient.get<ModelBenchmarkRunItem[]>(`/admin/model-benchmarks/runs/${id}/items`)
  return data ?? []
}

export async function listHistory(params?: Record<string, string | number>): Promise<ModelBenchmarkHistoryPoint[]> {
  const { data } = await apiClient.get<ModelBenchmarkHistoryPoint[]>('/admin/model-benchmarks/history', { params })
  return data ?? []
}

export async function getModelOverview(modelId: string): Promise<ModelBenchmarkModelOverview> {
  const { data } = await apiClient.get<ModelBenchmarkModelOverview>(`/admin/model-benchmarks/models/${encodeURIComponent(modelId)}/overview`)
  return data
}

/** 排名按题库三元组过滤（空对象 = 最新内置题库），不受批次/预算筛选影响 */
export async function listRankings(limit = 50, suite: Record<string, string> = {}): Promise<ModelBenchmarkRanking[]> {
  const { data } = await apiClient.get<ModelBenchmarkRanking[]>('/admin/model-benchmarks/rankings', { params: { limit, ...suite } })
  return data ?? []
}

export async function listRankingSuites(): Promise<ModelBenchmarkRankingSuite[]> {
  const { data } = await apiClient.get<ModelBenchmarkRankingSuite[]>('/admin/model-benchmarks/rankings/suites')
  return data ?? []
}

export async function getSettings(): Promise<ModelBenchmarkSettings> {
  const { data } = await apiClient.get<ModelBenchmarkSettings>('/admin/model-benchmarks/settings')
  return data
}

export async function updateSettings(req: ModelBenchmarkSettings): Promise<ModelBenchmarkSettings> {
  const { data } = await apiClient.put<ModelBenchmarkSettings>('/admin/model-benchmarks/settings', req)
  return data
}

export const modelBenchmarksAPI = {
  listSuites,
  listTargets,
  listPlans,
  createPlan,
  updatePlan,
  delete: deletePlan,
  queueRun,
  listRuns,
  getRun,
  listRunItems,
  listHistory,
  getModelOverview,
  listRankings,
  listRankingSuites,
  getSettings,
  updateSettings
}

export default modelBenchmarksAPI
