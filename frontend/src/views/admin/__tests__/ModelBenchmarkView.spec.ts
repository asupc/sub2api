import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelBenchmarkView from '../ModelBenchmarkView.vue'

const { getModelManifest, listSuites, listTargets, listPlans, listRankings, listRuns, listHistory } = vi.hoisted(() => ({
  getModelManifest: vi.fn(),
  listSuites: vi.fn(),
  listTargets: vi.fn(),
  listPlans: vi.fn(),
  listRankings: vi.fn(),
  listRuns: vi.fn(),
  listHistory: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  default: {
    models: { getModelManifest },
    modelBenchmarks: { listSuites, listTargets, listPlans, listRankings, listRuns, listHistory },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.count !== undefined ? `${key}:{count=${params.count}}` : key,
  }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { name: 'AppLayout', template: '<div><slot /></div>' },
}))

const account = (id: number, name: string, platform = 'openai') => ({
  id,
  name,
  platform,
  type: 'apikey',
  status: 'active',
  schedulable: true,
  rate_limited: false,
  concurrency: 5,
  priority: 5,
  groups: [],
  target: '',
})

function buildManifest() {
  return {
    summary: { total_models: 2, healthy_models: 2, danger_models: 0, total_accounts: 2 },
    models: [
      {
        id: 'supported-model',
        display_name: 'Supported Model',
        platforms: ['openai'],
        total_accounts: 1,
        active_accounts: 1,
        total_concurrency: 5,
        has_warning: false,
        accounts: [account(1, 'supported-account')],
      },
      {
        id: 'unsupported-model',
        display_name: 'Unsupported Model',
        platforms: ['anthropic'],
        total_accounts: 1,
        active_accounts: 1,
        total_concurrency: 5,
        has_warning: false,
        accounts: [account(2, 'unsupported-account', 'anthropic')],
      },
    ],
  }
}

function buildTargets() {
  const manifest = buildManifest()
  return [manifest.models[0]]
}

const wrappers: ReturnType<typeof mount>[] = []

function mountView() {
  const wrapper = mount(ModelBenchmarkView)
  wrappers.push(wrapper)
  return wrapper
}

describe('admin ModelBenchmarkView model selection', () => {
  beforeEach(() => {
    getModelManifest.mockReset().mockResolvedValue(buildManifest())
    listSuites.mockReset().mockResolvedValue([{
      id: 'hard-v1', version: '1.1.0', scoring_version: '1.0.0', item_count: 48,
      family_count: 24, min_difficulty: 3, max_difficulty: 5, category_weights: {},
    }])
    listTargets.mockReset().mockResolvedValue(buildTargets())
    listPlans.mockReset().mockResolvedValue([])
    listRankings.mockReset().mockResolvedValue([])
    listRuns.mockReset().mockResolvedValue([])
    listHistory.mockReset().mockResolvedValue([])
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
  })

  it('uses the full model manifest as the plan model source', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getModelManifest).toHaveBeenCalledTimes(1)
    const modelSelect = wrapper.findAll('select')[3]
    const optionValues = modelSelect.findAll('option').map(option => option.element.value)
    expect(optionValues).toEqual(['supported-model', 'unsupported-model'])
    expect((modelSelect.element as HTMLSelectElement).value).toBe('supported-model')
    expect(wrapper.text()).toContain('supported-account')
  })

  it('filters manifest models and clearly marks unsupported targets', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('input')[1].setValue('Unsupported')
    await flushPromises()

    const modelSelect = wrapper.findAll('select')[3]
    expect(modelSelect.findAll('option').map(option => option.element.value)).toEqual(['unsupported-model'])
    expect((modelSelect.element as HTMLSelectElement).value).toBe('unsupported-model')
    expect(wrapper.text()).toContain('admin.modelBenchmark.plans.unsupportedModel')
  })
})
