import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CodexTicketCredentialSettings from '../CodexTicketCredentialSettings.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('CodexTicketCredentialSettings', () => {
  it('offers global inheritance and all credential modes', async () => {
    const wrapper = mount(CodexTicketCredentialSettings, { props: { modelValue: { mode: 'inherit' } } })
    expect(wrapper.findAll('option').map(option => option.attributes('value'))).toEqual(['inherit', 'state', 'cookie_state', 'cookie'])
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.get('select').setValue('cookie')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ mode: 'cookie' }]])
    wrapper.unmount()
  })

  it('clears numeric overrides on inheritance and preserves zero separately from empty', async () => {
    const wrapper = mount(CodexTicketCredentialSettings, { props: { modelValue: { mode: 'cookie', ttl_seconds: 20, refresh_before_seconds: 5 } } })
    await wrapper.get('[data-testid="codex-ticket-credential-refresh_before_seconds"]').setValue('0')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ mode: 'cookie', ttl_seconds: 20, refresh_before_seconds: 0 }])
    await wrapper.get('[data-testid="codex-ticket-credential-refresh_before_seconds"]').setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ mode: 'cookie', ttl_seconds: 20 }])
    await wrapper.get('select').setValue('inherit')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ mode: 'inherit' }])
    wrapper.unmount()
  })

  it('shows invalid timing and prevents stale events while disabled', async () => {
    const wrapper = mount(CodexTicketCredentialSettings, { props: { modelValue: { mode: 'cookie_state', ttl_seconds: 20, refresh_before_seconds: 20 }, disabled: true } })
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.codexTicketCredential.refreshOrder')
    await wrapper.get('select').setValue('state')
    await wrapper.get('input').setValue('30')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
