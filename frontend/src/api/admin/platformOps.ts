/**
 * Admin Platform Ops & OAuth API
 * 三平台（WorkBuddy / Qoder / Trae）的设备授权建号端点 + 通用运营任务端点
 * （/admin/platform-ops，任务执行按账号平台在后端分派）。
 */

import { apiClient } from '../client'
import type { Account } from '@/types'

// ==================== 运营任务（/admin/platform-ops） ====================

export interface PlatformOpsTaskInfo {
  id: string
  platform: string
  name: string
  hours?: number[]
  interval_seconds?: number
  manual_only?: boolean
  include_disabled?: boolean
}

export interface PlatformOpsStateEntry {
  last_run?: string
  last_ok?: string
  last_err?: string
  streak?: number
  retries?: number
  next_run?: string
}

export interface PlatformOpsAccountTask extends PlatformOpsTaskInfo {
  state: PlatformOpsStateEntry
}

export interface PlatformOpsTaskCatalogEntry {
  platform: string
  tasks: PlatformOpsTaskInfo[]
}

export interface PlatformOpsAccountTasksResponse {
  platform: string
  tasks: PlatformOpsAccountTask[]
}

export interface PlatformOpsRunResult {
  done: boolean
  error?: string
}

/** GET /admin/platform-ops/tasks — 全部平台的运营任务注册表 */
export async function listTaskCatalog(): Promise<PlatformOpsTaskCatalogEntry[]> {
  const { data } = await apiClient.get<PlatformOpsTaskCatalogEntry[]>('/admin/platform-ops/tasks')
  return data
}

/** GET /admin/platform-ops/accounts/:id — 账号全部运营任务的描述与最近状态 */
export async function getAccountTasks(accountId: number): Promise<PlatformOpsAccountTasksResponse> {
  const { data } = await apiClient.get<PlatformOpsAccountTasksResponse>(
    `/admin/platform-ops/accounts/${accountId}`
  )
  return data
}

/** POST /admin/platform-ops/accounts/:id/tasks/:task — 手动执行一个运营任务 */
export async function runAccountTask(accountId: number, task: string): Promise<PlatformOpsRunResult> {
  const { data } = await apiClient.post<PlatformOpsRunResult>(
    `/admin/platform-ops/accounts/${accountId}/tasks/${task}`
  )
  return data
}

// ==================== 账号级任务调度配置 ====================

export interface PlatformOpsTaskConfig {
  /** 缺省（undefined）= 启用自动调度 */
  enabled?: boolean
  /** 每日定点小时（覆盖注册表默认；空数组 = 不定期） */
  hours?: number[]
  /** 固定间隔（小时；hours 覆盖优先） */
  interval_hours?: number
}

export interface PlatformOpsAccountConfigTask extends PlatformOpsTaskInfo {
  config: PlatformOpsTaskConfig
}

export interface PlatformOpsAccountConfigResponse {
  platform: string
  tasks: PlatformOpsAccountConfigTask[]
}

/** GET /admin/platform-ops/platforms/:platform/config — 平台任务默认调度 + 平台级覆盖 */
export async function getPlatformTaskConfig(platform: string): Promise<PlatformOpsAccountConfigResponse> {
  const { data } = await apiClient.get<PlatformOpsAccountConfigResponse>(
    `/admin/platform-ops/platforms/${platform}/config`
  )
  return data
}

/** PUT /admin/platform-ops/platforms/:platform/config — 保存平台级任务配置（对该平台全部账号统一生效） */
export async function updatePlatformTaskConfig(
  platform: string,
  tasks: Record<string, PlatformOpsTaskConfig>
): Promise<{ saved: boolean }> {
  const { data } = await apiClient.put<{ saved: boolean }>(
    `/admin/platform-ops/platforms/${platform}/config`,
    { tasks }
  )
  return data
}

// ==================== 设备授权轮询通用形状 ====================

export interface DeviceLoginStartResponse {
  url: string
  state: string
}

export interface DeviceLoginPollResponse {
  done: boolean
  credentials?: Record<string, unknown>
  user_id?: string
  nickname?: string
  display_name?: string
  message?: string
}

// ==================== WorkBuddy（/admin/workbuddy） ====================

export interface WorkbuddyQuotaSnapshot {
  remain: number
  total: number
  expiring?: number
  expiring_at?: string
  updated_at: string
  enterprise?: boolean
  unlimited?: boolean
}

export interface WorkbuddyCreateFromOAuthRequest {
  state?: string
  credentials?: Record<string, unknown>
  name?: string
  notes?: string | null
  proxy_id?: number
  concurrency?: number
  priority?: number
  group_ids?: number[]
  model_mapping?: Record<string, unknown>
}

/** POST /admin/workbuddy/oauth/login-start — 发起设备授权 */
export async function workbuddyLoginStart(
  realm: string,
  proxyId?: number
): Promise<DeviceLoginStartResponse> {
  const { data } = await apiClient.post<DeviceLoginStartResponse>(
    '/admin/workbuddy/oauth/login-start',
    { realm, proxy_id: proxyId }
  )
  return data
}

/** POST /admin/workbuddy/oauth/login-poll — 轮询授权进度 */
export async function workbuddyLoginPoll(state: string): Promise<DeviceLoginPollResponse> {
  const { data } = await apiClient.post<DeviceLoginPollResponse>(
    '/admin/workbuddy/oauth/login-poll',
    { state }
  )
  return data
}

/** POST /admin/workbuddy/oauth/create-from-oauth — 用登录凭证建号 */
export async function workbuddyCreateFromOAuth(
  payload: WorkbuddyCreateFromOAuthRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    '/admin/workbuddy/oauth/create-from-oauth',
    payload
  )
  return data
}

