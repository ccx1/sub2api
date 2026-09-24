import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, type PropType } from 'vue'
import ProxiesView from '../ProxiesView.vue'

const { list, listGroups, getAllWithCount, create, batchCreate, showError } = vi.hoisted(() => ({
  list: vi.fn(), listGroups: vi.fn(), getAllWithCount: vi.fn(),
  create: vi.fn(), batchCreate: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin', () => ({
  adminAPI: { proxies: { list, listGroups, getAllWithCount, create, batchCreate } },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess: vi.fn(), showInfo: vi.fn() }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params?: { days?: number }) => params?.days ? `${key}:${params.days}` : key,
  }),
}))

const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number], default: null },
    options: { type: Array as PropType<Array<{ value: string | number; label: string }>>, default: () => [] },
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    const change = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      const selected = props.options.find(option => String(option.value) === value)
      emit('update:modelValue', selected?.value ?? null)
      emit('change', selected?.value ?? null)
    }
    return { change }
  },
  template: `<select :value="modelValue ?? ''" @change="change">
    <option value=""></option>
    <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
  </select>`,
})

const ProxyGroupSelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number], default: null },
    groups: { type: Array as PropType<Array<{ id: number; name: string }>>, default: () => [] },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const change = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      emit('update:modelValue', value ? Number(value) : null)
    }
    return { change }
  },
  template: `<select :value="modelValue ?? ''" @change="change">
    <option value=""></option>
    <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
  </select>`,
})

const mountView = () => shallowMount(ProxiesView, {
  global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
    Select: SelectStub,
    ProxyGroupSelect: ProxyGroupSelectStub,
  } },
})
let wrapper: ReturnType<typeof mountView>
const batchText = 'http://first.example:8080\nsocks5://user:pass@second.example:1080'
const sharedDefaults = { expires_at: null, fallback_mode: 'none', backup_proxy_id: null, expiry_warn_days: 7 }

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 22, 12))
  list.mockResolvedValue({ items: [], total: 0, pages: 1 })
  listGroups.mockResolvedValue([{ id: 7, name: 'Tokyo pool', proxy_count: 1, active_proxy_count: 1 }])
  getAllWithCount.mockResolvedValue([
    { id: 41, name: 'Backup', protocol: 'http', host: 'backup.example', port: 8080, status: 'active' },
  ])
  create.mockResolvedValue({})
  batchCreate.mockResolvedValue({ created: 2, skipped: 0 })
})
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

async function clickButton(label: string) {
  const button = wrapper.findAll('button').find(candidate => candidate.text() === label)
  expect(button, `button ${label}`).toBeDefined()
  await button!.trigger('click')
}
async function openCreate(batch = true) {
  wrapper = mountView()
  await flushPromises()
  await clickButton('admin.proxies.createProxy')
  if (batch) await clickButton('admin.proxies.batchAdd')
}
function selectWithOption(value: string) {
  const select = wrapper.findAll<HTMLSelectElement>('#create-proxy-form select')
    .find(candidate => candidate.find(`option[value="${value}"]`).exists())
  expect(select, `select with option ${value}`).toBeDefined()
  return select!
}
async function submitBatch() {
  await wrapper.get('#create-proxy-form').trigger('submit')
  await flushPromises()
  expect(batchCreate).toHaveBeenCalledTimes(1)
  expect(batchCreate.mock.calls[0]).toHaveLength(1)
  expect(create).not.toHaveBeenCalled()
  return batchCreate.mock.calls[0][0]
}
async function configureBackup() {
  await wrapper.get('input[type="date"]').setValue('2026-10-22')
  await selectWithOption('proxy').setValue('proxy')
  await selectWithOption('41').setValue('41')
}

