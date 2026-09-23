import { afterEach, describe, expect, it, vi } from 'vitest'
import { adminSharedPoolAPI, sharedPoolAPI, testSharedAccount } from '../sharedPool'

const { post, get, put } = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), put: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post, get, put }, buildApiUrl: (path: string) => `/api/v1${path}` }))

it('loads the aggregate resource overview without a group or owner filter', async () => {
  const data = { total_accounts: 10, available_accounts: 9, participating_accounts: 7, schedulable_accounts: 7, tiers: [] }
  get.mockResolvedValue({ data })
  expect(await sharedPoolAPI.overview()).toEqual(data)
  expect(get).toHaveBeenLastCalledWith('/shared-pool/overview')
})

it('includes dispatch consent only for an explicit authorization request', async () => {
  post.mockResolvedValue({ data: {} })
  await sharedPoolAPI.enable(7, true)
  expect(post).toHaveBeenLastCalledWith('/shared-pool/accounts/7/enabled', { enabled: true })
  await sharedPoolAPI.enable(7, true, true)
  expect(post).toHaveBeenLastCalledWith('/shared-pool/accounts/7/enabled', { enabled: true, dispatch_consent: true })
})

it('sends administrative scheduling and tier updates without adding dispatch consent', async () => {
  put.mockResolvedValue({ data: {} })
  const input = { enabled: true, admin_disabled: false, subscription_tier: 'pro' }
  await adminSharedPoolAPI.allocate(7, input)
  expect(put).toHaveBeenLastCalledWith('/admin/shared-pool/accounts/7', input)
  await adminSharedPoolAPI.allocate(7, { subscription_tier: '' })
  expect(put).toHaveBeenLastCalledWith('/admin/shared-pool/accounts/7', { subscription_tier: '' })
})

it('loads administrator earnings totals grouped by contributor', async () => {
  const data = [{ user_id: 6, email: 'owner@example.com', account_count: 2, earnings_count: 3, total_earned: 12.5, available: 5, pending: 2.5, transferred: 5 }]
  get.mockResolvedValue({ data })
  expect(await adminSharedPoolAPI.userEarnings()).toEqual(data)
  expect(get).toHaveBeenLastCalledWith('/admin/shared-pool/user-earnings')
})

it('reads and saves user auto-transfer settings without sending server-owned fields', async () => {
  const data = { enabled: false, threshold: 1, daily_time: '00:00', timezone: 'Asia/Shanghai', last_run_date: '2026-09-23' }
  get.mockResolvedValue({ data })
  expect(await sharedPoolAPI.autoTransferSettings()).toEqual(data)
  expect(get).toHaveBeenLastCalledWith('/shared-pool/auto-transfer')
  put.mockResolvedValue({ data })
  expect(await sharedPoolAPI.saveAutoTransferSettings(data)).toEqual(data)
  expect(put).toHaveBeenLastCalledWith('/shared-pool/auto-transfer', { enabled: false, threshold: 1, daily_time: '00:00' })
})

it('persists the user settlement override including an explicit inheritance reset', async () => {
  put.mockResolvedValue({ data: {} })
  for (const multiplier of [0, 0.5, null]) {
    const input = { platform_rate_bps: null, proxy_rate_bps: null, settlement_multiplier: multiplier }
    await adminSharedPoolAPI.saveUserRate(9, input)
    expect(put).toHaveBeenLastCalledWith('/admin/shared-pool/user-rates/9', input)
  }
})

it('uses the user import route and retains the operation key for long batches', async () => {
  const input = { sources: [{ name: 'auth.json', content: '{}' }], defaults: { concurrency: 1, enabled: false, protection_enabled: true } }
  const result = { created: 1, failed: 0 }
  post.mockResolvedValue({ data: result })
  expect(await sharedPoolAPI.importAccounts(input, 'retry-key')).toEqual(result)
  expect(post).toHaveBeenCalledWith('/shared-pool/accounts/import', input, { timeout: 300000, headers: { 'Idempotency-Key': 'retry-key' } })
})

