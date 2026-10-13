import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import DashboardView from '../DashboardView.vue'

const { getDashboardStats, getDashboardSnapshotV2, getDashboardModels } = vi.hoisted(() => ({
  getDashboardStats: vi.fn(),
  getDashboardSnapshotV2: vi.fn(),
  getDashboardModels: vi.fn()
}))

const { getAdminSnapshotV2, getUserUsageTrend, getModelStats } = vi.hoisted(() => ({
  getAdminSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getModelStats: vi.fn()
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats,
    getDashboardSnapshotV2,
    getDashboardModels
  }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2: getAdminSnapshotV2,
      getUserUsageTrend,
      getModelStats
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 12.34 },
    refreshUser: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const createUserStats = () => ({
  total_api_keys: 1,
  active_api_keys: 1,
  total_requests: 100,
  total_input_tokens: 1000,
  total_output_tokens: 500,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 500,
  total_tokens: 2000,
  total_cost: 1,
  total_actual_cost: 0.5,
  today_requests: 10,
  today_input_tokens: 100,
  today_output_tokens: 50,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 50,
  today_tokens: 200,
  today_cost: 0.1,
  today_actual_cost: 0.05,
  average_duration_ms: 800,
  rpm: 3,
  tpm: 400
})

describe('user DashboardView (personal scope)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getDashboardStats.mockReset()
    getDashboardSnapshotV2.mockReset()
    getDashboardModels.mockReset()
    getAdminSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getModelStats.mockReset()

    getDashboardStats.mockResolvedValue(createUserStats())
    getDashboardSnapshotV2.mockResolvedValue({
      generated_at: '',
      start_date: '',
      end_date: '',
      granularity: 'hour',
      trend: [],
      models: []
    })
    getDashboardModels.mockResolvedValue({ models: [], start_date: '', end_date: '' })
  })

  const mountView = () =>
    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

  it('loads personal stats via user APIs and never touches admin endpoints', async () => {
    mountView()
    await flushPromises()

    expect(getDashboardStats).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getDashboardModels).toHaveBeenCalledTimes(1)

    expect(getAdminSnapshotV2).not.toHaveBeenCalled()
    expect(getUserUsageTrend).not.toHaveBeenCalled()
    expect(getModelStats).not.toHaveBeenCalled()
  })

  it('shows balance pill and hides site-wide user trend module', async () => {
    const wrapper = mountView()
    await flushPromises()

    // 个人视角展示余额胶囊
    expect(wrapper.text()).toContain('$12.34')
    expect(wrapper.text()).toContain('我的历史累计总用量')

    // 全站专属模块不渲染
    expect(wrapper.text()).not.toContain('用量态势与时序分布')
  })
})
