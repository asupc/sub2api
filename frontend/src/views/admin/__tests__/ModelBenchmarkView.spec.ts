import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelBenchmarkView from '../ModelBenchmarkView.vue'

const {
  listSuites,
  listRankings,
  listRankingSuites,
  listRuns,
  listHistory,
  getRun,
  listRunItems,
  getModelOverview,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listSuites: vi.fn(),
  listRankings: vi.fn(),
  listRankingSuites: vi.fn(),
  listRuns: vi.fn(),
  listHistory: vi.fn(),
  getRun: vi.fn(),
  listRunItems: vi.fn(),
  getModelOverview: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  default: {
    models: {},
    modelBenchmarks: {
      listSuites,
      listRankings,
      listRankingSuites,
      listRuns,
      listHistory,
      getRun,
      listRunItems,
      getModelOverview,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { name: 'AppLayout', template: '<div><slot /></div>' },
}))

vi.mock('vue-router', () => ({
  RouterLink: { name: 'RouterLink', template: '<a><slot /></a>' },
}))

const wrappers: ReturnType<typeof mount>[] = []

function mountView() {
  const wrapper = mount(ModelBenchmarkView)
  wrappers.push(wrapper)
  return wrapper
}

describe('admin ModelBenchmarkView', () => {
  beforeEach(() => {
    listSuites.mockReset().mockResolvedValue([{
      id: 'hard-v1', version: '7.0.0', scoring_version: '7.0.0', item_count: 106,
      family_count: 106, min_difficulty: 3, max_difficulty: 7, category_weights: { code: .28, algorithms: .18 },
    }])
    listRankings.mockReset().mockResolvedValue([{
      model_id: 'glm-5.3', suite_id: 'hard-v1', suite_version: '4.0.0', scoring_version: '4.0.0',
      comparison_key: 'baseline-v2', max_output_tokens: 2048, timeout_seconds: 90, sample_count: 1,
      run_count: 3, intelligence_score: 79.2, efficiency_score: 97.5, composite_score: 82.0,
      output_tokens: 11282, avg_latency_ms: 6040, estimated_cost: 0.1, last_run_at: '2026-10-15T22:09:40Z',
    }])
    listRankingSuites.mockReset().mockResolvedValue([{
      suite_id: 'hard-v1', suite_version: '1.3.0', scoring_version: '1.1.0', run_count: 5, last_run_at: '2026-09-01T00:00:00Z',
    }])
    listRuns.mockReset().mockResolvedValue([])
    listHistory.mockReset().mockResolvedValue([])
    getRun.mockReset()
    listRunItems.mockReset().mockResolvedValue([])
    getModelOverview.mockReset().mockResolvedValue({
      model_id: 'glm-5.3',
      ranking: {
        model_id: 'glm-5.3',
        run_count: 3,
        intelligence_score: 79.2,
        composite_score: 82.0,
        efficiency_score: 97.5,
        avg_latency_ms: 6040,
        category_scores: { math: 80, logic: 75, code: 90 },
      },
      plans: [],
      recent_runs: [],
      history: [],
    })
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
  })

  it('renders rankings without the plan management block', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('glm-5.3')
    expect(wrapper.text()).toContain('hard-v1 v7.0.0')
    expect(wrapper.text()).toContain('106 admin.modelBenchmark.items')
    expect(wrapper.text()).toContain('admin.modelBenchmark.category.code 28%')
    expect(wrapper.text()).toContain('admin.modelBenchmark.category.algorithms 18%')
    expect(wrapper.text()).not.toContain('admin.modelBenchmark.plans.create')
    expect(wrapper.text()).not.toContain('admin.modelBenchmark.plans.title')
    expect(wrapper.text()).toContain('admin.modelBenchmark.stats.managePlans')
  })

  it('loads rankings per suite and supports switching suite', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(listRankings).toHaveBeenLastCalledWith(100, {})
    expect(listRankingSuites).toHaveBeenCalledTimes(1)

    // 切换到历史题库
    await wrapper.get('select[aria-label="admin.modelBenchmark.rankings.suite"]').setValue('hard-v1|1.3.0|1.1.0')
    await flushPromises()
    expect(listRankings).toHaveBeenLastCalledWith(100, { suite_id: 'hard-v1', suite_version: '1.3.0', scoring_version: '1.1.0' })
  })

  it('opens model detail drawer when clicking a model row', async () => {
    const wrapper = mountView()
    await flushPromises()

    const row = wrapper.findAll('tr').find(item => item.text().includes('glm-5.3'))
    expect(row).toBeDefined()
    await row!.trigger('click')
    await flushPromises()

    expect(getModelOverview).toHaveBeenCalledWith('glm-5.3')
    expect(wrapper.text()).toContain('admin.modelBenchmark.drawer.subtitle')
  })

  it('renders run records in the runs view', async () => {
    listRuns.mockResolvedValue([{
      id: 12, plan_id: 4, suite_id: 'hard-v1', suite_version: '1.1.0', scoring_version: '1.0.0',
      model_id: 'minimax-m3', upstream_model_id: 'MiniMax-M3', status: 'partial',
      intelligence_score: 62.1, category_scores: {}, item_count: 48, answered_count: 3, correct_count: 2,
      usage_source: 'missing', usage_field_coverage: {}, error_message: '', started_at: '2026-10-15T22:51:10Z',
      created_at: '2026-10-15T22:51:10Z',
    }])
    const { default: OverviewTab } = await import('../model-capability/tabs/OverviewTab.vue')
    const wrapper = mount(OverviewTab, { props: { mode: 'runs' } })
    wrappers.push(wrapper)
    await flushPromises()

    expect(listRuns).toHaveBeenCalled()
    expect(wrapper.text()).toContain('minimax-m3')
    expect(wrapper.text()).toContain('admin.modelBenchmark.runStatus.partial')
  })
})
