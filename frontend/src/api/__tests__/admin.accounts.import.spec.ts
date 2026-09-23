import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AdminDataPayload, CreateAccountRequest } from '@/types'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { batchCreate, importData } from '@/api/admin/accounts'

describe('account import options', () => {
  const accounts: CreateAccountRequest[] = [{
    name: 'imported', platform: 'openai', type: 'oauth',
    credentials: { access_token: 'fixture-token' }, concurrency: 1, priority: 1
  }]
  const data: AdminDataPayload = { proxies: [], accounts }

  beforeEach(() => post.mockReset())

  it.each([[true, true], [true, false], [false, true], [false, false]])(
    'forwards independent protection=%s and ticket=%s switches on both paths',
    async (protection, ticket) => {
      const options = { protection_enabled: protection, codex_ticket_enabled: ticket }
      post.mockResolvedValueOnce({ data: { success: 1, failed: 0, results: [{ success: true, id: 71 }] } })
      const batch = await batchCreate(accounts, options)
      expect(post).toHaveBeenLastCalledWith('/admin/accounts/batch', { accounts, ...options })
      expect(batch.results[0]?.id).toBe(71)

      post.mockResolvedValueOnce({ data: { account_created: 1, account_ids: [71] } })
      const imported = await importData({ data, skip_default_group_bind: true, ...options })
      expect(post).toHaveBeenLastCalledWith('/admin/accounts/data', { data, skip_default_group_bind: true, ...options })
      expect(imported.account_ids).toEqual([71])
    }
  )

  it('keeps legacy batch calls free of creation overrides', async () => {
    post.mockResolvedValueOnce({ data: { success: 1, failed: 0, results: [] } })
    await batchCreate(accounts)
    expect(post).toHaveBeenCalledWith('/admin/accounts/batch', { accounts })
  })
})
