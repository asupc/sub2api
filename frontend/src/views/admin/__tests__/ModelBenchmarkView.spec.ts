import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelBenchmarkView from '../ModelBenchmarkView.vue'

const { listSuites, listRankings, listRuns, listHistory } = vi.hoisted(() => ({
  listSuites: vi.fn(),
  listRankings: vi.fn(),
  listRuns: vi.fn(),
  listHistory: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  default: {
    models: {},
    modelBenchmarks: { listSuites, listRankings, listRuns, listHistory },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
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
      id: 'hard-v1', version: '1.1.0', scoring_version: '1.0.0', item_count: 48,
      family_count: 24, min_difficulty: 3, max_difficulty: 5, category_weights: {},
    }])
    listRankings.mockReset().mockResolvedValue([{
      model_id: 'glm-5.3', suite_id: 'hard-v1', suite_version: '1.1.0', scoring_version: '1.0.0',
      run_count: 3, intelligence_score: 79.2, efficiency_score: 97.5, composite_score: 82.0,
      output_tokens: 11282, avg_latency_ms: 6040, estimated_cost: 0.1, last_run_at: '2026-10-15T22:09:40Z',
    }])
    listRuns.mockReset().mockResolvedValue([])
    listHistory.mockReset().mockResolvedValue([])
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
  })

  it('renders rankings without the plan management block', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('glm-5.3')
    // 计划管理已拆到独立页面：本页不再出现创建表单与计划表格
    expect(wrapper.text()).not.toContain('admin.modelBenchmark.plans.create')
    expect(wrapper.text()).not.toContain('admin.modelBenchmark.plans.title')
    // 提供跳转到测评计划页的入口
    expect(wrapper.text()).toContain('admin.modelBenchmark.stats.managePlans')
  })

  it('renders run records and loads them on mount', async () => {
    listRuns.mockResolvedValue([{
      id: 12, plan_id: 4, suite_id: 'hard-v1', suite_version: '1.1.0', scoring_version: '1.0.0',
      model_id: 'minimax-m3', upstream_model_id: 'MiniMax-M3', account_id: 570, status: 'partial',
      intelligence_score: 62.1, category_scores: {}, item_count: 48, answered_count: 3, correct_count: 2,
      usage_source: 'missing', usage_field_coverage: {}, error_message: '', started_at: '2026-10-15T22:51:10Z',
      created_at: '2026-10-15T22:51:10Z',
    }])
    const wrapper = mountView()
    await flushPromises()

    expect(listRuns).toHaveBeenCalled()
    expect(wrapper.text()).toContain('minimax-m3')
    expect(wrapper.text()).toContain('admin.modelBenchmark.runStatus.partial')
  })
})
