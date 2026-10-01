<template>
  <p v-if="!cookies?.length" class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.noCookies`) }}</p>
  <ul v-else class="min-w-0 space-y-2" data-testid="node-cookies">
    <li v-for="(cookie, index) in cookies" :key="`${cookie.name}-${index}`" class="min-w-0 space-y-1 rounded border border-gray-200 p-2 text-xs dark:border-dark-600">
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-mono font-medium text-gray-900 dark:text-gray-100">{{ cookie.name }}</span>
        <span v-if="cookie.node" class="rounded bg-cyan-50 px-1.5 py-0.5 font-mono text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300" data-testid="node-cookie-node">{{ nodeLabel(cookie.node) }}</span>
        <span v-if="cookie.excluded_by_mode" class="text-amber-600 dark:text-amber-400">{{ t(`${prefix}.excludedByMode`) }}</span>
        <span v-if="cookie.decode_error" class="break-all text-red-600 dark:text-red-400">{{ t(`${prefix}.decodeError`) }}: {{ cookie.decode_error }}</span>
      </div>
      <p class="break-all font-mono text-gray-500 dark:text-gray-400">{{ attributes(cookie) }}</p>
      <pre class="max-h-32 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-gray-700 dark:bg-dark-800 dark:text-gray-300" data-testid="node-cookie-value">{{ cookie.value }}</pre>
      <details v-if="cookie.claims">
        <summary class="cursor-pointer text-gray-600 dark:text-gray-400">claims</summary>
        <pre class="max-h-48 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ JSON.stringify(cookie.claims, null, 2) }}</pre>
      </details>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CodexTicketNodeCookie, CodexTicketNodeInfo } from '@/api/admin/codexTicketDiagnostics'
import { formatDateTime } from '@/utils/format'

defineProps<{ cookies?: CodexTicketNodeCookie[] | null }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketNodes'

function nodeLabel(node: CodexTicketNodeInfo): string {
  if (!node.recognized) return node.host
  return [node.name, [node.country, node.region].filter(Boolean).join('/')].filter(Boolean).join(' · ')
}

function attributes(cookie: CodexTicketNodeCookie): string {
  const parts: string[] = []
  if (cookie.domain) parts.push(`domain=${cookie.domain}`)
  if (cookie.path) parts.push(`path=${cookie.path}`)
  if (cookie.expires_at) parts.push(`expires=${formatDateTime(cookie.expires_at)}`)
  if (cookie.max_age) parts.push(`max-age=${cookie.max_age}`)
  if (cookie.same_site) parts.push(`samesite=${cookie.same_site}`)
  if (cookie.secure) parts.push('secure')
  if (cookie.http_only) parts.push('httponly')
  if (cookie.claim_expires_at) parts.push(`claim_exp=${formatDateTime(cookie.claim_expires_at)}`)
  return parts.join('; ')
}
</script>
