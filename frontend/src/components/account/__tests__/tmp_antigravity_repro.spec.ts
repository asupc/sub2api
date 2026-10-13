import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const { updateAccountMock, checkMixedChannelRiskMock } = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get isSimpleMode() { return true } })
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { update: updateAccountMock, checkMixedChannelRisk: checkMixedChannelRiskMock },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: { type: Array, default: () => [] },
    accountId: { type: Number, default: 0 }
  },
  template: '<span data-testid="whitelist-value">{{ (modelValue || []).join(",") }}</span>'
})

const globalStubs = {
  BaseDialog: BaseDialogStub,
  Select: true,
  Icon: true,
  ProxySelector: true,
  GroupSelector: true,
  ModelWhitelistSelector: ModelWhitelistSelectorStub
}

const antigravityAccount = {
  id: 563,
  name: 'Antigravity',
  notes: '',
  platform: 'antigravity',
  type: 'oauth',
  credentials: {
    email: 'x@example.com',
    project_id: 'proj-1',
    model_mapping: {
      'gemini-3.6-flash-low': 'gemini-3.6-flash-low',
      'gemini-3.6-flash-high': 'gemini-3.6-flash-high',
      'gemini-3.8-flash-high': 'gemini-3.8-flash-high'
    }
  },
  extra: {},
  proxy_id: null,
  concurrency: 1,
  priority: 1,
  rate_multiplier: 1,
  status: 'active',
  group_ids: [],
  expires_at: null,
  auto_pause_on_expired: false
} as any

describe('repro: antigravity edit round-trip', () => {
  beforeEach(() => {
    updateAccountMock.mockReset().mockResolvedValue(antigravityAccount)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('shows the stored model_mapping in the whitelist selector when opening the modal', async () => {
    const wrapper = mount(EditAccountModal, {
      props: { show: true, account: antigravityAccount, proxies: [], groups: [] },
      global: { stubs: globalStubs }
    })
    await new Promise((r) => setTimeout(r, 0))
    const spans = wrapper.findAll('[data-testid="whitelist-value"]')
    console.log('WHITELIST VALUES:', spans.map((s) => s.text()))
    expect(spans.length).toBeGreaterThan(0)
    expect(spans[0].text()).toBe('gemini-3.6-flash-low,gemini-3.6-flash-high,gemini-3.8-flash-high')
  })

  it('preserves model_mapping in the update payload when saving without edits', async () => {
    const wrapper = mount(EditAccountModal, {
      props: { show: true, account: antigravityAccount, proxies: [], groups: [] },
      global: { stubs: globalStubs }
    })
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await new Promise((r) => setTimeout(r, 0))
    console.log("CREDS:", JSON.stringify(updateAccountMock.mock.calls[0]?.[1]?.credentials))
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual(
      antigravityAccount.credentials.model_mapping
    )
  })
})
