<template>
  <BaseDialog :show="show" :title="t(`${prefix}.title`)" width="full" @close="emit('close')">
    <div class="min-w-0 space-y-4" data-testid="codex-ticket-nodes">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || !account" data-testid="nodes-refresh" @click="load">{{ t(`${prefix}.refresh`) }}</button>
          <button type="button" class="btn btn-secondary" :disabled="!snapshot && !probe" data-testid="nodes-copy" @click="copyAll">{{ t(`${prefix}.copyJson`) }}</button>
        </div>
      </div>
      <p class="rounded bg-amber-50 p-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t(`${prefix}.rawWarning`) }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t(`${prefix}.loading`) }}</p>
      <p v-if="error" role="alert" class="break-all text-sm text-red-600 dark:text-red-400" data-testid="nodes-error">{{ error }}</p>

      <template v-if="snapshot">
        <section class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="nodes-overview">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.overview`) }}</h3>
          <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="row in overviewRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.fields.${row.key}`) }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
          </dl>
        </section>

        <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="nodes-probe">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.probeTitle`) }}</h3>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.probeHint`) }}</p>
          <div class="flex flex-wrap items-end gap-3">
            <label class="text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.fields.model`) }}
              <select v-model="form.model" class="input mt-1 block" data-testid="probe-model">
                <option v-for="model in modelNames" :key="model" :value="model">{{ model }}</option>
              </select>
            </label>
            <label class="text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.fields.source`) }}
              <select v-model="form.source" class="input mt-1 block" data-testid="probe-source">
                <option v-for="source in sources" :key="source" :value="source">{{ t(`${prefix}.sources.${source}`) }}</option>
              </select>
            </label>
            <label v-if="form.source === 'ticket'" class="text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.fields.slot`) }}
              <select v-model="form.slot" class="input mt-1 block" data-testid="probe-slot">
                <option value="">{{ t(`${prefix}.businessSlot`) }}</option>
                <option v-for="label in slotLabels" :key="label" :value="label">{{ label }}</option>
              </select>
            </label>
            <label class="text-xs text-gray-600 dark:text-gray-400">{{ t(`${prefix}.fields.count`) }}
              <input v-model.number="form.count" type="number" min="1" max="5" class="input mt-1 block w-20" data-testid="probe-count" />
            </label>
            <button type="button" class="btn btn-primary" :disabled="probing || !form.model || !account" data-testid="probe-run" @click="runProbe">
              {{ t(`${prefix}.${probing ? 'probing' : 'runProbe'}`) }}
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.sourceHints.${form.source}`) }}</p>
          <p v-if="probeError" role="alert" class="break-all text-sm text-red-600 dark:text-red-400" data-testid="probe-error">{{ probeError }}</p>

          <div v-if="probe" class="min-w-0 space-y-3" data-testid="probe-result">
            <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-3">
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.source`) }}: </dt><dd class="inline font-mono">{{ t(`${prefix}.sources.${probe.source}`) }}{{ probe.slot ? ` · ${probe.slot}` : '' }}</dd></div>
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.slotNode`) }}: </dt><dd class="inline font-mono">{{ nodeText(probe.slot_node) }}</dd></div>
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.cookieMode`) }}: </dt><dd class="inline font-mono">{{ probe.cookie_mode }}</dd></div>
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.proxy`) }}: </dt><dd class="inline font-mono">{{ proxyText(probe.proxy) }}</dd></div>
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.egressCountry`) }}: </dt><dd class="inline font-mono">{{ probe.egress_country || '—' }}</dd></div>
              <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.duration`) }}: </dt><dd class="inline font-mono">{{ probe.duration_ms }} ms</dd></div>
            </dl>
            <div class="flex flex-wrap gap-2 text-xs" data-testid="probe-node-counts">
              <span v-for="(count, node) in probe.node_counts ?? {}" :key="node" class="rounded bg-cyan-50 px-2 py-0.5 font-mono text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300">{{ node }} × {{ count }}</span>
            </div>
            <div class="overflow-x-auto">
              <table class="min-w-full text-left text-xs">
                <thead class="text-gray-500 dark:text-gray-400">
                  <tr>
                    <th v-for="column in roundColumns" :key="column" class="whitespace-nowrap px-2 py-1 font-medium">{{ t(`${prefix}.rounds.${column}`) }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 font-mono text-gray-700 dark:divide-dark-700 dark:text-gray-300">
                  <tr v-for="round in probe.rounds ?? []" :key="round.index" data-testid="probe-round">
                    <td class="px-2 py-1">{{ round.index }}</td>
                    <td class="px-2 py-1" :class="round.status === 'ok' ? 'text-emerald-600' : 'text-red-600'">{{ round.status }}{{ round.reason ? ` · ${round.reason}` : '' }}</td>
                    <td class="px-2 py-1">{{ round.http_status || '—' }}</td>
                    <td class="whitespace-nowrap px-2 py-1">{{ round.header_latency_ms }} / {{ round.first_byte_ms ?? '—' }} / {{ round.first_delta_ms ?? '—' }} / {{ round.total_latency_ms }}</td>
                    <td class="whitespace-nowrap px-2 py-1">{{ nodeName(round.sent_node) }} → {{ nodeName(round.received_node) }}</td>
                    <td class="px-2 py-1">{{ t(`${prefix}.outcomes.${round.node_outcome}`) }}</td>
                    <td class="px-2 py-1">{{ nodeName(round.effective_node) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <details v-for="round in probe.rounds ?? []" :key="`detail-${round.index}`" class="rounded border border-gray-200 p-2 dark:border-dark-600" data-testid="probe-round-detail">
              <summary class="cursor-pointer text-xs font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.roundDetail`, { index: round.index }) }}</summary>
              <div class="mt-2 min-w-0 space-y-3">
                <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2">
                  <div><dt class="inline text-gray-500">session_id: </dt><dd class="inline font-mono">{{ round.session_id || '—' }}</dd></div>
                  <div><dt class="inline text-gray-500">response_id: </dt><dd class="inline font-mono">{{ round.response_id || '—' }}</dd></div>
                  <div><dt class="inline text-gray-500">{{ t(`${prefix}.fields.startedAt`) }}: </dt><dd class="inline font-mono">{{ timeText(round.started_at) }}</dd></div>
                  <div><dt class="inline text-gray-500">turn_state_length: </dt><dd class="inline font-mono">{{ round.turn_state_length }}</dd></div>
                </dl>
                <div class="grid min-w-0 gap-3 lg:grid-cols-2">
                  <div class="min-w-0 space-y-1">
                    <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.sentCookies`) }}</h5>
                    <CodexTicketNodeCookies :cookies="round.sent_cookies" />
                  </div>
                  <div class="min-w-0 space-y-1">
                    <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.receivedCookies`) }}</h5>
                    <CodexTicketNodeCookies :cookies="round.received_cookies" />
                  </div>
                </div>
                <template v-if="round.exchange">
                  <CodexTicketDiagnostics :exchange="round.exchange" />
                  <div class="grid min-w-0 gap-3 lg:grid-cols-2">
                    <div v-for="kind in messageKinds" :key="kind" class="min-w-0 space-y-1" :data-testid="`probe-${kind}`">
                      <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.${kind}`) }}</h5>
                      <pre v-if="round.exchange[kind]" class="max-h-96 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ messageText(round.exchange[kind]!) }}</pre>
                      <p v-else class="text-xs text-gray-500">{{ t(`${prefix}.notRecorded`) }}</p>
                    </div>
                  </div>
                </template>
              </div>
            </details>
          </div>
        </section>

        <section v-for="model in snapshot.models ?? []" :key="model.model" class="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="nodes-model">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ model.model }}<span v-if="!model.configured" class="ml-2 text-xs font-normal text-amber-600">{{ t(`${prefix}.notConfigured`) }}</span></h3>
          <p v-if="!model.slots.length" class="text-xs text-gray-500">{{ t(`${prefix}.noSlots`) }}</p>
          <details v-for="slot in model.slots" :key="slot.label" class="rounded border border-gray-200 p-2 dark:border-dark-600" data-testid="nodes-slot">
            <summary class="cursor-pointer text-xs text-gray-900 dark:text-gray-100">
              <span class="font-mono font-medium">{{ slot.label }}</span>
              <span v-if="slot.business_selected" class="ml-2 text-emerald-600">{{ t(`${prefix}.businessSelected`) }}</span>
              <span class="ml-2" :class="slot.usable ? 'text-emerald-600' : 'text-gray-500'">{{ t(`${prefix}.${slot.usable ? 'usable' : 'unusable'}`) }}</span>
              <span class="ml-2 font-mono text-cyan-700 dark:text-cyan-300">{{ nodeText(slot.sent_node) }}</span>
              <span v-if="slot.cross_region" class="ml-2 text-amber-600">{{ t(`${prefix}.crossRegion`) }}</span>
            </summary>
            <div class="mt-2 min-w-0 space-y-2">
              <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-3">
                <div v-for="row in slotRows(slot)" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ row.key }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
              </dl>
              <div v-if="slot.state" class="space-y-1">
                <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">STATE</h5>
                <pre class="max-h-32 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ slot.state }}</pre>
              </div>
              <pre v-if="slot.invalidation" class="max-h-48 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-red-50 p-2 font-mono text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ JSON.stringify(slot.invalidation, null, 2) }}</pre>
              <CodexTicketNodeCookies :cookies="slot.cookies" />
            </div>
          </details>
          <details v-if="model.pending" class="rounded border border-dashed border-gray-300 p-2 dark:border-dark-500" data-testid="nodes-pending">
            <summary class="cursor-pointer text-xs text-gray-900 dark:text-gray-100">
              {{ t(`${prefix}.pending`) }} · {{ model.pending.ticket_label || '—' }} · {{ nodeName(model.pending.ticket_node) }} → {{ nodeName(model.pending.candidate_node) }}
            </summary>
            <dl class="mt-2 grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2">
              <div><dt class="inline text-gray-500">header_changed: </dt><dd class="inline font-mono">{{ model.pending.header_changed }}</dd></div>
              <div><dt class="inline text-gray-500">expires_at: </dt><dd class="inline font-mono">{{ timeText(model.pending.expires_at) }}</dd></div>
              <div><dt class="inline text-gray-500">candidate_captured_at: </dt><dd class="inline font-mono">{{ timeText(model.pending.candidate_captured_at) }}</dd></div>
              <div><dt class="inline text-gray-500">candidate_expires_at: </dt><dd class="inline font-mono">{{ timeText(model.pending.candidate_expires_at) }}</dd></div>
            </dl>
            <CodexTicketNodeCookies :cookies="model.pending.cookies" class="mt-2" />
          </details>
        </section>

        <section class="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="nodes-connections">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.connections`) }} ({{ snapshot.connections?.length ?? 0 }})</h3>
          <p v-if="!snapshot.connections?.length" class="text-xs text-gray-500">{{ t(`${prefix}.noConnections`) }}</p>
          <details v-for="conn in snapshot.connections ?? []" :key="conn.id" class="rounded border border-gray-200 p-2 dark:border-dark-600" data-testid="nodes-connection">
            <summary class="cursor-pointer break-all text-xs text-gray-900 dark:text-gray-100">
              <span class="font-mono">{{ conn.id }}</span>
              <span class="ml-2">{{ conn.model || '—' }}{{ conn.slot_label ? ` · ${conn.slot_label}` : '' }}</span>
              <span class="ml-2 font-mono text-cyan-700 dark:text-cyan-300">{{ nodeName(conn.sent_node) }} → {{ nodeName(conn.received_node) }}</span>
              <span class="ml-2 text-gray-500">{{ connectionState(conn) }}</span>
            </summary>
            <div class="mt-2 min-w-0 space-y-2">
              <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-3">
                <div v-for="row in connectionRows(conn)" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ row.key }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
              </dl>
              <div class="grid min-w-0 gap-3 lg:grid-cols-2">
                <div class="min-w-0 space-y-1">
                  <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.sentCookies`) }}</h5>
                  <CodexTicketNodeCookies :cookies="conn.sent_cookies" />
                </div>
                <div class="min-w-0 space-y-1">
                  <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.receivedCookies`) }}</h5>
                  <CodexTicketNodeCookies :cookies="conn.received_cookies" />
                </div>
              </div>
              <h5 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.handshakeHeaders`) }}<span v-if="conn.handshake_headers_truncated" class="ml-1 text-amber-600">({{ t(`${prefix}.truncated`) }})</span></h5>
              <pre class="max-h-64 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ headersText(conn.handshake_headers) || '—' }}</pre>
            </div>
          </details>
        </section>

        <details class="rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="nodes-raw-json">
          <summary class="cursor-pointer text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.rawJson`) }}</summary>
          <pre class="mt-2 max-h-[32rem] max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ rawJson }}</pre>
        </details>
      </template>
    </div>
    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import CodexTicketDiagnostics from '@/components/account/CodexTicketDiagnostics.vue'