it('loads usage through the owned account route without force or admin headers', async () => {
  const signal = new AbortController().signal
  const usage = { five_hour: { utilization: 35 } }
  get.mockResolvedValue({ data: usage })
  expect(await sharedPoolAPI.getUsage(7, 'passive', signal)).toEqual(usage)
  expect(get).toHaveBeenLastCalledWith('/shared-pool/accounts/7/usage', { params: { source: 'passive' }, signal })
})

it('updates tickets through the owner route and preserves explicit false', async () => {
  const account = { id: 7, codex_ticket_enabled: false }
  post.mockResolvedValue({ data: account })
  expect(await sharedPoolAPI.codexTicket(7, false)).toEqual(account)
  expect(post).toHaveBeenLastCalledWith('/shared-pool/accounts/7/codex-ticket', { enabled: false })
})

it('forces usage only for an explicit query and uses owner quota routes with the reset timeout', async () => {
  const signal = new AbortController().signal
  get.mockResolvedValue({ data: {} })
  await sharedPoolAPI.getUsage(7, 'active', signal, true)
  expect(get).toHaveBeenLastCalledWith('/shared-pool/accounts/7/usage', { params: { source: 'active', force: true }, signal })
  post.mockResolvedValue({ data: { cache_persisted: true } })
  expect(await sharedPoolAPI.refreshQuota(7)).toEqual({ cache_persisted: true })
  expect(post).toHaveBeenLastCalledWith('/shared-pool/accounts/7/quota/refresh')
  const reset = { code: 'ok', windows_reset: 2, cache_refreshed: false, account_state_recovered: true }
  post.mockResolvedValue({ data: reset })
  expect(await sharedPoolAPI.resetQuota(7)).toEqual(reset)
  expect(post).toHaveBeenLastCalledWith('/shared-pool/accounts/7/reset-quota', undefined, { timeout: 90_000 })
})

function stream(chunks: string[]) {
  return new Response(new ReadableStream({
    start(controller) {
      chunks.forEach(chunk => controller.enqueue(new TextEncoder().encode(chunk)))
      controller.close()
    }
  }), { status: 200 })
}

describe('shared account connection test', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads models from the owned shared account route', async () => {
    const models = [{ id: 'gpt-5.4', display_name: 'GPT-5.4' }]
    get.mockResolvedValue({ data: models })
    expect(await sharedPoolAPI.getAvailableModels(12)).toEqual(models)
    expect(get).toHaveBeenCalledWith('/shared-pool/accounts/12/models')
  })

  it('handles split events and the final frame without a newline', async () => {
    const fetchMock = vi.fn().mockResolvedValue(stream([
      'data: {"type":"test_sta', 'rt","model":"test-model"}\r\n\r\n',
      'data: {"type":"test_complete","success":true}'
    ]))
    vi.stubGlobal('fetch', fetchMock)
    const event = vi.fn()
    await testSharedAccount(12, event, new AbortController().signal)
    expect(event.mock.calls.map(call => call[0].type)).toEqual(['test_start', 'test_complete'])
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/shared-pool/accounts/12/test')
    expect(fetchMock.mock.calls[0][1].body).toBe('{}')
  })

  it('rejects a truncated stream rather than reporting success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(stream(['data: {"type":"test_start"}\n'])))
    await expect(testSharedAccount(1, vi.fn(), new AbortController().signal)).rejects.toThrow('before the test completed')
  })

  it('retains server error events and HTTP ownership errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(stream(['data: {"type":"error","error":"Proxy unavailable"}\n']))
      .mockResolvedValueOnce(new Response(JSON.stringify({ message: 'Not found' }), { status: 404 })))
    const event = vi.fn()
    await testSharedAccount(1, event, new AbortController().signal)
    expect(event).toHaveBeenCalledWith({ type: 'error', error: 'Proxy unavailable' })
    await expect(testSharedAccount(2, event, new AbortController().signal)).rejects.toThrow('Not found')
  })
})
