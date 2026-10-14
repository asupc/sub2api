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

// 带占位符的 title 文案模板（断言插值内容用）；其余 key 原样返回。
const ZH_TEMPLATES: Record<string, string> = {
  'admin.ops.quota.checkinOkTitle': '签到成功：{time}{note}',
  'admin.ops.quota.checkinFailedTitle': '签到失败：{error}（{time}）'
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      const tpl = ZH_TEMPLATES[key] ?? key
      return params ? tpl.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? '')) : tpl
    }
  })
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

  describe('签到状态胶囊', () => {
    // 时间均相对当前时刻构造，任何时区/任何时刻运行都稳定：
    // 「今天」的判定基准（now）与相对偏移（-2d）不可能同日。
    const iso = (offsetMs: number) => new Date(Date.now() + offsetMs).toISOString()
    const TWO_DAYS = 2 * 24 * 3600 * 1000

    it('今日签到成功：绿色「已签到」，title 含 note', () => {
      const wrapper = mount(PlatformOpsQuotaCell, {
        props: {
          account: makeAccount('workbuddy', {
            ops_state: { checkin: { last_ok: iso(0), last_run: iso(0), note: '签到成功 +100' } }
          })
        }
      })
      expect(wrapper.text()).toContain('admin.ops.quota.checkinOk')
      expect(wrapper.text()).not.toContain('admin.ops.quota.checkinFailed')
      const badge = wrapper.get('span[title*="签到成功："]')
      expect(badge.attributes('title')).toContain('签到成功 +100')
      wrapper.unmount()
    })

    it('今日跑过但失败：红色「签到失败」，title 含错误信息', () => {
      const wrapper = mount(PlatformOpsQuotaCell, {
        props: {
          account: makeAccount('qoder', {
            ops_state: { checkin: { last_run: iso(0), last_ok: iso(-TWO_DAYS), last_err: 'upstream 500' } }
          })
        }
      })
      expect(wrapper.text()).toContain('admin.ops.quota.checkinFailed')
      expect(wrapper.text()).not.toContain('admin.ops.quota.checkinOk')
      const badge = wrapper.get('span[title*="签到失败："]')
      expect(badge.attributes('title')).toContain('upstream 500')
      wrapper.unmount()
    })

    it('今日未跑 / 从未跑过：灰色「未签到」', () => {
      const stale = mount(PlatformOpsQuotaCell, {
        props: {
          account: makeAccount('qoder', {
            ops_state: { checkin: { last_run: iso(-TWO_DAYS), last_ok: iso(-TWO_DAYS) } }
          })
        }
      })
      expect(stale.text()).toContain('admin.ops.quota.checkinPending')
      stale.unmount()

      const never = mount(PlatformOpsQuotaCell, { props: { account: makeAccount('qoder') } })
      expect(never.text()).toContain('admin.ops.quota.checkinPending')
      never.unmount()
    })

    it('trae 账号即使有签到状态也不渲染胶囊', () => {
      const wrapper = mount(PlatformOpsQuotaCell, {
        props: {
          account: makeAccount('trae', {
            ops_state: { checkin: { last_ok: iso(0), last_run: iso(0) } }
          })
        }
      })
      expect(wrapper.text()).not.toContain('admin.ops.quota.checkinOk')
      expect(wrapper.text()).not.toContain('admin.ops.quota.checkinPending')
      wrapper.unmount()
    })
  })
})