import CodexTicketNodeCookies from './CodexTicketNodeCookies.vue'
import {
  getCodexTicketNodes,
  probeCodexTicketNodes,
  type CodexTicketNodeConnection,
  type CodexTicketNodeInfo,
  type CodexTicketNodeProbeResult,
  type CodexTicketNodeProbeSource,
  type CodexTicketNodeSlot,
  type CodexTicketNodeSnapshot
} from '@/api/admin/codexTicketDiagnostics'
import type { CodexTicketHistoryProxy, CodexTicketHTTPMessage } from '@/api/admin/accounts'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean; account: { id: number; name: string } | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const stepUp = useStepUp()
const prefix = 'admin.accounts.codexTicketNodes'
const sources: CodexTicketNodeProbeSource[] = ['ticket', 'sticky_jar', 'empty_jar']
const roundColumns = ['index', 'status', 'http', 'latency', 'node', 'outcome', 'effective']
const messageKinds = ['request', 'response'] as const

const loading = ref(false)
const probing = ref(false)
const error = ref('')
const probeError = ref('')
const snapshot = ref<CodexTicketNodeSnapshot | null>(null)
const probe = ref<CodexTicketNodeProbeResult | null>(null)
const form = reactive<{ model: string; source: CodexTicketNodeProbeSource; slot: string; count: number }>({ model: '', source: 'ticket', slot: '', count: 3 })
let sequence = 0

