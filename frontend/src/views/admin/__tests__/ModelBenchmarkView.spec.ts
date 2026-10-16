import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelBenchmarkView from '../ModelBenchmarkView.vue'

const { listSuites, listRankings, listRuns, listHistory, getRun, listRunItems } = vi.hoisted(() => ({
  listSuites: vi.fn(),
  listRankings: vi.fn(),
  listRuns: vi.fn(),
  listHistory: vi.fn(),
  getRun: vi.fn(),
  listRunItems: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  default: {
    models: {},
    modelBenchmarks: { listSuites, listRankings, listRuns, listHistory, getRun, listRunItems },
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
      id: 'hard-v1', version: '2.0.0', scoring_version: '2.0.0', item_count: 112,
      family_count: 56, min_difficulty: 3, max_difficulty: 5, category_weights: {},
    }])
    listRankings.mockReset().mockResolvedValue([{
      model_id: 'glm-5.3', suite_id: 'hard-v1', suite_version: '1.1.0', scoring_version: '1.0.0',
      run_count: 3, intelligence_score: 79.2, efficiency_score: 97.5, composite_score: 82.0,
      output_tokens: 11282, avg_latency_ms: 6040, estimated_cost: 0.1, last_run_at: '2026-10-15T22:09:40Z',
    }])
    listRuns.mockReset().mockResolvedValue([])
    listHistory.mockReset().mockResolvedValue([])
    getRun.mockReset()
    listRunItems.mockReset().mockResolvedValue([])
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

  it('applies identical comparison keys and budgets to rankings and history', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(listRankings).toHaveBeenLastCalledWith(50, 'baseline-v2', { max_output_tokens: 2048, timeout_seconds: 90, sample_count: 1 })
    await wrapper.get('input[aria-label="公共试卷批次"]').setValue('reasoning-study-2')
    await wrapper.get('input[aria-label="输出预算"]').setValue(4096)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(listRankings).toHaveBeenLastCalledWith(50, 'reasoning-study-2', { max_output_tokens: 4096, timeout_seconds: 90, sample_count: 1 })
    expect(listHistory).toHaveBeenLastCalledWith(expect.objectContaining({ comparison_key: 'reasoning-study-2', max_output_tokens: 4096 }))
    // Diagnostic runs with provider-managed budgets remain discoverable.
    expect(listRuns).toHaveBeenLastCalledWith({ limit: 100, comparison_key: 'reasoning-study-2' })
  })

  it('shows new domains and explains low-coverage results without ranking them', async () => {
    const run = {
      id: 20, model_id: 'diagnostic-model', suite_version: '2.0.0', status: 'partial',
      comparison_key: 'baseline-v2', max_output_tokens: 2048, timeout_seconds: 90, sample_count: 1,
      category_scores: { abstraction: 70, understanding: 60, causal: 55 }, intelligence_score: 65,
      quality: { completion_rate: 40, format_compliance_rate: 95, format_error_count: 2, budget_exhausted_count: 3, ranking_eligible: false, ranking_reason: 'insufficient_coverage', category_coverage: {} },
    }
    listRuns.mockResolvedValue([run])
    getRun.mockResolvedValue(run)
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('40.0%')
    expect(wrapper.text()).toContain('admin.modelBenchmark.comparison.diagnostic')
    expect(wrapper.text()).toContain('admin.modelBenchmark.category.abstraction')
    expect(wrapper.text()).toContain('admin.modelBenchmark.category.understanding')
    expect(wrapper.text()).toContain('admin.modelBenchmark.category.causal')
    const row = wrapper.findAll('tr').find(item => item.text().includes('diagnostic-model'))
    await row!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.modelBenchmark.fields.formatCompliance')
    expect(wrapper.text()).toContain('admin.modelBenchmark.fields.budgetExhausted')
    expect(wrapper.text()).toContain('admin.modelBenchmark.comparison.coverageReason')
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
