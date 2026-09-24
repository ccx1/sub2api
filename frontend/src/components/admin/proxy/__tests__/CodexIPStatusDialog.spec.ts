import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexIPStatusDialog from '../CodexIPStatusDialog.vue'

const listCodexIPStatus = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/proxies', () => ({ listCodexIPStatus }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

const global = {
  stubs: {
    BaseDialog: {
      props: ['show'],
      template: '<div v-if="show"><slot /><slot name="footer" /></div>'
    },
    Select: {
      props: ['modelValue', 'options'],
      emits: ['update:modelValue', 'change'],
      template: '<select data-testid="status-select" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value); $emit(\'change\', $event.target.value, null)"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>'
    },
    Pagination: {
      props: ['page', 'total', 'pageSize'],
      emits: ['update:page'],
      template: '<button data-testid="next-page" @click="$emit(\'update:page\', page + 1)">next</button>'
    },
    Icon: true
  }
}

const mounted: Array<{ unmount: () => void }> = []

afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()))
beforeEach(() => {
  vi.clearAllMocks()
  listCodexIPStatus.mockResolvedValue({
    items: [{
      ip: '203.0.113.10',
      status: 'cooling',
      until_at: '2026-09-24T10:00:00Z',
      rounds: 2,
      failed_accounts: 3,
      last_failure_at: '2026-09-24T09:00:00Z'
    }],
    total: 1,
    page: 1,
    page_size: 10,
    pages: 1
  })
})

describe('CodexIPStatusDialog', () => {
  it('loads and renders cooled IPs when opened', async () => {
    const wrapper = mount(CodexIPStatusDialog, { props: { show: true }, global })
    mounted.push(wrapper)
    await flushPromises()

    expect(listCodexIPStatus).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 10 }))
    expect(wrapper.text()).toContain('203.0.113.10')
    expect(wrapper.text()).toContain('admin.proxies.ipStatusCooling')
  })

  it('reloads the first page when the status type changes', async () => {
    const wrapper = mount(CodexIPStatusDialog, { props: { show: true }, global })
    mounted.push(wrapper)
    await flushPromises()

    await wrapper.get('[data-testid="codex-ip-status-filter"]').setValue('disabled')
    await flushPromises()

    expect(listCodexIPStatus).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'disabled', page: 1 }))
  })
})