const modelNames = computed(() => (snapshot.value?.models ?? []).map(item => item.model))
const slotLabels = computed(() => snapshot.value?.models?.find(item => item.model === form.model)?.slots.map(slot => slot.label) ?? [])
const rawJson = computed(() => JSON.stringify({ snapshot: snapshot.value, probe: probe.value }, null, 2))
const overviewRows = computed(() => {
  const s = snapshot.value
  if (!s) return []
  return [
    { key: 'accountStatus', value: `${s.account_status} · ${s.schedulable ? 'schedulable' : 'unschedulable'}` },
    { key: 'cookieMode', value: s.cookie_mode },
    { key: 'proxy', value: s.proxy_available ? proxyText(s.proxy) : t(`${prefix}.proxyUnavailable`) },
    { key: 'egressCountry', value: [s.egress_country, s.egress_macro_region].filter(Boolean).join(' / ') || '—' },
    { key: 'ticketEnabled', value: `${s.ticket_enabled} / harvest ${s.harvest_enabled} / random_proxy ${s.random_proxy}` },
    { key: 'credentialMode', value: [s.config.credential_mode, s.config.session_mode, s.config.refresh_strategy].filter(Boolean).join(' · ') || '—' },
    { key: 'pool', value: `capacity ${s.config.pool_capacity} · cookie_ttl ${s.config.cookie_ttl_seconds}s · ttl ${s.config.ttl_seconds}s` },
    { key: 'strategy', value: [s.strategy.strategy, s.strategy.scope, s.strategy.enabled ? 'enabled' : 'disabled', s.strategy.applies ? 'applies' : 'not_applied'].filter(Boolean).join(' · ') },
    { key: 'routeAffinity', value: `${s.strategy.route_affinity_mode || '—'} · prewarm ${s.strategy.route_prewarm_connections}` },
    { key: 'serverTime', value: timeText(s.server_time) }
  ]
})

