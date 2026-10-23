import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { RouterLinkStub } from '@vue/test-utils'

import ManifestTab from '../model-capability/tabs/ManifestTab.vue'

const { getModelManifest } = vi.hoisted(() => ({
  getModelManifest: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    models: {
      getModelManifest,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn(),
    showWarning: vi.fn(),
  }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.count !== undefined ? `${key}:{count=${params.count}}` : key,
  }),
}))

// AppLayout 引入完整布局依赖链（stores/onboarding/i18n 初始化），视图测试只需布局透传 slot
vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { name: 'AppLayout', template: '<div><slot /></div>' },
}))

function buildManifest() {
  return {
    summary: {
      total_models: 2,
      healthy_models: 1,
      danger_models: 1,
      total_accounts: 3,
    },
    models: [
      {
        id: 'claude-alpha',
        display_name: 'Claude Alpha',
        platforms: ['anthropic'],
        total_accounts: 2,
        active_accounts: 1,
        total_concurrency: 10,
        has_warning: false,
        accounts: [
          {
            id: 1,
            name: 'acc-healthy',
            platform: 'anthropic',
            type: 'oauth',
            status: 'active',
            schedulable: true,
            rate_limited: false,
            concurrency: 10,
            priority: 5,
            groups: ['VIP分组'],
            target: 'claude-sonnet-5',
          },
          {
            id: 2,
            name: 'acc-disabled',
            platform: 'anthropic',
            type: 'apikey',
            status: 'inactive',
            schedulable: false,
            rate_limited: false,
            concurrency: 5,
            priority: 1,
            groups: [],
            target: '',
          },
        ],
      },
      {
        id: 'gpt-beta',
        display_name: '',
        platforms: ['openai'],
        total_accounts: 1,
        active_accounts: 0,
        total_concurrency: 0,
        has_warning: true,
        accounts: [
          {
            id: 3,
            name: 'acc-error',
            platform: 'openai',
            type: 'apikey',
            status: 'error',
            schedulable: false,
            rate_limited: false,
            concurrency: 8,
            priority: 2,
            groups: [],
            target: '',
          },
        ],
      },
    ],
  }
}

const wrappers: ReturnType<typeof mount>[] = []

function mountView() {
  const wrapper = mount(ManifestTab, {
    global: {
      stubs: { RouterLink: RouterLinkStub },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('admin ManifestTab（模型清单）', () => {
  beforeEach(() => {
    getModelManifest.mockReset().mockResolvedValue(buildManifest())
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('renders summary stats and model cards', async () => {
    const wrapper = mountView()
    await flushPromises()

    // 指标卡数值来自 summary
    const values = wrapper.findAll('.text-2xl').map(node => node.text())
    expect(values).toEqual(['2', '1', '1', '3'])

    // 模型视角默认渲染两张折叠卡片
    const modelIds = wrapper.findAll('.text-sm.font-bold').map(node => node.text())
    expect(modelIds).toContain('claude-alpha')
    expect(modelIds).toContain('gpt-beta')

    expect(getModelManifest).toHaveBeenCalledTimes(1)
  })

  it('expands a model card to show account details', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('table').exists()).toBe(false)
    await wrapper.find('button.w-full').trigger('click')
    const table = wrapper.find('table')
    expect(table.exists()).toBe(true)
    // 明细含映射目标与直通占位
    expect(table.text()).toContain('claude-sonnet-5')
    expect(table.text()).toContain('admin.modelManifest.directPassThrough')
    // 断供模型展开时应显示严重警报条（用 gpt-beta 卡片验证）
    const cards = wrapper.findAll('button.w-full')
    await cards[1].trigger('click')
    expect(wrapper.text()).toContain('admin.modelManifest.dangerAlert')
  })

  it('filters models by search keyword on account name', async () => {
    const wrapper = mountView()
    await flushPromises()

    const input = wrapper.find('input')
    await input.setValue('acc-error')
    await flushPromises()

    const modelIds = wrapper.findAll('.text-sm.font-bold').map(node => node.text())
    expect(modelIds).toEqual(['gpt-beta'])
  })

  it('switches to account view and lists inverted account rows', async () => {
    const wrapper = mountView()
    await flushPromises()

    const tabs = wrapper.findAll('button').filter(node => node.text().includes('admin.modelManifest.tabs.byAccount'))
    expect(tabs.length).toBe(1)
    await tabs[0].trigger('click')

    const table = wrapper.find('table')
    expect(table.exists()).toBe(true)
    // 倒推出的账号行：3 个账号，acc-healthy 提供 claude-alpha
    expect(table.text()).toContain('acc-healthy')
    expect(table.text()).toContain('claude-alpha')
    expect(table.text()).toContain('gpt-beta')
  })
})
