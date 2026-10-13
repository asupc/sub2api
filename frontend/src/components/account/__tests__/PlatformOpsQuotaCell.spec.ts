import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PlatformOpsQuotaCell from '../PlatformOpsQuotaCell.vue'
import type { Account } from '@/types'

const { workbuddyQueryQuota, qoderQueryQuota, traeQueryQuota } = vi.hoisted(() => ({
  workbuddyQueryQuota: vi.fn(),
  qoderQueryQuota: vi.fn(),
  traeQueryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    platformOps: { workbuddyQueryQuota, qoderQueryQuota, traeQueryQuota }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

function makeAccount(platform: string, extra: Record<string, unknown> = {}): Account {
  return {
    id: 7,
    name: 'acct',
    platform,
    type: 'oauth',
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    extra
  } as Account
}

describe('PlatformOpsQuotaCell', () => {
  beforeEach(() => {
    workbuddyQueryQuota.mockReset()
    qoderQueryQuota.mockReset()
    traeQueryQuota.mockReset()
  })

  it('WorkBuddy：展示余额与最早到期时间', () => {
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: {
        account: makeAccount('workbuddy', {
          wb_quota: { remain: 100, total: 500, expiring_at: '2026-10-15T08:00:00Z', updated_at: '2026-10-12T00:00:00Z' }
        })
      }
    })
    expect(wrapper.text()).toContain('100/500')
    expect(wrapper.text()).toContain('10-15')
    wrapper.unmount()
  })

  it('Qoder：展示积分与套餐徽标', () => {
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: {
        account: makeAccount('qoder', {
          qd_quota: { remain: 40, used: 60, total: 100, plan: 'Pro', updated_at: '2026-10-12T00:00:00Z' }
        })
      }
    })
    expect(wrapper.text()).toContain('40/100')
    expect(wrapper.text()).toContain('Pro')
    wrapper.unmount()
  })

  it('Trae：展示积分与临期告警徽标', () => {
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: {
        account: makeAccount('trae', {
          trae_quota: { total: 88.5, has_expire: true, expiring_soon: true }
        })
      }
    })
    expect(wrapper.text()).toContain('88.5')
    expect(wrapper.text()).toContain('admin.ops.quota.expiringSoon')
    wrapper.unmount()
  })

  it('无快照时展示占位符，手动查询后用响应即时更新', async () => {
    qoderQueryQuota.mockResolvedValue({ remain: 9, used: 1, total: 10, updated_at: 'now' })
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: { account: makeAccount('qoder') }
    })
    expect(wrapper.text()).toContain('-')

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(qoderQueryQuota).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('9/10')
    expect(wrapper.emitted('refreshed')![0][0]).toMatchObject({ id: 7 })
    wrapper.unmount()
  })

  it('Trae 查询走 trae 端点', async () => {
    traeQueryQuota.mockResolvedValue({ total: 12 })
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: { account: makeAccount('trae') }
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(traeQueryQuota).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('12')
    wrapper.unmount()
  })

  it('今日积分消耗：todayCredits 有值时渲染胶囊，整数直接显示', () => {
    const wrapper = mount(PlatformOpsQuotaCell, {
      props: {
        account: makeAccount('workbuddy', {
          wb_quota: { remain: 100, total: 500, updated_at: '2026-10-13T00:00:00Z' }
        }),
        todayCredits: 42
      }
    })
    expect(wrapper.text()).toContain('admin.ops.quota.todayCredits 42')
    wrapper.unmount()
  })

  it('今日积分消耗：小数最多 2 位（去尾零）；null 不渲染胶囊', () => {
    const qoderAccount = makeAccount('qoder', {
      qd_quota: { remain: 40, used: 60, total: 100, updated_at: '2026-10-13T00:00:00Z' }
    })
    // 单次消耗常在 0.1 以下，必须保留 2 位而不是 1 位。
    const small = mount(PlatformOpsQuotaCell, { props: { account: qoderAccount, todayCredits: 0.27 } })
    expect(small.text()).toContain('admin.ops.quota.todayCredits 0.27')
    small.unmount()

    const trailingZero = mount(PlatformOpsQuotaCell, { props: { account: qoderAccount, todayCredits: 3.5 } })
    expect(trailingZero.text()).toContain('admin.ops.quota.todayCredits 3.5')
    trailingZero.unmount()

    const withoutCredits = mount(PlatformOpsQuotaCell, {
      props: { account: qoderAccount, todayCredits: null }
    })
    expect(withoutCredits.text()).not.toContain('admin.ops.quota.todayCredits')
    withoutCredits.unmount()
  })
})