function nodeName(node?: CodexTicketNodeInfo | null): string {
  if (!node) return '—'
  return node.recognized && node.name ? node.name : node.host
}

function nodeText(node?: CodexTicketNodeInfo | null): string {
  if (!node) return '—'
  if (!node.recognized) return node.host
  return `${node.name} · ${[node.country, node.region].filter(Boolean).join('/')} · ${node.host}`
}

function proxyText(proxy?: CodexTicketHistoryProxy | null): string {
  if (!proxy) return '—'
  return [proxy.id ? `#${proxy.id}` : '', proxy.name, proxy.address].filter(Boolean).join(' ')
}

function timeText(value?: string | null): string {
  return value ? formatDateTime(value) : '—'
}

function headersText(headers?: Record<string, string[]> | null): string {
  return Object.entries(headers ?? {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n')
}

function messageText(message: CodexTicketHTTPMessage): string {
  const lines: string[] = []
  if (message.method || message.url) lines.push([message.method, message.url].filter(Boolean).join(' '))
  if (message.status_code) lines.push(`HTTP ${message.status_code}`)
  lines.push(headersText(message.headers), '', message.body ?? '')
  return lines.join('\n')
}

function slotRows(slot: CodexTicketNodeSlot): Array<{ key: string; value: string }> {
  return [
    { key: 'route_node', value: nodeText(slot.route_node) },
    { key: 'session_id', value: slot.session_id || '—' },
    { key: 'credential_mode', value: slot.credential_mode || '—' },
    { key: 'state_length', value: String(slot.state_length) },
    { key: 'verified', value: `${slot.verified}${slot.verification_skipped ? ' (skipped)' : ''}` },
    { key: 'revoked', value: String(slot.revoked) },
    { key: 'egress_matches_current', value: slot.egress_matches_current == null ? '—' : String(slot.egress_matches_current) },
    { key: 'harvest_proxy', value: [slot.harvest_proxy_id ? `#${slot.harvest_proxy_id}` : '', slot.harvest_proxy_name, slot.harvest_country].filter(Boolean).join(' ') || '—' },
    { key: 'harvest_egress_differs', value: String(!!slot.harvest_egress_differs) },
    { key: 'captured_at', value: timeText(slot.captured_at) },
    { key: 'origin_captured_at', value: timeText(slot.origin_captured_at) },
    { key: 'expires_at', value: timeText(slot.expires_at) },
    { key: 'hard_expires_at', value: timeText(slot.hard_expires_at) },
    { key: 'route_expires_at', value: timeText(slot.route_expires_at) },
    { key: 'revalidate_at', value: timeText(slot.revalidate_at) },
    { key: 'revalidated_at', value: timeText(slot.revalidated_at) },
    { key: 'attempts', value: String(slot.attempts) },
    { key: 'route_fingerprint', value: slot.route_fingerprint || '—' }
  ]
}

function connectionState(conn: CodexTicketNodeConnection): string {
  const flags = [conn.leased && 'leased', conn.prewarmed && 'prewarmed', conn.closed && 'closed', conn.unusable && 'unusable']
  return flags.filter(Boolean).join(' · ') || 'idle'
}

function connectionRows(conn: CodexTicketNodeConnection): Array<{ key: string; value: string }> {
  return [
    { key: 'routing_affinity', value: conn.routing_affinity || '—' },
    { key: 'route_fingerprint', value: conn.route_fingerprint || '—' },
    { key: 'strict', value: String(conn.strict) },
    { key: 'has_receipt', value: String(conn.has_receipt) },
    { key: 'leased_before', value: String(conn.leased_before) },
    { key: 'created_at', value: timeText(conn.created_at) },
    { key: 'last_used_at', value: timeText(conn.last_used_at) }
  ]
}

function stepUpMessage(err: unknown): string {
  if (isStepUpBlocked(err)) {
    return t(stepUpBlockReason(err) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  }
  return extractApiErrorMessage(err, t(`${prefix}.failed`))
}

function reset() {
  sequence++
  loading.value = false
  probing.value = false
  error.value = ''
  probeError.value = ''
  snapshot.value = null
  probe.value = null
  Object.assign(form, { model: '', source: 'ticket', slot: '', count: 3 })
}

async function load() {
  const account = props.account
  if (!account) return
  const current = ++sequence
  loading.value = true
  error.value = ''
  try {
    const data = await stepUp.run(() => getCodexTicketNodes(account.id))
    if (current !== sequence) return
    snapshot.value = data
    if (!modelNames.value.includes(form.model)) form.model = modelNames.value[0] ?? ''
    if (form.slot && !slotLabels.value.includes(form.slot)) form.slot = ''
  } catch (err) {
    if (current !== sequence || isStepUpCancelled(err)) return
    error.value = stepUpMessage(err)
  } finally {
    if (current === sequence) loading.value = false
  }
}

async function runProbe() {
  const account = props.account
  if (!account || !form.model || probing.value) return
  const current = sequence
  probing.value = true
  probeError.value = ''
  const count = Math.min(5, Math.max(1, Math.trunc(Number(form.count) || 1)))
  form.count = count
  try {
    const data = await stepUp.run(() => probeCodexTicketNodes(account.id, {
      model: form.model,
      source: form.source,
      slot: form.source === 'ticket' && form.slot ? form.slot : undefined,
      count
    }))
    if (current !== sequence) return
    probe.value = data
  } catch (err) {
    if (current !== sequence || isStepUpCancelled(err)) return
    probeError.value = stepUpMessage(err)
  } finally {
    if (current === sequence) probing.value = false
  }
}

async function copyAll() {
  await copyToClipboard(rawJson.value, t(`${prefix}.copied`))
}

watch(() => form.model, () => {
  if (form.slot && !slotLabels.value.includes(form.slot)) form.slot = ''
})

watch(() => [props.show, props.account?.id] as const, ([show]) => {
  reset()
  if (show) void load()
}, { immediate: true })
</script>
