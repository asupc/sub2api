import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DeviceAuthFlow from '../DeviceAuthFlow.vue'

const { copyToClipboard } = vi.hoisted(() => ({
  copyToClipboard: vi.fn()
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const POLL_INTERVAL_MS = 3000

function mountFlow(overrides: { needsCallback?: boolean; poll?: ReturnType<typeof vi.fn>; start?: ReturnType<typeof vi.fn> } = {}) {
  const start = overrides.start ?? vi.fn().mockResolvedValue({ url: 'https://auth.example/login', state: 'state-1' })
  const poll = overrides.poll ?? vi.fn().mockResolvedValue({ done: false })
  const wrapper = mount(DeviceAuthFlow, {
    props: {
      title: 'Device Auth',
      description: 'desc',
      start,
      poll,
      needsCallback: overrides.needsCallback ?? false,
      pollIntervalMs: POLL_INTERVAL_MS
    }
  })
  return { wrapper, start, poll }
}

const clickStart = async (wrapper: ReturnType<typeof mount>) => {
  await wrapper.get('button').trigger('click')
  await vi.advanceTimersByTimeAsync(0)
}

describe('DeviceAuthFlow', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('发起授权后展示授权 URL 并按间隔轮询，完成后 emit success', async () => {
    const poll = vi.fn().mockResolvedValue({ done: false })
    const { wrapper } = mountFlow({ poll })
    await clickStart(wrapper)

    expect((wrapper.find('input[readonly]').element as HTMLInputElement).value).toBe('https://auth.example/login')
    expect(poll).not.toHaveBeenCalled()

    // 第一次轮询：未完成
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)
    expect(poll).toHaveBeenCalledWith('state-1', undefined)

    // 第二次轮询：完成，带凭证
    poll.mockResolvedValue({ done: true, credentials: { access_token: 'at' } })
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS)

    const events = wrapper.emitted('success')
    expect(events).toHaveLength(1)
    expect(events![0][0]).toEqual({ state: 'state-1', credentials: { access_token: 'at' }, callbackQuery: undefined })
    expect(wrapper.text()).toContain('admin.accounts.oauth.deviceFlow.success')
    wrapper.unmount()
  })

  it('倒计时归零后进入超时态并停止轮询', async () => {
    const poll = vi.fn().mockResolvedValue({ done: false })
    const { wrapper } = mountFlow({ poll })
    await clickStart(wrapper)

    await vi.advanceTimersByTimeAsync(900 * 1000)

    expect(wrapper.text()).toContain('admin.accounts.oauth.deviceFlow.timeout')
    const callsAfterTimeout = poll.mock.calls.length
    vi.advanceTimersByTime(POLL_INTERVAL_MS * 2)
    expect(poll.mock.calls.length).toBe(callsAfterTimeout)
    wrapper.unmount()
  })

  it('needsCallback 模式：粘贴完整回跳 URL，提取查询串触发一次带 callback_query 的交换', async () => {
    const poll = vi.fn().mockResolvedValue({ done: false })
    const { wrapper } = mountFlow({ needsCallback: true, poll })
    await clickStart(wrapper)

    const callbackUrl = 'http://127.0.0.1:17388/callback?authCodeInfo=%7B%22AuthCode%22%3A%22x%22%7D&host=cn'
    await wrapper.find('textarea').setValue(callbackUrl)

    const completeButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('deviceFlow.completeAuth'))!
    poll.mockResolvedValueOnce({ done: true, credentials: { jwt: 'j' } })
    await completeButton.trigger('click')
    await vi.advanceTimersByTimeAsync(0)

    // 交换调用的第二个参数是 ? 之后的查询串
    expect(poll).toHaveBeenCalledWith('state-1', 'authCodeInfo=%7B%22AuthCode%22%3A%22x%22%7D&host=cn')
    const events = wrapper.emitted('success')
    expect(events).toHaveLength(1)
    expect(events![0][0]).toMatchObject({
      state: 'state-1',
      callbackQuery: 'authCodeInfo=%7B%22AuthCode%22%3A%22x%22%7D&host=cn'
    })
    wrapper.unmount()
  })

  it('发起失败进入错误态，可重新开始', async () => {
    const start = vi.fn().mockRejectedValue({ message: 'boom' })
    const { wrapper } = mountFlow({ start })
    await clickStart(wrapper)

    expect(wrapper.text()).toContain('boom')

    // 重新开始
    start.mockResolvedValue({ url: 'https://auth.example/2', state: 'state-2' })
    const restartButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('deviceFlow.restart'))!
    await restartButton.trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(wrapper.text()).toContain('admin.accounts.oauth.deviceFlow.start')
    wrapper.unmount()
  })
})