/** POST /admin/workbuddy/accounts/:id/refresh — 强制刷新 token */
export async function workbuddyRefreshToken(accountId: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/admin/workbuddy/accounts/${accountId}/refresh`)
  return data
}

/** GET /admin/workbuddy/accounts/:id/quota — 查积分余额（落 extra.wb_quota） */
export async function workbuddyQueryQuota(accountId: number): Promise<WorkbuddyQuotaSnapshot> {
  const { data } = await apiClient.get<WorkbuddyQuotaSnapshot>(
    `/admin/workbuddy/accounts/${accountId}/quota`
  )
  return data
}

// ==================== Qoder（/admin/qoder） ====================

export interface QoderQuotaSnapshot {
  remain: number
  used: number
  total: number
  exceeded?: boolean
  plan?: string
  updated_at: string
}

/** POST /admin/qoder/oauth/login-start — 发起设备授权 */
export async function qoderLoginStart(
  realm: string,
  proxyId?: number
): Promise<DeviceLoginStartResponse> {
  const { data } = await apiClient.post<DeviceLoginStartResponse>(
    '/admin/qoder/oauth/login-start',
    { realm, proxy_id: proxyId }
  )
  return data
}

/** POST /admin/qoder/oauth/login-poll — 轮询授权进度 */
export async function qoderLoginPoll(state: string): Promise<DeviceLoginPollResponse> {
  const { data } = await apiClient.post<DeviceLoginPollResponse>('/admin/qoder/oauth/login-poll', {
    state
  })
  return data
}

/**
 * POST /admin/qoder/oauth/create-from-pat — 导入 pt- 个人访问令牌。
 * 注意：Qoder 没有 create-from-oauth 端点，设备流与 PAT 拿到 credentials 后
 * 统一走 adminAPI.accounts.create（platform=qoder, type=oauth）建号。
 */
export async function qoderImportPAT(
  pat: string,
  realm: string,
  proxyId?: number
): Promise<DeviceLoginPollResponse> {
  const { data } = await apiClient.post<DeviceLoginPollResponse>(
    '/admin/qoder/oauth/create-from-pat',
    { pat, realm, proxy_id: proxyId }
  )
  return data
}

/** GET /admin/qoder/accounts/:id/quota — 查剩余积分（落 extra.qd_quota） */
export async function qoderQueryQuota(accountId: number): Promise<QoderQuotaSnapshot> {
  const { data } = await apiClient.get<QoderQuotaSnapshot>(`/admin/qoder/accounts/${accountId}/quota`)
  return data
}

// ==================== Trae（/admin/trae） ====================

export interface TraeQuotaSnapshot {
  total: number
  purchased_today?: number
  earliest_expire?: number
  has_expire?: boolean
  packages?: Array<Record<string, unknown>>
  expiring_soon?: boolean
}

export interface TraeCreateFromOAuthRequest {
  state: string
  callback_query?: string
  name?: string
  proxy_id?: number
  concurrency?: number
  priority?: number
  group_ids?: number[]
  model_mapping?: Record<string, unknown>
}

/**
 * POST /admin/trae/oauth/login-start — 生成授权页 URL（本机回调语义）。
 * deviceId 与 devicePublicKey 必须成对提供（同一台已知设备的 id+公钥对），
 * 单传其一会被后端 400；都省略时后端自动生成虚拟设备。
 */
export async function traeLoginStart(
  variant: string,
  proxyId?: number,
  deviceId?: string,
  devicePublicKey?: string
): Promise<DeviceLoginStartResponse> {
  const { data } = await apiClient.post<DeviceLoginStartResponse>(
    '/admin/trae/oauth/login-start',
    { variant, proxy_id: proxyId, device_id: deviceId || '', device_public_key: devicePublicKey || '' }
  )
  return data
}

/**
 * POST /admin/trae/oauth/login-poll — 回传授权页回跳查询串完成交换。
 * callbackQuery 为空时仅查询会话进度（done=false 无副作用）。
 */
export async function traeLoginPoll(
  state: string,
  callbackQuery?: string
): Promise<DeviceLoginPollResponse> {
  const { data } = await apiClient.post<DeviceLoginPollResponse>('/admin/trae/oauth/login-poll', {
    state,
    callback_query: callbackQuery || ''
  })
  return data
}

/** POST /admin/trae/oauth/create-from-oauth — 用完成的登录会话直接建号 */
export async function traeCreateFromOAuth(
  payload: TraeCreateFromOAuthRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>('/admin/trae/oauth/create-from-oauth', payload)
  return data
}

/** GET /admin/trae/accounts/:id/quota — 查剩余积分（落 extra.trae_quota） */
export async function traeQueryQuota(accountId: number): Promise<TraeQuotaSnapshot> {
  const { data } = await apiClient.get<TraeQuotaSnapshot>(`/admin/trae/accounts/${accountId}/quota`)
  return data
}

export default {
  listTaskCatalog,
  getAccountTasks,
  runAccountTask,
  getPlatformTaskConfig,
  updatePlatformTaskConfig,
  workbuddyLoginStart,
  workbuddyLoginPoll,
  workbuddyCreateFromOAuth,
  workbuddyRefreshToken,
  workbuddyQueryQuota,
  qoderLoginStart,
  qoderLoginPoll,
  qoderImportPAT,
  qoderQueryQuota,
  traeLoginStart,
  traeLoginPoll,
  traeCreateFromOAuth,
  traeQueryQuota
}
