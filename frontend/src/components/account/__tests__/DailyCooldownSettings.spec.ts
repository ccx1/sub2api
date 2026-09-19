import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DailyCooldownSettings from '../DailyCooldownSettings.vue'
import { normalizeDailyCooldown } from '@/utils/dailyCooldown'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('DailyCooldownSettings', () => {
  it('starts disabled and enables the default overnight schedule', async () => {
    const wrapper = mount(DailyCooldownSettings, { props: { modelValue: normalizeDailyCooldown() } })
    expect(wrapper.find('input[type="time"]').exists()).toBe(false)
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ enabled: true, start: '23:00', end: '08:00', timezone: 'Asia/Shanghai' }])
  })

  it('shows overnight context, invalid bounds and a disabled fieldset for unapplied bulk edits', async () => {
    const config = { ...normalizeDailyCooldown(), enabled: true }
    const wrapper = mount(DailyCooldownSettings, { props: { modelValue: config } })
    expect(wrapper.text()).toContain('admin.accounts.dailyCooldown.overnightHint')
    await wrapper.setProps({ modelValue: { ...config, end: config.start } })
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.dailyCooldown.equalTimes')
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
  })
})
