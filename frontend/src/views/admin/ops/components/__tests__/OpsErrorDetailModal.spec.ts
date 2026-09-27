import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpsErrorDetailModal from '../OpsErrorDetailModal.vue'
import { observerUsageContext } from '@/components/admin/usage/observerUsageContext'

const mocks = vi.hoisted(() => ({
  getRequestErrorDetail: vi.fn(),
  own: vi.fn(),
  listRequestErrorUpstreamErrors: vi.fn()
}))

vi.mock('@/api/observerUsage', () => ({ observerUsageAPI: { getErrorDetail: mocks.own } }))
vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getRequestErrorDetail: mocks.getRequestErrorDetail,
    getUpstreamErrorDetail: vi.fn(),
    listRequestErrorUpstreamErrors: mocks.listRequestErrorUpstreamErrors
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('OpsErrorDetailModal', () => {
  beforeEach(() => {
    mocks.getRequestErrorDetail.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockReset()
    mocks.listRequestErrorUpstreamErrors.mockResolvedValue({ items: [] })
  })

  it('prioritizes upstream root cause and deduplicates diagnostic payloads', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 1,
      created_at: '2026-08-19T00:00:00Z',
      phase: 'request',
      type: 'upstream_error',
      error_owner: 'provider',
      error_source: 'gateway',
      severity: 'P1',
      status_code: 502,
      upstream_status_code: 429,
      platform: 'openai',
      model: 'gpt-5.6',
      resolved: false,
      request_id: 'rid-1',
      message: 'All available accounts exhausted',
      error_body: '{"error":"same"}',
      upstream_error_message: 'provider rate limit exhausted',
      upstream_error_detail: '{"error":"same"}',
      upstream_errors: '[]',
      account_name: 'account',
      group_name: 'group',
      is_business_limited: false
    })

    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 1, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('provider rate limit exhausted')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.upstreamStatus')
    expect(wrapper.text()).toContain('429')
    expect(wrapper.findAll('pre')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('admin.ops.errorDetail.payloads.upstream_detail')
  })

  it('shows account for non-upstream errors and for each correlated upstream attempt', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 2,
      created_at: '2026-08-19T00:00:00Z',
      phase: 'request',
      type: 'invalid_request_error',
      error_owner: 'client',
      error_source: 'client_request',
      severity: 'P2',
      status_code: 400,
      platform: 'openai',
      model: 'gpt-5.6',
      resolved: false,
      request_id: 'rid-2',
      message: 'bad request',
      account_id: 42,
      account_name: 'apikey-account',
      user_email: 'user@example.com',
      is_business_limited: false
    })
    mocks.listRequestErrorUpstreamErrors.mockResolvedValue({
      items: [{ id: 9, status_code: 502, account_id: 7, account_name: 'retry-account', message: 'bad gateway' }]
    })

    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 2, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="error-detail-account"]').text()).toBe('apikey-account (#42)')
    expect(wrapper.text()).toContain('user@example.com')
    expect(wrapper.text()).toContain('retry-account (#7)')
  })

  it('falls back to upstream attempt account when top-level account is missing', async () => {
    mocks.getRequestErrorDetail.mockResolvedValue({
      id: 3,
      created_at: '2026-08-19T00:00:00Z',
      phase: 'routing',
      type: 'api_error',
      error_owner: 'platform',
      error_source: 'gateway',
      severity: 'P1',
      status_code: 503,
      platform: 'anthropic',
      model: 'claude',
      resolved: false,
      request_id: 'rid-3',
      message: 'no available accounts',
      account_id: null,
      account_name: '',
      upstream_errors: '[{"account_id":5,"account_name":"first"},{"account_id":6,"account_name":"last"}]',
      is_business_limited: false
    })

    const wrapper = shallowMount(OpsErrorDetailModal, {
      props: { show: true, errorId: 3, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="error-detail-account"]').text()).toBe('last (#6)')
  })
})

it('loads only the owned observer error and never requests correlated admin details', async () => {
  vi.clearAllMocks()
  mocks.own.mockResolvedValue({ id: 42, status_code: 502, message: 'own error' })
  const wrapper = shallowMount(OpsErrorDetailModal, {
    props: { show: true, errorId: 42, errorType: 'request' },
    global: { provide: { [observerUsageContext as symbol]: true } },
  })
  await flushPromises()
  expect(mocks.own).toHaveBeenCalledWith(42)
  expect(mocks.getRequestErrorDetail).not.toHaveBeenCalled()
  expect(mocks.listRequestErrorUpstreamErrors).not.toHaveBeenCalled()
  wrapper.unmount()
})
