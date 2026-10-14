import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'

const { getAccountTasks } = vi.hoisted(() => ({
  getAccountTasks: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    platformOps: { getAccountTasks }
  }
}))

const { showSuccess, showError } = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'test-account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 3,
    priority: 50,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

const anchorRect = new DOMRect(100, 100, 24, 24)

// AccountActionMenu 使用 <Teleport to="body">，内容渲染在 document.body。
const getBodyText = () => document.body.textContent ?? ''
const getBodyButton = (testId: string) =>
  document.body.querySelector<HTMLButtonElement>(`[data-testid="${testId}"]`)

describe('AccountActionMenu — 三平台运营任务子菜单', () => {
  beforeEach(() => {
    getAccountTasks.mockReset()
    document.body.innerHTML = ''
  })

  it.each(['workbuddy', 'qoder', 'trae'] as const)('%s 账号显示「运营任务」入口', (platform) => {
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ platform }), anchorRect },
      attachTo: document.body
    })
    expect(getBodyButton('ops-tasks-entry')).toBeTruthy()
    wrapper.unmount()
  })

  it('其他平台不显示「运营任务」入口', () => {
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ platform: 'grok' }), anchorRect },
      attachTo: document.body
    })
    expect(getBodyButton('ops-tasks-entry')).toBeNull()
    wrapper.unmount()
  })

  it('点击入口切换到运营任务视图并加载任务清单', async () => {
    getAccountTasks.mockResolvedValue({
      platform: 'qoder',
      tasks: [
        { id: 'checkin', platform: 'qoder', name: '每日签到', state: { last_ok: '2026-10-12T01:00:00Z' } },
        { id: 'balance', platform: 'qoder', name: '积分余额刷新', state: { last_err: 'quota endpoint 503', last_run: '2026-10-12T02:00:00Z' } }
      ]
    })
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ platform: 'qoder' }), anchorRect },
      attachTo: document.body
    })

    getBodyButton('ops-tasks-entry')!.click()
    await flushPromises()

    expect(getAccountTasks).toHaveBeenCalledWith(1)
    expect(getBodyText()).toContain('每日签到')
    expect(getBodyText()).toContain('积分余额刷新')
    expect(getBodyText()).toContain('admin.ops.tasks.failed')
    expect(document.body.querySelector('[data-testid="ops-task-checkin"]')).toBeTruthy()
    wrapper.unmount()
  })

  it('关闭再打开菜单回到主视图', async () => {
    getAccountTasks.mockResolvedValue({ platform: 'trae', tasks: [] })
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ platform: 'trae' }), anchorRect },
      attachTo: document.body
    })
    getBodyButton('ops-tasks-entry')!.click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="platform-ops-panel"]')).toBeTruthy()

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(document.body.querySelector('[data-testid="platform-ops-panel"]')).toBeNull()
    expect(getBodyButton('ops-tasks-entry')).toBeTruthy()
    wrapper.unmount()
  })
})

