import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpenAIQuotaResetCell from '../OpenAIQuotaResetCell.vue'
import type { Account } from '@/types'
import { refreshOpenAIQuota, resetOpenAIQuota } from '@/api/admin/accounts'

vi.mock('@/api/admin/accounts', () => ({
  refreshOpenAIQuota: vi.fn(),
  resetOpenAIQuota: vi.fn(),
  refreshOpenAIReferrals: vi.fn(),
  sendOpenAIReferralInvite: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params?.time ? `${key}:${params.time}` : params?.count ? `${key}:${params.count}` : key,
    }),
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'acc',
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
    ...overrides,
  }
}

describe('OpenAIQuotaResetCell — Popover 模式', () => {
  beforeEach(() => {
    vi.mocked(refreshOpenAIQuota).mockReset()
    vi.mocked(resetOpenAIQuota).mockReset()
  })

  it('popover 模式下渲染第 3 行四胶囊操作条（⚡ 重置 / 🪙 点数 / 🎁 邀请 / ⏳ 失效）', () => {
    const wrapper = mount(OpenAIQuotaResetCell, {
      props: {
        account: makeAccount({
          extra: {
            codex_reset_credit_snapshot: {
              available_count: 2,
              credits: [
                { expires_at: '2099-01-01T00:00:00Z' },
                { expires_at: '2099-01-02T00:00:00Z' }
              ]
            },
            codex_referral_snapshot: {
              available_invites: 3
            }
          }
        }),
        popover: true
      }
    })

    // ⚡ 重置次数胶囊（popover 触发器），徽章显示快照中的剩余次数 2
    const capsuleBtn = wrapper.find('[data-testid="openai-quota-capsule-btn"]')
    expect(capsuleBtn.exists()).toBe(true)
    expect(capsuleBtn.text()).toContain('⚡')
    expect(capsuleBtn.text()).toContain('admin.accounts.openaiQuotaReset.reset')
    expect(capsuleBtn.text()).toContain('2')

    // 🪙 点数胶囊（无快照时显示占位 —）
    expect(wrapper.find('[data-testid="codex-credits-capsule"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('🪙')

    // 🎁 邀请胶囊，徽章显示可邀请数 3
    expect(wrapper.text()).toContain('🎁')
    expect(wrapper.text()).toContain('admin.accounts.openaiReferral.inviteShort')
    expect(wrapper.text()).toContain('3')

    // ⏳ 最早一张重置次数的失效时间胶囊
    expect(wrapper.text()).toContain('⏳')

    // 操作条容器与用量双行宽度平齐（flex 摊开布局）
    expect(capsuleBtn.element.parentElement?.className).toContain('justify-between')
    wrapper.unmount()
  })

  it('点击胶囊按钮展开 Teleport 浮层，支持在浮层内点击查询与操作', async () => {
    const wrapper = mount(OpenAIQuotaResetCell, {
      props: {
        account: makeAccount({
          extra: {
            codex_credits_snapshot: {
              credits: { has_credits: true, unlimited: false, balance: '50' },
              fetched_at: 1770000000
            }
          }
        }),
        popover: true
      },
      attachTo: document.body
    })

    const capsuleBtn = wrapper.get('[data-testid="openai-quota-capsule-btn"]')
    expect(document.body.querySelector('.teleport-popover, [data-testid="codex-credits"]')).toBeNull()

    // 触发点击展开
    await capsuleBtn.trigger('click')
    await flushPromises()

    // 此时 Teleport 挂载到 body 上
    const creditsBtn = document.body.querySelector('[data-testid="codex-credits"]')
    expect(creditsBtn).not.toBeNull()
    expect(creditsBtn?.textContent).toContain('50')

    // 按 Esc 键关闭浮层
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(document.body.querySelector('[data-testid="codex-credits"]')).toBeNull()
    wrapper.unmount()
  })
})
