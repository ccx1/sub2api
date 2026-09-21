import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SharedPoolSettingsForm from '../SharedPoolSettingsForm.vue'
import Select from '@/components/common/Select.vue'
import type { SharedSettings } from '@/api/sharedPool'
import type { AdminGroup } from '@/types'
import sharedPool from '@/i18n/locales/zh/sharedPool'

enableAutoUnmount(afterEach)
type SelectWrapper = Omit<VueWrapper<InstanceType<typeof Select>>, 'exists'>

const { saveSettings, showSuccess } = vi.hoisted(() => ({ saveSettings: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ adminSharedPoolAPI: { saveSettings } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, values: Record<string, string | number> = {}) => {
    const message = (sharedPool as Record<string, unknown>)[key.replace('sharedPool.', '')]
    return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? '')) : key
  }
}) }))

function group(id: number, platform = 'openai', overrides: Partial<AdminGroup> = {}) {
  return { id, name: `Group ${id}`, platform, rate_multiplier: 1, is_shared_pool: true, status: 'active', subscription_type: 'standard', is_exclusive: false, ...overrides } as AdminGroup
}
const groups = [
  group(1), group(2), group(3, 'openai', { status: 'inactive' }),
  group(4, 'openai', { is_shared_pool: false }), group(5, 'gemini'), group(6, 'antigravity'),
  group(7, 'openai', { is_exclusive: true }), group(8, 'openai', { subscription_type: 'subscription' }), group(9, 'anthropic')
]
const original: SharedSettings = {
  platform_rate_bps: 500, proxy_rate_bps: 100, max_concurrency: 10, settlement_multiplier: 1,
  default_group_ids: { openai: 1, gemini: 5, antigravity: 6, anthropic: 9 }
}
function render(settings: SharedSettings = original) {
  return mount(SharedPoolSettingsForm, {
    props: { settings: structuredClone(settings), groups },
    global: {
      stubs: { RouterLink: true, teleport: true }
    }
  })
}
function defaultSelect(wrapper: VueWrapper, platform = 'openai') {
  return wrapper.findAllComponents(Select).find(select => select.props('id') === `default-group-${platform}`)!
}
async function open(select: SelectWrapper) {
  if (select.get('button').attributes('aria-expanded') !== 'true') await select.get('button').trigger('click')
  await flushPromises()
}
async function choose(select: SelectWrapper, label: string) {
  await open(select)
  const option = select.findAll('[role="option"]').find(item => item.text() === label)
  expect(option, `Missing option: ${label}`).toBeDefined()
  await option!.trigger('click')
  await flushPromises()
}
async function addRule(wrapper: VueWrapper, platform: string, tier: string, groupId: number) {
  const platformSection = wrapper.get(`[data-tier-platform="${platform}"]`)
  await platformSection.get('[data-add-rule]').trigger('click')
  const row = platformSection.findAll('[data-tier-row]').at(-1)!
  await choose(row.getComponent(Select), tier)
  await choose(row.getComponent<typeof Select>('[data-group-select]'), `Group ${groupId} · 1x`)
}
async function save(wrapper: VueWrapper) {
  await wrapper.get('form').trigger('submit')
  await flushPromises()
}

