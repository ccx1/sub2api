import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedAccountDialog from '../SharedAccountDialog.vue'
import SharedCredentialsForm from '../SharedCredentialsForm.vue'
import DailyCooldownSettings from '@/components/account/DailyCooldownSettings.vue'
import type { SharedAccount } from '@/api/sharedPool'

const { create, update } = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { create, update } }))
vi.mock('@/api/admin', () => ({ adminAPI: { grok: { getCapabilities: vi.fn() } } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const config = { platforms: ['openai'] as const, max_concurrency: 10, platform_rate_bps: 2000, proxy_rate_bps: 100, settlement_multiplier: 1 }
const account = { id: 7, name: 'Owner account', platform: 'openai', type: 'oauth', concurrency: 2, enabled: true, protection_enabled: true, proxy_mode: 'custom' } as SharedAccount
const unchangedFields = { name: account.name, platform: 'openai', type: 'oauth', enabled: true, protection_enabled: true }
function render(editing = false, overrides: Partial<SharedAccount> = {}) {
  return mount(SharedAccountDialog, {
    props: { show: true, config: { ...config, platforms: [...config.platforms] }, account: editing ? { ...account, ...overrides } : null },
    global: { stubs: {
      BaseDialog: { name: 'BaseDialog', props: ['width'], template: '<div><slot /><slot name="footer" /></div>' }, SharedCredentialsForm: true, SharedRevenueSplit: true
    } }
  })
}

describe('shared account creation and edit boundaries', () => {
  beforeEach(() => { vi.clearAllMocks(); create.mockResolvedValue({}); update.mockResolvedValue({}) })

  it('requires credentials for new accounts and sends only contributor fields', async () => {
    const wrapper = render()
    await wrapper.get('#shared-name').setValue(' My account ')
    await wrapper.get('form').trigger('submit')
    expect(create).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('sharedPool.credentialsRequired')
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith({ name: 'My account', platform: 'openai', type: 'oauth', concurrency: 1, proxy_url: '', protection_enabled: true, enabled: false, dispatch_consent: false, credentials: { access_token: 'fixture-token' }, confirm_disable: false, codex_ticket_enabled: true })
    expect(Object.keys(create.mock.calls[0][0])).not.toContain('group_ids')
    expect(Object.keys(create.mock.calls[0][0])).not.toContain('rate_multiplier')
  })

  it('preserves private proxy and credentials when editing without replacement', async () => {
    const wrapper = render(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { ...unchangedFields, concurrency: 2 })
  })

  it('accepts zero settlement and prevents authorization when settlement is unavailable', async () => {
    const wrapper = render()
    await wrapper.setProps({ config: { ...config, platforms: [...config.platforms], settlement_multiplier: 0 } })
    const sharing = wrapper.get('[role="switch"][aria-label="sharedPool.sharing"]')
    expect(sharing.attributes('disabled')).toBeUndefined()
    await sharing.trigger('click')
    expect(sharing.attributes('aria-checked')).toBe('true')
    await wrapper.setProps({ config: { ...config, platforms: [...config.platforms], settlement_multiplier: undefined } })
    expect(sharing.attributes('disabled')).toBeDefined()
    expect(sharing.attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).toContain('sharedPool.settlementUnconfigured')
  })

  it('exposes concurrency, proxy and cooldown without identity or authorization changes when editing', async () => {
    const wrapper = render(true)
    expect(wrapper.findComponent({ name: 'BaseDialog' }).props('width')).toBe('normal')
    expect(wrapper.text()).toContain(account.name)
    expect(wrapper.find('#shared-name').exists()).toBe(false)
    expect(wrapper.find('[data-platform]').exists()).toBe(false)
    expect(wrapper.find('[data-account-type]').exists()).toBe(false)
    expect(wrapper.findComponent(SharedCredentialsForm).exists()).toBe(false)
    expect(wrapper.find('#shared-codex-ticket').exists()).toBe(false)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    expect(wrapper.findComponent(DailyCooldownSettings).exists()).toBe(true)
    await wrapper.get('#shared-concurrency').setValue(5)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { ...unchangedFields, concurrency: 5 })
  })

  it('only sends proxy_url when replacement is selected, including an empty random-pool replacement', async () => {
    const wrapper = render(true)
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('#shared-proxy').setValue('http://fixture.example:8080')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(7, { ...unchangedFields, concurrency: 2, proxy_url: 'http://fixture.example:8080' })
    await wrapper.get('#shared-proxy').setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(7, { ...unchangedFields, concurrency: 2, proxy_url: '' })
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(7, { ...unchangedFields, concurrency: 2 })
  })

  it('still allows choosing sharing and protection when creating an account', async () => {
    const wrapper = render()
    await wrapper.get('#shared-name').setValue('New shared account')
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    await wrapper.get('[role="switch"][aria-label="sharedPool.sharing"]').trigger('click')
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, dispatch_consent: true, protection_enabled: false }))
  })

  it('does not allow invalid JSON credentials when creating an account', async () => {
    const wrapper = render()
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('valid', false)
    await wrapper.get('form').trigger('submit')
    expect(create).not.toHaveBeenCalled()
  })

  it('uses the original wide layout and carries current settings into account import', async () => {
    const wrapper = render()
    expect(wrapper.findComponent({ name: 'BaseDialog' }).props('width')).toBe('wide')
    await wrapper.get('#shared-name').setValue('Imported account')
    await wrapper.get('#shared-concurrency').setValue(3)
    await wrapper.get('#shared-proxy').setValue('http://fixture.example:8080')
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
    expect(wrapper.emitted('import')?.[0]).toEqual([{ name: 'Imported account', platform: 'openai', type: 'oauth', concurrency: 3, proxy_url: 'http://fixture.example:8080', enabled: false, dispatch_consent: false, protection_enabled: true, codex_ticket_enabled: true }])
    expect(wrapper.find('select').exists()).toBe(false)
  })

  it('blocks save while authorization is running and prevents duplicate saves', async () => {
    const wrapper = render()
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('busy', true)
    await wrapper.get('form').trigger('submit')
    expect(create).not.toHaveBeenCalled()
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('busy', false)
    let resolve!: () => void
    create.mockReturnValueOnce(new Promise<void>(done => { resolve = done }))
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(create).toHaveBeenCalledTimes(1)
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    resolve()
    await flushPromises()
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('offers only OAuth for Antigravity and resets the selected API-key type', async () => {
    const wrapper = render()
    await wrapper.setProps({ config: { ...config, platforms: ['openai', 'antigravity'] } })
    await wrapper.get('[data-account-type="apikey"]').trigger('click')
    await wrapper.get('[data-platform="antigravity"]').trigger('click')
    expect(wrapper.find('[data-account-type="apikey"]').exists()).toBe(false)
    expect(wrapper.findComponent(SharedCredentialsForm).props('type')).toBe('oauth')
  })

  it('preserves an explicit disabled ticket choice for creation and import', async () => {
    const wrapper = render()
    await wrapper.get('#shared-codex-ticket').trigger('click')
    wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ codex_ticket_enabled: false }))
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
    expect(wrapper.emitted('import')?.[0]?.[0]).toEqual(expect.objectContaining({ codex_ticket_enabled: false }))
  })

  it.each(['pro', 'chatgpt_pro', 'prolite'])('forces tickets for recognized OpenAI OAuth %s credentials', async planType => {
    const wrapper = render()
    await wrapper.get('#shared-codex-ticket').trigger('click')
    wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token', plan_type: planType })
    await wrapper.vm.$nextTick()
    const ticket = wrapper.get('#shared-codex-ticket')
    expect(ticket.attributes('aria-checked')).toBe('true')
    expect(ticket.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('sharedPool.codexTicketRequiredHint')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ codex_ticket_enabled: true }))
    wrapper.unmount()
  })

  it('does not force tickets for Business Premium despite its prolite suffix', async () => {
    const wrapper = render()
    wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token', plan_type: 'self_serve_business_prolite' })
    await wrapper.vm.$nextTick()
    expect(wrapper.get('#shared-codex-ticket').attributes('disabled')).toBeUndefined()
    await wrapper.get('#shared-codex-ticket').trigger('click')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ codex_ticket_enabled: false }))
    wrapper.unmount()
  })

  it('omits ticket configuration for API-key and other platform accounts', async () => {
    const wrapper = render()
    await wrapper.setProps({ config: { ...config, platforms: ['openai', 'gemini'] } })
    for (const selector of ['[data-account-type="apikey"]', '[data-platform="gemini"]']) {
      await wrapper.get(selector).trigger('click')
      expect(wrapper.find('#shared-codex-ticket').exists()).toBe(false)
      wrapper.findComponent(SharedCredentialsForm).vm.$emit('change', { api_key: 'fixture-key' })
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(create.mock.lastCall?.[0]).not.toHaveProperty('codex_ticket_enabled')
    }
  })

  it('creates and hands off import with the configured cooldown using only its dedicated field', async () => {
    const wrapper = render()
    const cooldown = { enabled: true, start: '23:30', end: '07:15', timezone: 'Asia/Shanghai' }
    wrapper.getComponent(DailyCooldownSettings).vm.$emit('update:modelValue', cooldown)
    wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(create.mock.lastCall?.[0].daily_cooldown).toEqual(cooldown)
    expect(create.mock.lastCall?.[0]).not.toHaveProperty('extra')
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
    expect(wrapper.emitted('import')?.[0]?.[0]).toEqual(expect.objectContaining({ daily_cooldown: cooldown }))
    wrapper.unmount()
  })

  it('fills existing cooldown during edit and sends a minimal explicit disable', async () => {
    const cooldown = { enabled: true, start: '01:00', end: '03:00', timezone: 'UTC' }
    const wrapper = render(true, { daily_cooldown: cooldown })
    expect(wrapper.getComponent(DailyCooldownSettings).props('modelValue')).toEqual(cooldown)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.lastCall?.[1]).toEqual({ ...unchangedFields, concurrency: 2, daily_cooldown: cooldown })
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').trigger('click')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update.mock.lastCall?.[1].daily_cooldown).toEqual({ enabled: false })
    expect(update.mock.lastCall?.[1]).not.toHaveProperty('extra')
    wrapper.unmount()
  })

  it('sends an explicit disabled cooldown after toggling it off during creation', async () => {
    const wrapper = render()
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').trigger('click')
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').trigger('click')
    wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(create.mock.lastCall?.[0].daily_cooldown).toEqual({ enabled: false })
    wrapper.unmount()
  })

  it.each([
    { start: '08:00', end: '08:00', timezone: 'UTC', error: 'equalTimes' },
    { start: '25:00', end: '08:00', timezone: 'UTC', error: 'invalidTime' },
    { start: '23:00', end: '08:00', timezone: 'Invalid/Zone', error: 'invalidTimezone' }
  ])('blocks create, edit and import handoff for $error', async ({ error, ...schedule }) => {
    for (const editing of [false, true]) {
      const wrapper = render(editing)
      wrapper.getComponent(DailyCooldownSettings).vm.$emit('update:modelValue', { enabled: true, ...schedule })
      if (!editing) wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token' })
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(create).not.toHaveBeenCalled()
      expect(update).not.toHaveBeenCalled()
      expect(wrapper.text()).toContain(`admin.accounts.dailyCooldown.${error}`)
      if (!editing) {
        await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
        expect(wrapper.emitted('import')).toBeUndefined()
      }
      wrapper.unmount()
    }
  })
})