describe('proxy batch create expiry and fallback', () => {
  it('applies the selected expiry, numeric backup ID and fallback to every parsed proxy', async () => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)
    await configureBackup()

    const items = await submitBatch()

    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ protocol: 'http', host: 'first.example', port: 8080 })
    expect(items[1]).toMatchObject({ protocol: 'socks5', host: 'second.example', port: 1080, username: 'user', password: 'pass' })
    for (const item of items) {
      expect(item).toMatchObject({
        expires_at: Date.parse('2026-10-22') / 1000,
        fallback_mode: 'proxy', backup_proxy_id: 41, expiry_warn_days: 7,
      })
    }
  })

  it('keeps no expiry and no fallback as defaults for every proxy', async () => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)

    const items = await submitBatch()

    expect(items).toHaveLength(2)
    for (const item of items) expect(item).toMatchObject(sharedDefaults)
  })

  it('applies the optional country and group to every imported proxy', async () => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)
    await selectWithOption('7').setValue('7')
    await wrapper.get('[data-testid="batch-proxy-country"]').setValue('JP')

    const items = await submitBatch()
    expect(items).toHaveLength(2)
    for (const item of items) expect(item).toMatchObject({ group_id: 7, country_code: 'JP' })
  })

  it.each(['direct', 'none'])('clears the submitted backup after changing proxy fallback to %s', async (mode) => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)
    await configureBackup()
    await selectWithOption('proxy').setValue(mode)
    expect(wrapper.find('option[value="41"]').exists()).toBe(false)

    const items = await submitBatch()

    for (const item of items) expect(item).toMatchObject({ fallback_mode: mode, backup_proxy_id: null })
  })

  it('links preset days, custom day counts and the expiry calendar in batch mode', async () => {
    await openCreate()
    const date = wrapper.get<HTMLInputElement>('input[type="date"]')
    const days = wrapper.get<HTMLInputElement>('input[placeholder="admin.proxies.expiryDaysPlaceholder"]')

    await clickButton('admin.proxies.nDays:30')
    expect(date.element.value).toBe('2026-10-22')
    expect(days.element.value).toBe('30')
    await date.setValue('2026-10-02')
    expect(days.element.value).toBe('10')
    await days.setValue(7)
    expect(date.element.value).toBe('2026-09-29')
    await days.setValue(0)
    expect(date.element.value).toBe('')
    expect(days.element.value).toBe('')
  })

  it('resets shared settings and batch input after closing and reopening the dialog', async () => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)
    await wrapper.get('[data-testid="batch-proxy-country"]').setValue('JP')
    await configureBackup()
    await clickButton('common.cancel')
    expect(wrapper.find('#create-proxy-form').exists()).toBe(false)

    await clickButton('admin.proxies.createProxy')
    await clickButton('admin.proxies.batchAdd')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('input[type="date"]').element.value).toBe('')
    expect(selectWithOption('proxy').element.value).toBe('none')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="batch-proxy-country"]').element.value).toBe('')
    await wrapper.get('textarea').setValue(batchText)
    for (const item of await submitBatch()) expect(item).toMatchObject(sharedDefaults)
  })

  it('rejects proxy fallback without a selected backup and allows correction', async () => {
    await openCreate()
    await wrapper.get('textarea').setValue(batchText)
    await selectWithOption('proxy').setValue('proxy')
    await wrapper.get('#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(batchCreate).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.proxies.backupProxyRequired')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe(batchText)
    await selectWithOption('41').setValue('41')
    for (const item of await submitBatch()) expect(item.backup_proxy_id).toBe(41)
  })

  it('retains user settings after a failed request so the same batch can be retried', async () => {
    batchCreate.mockRejectedValueOnce({ response: { data: { detail: 'Temporary failure' } } })
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      await openCreate()
      await wrapper.get('textarea').setValue(batchText)
      await configureBackup()
      const firstPayload = await submitBatch()

      expect(showError).toHaveBeenCalledWith('Temporary failure')
      expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe(batchText)
      expect(wrapper.get<HTMLInputElement>('input[type="date"]').element.value).toBe('2026-10-22')
      expect(selectWithOption('proxy').element.value).toBe('proxy')
      expect(selectWithOption('41').element.value).toBe('41')
      await wrapper.get('#create-proxy-form').trigger('submit')
      await flushPromises()

      expect(batchCreate).toHaveBeenCalledTimes(2)
      expect(batchCreate.mock.calls[1][0]).toEqual(firstPayload)
      expect(wrapper.find('#create-proxy-form').exists()).toBe(false)
    } finally {
      consoleError.mockRestore()
    }
  })

  it('creates an IPv6 proxy with the selected country and shared settings', async () => {
    await openCreate(false)
    await wrapper.get('input[placeholder="admin.proxies.enterProxyName"]').setValue(' Single proxy ')
    await wrapper.get('input[placeholder="admin.proxies.form.hostPlaceholder"]').setValue(' 2001:db8::10 ')
    await wrapper.get('[data-testid="create-proxy-country"]').setValue('JP')
    await configureBackup()
    await wrapper.get('#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Single proxy', host: '2001:db8::10', protocol: 'http', port: 8080, country_code: 'JP',
      expires_at: Date.parse('2026-10-22') / 1000,
      fallback_mode: 'proxy', backup_proxy_id: 41, expiry_warn_days: 7,
    }))
    expect(batchCreate).not.toHaveBeenCalled()
  })

  it('allows standard creation without assigning a country', async () => {
    await openCreate(false)
    await wrapper.get('input[placeholder="admin.proxies.enterProxyName"]').setValue('Unassigned')
    await wrapper.get('input[placeholder="admin.proxies.form.hostPlaceholder"]').setValue('proxy.example')
    await wrapper.get('#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({ country_code: null }))
  })
})
