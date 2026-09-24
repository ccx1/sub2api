import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexRequestStrategyView from '../CodexRequestStrategyView.vue'
import type { CodexRequestStrategyPolicy } from '@/api/admin/codexRequestStrategy'

const mocks = vi.hoisted(() => ({ load: vi.fn(), save: vi.fn(), settings: vi.fn(), updateSettings: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/codexRequestStrategy', () => ({
  getCodexRequestStrategyPolicy: mocks.load,
  saveCodexRequestStrategyPolicy: mocks.save,
}))
vi.mock('@/api/admin/settings', () => ({ getSettings: mocks.settings, updateSettings: mocks.updateSettings }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

const policy = (): CodexRequestStrategyPolicy => ({
  enabled: true, strategy: 'native', scope: 'dedicated', failure_mode: 'fallback',
  probe_timeout_seconds: 10, cookie_mode: 'preserve', route_affinity_mode: 'strict', route_prewarm_connections: 4,
  route_failure_cooldown_seconds: 120, region_mode: 'strip', account_routing_override: '',
  residency: '', time_context_mode: 'strip', timezone: '', compliance_mode: 'account',
})

let wrapper: ReturnType<typeof mount<typeof CodexRequestStrategyView>> | undefined
async function render() {
  wrapper = mount(CodexRequestStrategyView)
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.load.mockResolvedValue(policy())
  mocks.save.mockImplementation(async value => value)
  mocks.settings.mockResolvedValue({ openai_request_timezone: '' })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

describe('CodexRequestStrategyView route management', () => {
  it.each(['preserve', 'strip_routing', 'strip_cloudflare', 'strip_infrastructure'] as const)('loads and saves the %s cookie policy', async cookieMode => {
    const current = { ...policy(), cookie_mode: cookieMode, route_affinity_mode: 'prefer' as const }
    mocks.load.mockResolvedValueOnce(current)
    const page = await render()
    const field = page.get<HTMLSelectElement>('[data-testid="cookie-mode"]')
    expect(field.element.value).toBe(cookieMode)
    expect(field.findAll('option')).toHaveLength(4)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(current)
    expect(field.element.value).toBe(cookieMode)
  })

  it('defaults a legacy cookie policy to preserve and saves the default', async () => {
    mocks.load.mockResolvedValueOnce({ ...policy(), cookie_mode: undefined })
    const page = await render()
    expect(page.get<HTMLSelectElement>('[data-testid="cookie-mode"]').element.value).toBe('preserve')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(policy())
  })

  it.each(['strip_routing', 'strip_infrastructure'])('blocks %s with strict affinity and allows saving after switching affinity', async cookieMode => {
    const page = await render()
    await page.get('[data-testid="cookie-mode"]').setValue(cookieMode)
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(true)
    expect(page.text()).toContain('codexRequestStrategy.cookieStrictConflict')
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    await page.get('[data-testid="route-affinity-mode"]').setValue('prefer')
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(false)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ ...policy(), cookie_mode: cookieMode, route_affinity_mode: 'prefer' })
  })

  it('allows omitting Cloudflare state with strict affinity', async () => {
    const page = await render()
    await page.get('[data-testid="cookie-mode"]').setValue('strip_cloudflare')
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(false)
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ ...policy(), cookie_mode: 'strip_cloudflare' })
  })

  it('preserves disabled prewarming when loading, saving, and reloading', async () => {
    mocks.load.mockResolvedValueOnce({ ...policy(), route_prewarm_connections: 0 })
    const page = await render()
    const field = page.get<HTMLInputElement>('[data-testid="route-prewarm-connections"]')
    expect(field.element.value).toBe('0')
    expect(field.attributes('min')).toBe('0')
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(false)
    await page.get('[data-testid="route-affinity-mode"]').setValue('prefer')
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith({ ...policy(), route_affinity_mode: 'prefer', route_prewarm_connections: 0 })
    expect(field.element.value).toBe('0')
    page.unmount()
    mocks.load.mockResolvedValueOnce(mocks.save.mock.calls[0][0])
    const reloaded = await render()
    expect(reloaded.get<HTMLInputElement>('[data-testid="route-prewarm-connections"]').element.value).toBe('0')
  })

  it('loads defaults for legacy settings without route fields', async () => {
    mocks.load.mockResolvedValueOnce({ ...policy(), route_affinity_mode: undefined, route_prewarm_connections: undefined, route_failure_cooldown_seconds: undefined })
    const page = await render()
    expect(page.get<HTMLSelectElement>('[data-testid="route-affinity-mode"]').element.value).toBe('off')
    expect(page.get<HTMLInputElement>('[data-testid="route-prewarm-connections"]').element.value).toBe('4')
    expect(page.get<HTMLInputElement>('[data-testid="route-failure-cooldown"]').element.value).toBe('120')
  })

  it.each([
    ['route-prewarm-connections', '2.5', 'invalidRoutePrewarm'],
    ['route-prewarm-connections', '', 'invalidRoutePrewarm'],
    ['route-prewarm-connections', '13', 'invalidRoutePrewarm'],
    ['route-failure-cooldown', '120.5', 'invalidRouteCooldown'],
    ['route-failure-cooldown', '29', 'invalidRouteCooldown'],
  ])('blocks invalid %s input %s', async (field, value, error) => {
    const page = await render()
    await page.get(`[data-testid="${field}"]`).setValue(value)
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(true)
    expect(page.text()).toContain(`codexRequestStrategy.${error}`)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([Number.NaN, Number.POSITIVE_INFINITY])('blocks non-finite prewarm settings %s from the API', async value => {
    mocks.load.mockResolvedValueOnce({ ...policy(), route_prewarm_connections: value })
    const page = await render()
    expect(page.get<HTMLButtonElement>('[data-testid="save-strategy"]').element.disabled).toBe(true)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })
})
