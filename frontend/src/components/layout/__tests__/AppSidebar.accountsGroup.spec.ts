import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'

import AppSidebar from '../AppSidebar.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

// hoisted 共享状态：vue-router 与 stores 的 mock 工厂在模块加载期执行，
// 需要通过 holder 把可变对象交给测试用例。
const ctx = vi.hoisted(() => ({
  route: null as { name: string; path: string; params: Record<string, string> } | null,
  push: vi.fn(),
  adminSettings: null as Record<string, unknown> | null,
}))

vi.mock('vue-router', () => ({
  useRoute: () => ctx.route,
  useRouter: () => ({ push: ctx.push }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

// AppSidebar 从 stores barrel 取 store；featureFlags.ts 直接从 '@/stores/app' 取
// useAppStore，因此两个模块都要 mock 到同一个 fake 上。
const appStoreFake = vi.hoisted(() => ({
  sidebarCollapsed: false,
  mobileOpen: false,
  backendModeEnabled: false,
  siteName: 'Sub2API',
  siteLogo: '',
  siteVersion: 'v1.0.0',
  publicSettingsLoaded: false,
  cachedPublicSettings: null,
  sidebarScrollTop: 0,
  toggleSidebar: vi.fn(),
  setMobileOpen: vi.fn(),
}))

vi.mock('@/stores', async () => {
  const { reactive } = await vi.importActual<typeof import('vue')>('vue')
  // adminSettings fake 用 reactive 包装：AppSidebar 的 computed 依赖
  // accountMenuHiddenPlatforms，黑名单变更后菜单需要即时重算。
  ctx.adminSettings = reactive({
    loaded: false,
    loading: false,
    customMenuItems: [],
    accountMenuHiddenPlatforms: [] as string[],
    accountMenuDefaultPlatform: '',
    opsMonitoringEnabled: true,
    opsRealtimeMonitoringEnabled: true,
    opsQueryModeDefault: 'auto',
    fetch: vi.fn(),
  })
  return {
    useAppStore: () => appStoreFake,
    useAuthStore: () => ({ isAdmin: true, isSimpleMode: false }),
    useOnboardingStore: () => ({ isCurrentStep: vi.fn(() => false), nextStep: vi.fn() }),
    useAdminSettingsStore: () => ctx.adminSettings,
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStoreFake,
}))

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: { value: false }, refreshBatchImageAccess: vi.fn() }),
}))

function mountSidebar() {
  return mount(AppSidebar, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        VersionBadge: true,
        Teleport: true,
      },
    },
  })
}

/** 账号管理分组子项的跳转目标（/admin/accounts 前缀的 router-link）。 */
function accountGroupChildPaths(wrapper: ReturnType<typeof mountSidebar>): string[] {
  return wrapper
    .findAllComponents(RouterLinkStub)
    .map((link) => link.props('to') as string)
    .filter((to) => to === '/admin/accounts' || to.startsWith('/admin/accounts/p/'))
}

