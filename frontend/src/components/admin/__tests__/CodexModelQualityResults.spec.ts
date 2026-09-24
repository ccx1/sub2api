import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexModelQualityResults from '../CodexModelQualityResults.vue'
import type { ModelQualityStatus } from '@/api/admin/codexModelQuality'

const mocks = vi.hoisted(() => ({ load: vi.fn(), schedule: vi.fn(), accounts: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/api/admin/codexModelQuality', () => ({ getCodexModelQuality: mocks.load, scheduleCodexModelQuality: mocks.schedule }))
vi.mock('@/api/admin/accounts', () => ({ list: mocks.accounts }))
const status = (id = 10, overrides: Partial<ModelQualityStatus> = {}): ModelQualityStatus => ({
  account_id: id, model: 'gpt-test', status: 'passed', reason: 'capability_passed',
  sample_count: 2, requests: 1, model_identity: 'unknown', source: 'manual', capability_score: 100,
  ...overrides
})
let wrapper: ReturnType<typeof mount<typeof CodexModelQualityResults>> | undefined
function render() {
  wrapper = mount(CodexModelQualityResults, {
    props: { models: ['gpt-test'], enabled: true }
  })
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.accounts.mockResolvedValue({ total: 4, items: [
    { id: 10, name: 'First account', type: 'oauth' }, { id: 20, name: 'Second account', type: 'setup-token' },
    { id: 30, name: 'Shadow account', type: 'oauth', parent_account_id: 10 }, { id: 40, name: 'API Key account', type: 'apikey' }
  ] })
  mocks.load.mockResolvedValue([])
  mocks.schedule.mockResolvedValue({ scheduled: true, reason: 'scheduled' })
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

describe('CodexModelQuality results', () => {
  it('shows the failure streak and cooldown and blocks rechecks until cooldown ends', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-24T12:00:00Z'))
    const pausedUntil = '2026-09-24T12:15:00Z'
    mocks.load.mockResolvedValue([status(10, {
      status: 'quarantined', reason: 'capability_failed', consecutive_low_quality: 3,
      quality_paused_until: pausedUntil
    })])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    expect(page.get('[data-testid="quality-consecutive-count"]').text()).toBe('3')
    expect(page.get('[data-testid="quality-paused-until"]').text()).toBe(new Date(pausedUntil).toLocaleString())
    expect(page.find('[data-testid="quality-cooldown-notice"]').exists()).toBe(true)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    await page.get('[data-testid="quality-retest"]').trigger('click')
    expect(mocks.schedule).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(899999)
    expect(mocks.load).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(51)
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledTimes(2)
    expect(page.find('[data-testid="quality-cooldown-notice"]').exists()).toBe(false)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
  })

  it('does not block another model while the selected account has a cooling model', async () => {
    mocks.load.mockResolvedValue([
      status(10, { quality_paused_until: new Date(Date.now() + 60000).toISOString(), consecutive_low_quality: 3 }),
      status(10, { model: 'gpt-other', status: 'pending', reason: 'not_checked' })
    ])
    const page = render()
    await page.setProps({ models: ['gpt-test', 'gpt-other'] })
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    await page.get('[data-testid="quality-model"]').setValue('gpt-other')
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
  })

  it.each(['not_checked', 'no_ticket'])('allows a first manual check for pending %s', async reason => {
    vi.useFakeTimers()
    mocks.load.mockResolvedValue([status(10, { status: 'pending', reason })])
    mocks.schedule.mockResolvedValueOnce({ scheduled: false, reason: 'no_ticket' })
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledWith(10, { schedule: false })
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.load).toHaveBeenCalledTimes(1)
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await flushPromises()
    expect(mocks.schedule).toHaveBeenCalledWith(10, 'gpt-test')
    expect(page.get('[data-testid="quality-schedule-notice"]').text()).toContain('no_ticket')
  })

  it('keeps polling an accepted check until the previous result changes', async () => {
    vi.useFakeTimers()
    const previous = status(10, { checked_at: '2026-09-23T12:00:00Z' })
    mocks.load.mockResolvedValueOnce([previous]).mockResolvedValueOnce([previous])
    mocks.load.mockResolvedValueOnce([status(10, { checked_at: '2026-09-23T12:05:00Z' })])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledTimes(3)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
    await vi.advanceTimersByTimeAsync(10000)
    expect(mocks.load).toHaveBeenCalledTimes(3)
  })

  it('expires a local scheduling wait when no new result arrives', async () => {
    vi.useFakeTimers()
    mocks.load.mockResolvedValue([status(10)])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    await vi.advanceTimersByTimeAsync(180000)
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
    const requests = mocks.load.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(mocks.load).toHaveBeenCalledTimes(requests)
  })

  it('explains an insufficient lifetime budget without presenting a capability failure', async () => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    mocks.schedule.mockResolvedValueOnce({ scheduled: false, reason: 'insufficient_ttl' })
    mocks.load.mockResolvedValueOnce([status(10, { status: 'skipped', reason: 'insufficient_ttl', capability_score: undefined })])
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await flushPromises()
    expect(page.get('[data-testid="quality-schedule-notice"]').text()).toContain('insufficient_ttl')
    expect(page.text()).toContain('codexModelQuality.incompleteHint')
    expect(page.text()).not.toContain('codexModelQuality.status.quarantined')
    expect(page.text()).not.toContain('Shadow account')
    expect(page.text()).not.toContain('API Key account')
    expect(page.text()).toContain('Second account')
    expect(mocks.accounts).toHaveBeenCalledWith(1, 20, expect.objectContaining({ platform: 'openai' }))
  })

  it('does not show a late result under another account', async () => {
    let finish!: (value: ModelQualityStatus[]) => void
    mocks.load.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    mocks.load.mockResolvedValueOnce([status(20, { reason: 'second account result', capability_score: 75 })])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await page.get('[data-testid="quality-account"]').setValue('20')
    await flushPromises()
    expect(page.text()).toContain('second account result')
    finish([status(10, { reason: 'first account late result' })])
    await flushPromises()
    expect(page.text()).toContain('second account result')
    expect(page.text()).not.toContain('first account late result')
  })

  it('locks repeated recheck submissions until the request completes', async () => {
    let finish!: (value: { scheduled: boolean; reason: string }) => void
    mocks.schedule.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await page.get('[data-testid="quality-retest"]').trigger('click')
    expect(mocks.schedule).toHaveBeenCalledTimes(1)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    finish({ scheduled: false, reason: 'cooldown' })
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
  })

  it('does not show a late scheduling notice under another account', async () => {
    let finish!: (value: { scheduled: boolean; reason: string }) => void
    mocks.schedule.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await page.get('[data-testid="quality-account"]').setValue('20')
    await flushPromises()
    finish({ scheduled: true, reason: 'scheduled' })
    await flushPromises()
    expect(page.find('[data-testid="quality-schedule-notice"]').exists()).toBe(false)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
  })

  it('does not show a late scheduling notice under another model', async () => {
    let finish!: (value: { scheduled: boolean; reason: string }) => void
    mocks.schedule.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const page = render()
    await page.setProps({ models: ['gpt-test', 'gpt-other'] })
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-retest"]').trigger('click')
    await page.get('[data-testid="quality-model"]').setValue('gpt-other')
    finish({ scheduled: true, reason: 'scheduled' })
    await flushPromises()
    expect(page.find('[data-testid="quality-schedule-notice"]').exists()).toBe(false)
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
  })

  it('retries an interrupted poll and stops polling when the run finishes', async () => {
    vi.useFakeTimers()
    mocks.load.mockResolvedValueOnce([status(10, { status: 'running' })])
    mocks.load.mockRejectedValueOnce(new Error('temporary network failure'))
    mocks.load.mockResolvedValueOnce([status(10)])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(page.text()).toContain('temporary network failure')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(page.text()).not.toContain('temporary network failure')
    expect(page.get<HTMLButtonElement>('[data-testid="quality-retest"]').element.disabled).toBe(false)
    await vi.advanceTimersByTimeAsync(15000)
    expect(mocks.load).toHaveBeenCalledTimes(3)
  })

  it('cancels polling on account deselection and component unmount', async () => {
    vi.useFakeTimers()
    mocks.load.mockResolvedValue([status(10, { status: 'pending', reason: 'scheduled' })])
    const page = render()
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    await page.get('[data-testid="quality-account"]').setValue('')
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.load).toHaveBeenCalledTimes(1)
    await page.get('[data-testid="quality-account"]').setValue('10')
    await flushPromises()
    page.unmount()
    wrapper = undefined
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.load).toHaveBeenCalledTimes(2)
  })
})
