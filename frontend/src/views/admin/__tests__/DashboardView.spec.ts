import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getModelStats, getUserUsageTrend, getUserSpendingRanking, getApiKeyUsageStats } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getModelStats: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn(),
  getApiKeyUsageStats: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getModelStats,
      getUserUsageTrend,
      getUserSpendingRanking,
      getApiKeyUsageStats
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
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

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getModelStats.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()
    getApiKeyUsageStats.mockReset()

    getApiKeyUsageStats.mockResolvedValue({
      items: [],
      start_date: '',
      end_date: ''
    })

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getModelStats.mockResolvedValue({
      models: [],
      start_date: '',
      end_date: ''
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses today as default dashboard range', async () => {
    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true
        }
      }
    })

    await flushPromises()

    const now = new Date()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(now),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
  })

  it('does not render groupPricing quick action nor the hero model usage distribution', async () => {
    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getModelStats.mockResolvedValue({
      models: [
        {
          model: 'claude-3-7-sonnet',
          requests: 100,
          input_tokens: 3000,
          output_tokens: 1000,
          cache_creation_tokens: 0,
          cache_read_tokens: 7000,
          total_tokens: 11000,
          cost: 0,
          actual_cost: 0
        }
      ],
      start_date: '',
      end_date: ''
    })

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true
        }
      }
    })

    await flushPromises()

    // Verify quick actions card is completely removed
    expect(wrapper.text()).not.toContain('admin.dashboard.quickActions')
    expect(wrapper.text()).not.toContain('admin.dashboard.groupPricing')
    expect(wrapper.text()).not.toContain('admin.dashboard.batchImage')

    // admin 视角已移除 hero 核心模型用量分布：今日模型数据不再渲染为模型份额/缓存命中率
    expect(getModelStats).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(new Date()),
      end_date: formatLocalDate(new Date())
    }))
    expect(wrapper.text()).not.toContain('claude-3-7-sonnet')
    expect(wrapper.text()).not.toContain('核心模型用量分布')
  })

  it('renders enhanced admin statistics: resource cards, traffic trend, model & group distribution and lifetime scale', async () => {
    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getModelStats.mockResolvedValue({
      models: [
        {
          model: 'today-hero-model',
          requests: 50,
          input_tokens: 2000,
          output_tokens: 1000,
          cache_creation_tokens: 0,
          cache_read_tokens: 0,
          total_tokens: 3000,
          cost: 0,
          actual_cost: 0
        }
      ],
      start_date: '',
      end_date: ''
    })

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: {
            props: ['startDate', 'endDate'],
            emits: ['change'],
            template: '<div class="stub-date-picker" @click="$emit(\'change\', { startDate: \'2026-08-01\', endDate: \'2026-08-31\', preset: null })" />'
          },
          Select: true,
          ModelDistributionChart: {
            props: ['modelStats'],
            template: '<div class="stub-model-chart" :data-models="JSON.stringify(modelStats)" />'
          },
          TokenUsageTrend: {
            template: '<div class="stub-token-trend" />'
          },
          GroupUsageStats: {
            template: '<div class="stub-group-stats" />'
          }
        }
      }
    })

    await flushPromises()

    // 1. admin 视角展示健康度大盘、流量趋势图、全生命周期累计规模与渠道分组
    expect(wrapper.text()).toContain('admin.dashboard.accountPool')
    expect(wrapper.text()).toContain('admin.dashboard.lifetimeScale')
    expect(wrapper.find('.stub-token-trend').exists()).toBe(true)
    expect(wrapper.find('.stub-group-stats').exists()).toBe(true)

    // 2. hero 区域不再渲染旧版核心模型份额；旧版用户时序图不再渲染
    expect(wrapper.text()).not.toContain('today-hero-model')
    expect(wrapper.text()).not.toContain('用量态势与时序分布')

    // 2. getModelStats 仍以「今天」为区间加载（个人视角 hero 模型份额仍使用该口径）
    expect(getModelStats).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(new Date()),
      end_date: formatLocalDate(new Date())
    }))

    // 3. Mock a range update with different models (e.g. historical 30 days)
    getSnapshotV2.mockResolvedValueOnce({
      stats: null,
      trend: [],
      models: [
        {
          model: 'historical-30d-model',
          requests: 800,
          input_tokens: 50000,
          output_tokens: 20000,
          cache_creation_tokens: 0,
          cache_read_tokens: 0,
          total_tokens: 70000,
          cost: 0,
          actual_cost: 0
        }
      ]
    })

    // Trigger date range change
    await wrapper.find('.stub-date-picker').trigger('click')
    await flushPromises()

    // 4. ModelDistributionChart DOES receive the historical-30d-model for its own view
    const modelChart = wrapper.find('.stub-model-chart')
    expect(modelChart.attributes('data-models')).toContain('historical-30d-model')

    // 5. Check layout: no lg:grid-cols-2 wrapper enclosing both charts
    expect(wrapper.find('.lg\\:grid-cols-2').exists()).toBe(false)
  })
})
