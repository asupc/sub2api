import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import ApiKeyUsageTable from '../ApiKeyUsageTable.vue'
import type { ApiKeyUsageStatItem } from '@/types'

const makeRow = (overrides: Partial<ApiKeyUsageStatItem> = {}): ApiKeyUsageStatItem => ({
  api_key_id: 1,
  name: 'key-one',
  username: 'alice',
  request_count: 10,
  input_tokens: 200,
  output_tokens: 100,
  cache_creation_tokens: 0,
  cache_read_tokens: 800,
  total_tokens: 1100,
  avg_tokens_per_sec: 12.5,
  avg_duration_ms: 800,
  models: [
    {
      model: 'model-a',
      request_count: 6,
      input_tokens: 100,
      output_tokens: 60,
      cache_creation_tokens: 0,
      cache_read_tokens: 500,
      total_tokens: 660,
      avg_tokens_per_sec: 15,
      avg_duration_ms: 700
    }
  ],
  ...overrides
})

describe('ApiKeyUsageTable', () => {
  it('renders loading state', () => {
    const wrapper = mount(ApiKeyUsageTable, {
      props: { items: [], loading: true },
      global: { stubs: { LoadingSpinner: true } }
    })
    expect(wrapper.findComponent({ name: 'LoadingSpinner' }).exists()).toBe(true)
  })

  it('renders error state', () => {
    const wrapper = mount(ApiKeyUsageTable, {
      props: { items: [], error: 'boom' },
      global: { stubs: { LoadingSpinner: true } }
    })
    expect(wrapper.text()).toContain('boom')
  })

  it('renders empty state with custom text', () => {
    const wrapper = mount(ApiKeyUsageTable, {
      props: { items: [], emptyText: 'nothing here' },
      global: { stubs: { LoadingSpinner: true } }
    })
    expect(wrapper.text()).toContain('nothing here')
  })

  it('renders rows with cache hit rate and expands model breakdown on click', async () => {
    const wrapper = mount(ApiKeyUsageTable, {
      props: { items: [makeRow()] },
      global: { stubs: { LoadingSpinner: true } }
    })

    expect(wrapper.text()).toContain('key-one')
    expect(wrapper.text()).toContain('alice')
    // 800 cache_read / (200 input + 800 cache_read) = 80%
    expect(wrapper.text()).toContain('80%')

    // 明细默认收起；点击单行展开
    expect(wrapper.text()).not.toContain('model-a')
    await wrapper.find('tbody tr').trigger('click')
    expect(wrapper.text()).toContain('model-a')

    // 此时单行已展开 → 按钮为"收起全部"，点一下收起，再点一下展开全部
    await wrapper.find('button').trigger('click')
    expect(wrapper.text()).not.toContain('model-a')
    await wrapper.find('button').trigger('click')
    expect(wrapper.text()).toContain('model-a')
  })

  it('hides username when showUsername is false', () => {
    const wrapper = mount(ApiKeyUsageTable, {
      props: { items: [makeRow()], showUsername: false },
      global: { stubs: { LoadingSpinner: true } }
    })
    expect(wrapper.text()).toContain('key-one')
    expect(wrapper.text()).not.toContain('alice')
  })
})