describe('AppSidebar 账号管理分组', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    appStoreFake.sidebarCollapsed = false
    ctx.route = { name: 'AdminAccounts', path: '/admin/accounts', params: {} }
    ctx.adminSettings!.accountMenuHiddenPlatforms = []
    ctx.adminSettings!.accountMenuDefaultPlatform = ''
  })

  it('父项渲染为分组按钮并保留 tour 锚点 #sidebar-channel-manage', () => {
    const wrapper = mountSidebar()
    const anchor = wrapper.find('#sidebar-channel-manage')
    expect(anchor.exists()).toBe(true)
    expect(anchor.text()).toContain('nav.accounts')
    wrapper.unmount()
  })

  it('子项 = 平台清单 +「全部账号」置末（未设默认平台时乐观全显）', async () => {
    const wrapper = mountSidebar()
    await flushPromises()

    const paths = accountGroupChildPaths(wrapper)
    // 当前路由命中「全部账号」子项 → 分组自动展开，children 可见
    expect(paths).toEqual([
      ...CONCRETE_PLATFORM_OPTIONS.map((option) => `/admin/accounts/p/${option.value}`),
      '/admin/accounts',
    ])

    const labels = wrapper.findAllComponents(RouterLinkStub).map((link) => link.text())
    expect(labels).toContain('nav.accountsAll')
    for (const option of CONCRETE_PLATFORM_OPTIONS) {
      expect(labels).toContain(option.label)
    }
    wrapper.unmount()
  })

  it('默认平台：父项跳平台路径，子项末尾仍为全部账号', async () => {
    const def = CONCRETE_PLATFORM_OPTIONS[0].value
    ctx.route = { name: 'AdminUsers', path: '/admin/users', params: {} }
    const wrapper = mountSidebar()
    await flushPromises()

    ctx.adminSettings!.accountMenuDefaultPlatform = def
    await flushPromises()

    await wrapper.find('#sidebar-channel-manage').trigger('click')
    expect(ctx.push).toHaveBeenCalledWith(`/admin/accounts/p/${def}`)

    const paths = accountGroupChildPaths(wrapper)
    expect(paths[paths.length - 1]).toBe('/admin/accounts')
    expect(paths).toContain(`/admin/accounts/p/${def}`)
    wrapper.unmount()
  })

  it('默认平台即使被黑名单隐藏，其子项入口仍强制保留', async () => {
    const def = CONCRETE_PLATFORM_OPTIONS[0].value
    const hiddenOther = CONCRETE_PLATFORM_OPTIONS[1].value
    const wrapper = mountSidebar()
    await flushPromises()

    ctx.adminSettings!.accountMenuDefaultPlatform = def
    ctx.adminSettings!.accountMenuHiddenPlatforms = [def, hiddenOther]
    await flushPromises()

    const paths = accountGroupChildPaths(wrapper)
    expect(paths).toContain(`/admin/accounts/p/${def}`)
    expect(paths).not.toContain(`/admin/accounts/p/${hiddenOther}`)
    wrapper.unmount()
  })

  it('黑名单过滤：隐藏的平台不出现在子项中', async () => {
    const hidden = [CONCRETE_PLATFORM_OPTIONS[0].value, CONCRETE_PLATFORM_OPTIONS[1].value]
    const wrapper = mountSidebar()
    await flushPromises()

    ctx.adminSettings!.accountMenuHiddenPlatforms = [...hidden]
    await flushPromises()

    const paths = accountGroupChildPaths(wrapper)
    expect(paths).toHaveLength(1 + CONCRETE_PLATFORM_OPTIONS.length - hidden.length)
    expect(paths).not.toContain(`/admin/accounts/p/${hidden[0]}`)
    expect(paths).not.toContain(`/admin/accounts/p/${hidden[1]}`)
    expect(paths).toContain('/admin/accounts')
    wrapper.unmount()
  })

  it('点击父项：跳转父路径并展开分组', async () => {
    ctx.route = { name: 'AdminUsers', path: '/admin/users', params: {} }
    const wrapper = mountSidebar()
    await flushPromises()

    // 未命中子项 → 分组默认收起
    expect(accountGroupChildPaths(wrapper)).toHaveLength(0)

    await wrapper.find('#sidebar-channel-manage').trigger('click')
    expect(ctx.push).toHaveBeenCalledWith('/admin/accounts')
    expect(accountGroupChildPaths(wrapper).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('侧边栏收起时点击父项仍跳转父路径', async () => {
    appStoreFake.sidebarCollapsed = true
    ctx.route = { name: 'AdminUsers', path: '/admin/users', params: {} }
    const wrapper = mountSidebar()
    await flushPromises()

    await wrapper.find('#sidebar-channel-manage').trigger('click')
    expect(ctx.push).toHaveBeenCalledWith('/admin/accounts')
    wrapper.unmount()
  })
})
