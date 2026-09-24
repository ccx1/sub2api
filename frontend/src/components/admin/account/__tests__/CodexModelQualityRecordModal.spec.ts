import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexModelQualityRecordModal from '../CodexModelQualityRecordModal.vue'
import type { ModelQualityStatus } from '@/api/admin/codexModelQuality'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  diagnose: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/api/admin/codexModelQuality', () => ({
  getCodexModelQuality: mocks.load,
  diagnoseCodexModelQuality: mocks.diagnose
}))

const status = (overrides: Partial<ModelQualityStatus> = {}): ModelQualityStatus => ({
  account_id: 41,
  model: 'gpt-6-astra',
  status: 'passed',
  reason: 'capability_passed',
  checked_at: '2026-09-24T00:00:00Z',
  sample_count: 1,
  requests: 1,
  model_identity: 'unknown',
  source: 'diagnostic',
  ...overrides
})

function render() {
  return mount(CodexModelQualityRecordModal, {
    props: { show: true, account: { id: 41, name: 'test-account' } },
    global: {
      stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
        CodexModelQualityResult: { props: ['item'], template: '<div data-testid="quality-result">{{ item.status }}:{{ item.reason }}</div>' }
      }
    }
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
})
afterEach(() => vi.useRealTimers())

describe('CodexModelQualityRecordModal', () => {
  it('keeps a scheduled diagnosis visible while the worker is running and never opens a window', async () => {
    const initial = status()
    const running = status({ status: 'running', reason: 'checking', checked_at: undefined })
    const finished = status({ checked_at: '2026-09-24T00:00:03Z' })
    mocks.load.mockResolvedValueOnce([initial]).mockResolvedValueOnce([running]).mockResolvedValueOnce([finished])
    mocks.diagnose.mockResolvedValue({
      account_id: 41,
      items: [{ model: initial.model, scheduled: true, reason: 'scheduled', current: initial }]
    })
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const page = render()
    await flushPromises()
    await page.get('[data-testid="codex-model-quality-diagnose"]').trigger('click')
    await flushPromises()

    expect(open).not.toHaveBeenCalled()
    expect(page.get('[data-testid="quality-result"]').text()).toContain('running:checking')

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(page.get('[data-testid="quality-result"]').text()).toContain('passed:capability_passed')
    open.mockRestore()
  })

  it('shows the backend reason when no model is scheduled', async () => {
    mocks.load.mockResolvedValue([])
    mocks.diagnose.mockResolvedValue({
      account_id: 41,
      items: [{ model: 'gpt-6-astra', scheduled: false, reason: 'no_ticket' }]
    })
    const page = render()
    await flushPromises()
    await page.get('[data-testid="codex-model-quality-diagnose"]').trigger('click')
    await flushPromises()
    expect(page.get('[data-testid="codex-model-quality-diagnosis-reasons"]').text()).toContain('no_ticket')
  })
})
