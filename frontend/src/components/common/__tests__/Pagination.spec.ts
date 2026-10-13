import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, _params?: Record<string, unknown>) => key })
}))

describe('Pagination', () => {
  it('renders standard size by default', () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 1,
        pageSize: 20
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const root = wrapper.find('div')
    expect(root.classes()).toContain('py-1.5')
    expect(root.classes()).toContain('px-4')
  })

  it('renders compact layout when dense is true', async () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 1,
        pageSize: 20,
        dense: true
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const root = wrapper.find('div')
    expect(root.classes()).toContain('py-1')
    expect(root.classes()).toContain('px-3')

    const pageBtn = wrapper.findAll('nav button')[1] // first page number button
    expect(pageBtn.classes()).toContain('text-[11px]')
  })

  it('emits update:page on page button click', async () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 1,
        pageSize: 20,
        dense: true
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const page2Btn = wrapper.findAll('nav button')[2] // page 2
    await page2Btn.trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[2]])
  })

  it('disables previous button on page 1 and disables next button on last page', () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 1,
        pageSize: 20
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const prevBtn = wrapper.findAll('nav button')[0]
    expect(prevBtn.attributes('disabled')).toBeDefined()

    const wrapperLast = mount(Pagination, {
      props: {
        total: 100,
        page: 5,
        pageSize: 20
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const navButtons = wrapperLast.findAll('nav button')
    const nextBtn = navButtons[navButtons.length - 1]
    expect(nextBtn.attributes('disabled')).toBeDefined()
  })

  it('handles page jump input and action', async () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 1,
        pageSize: 20,
        showJump: true
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const input = wrapper.find('input[type="number"]')
    await input.setValue('4')
    await input.trigger('keyup.enter')

    expect(wrapper.emitted('update:page')).toEqual([[4]])
  })

  it('emits page navigation on mobile button clicks', async () => {
    const wrapper = mount(Pagination, {
      props: {
        total: 100,
        page: 2,
        pageSize: 20
      },
      global: {
        stubs: { Icon: true, Select: true }
      }
    })

    const mobileButtons = wrapper.findAll('.sm\\:hidden button')
    expect(mobileButtons).toHaveLength(2)

    await mobileButtons[0].trigger('click') // previous
    expect(wrapper.emitted('update:page')).toContainEqual([1])

    await mobileButtons[1].trigger('click') // next
    expect(wrapper.emitted('update:page')).toContainEqual([3])
  })
})