describe('shared pool subscription group settings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    saveSettings.mockImplementation(value => Promise.resolve(value))
  })

  it.each([0, 100])('persists new account default priority %s', async priority => {
    const wrapper = render({ ...original, default_priority: 50 })
    await wrapper.get('#pool-default-priority').setValue(priority)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...original, subscription_group_ids: {}, default_priority: priority })
    expect(wrapper.text()).toContain('仅影响此后创建或导入的账号')
  })

  it.each([-1, 101, 1.5, ''])('rejects invalid default priority %s', async priority => {
    const wrapper = render()
    await wrapper.get('#pool-default-priority').setValue(priority)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('调度优先级必须为 0 到 100 之间的整数。')
  })

  it('loads legacy settings without tier rules and sends an explicit empty map', async () => {
    const wrapper = render()
    expect(wrapper.findAll('[data-tier-row]')).toHaveLength(0)
    expect(wrapper.get('[data-tier-platform="anthropic"]').text()).toContain('没有可用于分组的订阅档位信息')
    expect(wrapper.get('[data-tier-platform="anthropic"]').find('[data-add-rule]').exists()).toBe(false)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...original, subscription_group_ids: {} })
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('saves one global settlement multiplier independently of subscription group rules', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: 2 } } })
    expect(wrapper.findAll('[data-settlement-default]')).toHaveLength(1)
    await wrapper.get('[data-settlement-default]').setValue(0)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...original, settlement_multiplier: 0, subscription_group_ids: { openai: { plus: 2 } } })
  })

  it.each(['', '-1', '101'])('rejects invalid global settlement multiplier %s', async value => {
    const wrapper = render()
    await wrapper.get('[data-settlement-default]').setValue(value)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('结算倍率必须在 0 到 100 之间')
  })

  it('adds a supported tier and preserves defaults, rates and other platform rules', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { gemini: { google_ai_pro: 5 }, antigravity: { ultra: 6 } } })
    await addRule(wrapper, 'openai', 'Pro 5x', 2)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...original, subscription_group_ids: { openai: { prolite: 2 }, gemini: { google_ai_pro: 5 }, antigravity: { ultra: 6 } } })
    expect(showSuccess).toHaveBeenCalledWith('已保存')
  })

  it('removes the last rule and persists an empty map to clear old settings', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: 2 } } })
    await wrapper.get('[data-remove-rule]').trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...original, subscription_group_ids: {} })
  })

  it('offers active paid standard non-exclusive groups from the matching platform', async () => {
    const wrapper = render()
    await wrapper.get('[data-tier-platform="openai"] [data-add-rule]').trigger('click')
    const openaiSelect = wrapper.getComponent<typeof Select>('[data-group-select]')
    await open(openaiSelect)
    expect(openaiSelect.findAll('[role="option"]').map(option => option.text())).toEqual(['选择接入分组', 'Group 1 · 1x', 'Group 2 · 1x', 'Group 4 · 1x'])
    expect(openaiSelect.props('modelValue')).toBe(0)
    expect(openaiSelect.props('options').map(option => option.value)).toEqual([0, 1, 2, 4])
    await wrapper.get('[data-tier-platform="gemini"] [data-add-rule]').trigger('click')
    const geminiSelect = wrapper.getComponent<typeof Select>('[data-tier-platform="gemini"] [data-group-select]')
    await open(geminiSelect)
    expect(geminiSelect.findAll('[role="option"]').map(option => option.text())).toEqual(['选择接入分组', 'Group 5 · 1x'])
    expect(defaultSelect(wrapper).props('options').map(option => option.value)).toEqual([0, 1, 2, 4])
    expect(defaultSelect(wrapper, 'gemini').props('options').map(option => option.value)).toEqual([0, 5])
  })

  it('rejects duplicate tiers instead of silently overwriting a rule', async () => {
    const wrapper = render()
    await addRule(wrapper, 'openai', 'Plus', 1)
    await addRule(wrapper, 'openai', 'Plus', 2)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('OpenAI 的订阅档位不能重复')
  })

  it('allows tier rules without a default group so unmatched accounts can await allocation', async () => {
    const wrapper = render({ ...original, default_group_ids: { gemini: 5 } })
    await addRule(wrapper, 'openai', 'Pro 20x', 2)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: { gemini: 5 }, subscription_group_ids: { openai: { pro: 2 } } }))
    await choose(defaultSelect(wrapper), 'Group 1 · 1x')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: { openai: 1, gemini: 5 }, subscription_group_ids: { openai: { pro: 2 } } }))
  })

  it.each([3, 5, 99])('keeps an unavailable target visible and requires a replacement or removal (%s)', async target => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: target } } })
    const targetSelect = wrapper.getComponent<typeof Select>('[data-group-select]')
    expect(targetSelect.props('modelValue')).toBe(target)
    expect(targetSelect.props('error')).toBe(true)
    expect(targetSelect.get('button').text()).toContain('不可用')
    await open(targetSelect)
    const disabledOption = targetSelect.get('[role="option"][aria-disabled="true"]')
    expect(disabledOption.text()).toContain('不可用')
    await disabledOption.trigger('click')
    expect(targetSelect.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('账号匹配时将回退到默认分组')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('同平台标准付费分组')
    await choose(wrapper.getComponent<typeof Select>('[data-group-select]'), 'Group 2 · 1x')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ subscription_group_ids: { openai: { plus: 2 } } }))
  })

  it('does not silently replace an unavailable default group', async () => {
    const wrapper = render({ ...original, default_group_ids: { openai: 99 } })
    expect(defaultSelect(wrapper).props('modelValue')).toBe(99)
    expect(defaultSelect(wrapper).props('error')).toBe(true)
    expect(defaultSelect(wrapper).get('button').text()).toContain('#99（不可用）')
    await open(defaultSelect(wrapper))
    expect(defaultSelect(wrapper).get('[role="option"][aria-disabled="true"]').text()).toContain('#99（不可用）')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('OpenAI 的默认分组不可用')
    await choose(defaultSelect(wrapper), sharedPool.noDefault)
    expect(defaultSelect(wrapper).props('modelValue')).toBe(0)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: {}, subscription_group_ids: {} }))
  })

  it('rejects incomplete and unsupported rules and permits removing legacy unsupported tiers', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { legacy_tier: 2 } } })
    expect(wrapper.get('[data-tier-select]').text()).toContain('legacy_tier（不支持的档位）')
    await open(wrapper.getComponent<typeof Select>('[data-tier-select]'))
    expect(wrapper.get('[data-tier-select] [role="option"][aria-disabled="true"]').text()).toContain('legacy_tier')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('支持的订阅档位')
    await choose(wrapper.getComponent<typeof Select>('[data-tier-select]'), 'Plus')
    await choose(wrapper.getComponent<typeof Select>('[data-group-select]'), sharedPool.subscriptionSelectGroup)
    expect(wrapper.getComponent<typeof Select>('[data-group-select]').props('modelValue')).toBe(0)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('同平台标准付费分组')
    await wrapper.get('[data-remove-rule]').trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ subscription_group_ids: {} }))
  })

  it('locks rule edits during save and reports a failed save without clearing changes', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: 2 } } })
    let reject!: (reason: Error) => void
    saveSettings.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    await open(wrapper.getComponent<typeof Select>('[data-tier-select]'))
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    for (const select of wrapper.findAllComponents(Select)) {
      expect(select.get('button').attributes('disabled')).toBeDefined()
      await select.get('button').trigger('click')
      expect(select.find('[role="listbox"]').exists()).toBe(false)
    }
    wrapper.getComponent<typeof Select>('[data-tier-select]').vm.$emit('update:modelValue', 'pro')
    wrapper.getComponent<typeof Select>('[data-group-select]').vm.$emit('update:modelValue', 1)
    defaultSelect(wrapper).vm.$emit('update:modelValue', 2)
    expect(wrapper.get('[data-remove-rule]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(saveSettings).toHaveBeenCalledTimes(1)
    reject(new Error('保存失败'))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('保存失败')
    expect(wrapper.getComponent<typeof Select>('[data-tier-select]').props('modelValue')).toBe('plus')
    expect(wrapper.getComponent<typeof Select>('[data-group-select]').props('modelValue')).toBe(2)
    expect(defaultSelect(wrapper).props('modelValue')).toBe(1)
    expect(wrapper.get('[data-remove-rule]').attributes('disabled')).toBeUndefined()
  })

  it('keeps accessible trigger labels and supports keyboard selection', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: 2 } } })
    for (const select of wrapper.findAllComponents(Select)) {
      const button = select.get('button')
      expect(button.attributes('aria-label')).toBeTruthy()
      expect(wrapper.find(`label[for="${button.attributes('id')}"]`).exists()).toBe(true)
    }
    const select = defaultSelect(wrapper)
    await select.get('button').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    await select.get('[role="listbox"]').trigger('keydown', { key: 'ArrowDown' })
    await select.get('[role="listbox"]').trigger('keydown', { key: 'Enter' })
    expect(select.props('modelValue')).toBe(2)
  })
})
