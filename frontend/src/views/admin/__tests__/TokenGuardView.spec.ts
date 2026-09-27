import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import TokenGuardView from '../ops/TokenGuardView.vue'
import * as guard from '@/api/admin/accountTokenGuard'
import { groupsAPI } from '@/api/admin/groups'
import type { TokenGuardJob, TokenGuardStats, TokenGuardStatus } from '@/api/admin/accountTokenGuard'

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/admin/operations/SmartOpsNav.vue', () => ({ default: { template: '<nav />' } }))
vi.mock('@/components/common/Select.vue', () => ({ default: { props: ['modelValue', 'options'], template: '<div />' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/accountTokenGuard', () => ({
  getTokenGuardStatus: vi.fn(), getTokenGuardJob: vi.fn(), cancelTokenGuardJob: vi.fn(),
  startTokenGuardRun: vi.fn(), saveTokenGuardConfig: vi.fn(), reloginTokenGuardAccount: vi.fn()
}))
vi.mock('@/api/admin/groups', () => ({ groupsAPI: { getAll: vi.fn() } }))

const stats = (): TokenGuardStats => ({ probed: 0, healthy: 0, auth_failed: 0, transient: 0,
  repaired: 0, state_fixed: 0, failed: 0, duration_ms: 0, started_at: 0 })
const job = (status = 'pending', completed = 0): TokenGuardJob => ({ id: 'job-1', status, manual: true,
  started_at: null, finished_at: null, total: 3, completed, stats: { ...stats(), probed: completed } })
const status = (): TokenGuardStatus => ({
  config: { enabled: true, activation_mode: 'builtin', group_ids: [21, 99], interval_seconds: 60,
    probe_endpoint: '', probe_model: 'test-model', probe_headers: {}, probe_timeout_seconds: 30,
    probe_concurrency: 2, max_probe_per_cycle: 20, auto_relogin: true, relogin_endpoint: '',
    relogin_headers: {}, relogin_accounts: [{ email: 'USER@example.com', password: 'test-password',
      mfa_secret: 'test-secret', disabled: true, expires_at: 2_000_000_000 }],
    restore_schedulable: true, fail_streak_threshold: 2, bark_key: '', notify_on_fix: false, notify_on_fail: false },
  accounts: [], events: [], runtime: { running: false, last_run: null, last_message: '', stats: stats() }
})
let wrapper: VueWrapper
async function mountView() { wrapper = mount(TokenGuardView); await flushPromises(); return wrapper.vm as any }
async function tick(ms = 1000) { await vi.advanceTimersByTimeAsync(ms); await flushPromises() }

beforeEach(() => {
  vi.useFakeTimers(); vi.resetAllMocks()
  vi.mocked(guard.getTokenGuardStatus).mockResolvedValue(status())
  vi.mocked(guard.startTokenGuardRun).mockResolvedValue(job())
  vi.mocked(guard.saveTokenGuardConfig).mockImplementation(async config => config)
  vi.mocked(groupsAPI.getAll).mockResolvedValue([])
})
afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

describe('token guard background jobs', () => {
  it('starts once, follows the returned job ID and reports progress and completion', async () => {
    vi.mocked(guard.getTokenGuardJob).mockResolvedValueOnce(job('running', 1)).mockResolvedValueOnce(job('succeeded', 3))
    const vm = await mountView(); const task = vm.run(); await flushPromises()
    await vm.run(); expect(guard.startTokenGuardRun).toHaveBeenCalledTimes(1)
    await tick()
    expect(guard.getTokenGuardJob).toHaveBeenCalledWith('job-1', expect.any(AbortSignal))
    expect(wrapper.get('[data-testid="guard-progress"]').text()).toBe('1/3')
    await tick(); await task
    expect(vm.running).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toBe('tokenGuard.runDone')
  })

  it('retains cancellation intent through stale polls and submits cancellation only once', async () => {
    vi.mocked(guard.cancelTokenGuardJob).mockResolvedValue({ ...job('running'), cancel_requested: true })
    vi.mocked(guard.getTokenGuardJob).mockResolvedValueOnce(job('running', 1)).mockResolvedValueOnce(job('canceled', 1))
    const vm = await mountView(); const task = vm.run(); await flushPromises()
    await vm.cancelRun(); await vm.cancelRun()
    expect(guard.cancelTokenGuardJob).toHaveBeenCalledOnce()
    await tick()
    expect(wrapper.get('[data-testid="cancel-guard-run"]').attributes('disabled')).toBeDefined()
    await tick(); await task
    expect(vm.running).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toBe('tokenGuard.runCanceled')
  })

  it('does not replace terminal cancellation with a previously requested running response', async () => {
    let resolvePoll!: (value: TokenGuardJob) => void
    vi.mocked(guard.getTokenGuardJob).mockImplementationOnce(() => new Promise(resolve => { resolvePoll = resolve }))
    vi.mocked(guard.cancelTokenGuardJob).mockResolvedValue(job('canceled', 1))
    const vm = await mountView(); const task = vm.run(); await flushPromises(); await tick()
    await vm.cancelRun(); resolvePoll(job('running', 1)); await flushPromises(); await task
    expect(vm.currentJob.status).toBe('canceled')
    expect(vm.notice).toBe('tokenGuard.runCanceled')
  })

  it.each(['missing', 'wrong-id'])('stops tracking a %s job and avoids restarting it from stale status', async reason => {
    const snapshot = status(); snapshot.runtime.job = job(); snapshot.runtime.running = true
    vi.mocked(guard.getTokenGuardStatus).mockResolvedValue(snapshot)
    if (reason === 'missing') vi.mocked(guard.getTokenGuardJob).mockRejectedValue({ response: { status: 404 } })
    else vi.mocked(guard.getTokenGuardJob).mockResolvedValue({ ...job(), id: 'other-job' })
    const vm = await mountView(); await tick()
    expect(vm.error).toBe('tokenGuard.jobUnavailable')
    expect(vm.running).toBe(false)
    await tick(30_000)
    expect(guard.getTokenGuardJob).toHaveBeenCalledTimes(1)
    expect(guard.startTokenGuardRun).not.toHaveBeenCalled()
  })

  it('stops after three consecutive polling failures without blocking further runs', async () => {
    vi.mocked(guard.getTokenGuardJob).mockRejectedValue(new Error('temporary network failure'))
    const vm = await mountView(); const task = vm.run(); await flushPromises()
    await tick(3000); await task
    expect(guard.getTokenGuardJob).toHaveBeenCalledTimes(3)
    expect(vm.running).toBe(false)
    expect(vm.error).toBe('tokenGuard.jobPollingFailed')
  })

  it('recovers a running job from status without starting another job', async () => {
    const snapshot = status(); snapshot.runtime.job = job('running', 1)
    vi.mocked(guard.getTokenGuardStatus).mockResolvedValue(snapshot)
    vi.mocked(guard.getTokenGuardJob).mockResolvedValue(job('succeeded', 3))
    const vm = await mountView()
    expect(vm.running).toBe(true); expect(guard.startTokenGuardRun).not.toHaveBeenCalled()
    await tick()
    expect(vm.notice).toBe('tokenGuard.runDone')
    expect(guard.getTokenGuardJob).toHaveBeenCalledTimes(1)
  })

  it('shows terminal and startup errors and releases the run lock', async () => {
    const vm = await mountView()
    vi.mocked(guard.startTokenGuardRun).mockRejectedValueOnce(new Error('start failed'))
    await vm.run(); expect(vm.error).toBe('start failed'); expect(vm.running).toBe(false)
    vi.mocked(guard.startTokenGuardRun).mockResolvedValueOnce({ ...job('failed'), error: 'inspection failed' })
    await vm.run(); expect(vm.error).toBe('inspection failed'); expect(vm.running).toBe(false)
  })

  it('refreshes summary statistics after another completed background round', async () => {
    const vm = await mountView()
    vi.mocked(guard.startTokenGuardRun).mockResolvedValueOnce(job('succeeded', 3))
    await vm.run(); await flushPromises()
    const next = status()
    next.runtime.stats = { ...stats(), probed: 9, repaired: 4, state_fixed: 2 }
    next.runtime.job = { ...job('succeeded', 9), id: 'job-2', stats: next.runtime.stats }
    vi.mocked(guard.getTokenGuardStatus).mockResolvedValueOnce(next)
    await vm.load(true); await flushPromises()
    expect(wrapper.findAll('.summary-card strong')[0].text()).toBe('9')
    expect(wrapper.findAll('.summary-card strong')[2].text()).toBe('4')
    expect(wrapper.findAll('.summary-card small')[2].text()).toBe('tokenGuard.stateFixed 2')
    expect(guard.getTokenGuardJob).not.toHaveBeenCalled()
  })

  it('aborts an in-flight poll and clears polling and refresh timers on unmount', async () => {
    vi.mocked(guard.getTokenGuardJob).mockImplementationOnce((_id, signal) => new Promise((_resolve, reject) => {
      signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true })
    }))
    const vm = await mountView(); const task = vm.run(); await flushPromises(); await tick()
    const signal = vi.mocked(guard.getTokenGuardJob).mock.calls[0][1]!
    wrapper.unmount(); await flushPromises(); await task
    expect(signal.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('token guard local configuration', () => {
  it('preserves dirty drafts, unknown selected groups and credential lifecycle fields', async () => {
    vi.mocked(groupsAPI.getAll).mockRejectedValue(new Error('group lookup failed'))
    const vm = await mountView()
    expect(vm.groupOptions).toEqual([{ value: 21, label: '#21' }, { value: 99, label: '#99' }])
    expect(vm.groupsLoadError).toBe(true)
    vm.draft.interval_seconds = 120
    const refresh = status(); refresh.config.activation_mode = 'external'; refresh.config.group_ids = []
    vi.mocked(guard.getTokenGuardStatus).mockResolvedValue(refresh)
    await vm.load(true)
    expect(vm.draft.activation_mode).toBe('builtin'); expect(vm.draft.interval_seconds).toBe(120)
    await vm.save()
    expect(guard.saveTokenGuardConfig).toHaveBeenCalledWith(expect.objectContaining({
      activation_mode: 'builtin', interval_seconds: 120, group_ids: [21, 99],
      relogin_accounts: [{ email: 'user@example.com', password: 'test-password', mfa_secret: 'test-secret',
        disabled: true, expires_at: 2_000_000_000 }]
    }))
  })
})
