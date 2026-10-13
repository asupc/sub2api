import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WorkBuddyForm from '../WorkBuddyForm.vue'
import QoderForm from '../QoderForm.vue'
import TraeForm from '../TraeForm.vue'
import DeviceAuthFlow from '../../DeviceAuthFlow.vue'
import type { Account } from '@/types'

const { workbuddyCreateFromOAuth, qoderImportPAT, traeCreateFromOAuth, accountsCreate, showSuccess, showError } =
  vi.hoisted(() => ({
    workbuddyCreateFromOAuth: vi.fn(),
    qoderImportPAT: vi.fn(),
    traeCreateFromOAuth: vi.fn(),
    accountsCreate: vi.fn(),
    showSuccess: vi.fn(),
    showError: vi.fn()
  }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    platformOps: { workbuddyCreateFromOAuth, qoderImportPAT, traeCreateFromOAuth },
    accounts: { create: accountsCreate }
  }
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

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() })
}))

const baseAccount = { id: 1, name: 'a', platform: 'qoder', type: 'oauth' } as Account

describe('三平台建号表单', () => {
  beforeEach(() => {
    workbuddyCreateFromOAuth.mockReset().mockResolvedValue(baseAccount)
    qoderImportPAT.mockReset().mockResolvedValue({ done: true, credentials: { access_token: 'drt-x' } })
    traeCreateFromOAuth.mockReset().mockResolvedValue(baseAccount)
    accountsCreate.mockReset().mockResolvedValue(baseAccount)
    showSuccess.mockReset()
    showError.mockReset()
  })

  it('QoderForm：PAT 导入后走通用建号 API（platform=qoder, type=oauth），昵称兜底账号名', async () => {
    const wrapper = mount(QoderForm, {
      props: { name: '', notes: null, groupIds: [3, 5] }
    })

    // 切到 PAT 导入
    const patRadio = wrapper.findAll('input[type="radio"]').find(r => (r.element as HTMLInputElement).value === 'pat')!
    await patRadio.setValue()
    await wrapper.find('input[type="password"]').setValue('pt-abc123')

    const importButton = wrapper
      .findAll('button')
      .find(b => b.text().includes('qoder.exchange'))!
    await importButton.trigger('click')
    await flushPromises()

    expect(qoderImportPAT).toHaveBeenCalledWith('pt-abc123', 'cn')

    const createButton = wrapper
      .findAll('button')
      .find(b => b.text().includes('qoder.create'))!
    await createButton.trigger('click')
    await flushPromises()

    expect(accountsCreate).toHaveBeenCalledTimes(1)
    const payload = accountsCreate.mock.calls[0][0]
    expect(payload.platform).toBe('qoder')
    expect(payload.type).toBe('oauth')
    expect(payload.credentials).toEqual({ access_token: 'drt-x' })
    expect(payload.group_ids).toEqual([3, 5])
    // 空 name 用凭据昵称兜底
    expect(payload.name).toBe('Qoder OAuth Account')
    expect(wrapper.emitted('created')).toHaveLength(1)
    wrapper.unmount()
  })

  it('QoderForm：PAT 前缀不合法时导入按钮禁用', async () => {
    const wrapper = mount(QoderForm, { props: { name: 'x', notes: null, groupIds: [] } })
    const patRadio = wrapper.findAll('input[type="radio"]').find(r => (r.element as HTMLInputElement).value === 'pat')!
    await patRadio.setValue()
    await wrapper.find('input[type="password"]').setValue('sk-wrong')
    const importButton = wrapper.findAll('button').find(b => b.text().includes('qoder.exchange'))!
    expect(importButton.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('WorkBuddyForm：设备流授权后以 state 建号；填版本覆盖则改用 credentials', async () => {
    const wrapper = mount(WorkBuddyForm, {
      props: { name: 'wb-1', notes: 'n', groupIds: [] }
    })

    // 模拟 DeviceAuthFlow 授权成功
    const flow = wrapper.findComponent(DeviceAuthFlow)
    flow.vm.$emit('success', { state: 'st-1', credentials: { access_token: 'at', nickname: 'nick' } })
    await flushPromises()

    let createButton = wrapper.findAll('button').find(b => b.text().includes('workbuddy.create'))!
    expect(createButton.attributes('disabled')).toBeUndefined()
    await createButton.trigger('click')
    await flushPromises()

    // 无覆盖：state 模式
    expect(workbuddyCreateFromOAuth).toHaveBeenCalledWith(
      expect.objectContaining({ state: 'st-1', name: 'wb-1', notes: 'n' })
    )

    // 填入版本覆盖：credentials 模式（合并覆盖项）
    workbuddyCreateFromOAuth.mockClear()
    const advancedToggle = wrapper.findAll('button').find(b => b.text().includes('workbuddy.advanced'))!
    await advancedToggle.trigger('click')
    await wrapper.findAll('input').filter(i => i.attributes('placeholder') === '1.9.0')[0].setValue('2.0.0')

    createButton = wrapper.findAll('button').find(b => b.text().includes('workbuddy.create'))!
    await createButton.trigger('click')
    await flushPromises()

    expect(workbuddyCreateFromOAuth).toHaveBeenCalledWith(
      expect.objectContaining({
        credentials: expect.objectContaining({ access_token: 'at', client_version: '2.0.0' })
      })
    )
    expect(wrapper.emitted('created')).toHaveLength(2)
    wrapper.unmount()
  })

  it('WorkBuddyForm：JSON 凭据导入（解析失败时按钮禁用）', async () => {
    const wrapper = mount(WorkBuddyForm, {
      props: { name: '', notes: null, groupIds: [] }
    })

    const jsonRadio = wrapper.findAll('input[type="radio"]').find(r => (r.element as HTMLInputElement).value === 'json')!
    await jsonRadio.setValue()
    await wrapper.find('textarea').setValue('not-json')
    expect(wrapper.text()).toContain('workbuddy.jsonInvalid')
    let createButton = wrapper.findAll('button').find(b => b.text().includes('workbuddy.create'))!
    expect(createButton.attributes('disabled')).toBeDefined()

    await wrapper.find('textarea').setValue('{"access_token":"at2","realm":"cn"}')
    createButton = wrapper.findAll('button').find(b => b.text().includes('workbuddy.create'))!
    await createButton.trigger('click')
    await flushPromises()

    expect(workbuddyCreateFromOAuth).toHaveBeenCalledWith(
      expect.objectContaining({ credentials: { access_token: 'at2', realm: 'cn' }, name: undefined })
    )
    wrapper.unmount()
  })

  it('TraeForm：授权成功后以 state + callback_query 建号', async () => {
    const wrapper = mount(TraeForm, {
      props: { name: '', notes: null, groupIds: [9] }
    })

    // 变体下拉可选四项
    const options = wrapper.findAll('option')
    expect(options).toHaveLength(4)

    const flow = wrapper.findComponent(DeviceAuthFlow)
    expect(flow.props('needsCallback')).toBe(true)
    flow.vm.$emit('success', { state: 'st-t', callbackQuery: 'authCodeInfo=%7B%7D' })
    await flushPromises()

    const createButton = wrapper.findAll('button').find(b => b.text().includes('trae.create'))!
    await createButton.trigger('click')
    await flushPromises()

    expect(traeCreateFromOAuth).toHaveBeenCalledWith({
      state: 'st-t',
      callback_query: 'authCodeInfo=%7B%7D',
      name: undefined,
      concurrency: 10,
      priority: 10,
      group_ids: [9],
      model_mapping: undefined
    })
    expect(wrapper.emitted('created')).toHaveLength(1)
    wrapper.unmount()
  })

  it('QoderForm：建号失败弹错误且不 emit created', async () => {
    accountsCreate.mockRejectedValue({ message: 'duplicated' })
    const wrapper = mount(QoderForm, { props: { name: 'q', notes: null, groupIds: [] } })
    const flow = wrapper.findComponent(DeviceAuthFlow)
    flow.vm.$emit('success', { state: 'st', credentials: { access_token: 'x' } })
    await flushPromises()

    const createButton = wrapper.findAll('button').find(b => b.text().includes('qoder.create'))!
    await createButton.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('duplicated')
    expect(wrapper.emitted('created')).toBeUndefined()
    wrapper.unmount()
  })
})
