import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SharedPoolSettingsForm from '../SharedPoolSettingsForm.vue'
import Select from '@/components/common/Select.vue'
import CodexTicketTagSelect from '@/components/admin/CodexTicketTagSelect.vue'
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
const persistedOriginal = { ...original, default_group_ids: { openai: [1], gemini: [5], antigravity: [6], anthropic: [9] } }
function render(settings: SharedSettings = original) {
  return mount(SharedPoolSettingsForm, {
    props: { settings: structuredClone(settings), groups },
    global: {
      stubs: { RouterLink: true, teleport: true }
    }
  })
}
function defaultSelect(wrapper: VueWrapper, platform = 'openai') {
  return wrapper.getComponent<typeof CodexTicketTagSelect>(`[data-default-platform="${platform}"] [data-testid="tag-select"]`)
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
    expect(saveSettings).toHaveBeenCalledWith({ ...persistedOriginal, subscription_group_ids: {}, default_priority: priority })
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
    expect(saveSettings).toHaveBeenCalledWith({ ...persistedOriginal, subscription_group_ids: {} })
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('saves one global settlement multiplier independently of subscription group rules', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: [2] } } })
    expect(wrapper.findAll('[data-settlement-default]')).toHaveLength(1)
    await wrapper.get('[data-settlement-default]').setValue(0)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...persistedOriginal, settlement_multiplier: 0, subscription_group_ids: { openai: { plus: [2] } } })
  })

  it.each(['', '-1', '101'])('rejects invalid global settlement multiplier %s', async value => {
    const wrapper = render()
    await wrapper.get('[data-settlement-default]').setValue(value)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('结算倍率必须在 0 到 100 之间')
  })

  it('adds a supported tier and preserves defaults, rates and other platform rules', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { gemini: { google_ai_pro: [5] }, antigravity: { ultra: [6] } } })
    await addRule(wrapper, 'openai', 'Pro 5x', 2)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...persistedOriginal, subscription_group_ids: { openai: { prolite: [2] }, gemini: { google_ai_pro: [5] }, antigravity: { ultra: [6] } } })
    expect(showSuccess).toHaveBeenCalledWith('已保存')
  })

  it('removes the last rule and persists an empty map to clear old settings', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: [2] } } })
    await wrapper.get('[data-remove-rule]').trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith({ ...persistedOriginal, subscription_group_ids: {} })
  })

  it('offers active paid standard groups, including exclusive groups, for tier routing', async () => {
    const wrapper = render()
    await wrapper.get('[data-tier-platform="openai"] [data-add-rule]').trigger('click')
    const openaiSelect = wrapper.getComponent<typeof Select>('[data-group-select]')
    await open(openaiSelect)
    expect(openaiSelect.findAll('[role="option"]').map(option => option.text())).toEqual(['Group 1 · 1x', 'Group 2 · 1x', 'Group 4 · 1x', 'Group 7 · 1x'])
    expect(openaiSelect.props('modelValue')).toEqual([])
    expect(openaiSelect.props('options').map(option => option.value)).toEqual(['1', '2', '4', '7'])
    await wrapper.get('[data-tier-platform="gemini"] [data-add-rule]').trigger('click')
    const geminiSelect = wrapper.getComponent<typeof Select>('[data-tier-platform="gemini"] [data-group-select]')
    await open(geminiSelect)
    expect(geminiSelect.findAll('[role="option"]').map(option => option.text())).toEqual(['Group 5 · 1x'])
    expect(defaultSelect(wrapper).props('options').map(option => option.value)).toEqual(['1', '2', '4'])
    expect(defaultSelect(wrapper, 'gemini').props('options').map(option => option.value)).toEqual(['5'])
  })

  it('persists an exclusive group selected for a subscription tier without exposing it as a default', async () => {
    const wrapper = render()
    await addRule(wrapper, 'openai', 'Plus', 7)
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({
      default_group_ids: persistedOriginal.default_group_ids,
      subscription_group_ids: { openai: { plus: [7] } }
    }))
    expect(defaultSelect(wrapper).props('options').map(option => option.value)).not.toContain('7')
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
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: { gemini: [5] }, subscription_group_ids: { openai: { pro: [2] } } }))
    await choose(defaultSelect(wrapper).getComponent(Select), 'Group 1 · 1x')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: { openai: [1], gemini: [5] }, subscription_group_ids: { openai: { pro: [2] } } }))
  })

  it.each([3, 5, 99])('keeps an unavailable target visible and requires a replacement or removal (%s)', async target => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: [target] } } })
    const targetSelect = wrapper.getComponent<typeof Select>('[data-group-select]')
    expect(targetSelect.props('modelValue')).toEqual([String(target)])
    await open(targetSelect)
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('同平台标准付费分组')
    await choose(wrapper.getComponent<typeof Select>('[data-group-select]'), 'Group 2 · 1x')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ subscription_group_ids: { openai: { plus: [2] } } }))
  })

  it('does not silently replace an unavailable default group', async () => {
    const wrapper = render({ ...original, default_group_ids: { openai: 99 } })
    expect(defaultSelect(wrapper).props('modelValue')).toEqual(['99'])
    expect(defaultSelect(wrapper).get('[data-testid="selected-tag"]').text()).toContain('#99（不可用）')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('OpenAI 的默认分组不可用')
    await defaultSelect(wrapper).get('[data-testid="tag-remove-99"]').trigger('click')
    expect(defaultSelect(wrapper).props('modelValue')).toEqual([])
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: {}, subscription_group_ids: {} }))
  })

  it('rejects incomplete and unsupported rules and permits removing legacy unsupported tiers', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { legacy_tier: [2] } } })
    expect(wrapper.get('[data-tier-select]').text()).toContain('legacy_tier（不支持的档位）')
    await open(wrapper.getComponent<typeof Select>('[data-tier-select]'))
    expect(wrapper.get('[data-tier-select] [role="option"][aria-disabled="true"]').text()).toContain('legacy_tier')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('支持的订阅档位')
    await choose(wrapper.getComponent<typeof Select>('[data-tier-select]'), 'Plus')
    await wrapper.get('[data-group-select] [data-testid="tag-remove-2"]').trigger('click')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('同平台标准付费分组')
    await wrapper.get('[data-remove-rule]').trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ subscription_group_ids: {} }))
  })

  it('locks rule edits during save and reports a failed save without clearing changes', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: [2] } } })
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
    wrapper.getComponent<typeof Select>('[data-group-select]').vm.$emit('update:modelValue', ['1'])
    defaultSelect(wrapper).vm.$emit('update:modelValue', ['2'])
    expect(wrapper.get('[data-remove-rule]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(saveSettings).toHaveBeenCalledTimes(1)
    reject(new Error('保存失败'))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('保存失败')
    expect(wrapper.getComponent<typeof Select>('[data-tier-select]').props('modelValue')).toBe('plus')
    expect(wrapper.getComponent<typeof Select>('[data-group-select]').props('modelValue')).toEqual(['2'])
    expect(defaultSelect(wrapper).props('modelValue')).toEqual(['1'])
    expect(wrapper.get('[data-remove-rule]').attributes('disabled')).toBeUndefined()
  })

  it('keeps accessible trigger labels and supports keyboard selection', async () => {
    const wrapper = render({ ...original, subscription_group_ids: { openai: { plus: [2] } } })
    for (const select of wrapper.findAllComponents(Select)) {
      const button = select.get('button')
      expect(button.attributes('aria-label')).toBeTruthy()
      expect(wrapper.find(`label[for="${button.attributes('id')}"]`).exists()).toBe(true)
    }
    const select = defaultSelect(wrapper).getComponent(Select)
    await select.get('button').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    await select.get('[role="listbox"]').trigger('keydown', { key: 'ArrowDown' })
    await select.get('[role="listbox"]').trigger('keydown', { key: 'Enter' })
    expect(defaultSelect(wrapper).props('modelValue')).toEqual(['1', '4'])
  })

  it('loads scalar defaults as tags, adds several groups with search, and removes a tag', async () => {
    const wrapper = render()
    const tags = defaultSelect(wrapper)
    expect(tags.findAll('[data-testid="selected-tag"]').map(tag => tag.text())).toEqual(['Group 1 · 1x'])
    let select = tags.getComponent(Select)
    await open(select)
    await select.get('input[type="text"]').setValue('Group 2')
    expect(select.findAll('[role="option"]').map(option => option.text())).toEqual(['Group 2 · 1x'])
    await select.get('[role="option"]').trigger('click')
    await flushPromises()
    select = tags.getComponent(Select)
    await choose(select, 'Group 4 · 1x')
    expect(tags.props('modelValue')).toEqual(['1', '2', '4'])
    await tags.get('[data-testid="tag-remove-2"]').trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({
      default_group_ids: { ...persistedOriginal.default_group_ids, openai: [1, 4] }
    }))
    expect(wrapper.get('[data-tier-platform="openai"]').text()).toContain('Group 1, Group 4')
  })

  it('deduplicates array defaults and keeps tier rules as group ID arrays', async () => {
    const wrapper = render({ ...original, default_group_ids: { openai: [1, 2, 1] }, subscription_group_ids: { openai: { pro: [4] } } })
    expect(defaultSelect(wrapper).props('modelValue')).toEqual(['1', '2'])
    expect(wrapper.getComponent<typeof Select>('[data-group-select]').props('modelValue')).toEqual(['4'])
    expect(wrapper.get('[data-group-select] [data-testid="selected-tag"]').text()).toContain('Group 4 · 1x')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({
      default_group_ids: { openai: [1, 2] }, subscription_group_ids: { openai: { pro: [4] } }
    }))
  })

  it.each([3, 5, 99])('blocks saving a mixed default selection until the unavailable tag is removed (%s)', async invalid => {
    const wrapper = render({ ...original, default_group_ids: { openai: [1, invalid, 2] } })
    expect(defaultSelect(wrapper).findAll('[data-testid="selected-tag"]')).toHaveLength(3)
    expect(defaultSelect(wrapper).text()).toContain('不可用')
    await save(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    await defaultSelect(wrapper).get(`[data-testid="tag-remove-${invalid}"]`).trigger('click')
    await save(wrapper)
    expect(saveSettings).toHaveBeenCalledWith(expect.objectContaining({ default_group_ids: { openai: [1, 2] } }))
  })

  it('locks tag removal and an already-open group menu while saving', async () => {
    const wrapper = render({ ...original, default_group_ids: { openai: [1, 2] } })
    let resolve!: (value: SharedSettings) => void
    saveSettings.mockReturnValueOnce(new Promise(complete => { resolve = complete }))
    await open(defaultSelect(wrapper).getComponent(Select))
    await wrapper.get('form').trigger('submit')
    expect(defaultSelect(wrapper).find('[role="listbox"]').exists()).toBe(false)
    const remove = defaultSelect(wrapper).get('[data-testid="tag-remove-1"]')
    expect(remove.attributes('disabled')).toBeDefined()
    await remove.trigger('click')
    defaultSelect(wrapper).vm.$emit('update:modelValue', [])
    expect(defaultSelect(wrapper).props('modelValue')).toEqual(['1', '2'])
    resolve({ ...original, default_group_ids: { openai: [1, 2] } })
    await flushPromises()
    expect(defaultSelect(wrapper).get('[data-testid="tag-remove-1"]').attributes('disabled')).toBeUndefined()
  })
})
