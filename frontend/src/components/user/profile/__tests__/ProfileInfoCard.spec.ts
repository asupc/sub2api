import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ProfileInfoCard from '../ProfileInfoCard.vue'
import type { User } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

// 二开说明：第三方 OAuth 登录裁剪后，绑定/来源提示区块已移除，仅保留基础信息渲染断言。
const makeUser = (overrides: Partial<User> = {}): User =>
  ({
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    role: 'user',
    balance: 12.5,
    concurrency: 5,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-01-15T00:00:00Z',
    updated_at: '2026-01-15T00:00:00Z',
    ...overrides,
  }) as User

const mountCard = (user: User | null) =>
  mount(ProfileInfoCard, {
    props: { user },
    global: {
      stubs: {
        ProfileAvatarCard: { template: '<div data-testid="avatar-card" />' },
        ProfileEditForm: { template: '<div data-testid="edit-form" />' },
      },
    },
  })

describe('ProfileInfoCard', () => {
  it('renders basic account information inside the overview shell', async () => {
    const wrapper = mountCard(makeUser())
    await flushPromises()

    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.find('[data-testid="profile-overview-metric-balance"]').text()).toContain('$12.50')
    expect(wrapper.find('[data-testid="profile-overview-metric-concurrency"]').text()).toContain('5')
    expect(wrapper.find('[data-testid="avatar-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="edit-form"]').exists()).toBe(true)
  })

  it('renders empty placeholders for a null user', async () => {
    const wrapper = mountCard(null)
    await flushPromises()

    expect(wrapper.find('[data-testid="profile-overview-metric-balance"]').text()).toContain('$0.00')
  })
})