describe('PlatformOpsTasksPanel 调度设置', () => {
  it('切到调度设置视图展示默认调度，保存提交账号级覆盖', async () => {
    // 本用例局部扩 mock：配置读写两个方法。
    const { adminAPI } = await import('@/api/admin')
    const platformOps = adminAPI.platformOps as unknown as Record<string, ReturnType<typeof vi.fn>>
    platformOps.getPlatformTaskConfig = vi.fn().mockResolvedValue({
      platform: 'workbuddy',
      tasks: [
        { id: 'checkin', name: '每日签到', hours: [9, 21], config: {} },
        { id: 'balance', name: '余额刷新', interval_seconds: 21600, config: { interval_hours: 12 } },
        { id: 'growth', name: '成长任务', hours: [1], config: { enabled: false } }
      ]
    })
    platformOps.updatePlatformTaskConfig = vi.fn().mockResolvedValue({ saved: true })

    const wrapper = mount(AccountActionMenu, {
      props: {
        show: true,
        account: makeAccount({ id: 7, platform: 'workbuddy' }),
        anchorRect: new DOMRect(10, 10, 100, 100)
      },
      attachTo: document.body
    })

    // 进入运营任务视图 → 切调度设置（Teleport 到 body，用原生 DOM 操作）。
    getBodyButton('ops-tasks-entry')!.click()
    await flushPromises()
    getBodyButton('ops-config-tab')!.click()
    await flushPromises()

    // 默认与覆盖值渲染：checkin 预填默认 9,21；balance 预填覆盖 12；growth 关闭。
    const getBodyInput = (testId: string) =>
      document.body.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`)!
    expect(getBodyInput('ops-config-hours-checkin').value).toBe('9,21')
    expect(getBodyInput('ops-config-interval-balance').value).toBe('12')
    expect(getBodyInput('ops-config-enabled-growth').checked).toBe(false)

    // 修改 checkin 时刻并保存：提交账号级覆盖（显式 hours + enabled）。
    const checkinInput = getBodyInput('ops-config-hours-checkin')
    checkinInput.value = '8, 20，22'
    checkinInput.dispatchEvent(new Event('input'))
    await flushPromises()
    getBodyButton('ops-config-save')!.click()
    await flushPromises()

    expect(platformOps.updatePlatformTaskConfig).toHaveBeenCalledWith('workbuddy', {
      checkin: { enabled: true, hours: [8, 20, 22] },
      balance: { enabled: true, interval_hours: 12 },
      // growth 未改时刻：保存时按预填的默认时刻显式化提交。
      growth: { enabled: false, hours: [1] }
    })
    expect(showSuccess).toHaveBeenCalled()

    wrapper.unmount()
  })
})

describe('PlatformOpsTasksPanel 手动执行结论透传', () => {
  it('后端返回 note 时 toast 使用 runSuccessNote 并展示结论；无 note 沿用 runSuccess', async () => {
    const { adminAPI } = await import('@/api/admin')
    const platformOps = adminAPI.platformOps as unknown as Record<string, ReturnType<typeof vi.fn>>
    platformOps.runAccountTask = vi
      .fn()
      .mockResolvedValueOnce({ done: true, note: '活动领取成功 +100 Credits（每日领取）' })
      .mockResolvedValueOnce({ done: true })
    getAccountTasks.mockResolvedValue({
      platform: 'qoder',
      tasks: [{ id: 'checkin', platform: 'qoder', name: '每日签到', state: {} }]
    })
    showSuccess.mockClear()

    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ id: 3, platform: 'qoder' }), anchorRect },
      attachTo: document.body
    })
    getBodyButton('ops-tasks-entry')!.click()
    await flushPromises()

    // 有 note：toast 走 note 分支（mock 的 t 仅回传 key，故断言 key）。
    getBodyButton('ops-task-checkin')!.click()
    await flushPromises()
    await flushPromises()
    expect(showSuccess).toHaveBeenCalledWith('admin.ops.tasks.runSuccessNote')

    // 无 note：回退旧文案。
    getBodyButton('ops-task-checkin')!.click()
    await flushPromises()
    await flushPromises()
    expect(showSuccess).toHaveBeenLastCalledWith('admin.ops.tasks.runSuccess')

    wrapper.unmount()
  })

  it('状态徽标 hover 标题包含成功结论 note', async () => {
    getAccountTasks.mockResolvedValue({
      platform: 'qoder',
      tasks: [
        {
          id: 'checkin',
          platform: 'qoder',
          name: '每日签到',
          state: { last_ok: '2026-10-14T10:00:00Z', note: '今日活动奖励已领取（每日领取）' }
        }
      ]
    })
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ id: 4, platform: 'qoder' }), anchorRect },
      attachTo: document.body
    })
    getBodyButton('ops-tasks-entry')!.click()
    await flushPromises()

    const title = document.body
      .querySelector('[data-testid="ops-task-checkin"] span[title]')!
      .getAttribute('title')
    expect(title).toContain('今日活动奖励已领取（每日领取）')

    wrapper.unmount()
  })
})
