import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import AccountsView from '../AccountsView.vue'
import AccountTableFilters from '@/components/admin/account/AccountTableFilters.vue'
import { CreateAccountModal } from '@/components/account'

// hoisted 共享状态：vue-router mock 工厂在模块加载期执行，route 必须是
// reactive 对象，组件对 route.params.platform 的 watch 才能在用例修改后触发。
const ctx = vi.hoisted(() => ({
  route: null as any,
  replace: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const { reactive } = await vi.importActual<typeof import('vue')>('vue')
  ctx.route = reactive({
    name: 'AdminAccounts',
    path: '/admin/accounts',
    params: {} as Record<string, string>,
  })
  return {
    useRoute: () => ctx.route,
    useRouter: () => ({ replace: ctx.replace }),
  }
})

const {
  listAccounts,
  listWithEtag,
  getById,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  refreshCredentials,
  showError,
  showWarning
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getById: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  refreshCredentials: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      getById,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
      refreshCredentials
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ siteName: 'Sub2API', showError, showWarning, showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function mountView() {
  return mount(AccountsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: true,
        AccountTableActions: { template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        UpstreamBillingRateCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

function setRoute(name: string, path: string, platform?: string) {
  ctx.route.name = name
  ctx.route.path = path
  if (platform === undefined) {
    delete ctx.route.params.platform
  } else {
    ctx.route.params.platform = platform
  }
}

/** 通过过滤器 stub 触发平台过滤变化（等价于下拉框选择）。 */
async function emitPlatformFilter(wrapper: ReturnType<typeof mountView>, platform: string) {
  wrapper.getComponent(AccountTableFilters).vm.$emit('update:filters', { platform })
  await nextTick()
  // 模拟 router.replace 生效后的路由状态同步
  if (platform) {
    setRoute('AdminAccountsPlatform', `/admin/accounts/p/${platform}`, platform)
  } else {
    setRoute('AdminAccounts', '/admin/accounts')
  }
  await nextTick()
}

describe('admin AccountsView 平台路由', () => {
  beforeEach(() => {
    localStorage.clear()
    setRoute('AdminAccounts', '/admin/accounts')
    ctx.replace.mockReset()
    listAccounts.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: 'etag', data: null })
    getById.mockReset()
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: true })
    getAllProxies.mockReset().mockResolvedValue([])
    getAllGroups.mockReset().mockResolvedValue([])
    refreshCredentials.mockReset()
    showError.mockReset()
    showWarning.mockReset()
  })

  afterEach(() => {
    document.title = ''
    vi.restoreAllMocks()
  })

  it('平台路由进入时 initialParams 注入 platform 过滤', async () => {
    setRoute('AdminAccountsPlatform', '/admin/accounts/p/gemini', 'gemini')
    const wrapper = mountView()
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ platform: 'gemini' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    // 文档标题带平台显示名
    expect(document.title).toContain('·')
    expect(document.title).toContain('Gemini')
    wrapper.unmount()
  })

  it('路由参数变化时更新过滤参数并重载，且不回写 URL', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledTimes(1)

    setRoute('AdminAccountsPlatform', '/admin/accounts/p/openai', 'openai')
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledTimes(2)
    expect(listAccounts).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ platform: 'openai' }),
      expect.anything()
    )
    // 路由驱动的变化不应再触发 router.replace（防回环）
    expect(ctx.replace).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('向 CreateAccountModal 传递当前平台作为 defaultPlatform', async () => {
    setRoute('AdminAccountsPlatform', '/admin/accounts/p/gemini', 'gemini')
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.getComponent(CreateAccountModal).props('defaultPlatform')).toBe('gemini')

    setRoute('AdminAccounts', '/admin/accounts')
    await nextTick()
    expect(wrapper.getComponent(CreateAccountModal).props('defaultPlatform')).toBe('')
    wrapper.unmount()
  })

  it('过滤器选择平台时同步 URL 到平台路由且不回环', async () => {
    const wrapper = mountView()
    await flushPromises()
    const callsBefore = listAccounts.mock.calls.length

    await emitPlatformFilter(wrapper, 'kimi')

    expect(ctx.replace).toHaveBeenCalledTimes(1)
    expect(ctx.replace).toHaveBeenCalledWith('/admin/accounts/p/kimi')
    // 只发 update:filters（不发 change）→ 不触发防抖重载；watch 侧判等短路，
    // 因此不应产生额外的列表请求
    expect(listAccounts.mock.calls.length).toBe(callsBefore)
    wrapper.unmount()
  })

  it('清空平台过滤时回退到全部账号 URL', async () => {
    setRoute('AdminAccountsPlatform', '/admin/accounts/p/kimi', 'kimi')
    const wrapper = mountView()
    await flushPromises()

    await emitPlatformFilter(wrapper, '')

    expect(ctx.replace).toHaveBeenCalledTimes(1)
    expect(ctx.replace).toHaveBeenCalledWith('/admin/accounts')
    wrapper.unmount()
  })

  it('离开账号页时不触发平台过滤重置与多余请求', async () => {
    setRoute('AdminAccountsPlatform', '/admin/accounts/p/openai', 'openai')
    const wrapper = mountView()
    await flushPromises()
    const callsBefore = listAccounts.mock.calls.length

    setRoute('AdminUsers', '/admin/users')
    await flushPromises()

    expect(listAccounts.mock.calls.length).toBe(callsBefore)
    expect(ctx.replace).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
