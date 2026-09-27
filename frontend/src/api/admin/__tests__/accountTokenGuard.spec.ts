import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cancelTokenGuardJob, getTokenGuardJob, getTokenGuardStatus, reloginTokenGuardAccount,
  runTokenGuard, saveTokenGuardConfig, startTokenGuardRun, type TokenGuardConfig } from '../accountTokenGuard'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('../../client', () => ({ apiClient: client }))
beforeEach(() => { vi.resetAllMocks(); for (const method of Object.values(client)) method.mockResolvedValue({ data: { id: 'job-1' } }) })

describe('token guard API contracts', () => {
  it('starts a background inspection and returns the job', async () => {
    expect(await startTokenGuardRun()).toEqual({ id: 'job-1' })
    expect(client.post).toHaveBeenCalledWith('/admin/account-ops/token-guard/run/start')
  })
  it('encodes the job ID and forwards an abort signal when polling', async () => {
    const signal = new AbortController().signal
    expect(await getTokenGuardJob('job/1?x', signal)).toEqual({ id: 'job-1' })
    expect(client.get).toHaveBeenCalledWith('/admin/account-ops/token-guard/jobs/job%2F1%3Fx', { signal })
  })
  it('cancels only the specified job', async () => {
    await cancelTokenGuardJob('job/1')
    expect(client.post).toHaveBeenCalledWith('/admin/account-ops/token-guard/jobs/job%2F1/cancel')
  })
  it('keeps configuration lifecycle fields and the existing endpoints', async () => {
    const config = { activation_mode: 'builtin', relogin_accounts: [{ email: 'user@example.com',
      password: 'test-password', mfa_secret: 'test-secret', disabled: true, expires_at: 2_000_000_000 }] } as TokenGuardConfig
    await saveTokenGuardConfig(config)
    expect(client.put).toHaveBeenCalledWith('/admin/account-ops/token-guard/config', config)
    await getTokenGuardStatus(); expect(client.get).toHaveBeenCalledWith('/admin/account-ops/token-guard/status')
    await runTokenGuard(); expect(client.post).toHaveBeenCalledWith('/admin/account-ops/token-guard/run')
    await reloginTokenGuardAccount(42)
    expect(client.post).toHaveBeenCalledWith('/admin/account-ops/token-guard/accounts/42/relogin')
  })
})
