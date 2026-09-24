import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexModelQuality from '../CodexModelQuality.vue'
import type { CodexModelQualityPolicy } from '@/api/admin/codexModelQuality'

const mocks = vi.hoisted(() => ({ load: vi.fn(), save: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/api/admin/codexModelQuality', async original => ({
  ...await original<typeof import('@/api/admin/codexModelQuality')>(),
  getCodexModelQualityPolicy: mocks.load,
  saveCodexModelQualityPolicy: mocks.save
}))

const policy = (): CodexModelQualityPolicy => ({
  enabled: false, interval_seconds: 3600, timeout_seconds: 30, reserve_seconds: 30,
  max_ttl_percent: 10, concurrency: 1, retry_interval_seconds: 300,
  account_concurrency: 1,
  low_quality_consecutive_threshold: 3,
  low_quality_cooldown_seconds: 900,
  replacement_check_delay_seconds: 300,
  quarantine_on_failure: true, fingerprint_enabled: true, reasoning_effort: 'low'
})
let wrapper: ReturnType<typeof mount<typeof CodexModelQuality>> | undefined
function render() {
  wrapper = mount(CodexModelQuality, {
    props: { models: ['gpt-test'] },
    global: {
      stubs: { CodexModelQualityResults: true }
    }
  })
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.load.mockResolvedValue(policy())
  mocks.save.mockImplementation(async value => value)
})
afterEach(() => wrapper?.unmount())

describe('CodexModelQuality policy', () => {
  it('saves the low quality recovery policy and preserves the hidden legacy interval', async () => {
    const page = render()
    await flushPromises()
    expect(page.find('[data-testid="quality-interval_seconds"]').exists()).toBe(false)
    expect(page.get<HTMLInputElement>('[data-testid="quality-low_quality_consecutive_threshold"]').element.value).toBe('3')
    expect(page.get<HTMLInputElement>('[data-testid="quality-low_quality_cooldown_seconds"]').element.value).toBe('900')
    expect(page.get<HTMLInputElement>('[data-testid="quality-replacement_check_delay_seconds"]').element.value).toBe('300')
    await page.get('[data-testid="quality-low_quality_consecutive_threshold"]').setValue(5)
    await page.get('[data-testid="quality-low_quality_cooldown_seconds"]').setValue(1800)
    await page.get('[data-testid="quality-replacement_check_delay_seconds"]').setValue(600)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenLastCalledWith(expect.objectContaining({
      interval_seconds: 3600, low_quality_consecutive_threshold: 5,
      low_quality_cooldown_seconds: 1800, replacement_check_delay_seconds: 600
    }))
  })

  it.each([
    ['low_quality_consecutive_threshold', 0], ['low_quality_consecutive_threshold', 101],
    ['low_quality_consecutive_threshold', 2.5], ['low_quality_cooldown_seconds', 59],
    ['low_quality_cooldown_seconds', 86401], ['replacement_check_delay_seconds', 59],
    ['replacement_check_delay_seconds', 86401]
  ])('blocks an invalid recovery policy value %s=%s', async (key, value) => {
    const page = render()
    await flushPromises()
    await page.get(`[data-testid="quality-${key}"]`).setValue(value)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-save"]').element.disabled).toBe(true)
    expect(page.text()).toContain('codexModelQuality.invalidRange')
  })

  it('keeps edited values and persisted enable state after a failed save', async () => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-timeout_seconds"]').setValue(45)
    await page.get('[data-testid="quality-enabled"]').setValue(true)
    mocks.save.mockRejectedValueOnce(new Error('temporary save failure'))
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(page.get<HTMLInputElement>('[data-testid="quality-timeout_seconds"]').element.value).toBe('45')
    expect(page.get<HTMLInputElement>('[data-testid="quality-enabled"]').element.checked).toBe(true)
    expect(page.text()).toContain('temporary save failure')
    expect(page.getComponent({ name: 'CodexModelQualityResults' }).props('enabled')).toBe(false)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenLastCalledWith({ ...policy(), enabled: true, timeout_seconds: 45, model_priorities: undefined })
    expect(page.getComponent({ name: 'CodexModelQualityResults' }).props('enabled')).toBe(true)
  })

  it('saves an edited model priority', async () => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-priority-gpt-test"]').setValue('80')
    await page.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.save).toHaveBeenLastCalledWith(expect.objectContaining({
      model_priorities: { 'gpt-test': 80 }
    }))
  })

  it.each([-1, 101, 5.5])('blocks invalid model priority %s', async value => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-priority-gpt-test"]').setValue(String(value))
    await page.get('form').trigger('submit')

    expect(mocks.save).not.toHaveBeenCalled()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-save"]').element.disabled).toBe(true)
    expect(page.text()).toContain('codexModelQuality.invalidPriority')
  })

  it('clearing a model priority restores the default', async () => {
    mocks.load.mockResolvedValueOnce({ ...policy(), model_priorities: { 'gpt-test': 80 } })
    const page = render()
    await flushPromises()
    const input = page.get<HTMLInputElement>('[data-testid="quality-priority-gpt-test"]')
    expect(input.element.value).toBe('80')
    await input.setValue('')
    await page.get('form').trigger('submit')
    await flushPromises()

    const payload = mocks.save.mock.calls.at(-1)?.[0] as CodexModelQualityPolicy
    expect(payload.model_priorities).toBeUndefined()
  })

  it.each([0, 26, 5.5])('blocks unsafe lifetime percentages %s', async value => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-max_ttl_percent"]').setValue(value)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-save"]').element.disabled).toBe(true)
    expect(page.text()).toContain('codexModelQuality.invalidRange')
  })

  it('locks policy submission until the request completes', async () => {
    const page = render()
    await flushPromises()
    let finish!: (value: CodexModelQualityPolicy) => void
    mocks.save.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await page.get('form').trigger('submit')
    await page.get('form').trigger('submit')
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(page.get('fieldset').attributes()).toHaveProperty('disabled')
    finish(policy())
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-save"]').element.disabled).toBe(false)
  })
})
