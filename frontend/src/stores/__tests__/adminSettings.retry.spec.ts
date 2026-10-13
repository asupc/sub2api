// 二开适配版：支付功能已移除（adminAPI.payment 不存在），仅保留 settings 请求的
// 失败重试与缓存语义测试（原上游测试还覆盖 payment.getConfig 请求）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminSettingsStore } from '../adminSettings'

const mocks = vi.hoisted(() => ({ getSettings: vi.fn() }))
vi.mock('@/api', () => ({ adminAPI: { settings: { getSettings: mocks.getSettings } } }))
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  setActivePinia(createPinia())
  mocks.getSettings.mockResolvedValue({ ops_monitoring_enabled: true, custom_menu_items: [{ id: 'custom', title: 'Custom' }] })
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { vi.restoreAllMocks(); localStorage.clear() })

describe('admin settings fetch retry', () => {
  it('retries a failed initial settings request without forcing a refresh', async () => {
    mocks.getSettings.mockRejectedValueOnce(new Error('Temporarily unavailable'))
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.loaded).toBe(false)
    expect(store.loading).toBe(false)
    await store.fetch()
    expect(mocks.getSettings).toHaveBeenCalledTimes(2)
    expect(store.loaded).toBe(true)
    expect(store.customMenuItems).toEqual([{ id: 'custom', title: 'Custom' }])
  })

  it('keeps cached values visible when the initial request fails', async () => {
    localStorage.setItem('ops_monitoring_enabled_cached', 'false')
    mocks.getSettings.mockRejectedValueOnce(new Error('Offline'))
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.opsMonitoringEnabled).toBe(false)
    expect(localStorage.getItem('ops_monitoring_enabled_cached')).toBe('false')
  })

  it('still reuses successfully loaded settings', async () => {
    const store = useAdminSettingsStore()
    await store.fetch()
    await store.fetch()
    expect(mocks.getSettings).toHaveBeenCalledTimes(1)
  })
})
