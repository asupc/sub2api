import { describe, expect, it } from 'vitest'

import zhAdminAccounts from '../locales/zh/admin/accounts'
import zhAdminChannels from '../locales/zh/admin/channels'
import zhAdminOps from '../locales/zh/admin/ops'
import zhAdminOverview from '../locales/zh/admin/overview'
import zhAdminResources from '../locales/zh/admin/resources'
import zhAdminSettings from '../locales/zh/admin/settings'
import zhCommon from '../locales/zh/common'
import zhDashboard from '../locales/zh/dashboard'
import zhLanding from '../locales/zh/landing'
import zhMisc from '../locales/zh/misc'

// locales/{zh,en}/index.ts 与 admin/index.ts 使用对象展开聚合各域模块，
// 展开模块之间若出现同名顶层键会静默覆盖。本测试将该风险固化为显式失败。
type Modules = Record<string, Record<string, unknown>>

function collisions(modules: Modules): string[] {
  const seen = new Map<string, string>()
  const out: string[] = []
  for (const [name, mod] of Object.entries(modules)) {
    for (const key of Object.keys(mod)) {
      const prev = seen.get(key)
      if (prev) {
        out.push(`"${key}" in both ${prev} and ${name}`)
      } else {
        seen.set(key, name)
      }
    }
  }
  return out
}

const roots: Record<string, Modules> = {
  zh: { landing: zhLanding, common: zhCommon, dashboard: zhDashboard, misc: zhMisc }
}

const admins: Record<string, Modules> = {
  zh: {
    overview: zhAdminOverview,
    channels: zhAdminChannels,
    accounts: zhAdminAccounts,
    resources: zhAdminResources,
    ops: zhAdminOps,
    settings: zhAdminSettings
  }
}

describe.each(Object.keys(roots))('locale %s spread assembly', (locale) => {
  it('root modules have no overlapping top-level keys', () => {
    expect(collisions(roots[locale])).toEqual([])
  })

  it('root modules do not shadow the explicit "admin" namespace', () => {
    for (const [name, mod] of Object.entries(roots[locale])) {
      expect(Object.keys(mod), `module ${name} must not define "admin"`).not.toContain('admin')
    }
  })

  it('admin modules have no overlapping top-level keys', () => {
    expect(collisions(admins[locale])).toEqual([])
  })
})
