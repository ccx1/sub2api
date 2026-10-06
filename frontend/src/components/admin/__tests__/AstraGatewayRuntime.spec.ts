import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AstraGatewayRuntime from '../AstraGatewayRuntime.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn(), test: vi.fn(), exists: vi.fn() }))
vi.mock('@/api/admin/astraGateway', () => ({ getAstraGatewayRuntime: mocks.get, testAstraGateway: mocks.test }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: mocks.exists }) }))
const settings = { cookie_pool: { enabled: true, source_account_ids: [299], target_account_ids: [300] }, ws_session: { enabled: true, account_ids: [300] }, revision: 'one' }
function snapshot() { return { generated_at: new Date().toISOString(), revision: 'one', sources: [{ account_id: 299, state: 'ready', reason: 'qualified', expires_at: new Date(Date.now() + 120000).toISOString(), gateway: 'chat.gateway.unified-196.api.openai.com' }], targets: [{ account_id: 300, reason: 'route_available' }], ws: [{ account_id: 300, ready: false, reason: 'account_disabled', active_sessions: 0 }], ready_routes: 1, preparing: false } }
beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks(); mocks.exists.mockReturnValue(true); mocks.get.mockResolvedValue(snapshot()); mocks.test.mockResolvedValue({ action: 'prepare', success: true, reason: 'success', duration_ms: 1000 }) })
afterEach(() => vi.useRealTimers())
describe('Astra gateway runtime', () => {
  it('never shows a countdown for failed or unverified routes', async () => {
    const data = snapshot(); data.sources[0].state = 'candidate'; data.sources[0].reason = 'source_passed_target_failed'; data.ready_routes = 0
    mocks.get.mockResolvedValue(data)
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.find('tbody').text()).not.toContain('120 s'); expect(w.find('tbody').text()).toContain('—'); w.unmount()
  })
  it('shows automatic setup progress and disables competing tests', async () => {
    mocks.get.mockResolvedValue({ ...snapshot(), setup: { revision: 'one', state: 'running', phase: 'target', account_id: 300, reason: '' } })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.get('[data-testid="setup-status"]').text()).toContain('#300'); expect(w.get('[data-testid="prepare"]').attributes('disabled')).toBeDefined(); w.unmount()
  })
  it('renders dynamic selection failures with readable hints and preserves unknown reasons', async () => {
    mocks.exists.mockReturnValue(false)
    mocks.get.mockResolvedValue({ ...snapshot(), ready_routes: 0,
      setup: { revision: 'one', state: 'failed', phase: '', reason: 'astra_group_accounts_empty' },
      targets: ['astra_account_overlap', 'astra_ws_outside_targets', 'astra_selection_invalid', 'astra_group_selection_invalid', 'astra_group_unknown_reason'].map((reason, i) => ({ account_id: 300 + i, reason }))
    })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.get('[data-testid="setup-status"]').text()).toContain('admin.astraGateway.groupAccountsEmpty')
    expect(w.text()).toContain('admin.astraGateway.overlap')
    expect(w.text()).toContain('admin.astraGateway.wsOutsideTargets')
    expect(w.text()).toContain('admin.astraGateway.selectionInvalid')
    expect(w.text()).toContain('astra_group_unknown_reason')
    expect(w.text()).not.toContain('admin.astraGateway.reasons.astra_group_unknown_reason')
    w.unmount()
  })
  it('labels a recorded busy result as history without claiming a test is still running', async () => {
    mocks.get.mockResolvedValue({ ...snapshot(), last_test: { action: 'verify', account_id: 300, reason: 'target_validation_in_progress', duration_ms: 200, checked_at: '2026-10-06T14:02:56Z' } })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.get('[data-testid="last-test"]').text()).toContain('admin.astraGateway.lastTestTitle')
    expect(w.get('[data-testid="last-test"]').text()).toContain('admin.astraGateway.lastTestHint')
    expect(w.find('[data-testid="current-test"]').exists()).toBe(false)
    expect(w.get('[data-testid="prepare"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })
  it('shows the pending manual test until the request completes and prevents duplicates', async () => {
    let finish!: (result: unknown) => void
    mocks.test.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    await w.get('[data-testid="prepare"]').trigger('click')
    expect(w.get('[data-testid="current-test"]').text()).toContain('admin.astraGateway.manualTesting')
    await w.get('[data-testid="prepare"]').trigger('click')
    expect(mocks.test).toHaveBeenCalledTimes(1)
    finish({ action: 'prepare', reason: 'success', duration_ms: 2000 }); await flushPromises()
    expect(w.find('[data-testid="current-test"]').exists()).toBe(false)
    expect(w.get('[data-testid="last-test"]').text()).toContain('admin.astraGateway.lastTestTitle')
    w.unmount()
  })
  it('verifies targets with the shared state probe and exposes no candy selector', async () => {
    mocks.test.mockResolvedValue({ action: 'verify', account_id: 300, test_kind: 'state_probe', success: false, reason: 'target_probe_degraded', duration_ms: 2000 })
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.find('[data-testid="test-kind"]').exists()).toBe(false)
    expect(w.get('[data-testid="probe-hint"]').text()).toContain('admin.astraGateway.probeHint')
    await w.findAll('button').find(b => b.text() === 'admin.astraGateway.verifyTarget')!.trigger('click'); await flushPromises()
    expect(mocks.test).toHaveBeenCalledWith('verify', 300, 'state_probe')
    expect(w.find('[role="status"]').text()).toContain('target_probe_degraded'); w.unmount()
  })
  it('shows route lifetime, blocks invalid WS and refreshes status', async () => {
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    expect(w.text()).toContain('120 s'); expect(w.text()).toContain('unified-196')
    const ws = w.findAll('button').find(b => b.text() === 'admin.astraGateway.verifyWS')!
    expect(ws.attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(5000); await flushPromises(); expect(mocks.get).toHaveBeenCalledTimes(2)
    w.unmount(); await vi.advanceTimersByTimeAsync(5000); expect(mocks.get).toHaveBeenCalledTimes(2)
  })
  it('prepares a route and shows the result', async () => {
    const w = mount(AstraGatewayRuntime, { props: { settings, dirty: false } }); await flushPromises()
    await w.get('[data-testid="prepare"]').trigger('click'); await flushPromises()
    expect(mocks.test).toHaveBeenCalledWith('prepare', 0, 'state_probe'); expect(w.find('[role="status"]').exists()).toBe(true)
    await w.setProps({ dirty: true }); expect(w.get('[data-testid="prepare"]').attributes('disabled')).toBeDefined(); w.unmount()
  })
})
