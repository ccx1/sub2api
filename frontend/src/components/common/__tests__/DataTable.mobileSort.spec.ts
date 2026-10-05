import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DataTable from '../DataTable.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const props = {
  columns: [{ key: 'name', label: 'Name', sortable: true }, { key: 'cost', label: 'Cost', sortable: true }],
  data: [{ id: 1, name: 'Account', cost: 2 }],
  serverSideSort: true,
  defaultSortKey: 'name',
  defaultSortOrder: 'desc' as const,
  sortStorageKey: 'mobile-accounts'
}

describe('DataTable mobile sorting', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    })
  })

  it('uses the server sort contract and persists field and direction', async () => {
    const wrapper = mount(DataTable, { props })
    await wrapper.find('[data-test="mobile-sort-field"]').setValue('cost')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['cost', 'asc'])
    await wrapper.find('[data-test="mobile-sort-direction"]').trigger('click')
    expect(wrapper.emitted('sort')?.at(-1)).toEqual(['cost', 'desc'])
    expect(JSON.parse(localStorage.getItem('mobile-accounts')!)).toEqual({ key: 'cost', order: 'desc' })
    wrapper.unmount()
    const restored = mount(DataTable, { props })
    await restored.vm.$nextTick()
    expect((restored.find('select').element as HTMLSelectElement).value).toBe('cost')
    expect(restored.find('button').attributes('aria-label')).toBe('common.sortAscending')
    restored.unmount()
  })

  it('keeps independent accessible controls without duplicate IDs', () => {
    const first = mount(DataTable, { props })
    const second = mount(DataTable, { props })
    for (const wrapper of [first, second]) {
      expect(wrapper.find('select').attributes('aria-label')).toBe('common.sortBy')
      expect(wrapper.find('select').attributes('id')).toBeUndefined()
      wrapper.unmount()
    }
  })

  it('disables sorting during reload and omits it for client sorting', () => {
    const loading = mount(DataTable, { props: { ...props, loading: true } })
    expect(loading.find('select').attributes('disabled')).toBeDefined()
    expect(loading.find('button').attributes('disabled')).toBeDefined()
    loading.unmount()
    const client = mount(DataTable, { props: { ...props, serverSideSort: false } })
    expect(client.find('[data-test="mobile-sort-toolbar"]').exists()).toBe(false)
    client.unmount()
  })
})
