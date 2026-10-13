import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

describe('selectors disabled while open', () => {
  it('closes an open Select and can reopen when enabled again', async () => {
    const wrapper = mount(Select, {
      props: { modelValue: 1, options: [{ value: 1, label: 'One' }, { value: 2, label: 'Two' }] },
      global: { stubs: { Teleport: true, Transition: true } },
    })
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
    await wrapper.setProps({ disabled: true })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: false })
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
  })
})
