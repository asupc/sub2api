import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelBenchmarkPlansView from '../ModelBenchmarkPlansView.vue'

const { getModelManifest, listSuites, listTargets, listPlans, queueRun, showSuccess, showError } = vi.hoisted(() => ({
  getModelManifest: vi.fn(),
  listSuites: vi.fn(),
  listTargets: vi.fn(),
  listPlans: vi.fn(),
  queueRun: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  default: {
    models: { getModelManifest },
    modelBenchmarks: { listSuites, listTargets, listPlans, queueRun },
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

function buildPlan(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    name: 'weekly-hard',
    suite_id: 'hard-v1',
    suite_version: '1.1.0',
    scoring_version: '1.0.0',
    model_id: 'supported-model',
    account_id: 1,
    cron_expression: '0 4 * * 1',
    enabled: false,
    sample_count: 1,
    max_output_tokens: 768,
    timeout_seconds: 90,
    max_results: 50,
    running_status: 'idle',
    created_at: '2026-10-16T00:00:00Z',
    updated_at: '2026-10-16T00:00:00Z',
    ...overrides,
  }
}

const wrappers: ReturnType<typeof mount>[] = []

function mountView() {
  const wrapper = mount(ModelBenchmarkPlansView)
  wrappers.push(wrapper)
  return wrapper
}

describe('admin ModelBenchmarkPlansView', () => {
  beforeEach(() => {
    getModelManifest.mockReset().mockResolvedValue(buildManifest())
    listSuites.mockReset().mockResolvedValue([{
      id: 'hard-v1', version: '1.1.0', scoring_version: '1.0.0', item_count: 48,
      family_count: 24, min_difficulty: 3, max_difficulty: 5, category_weights: {},
    }])
    listTargets.mockReset().mockResolvedValue([buildManifest().models[0]])
    listPlans.mockReset().mockResolvedValue([buildPlan()])
    queueRun.mockReset().mockResolvedValue(buildPlan())
    showSuccess.mockReset()
    showError.mockReset()
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
  })

  it('renders the plan table with model and account columns', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getModelManifest).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('weekly-hard')
    expect(wrapper.text()).toContain('supported-model')
    expect(wrapper.text()).toContain('#1')
    // 停用计划渲染停用态
    expect(wrapper.text()).toContain('admin.modelBenchmark.status.disabled')
  })

  it('offers the full manifest as model options and marks unsupported targets', async () => {
    const wrapper = mountView()
    await flushPromises()

    // 全量模型清单（含不可测评模型）都进入模型下拉，平台选择已被移除
    expect(wrapper.text()).not.toContain('admin.modelBenchmark.plans.platformFilter')
    await wrapper.find('[role="listbox"], .select-trigger').trigger('click')
    await flushPromises()
    const optionLabels = document.body.textContent ?? ''
    expect(optionLabels).toContain('supported-model')
    expect(optionLabels).toContain('unsupported-model')
  })

  it('asks for confirmation before queuing and reports success', async () => {
    const wrapper = mountView()
    await flushPromises()

    const runButton = wrapper.findAll('button').find(button => button.text() === 'admin.modelBenchmark.plans.runNow')
    expect(runButton).toBeTruthy()
    await runButton!.trigger('click')
    await flushPromises()

    // 确认弹窗经 Teleport 渲染到 body：确认前不得发起请求，且必须展示执行提醒
    expect(queueRun).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('admin.modelBenchmark.plans.runConfirmTitle')

    const dialogs = Array.from(document.body.querySelectorAll('button'))
      .filter(button => button.textContent?.trim() === 'admin.modelBenchmark.plans.runNow')
    expect(dialogs.length).toBeGreaterThan(0)
    dialogs[dialogs.length - 1].click()
    await flushPromises()

    expect(queueRun).toHaveBeenCalledWith(7)
    expect(showSuccess).toHaveBeenCalled()
    document.body.innerHTML = ''
  })
})
