import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexTicketNodesModal from '../CodexTicketNodesModal.vue'
import type { CodexTicketNodeProbeResult, CodexTicketNodeSnapshot } from '@/api/admin/codexTicketDiagnostics'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  probe: vi.fn(),
  run: vi.fn((fn: () => unknown) => fn()),
  copy: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key) })
  }
})
vi.mock('@/api/admin/codexTicketDiagnostics', () => ({
  getCodexTicketNodes: mocks.load,
  probeCodexTicketNodes: mocks.probe
}))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: mocks.run }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: (err: unknown) => (err as { code?: string })?.code === 'STEP_UP_CANCELLED',
  stepUpBlockReason: () => ''
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: mocks.copy })
}))

const node = (n: number) => ({ host: `chat.gateway.unified-${n}.api.openai.com`, recognized: true, name: `unified-${n}`, number: n, country: 'us', region: 'east' })

const snapshot = (): CodexTicketNodeSnapshot => ({
  account_id: 7,
  account_name: 'acc',
  account_status: 'active',
  schedulable: true,
  ticket_enabled: true,
  harvest_enabled: true,
  random_proxy: false,
  proxy_available: true,
  proxy: { address: 'direct' },
  cookie_mode: 'preserve',
  config: { enabled: true, credential_mode: 'cookie', pool_capacity: 2, cookie_ttl_seconds: 600, ttl_seconds: 600, models: ['gpt-6-astra'] },
  strategy: { enabled: true, applies: true, route_prewarm_connections: 0 },
  models: [{
    model: 'gpt-6-astra',
    configured: true,
    slots: [{
      label: 'primary', business_selected: true, usable: true, revoked: false, verified: true, verification_skipped: false,
      session_id: 'sess-ticket', state_length: 0, attempts: 1, cross_region: false, sent_node: node(12),
      cookies: [{ name: '__oailb', value: 'raw-oailb-value', node: node(12), claims: { host: node(12).host } }]
    }]
  }],
  connections: [],
  server_time: '2026-09-29T00:00:00Z'
})

const probeResult = (): CodexTicketNodeProbeResult => ({
  account_id: 7,
  model: 'gpt-6-astra',
  source: 'sticky_jar',
  cookie_mode: 'preserve',
  node_counts: { 'unified-7': 2 },
  rounds: [1, 2].map(index => ({
    index, status: 'ok', http_status: 200, header_latency_ms: 10, first_byte_ms: 12, first_delta_ms: 30, total_latency_ms: 50,
    node_outcome: index === 1 ? 'assigned' : 'kept', received_node: index === 1 ? node(7) : undefined, effective_node: node(7),
    sent_cookies: [], received_cookies: [], turn_state_length: 0, response_id: 'resp_1',
    exchange: { capture_mode: 'raw', requested_model: 'gpt-6-astra', request: { method: 'POST', url: 'https://chatgpt.com/backend-api/codex/responses', headers: { Authorization: ['Bearer tok'] }, body: '{}', body_bytes: 2 } }
  })),
  started_at: '2026-09-29T00:00:00Z',
  duration_ms: 100
})

function render() {
  return mount(CodexTicketNodesModal, {
    props: { show: true, account: { id: 7, name: 'acc' } },
    global: {
      stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        TotpStepUpDialog: true,
        CodexTicketDiagnostics: true
      }
    }
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.run.mockImplementation((fn: () => unknown) => fn())
})

describe('CodexTicketNodesModal', () => {
  it('loads the snapshot through step-up and shows raw cookie values and nodes', async () => {
    mocks.load.mockResolvedValue(snapshot())
    const wrapper = render()
    await flushPromises()

    expect(mocks.run).toHaveBeenCalledTimes(1)
    expect(mocks.load).toHaveBeenCalledWith(7)
    expect(wrapper.find('[data-testid="node-cookie-value"]').text()).toBe('raw-oailb-value')
    expect(wrapper.find('[data-testid="nodes-slot"]').text()).toContain('unified-12')
    expect((wrapper.find('[data-testid="probe-model"]').element as HTMLSelectElement).value).toBe('gpt-6-astra')
  })

  it('runs a probe with clamped count and renders per-round nodes and raw request', async () => {
    mocks.load.mockResolvedValue(snapshot())
    mocks.probe.mockResolvedValue(probeResult())
    const wrapper = render()
    await flushPromises()

    await wrapper.find('[data-testid="probe-source"]').setValue('sticky_jar')
    await wrapper.find('[data-testid="probe-count"]').setValue('9')
    await wrapper.find('[data-testid="probe-run"]').trigger('click')
    await flushPromises()

    expect(mocks.probe).toHaveBeenCalledWith(7, { model: 'gpt-6-astra', source: 'sticky_jar', slot: undefined, count: 5 })
    expect(wrapper.findAll('[data-testid="probe-round"]')).toHaveLength(2)
    expect(wrapper.find('[data-testid="probe-node-counts"]').text()).toContain('unified-7 × 2')
    expect(wrapper.find('[data-testid="probe-request"]').text()).toContain('Authorization: Bearer tok')
  })

  it('passes the selected slot for ticket replay and surfaces backend errors', async () => {
    mocks.load.mockResolvedValue(snapshot())
    mocks.probe.mockRejectedValue({ message: 'Node probe unavailable: egress_changed' })
    const wrapper = render()
    await flushPromises()

    await wrapper.find('[data-testid="probe-slot"]').setValue('primary')
    await wrapper.find('[data-testid="probe-count"]').setValue('1')
    await wrapper.find('[data-testid="probe-run"]').trigger('click')
    await flushPromises()

    expect(mocks.probe).toHaveBeenCalledWith(7, { model: 'gpt-6-astra', source: 'ticket', slot: 'primary', count: 1 })
    expect(wrapper.find('[data-testid="probe-error"]').text()).toContain('egress_changed')
  })

  it('stays silent when the step-up prompt is cancelled', async () => {
    mocks.run.mockRejectedValueOnce({ code: 'STEP_UP_CANCELLED' })
    const wrapper = render()
    await flushPromises()

    expect(wrapper.find('[data-testid="nodes-error"]').exists()).toBe(false)
  })
})
