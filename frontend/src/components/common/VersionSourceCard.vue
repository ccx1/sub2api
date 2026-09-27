<template>
  <section
    class="rounded-lg border p-3 transition-colors"
    :class="hasUpdate ? updateColors.panel : 'border-gray-200 dark:border-dark-700'"
    :aria-label="title"
    :data-version-source="kind"
  >
    <div class="flex items-start justify-between gap-2">
      <h3 class="text-sm font-semibold" :class="hasUpdate ? updateColors.text : 'text-gray-800 dark:text-dark-100'">{{ title }}</h3>
      <span
        v-if="hasUpdate"
        class="shrink-0 rounded px-1.5 py-0.5 text-xs font-medium"
        :class="[updateColors.badge, updateColors.text]"
      >{{ t(kind === 'ranxi' ? 'version.ranxiUpdateAvailable' : 'version.localUpdateAvailable') }}</span>
    </div>
    <dl class="mt-2 space-y-1 text-xs">
      <div class="flex justify-between gap-2">
        <dt class="text-gray-500 dark:text-dark-400">{{ t('version.currentVersion') }}</dt>
        <dd class="break-all font-medium text-gray-900 dark:text-white">{{ formatVersion(source?.current_version) }}</dd>
      </div>
      <div class="flex justify-between gap-2">
        <dt class="text-gray-500 dark:text-dark-400">{{ t('version.latestVersion') }}</dt>
        <dd class="break-all" :class="hasUpdate ? ['font-semibold', updateColors.text] : 'text-gray-700 dark:text-dark-200'">{{ formatVersion(latestVersion) }}</dd>
      </div>
    </dl>
    <p v-if="warning" role="status" class="mt-2 text-xs text-amber-700 dark:text-amber-400">{{ warning }}</p>
    <p v-else-if="source?.cached" role="status" class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('version.cachedInfo') }}</p>
    <p v-if="!warning && !latestVersion" class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('version.unavailable') }}</p>
    <p v-else-if="!warning && !hasUpdate" class="mt-2 text-xs text-green-700 dark:text-green-400">{{ t('version.upToDate') }}</p>
    <p v-if="hint" class="mt-2 text-xs leading-relaxed" :class="hasUpdate ? updateColors.text : 'text-gray-500 dark:text-dark-400'">{{ hint }}</p>
    <details v-if="source?.release_info?.body" class="mt-2 text-xs">
      <summary class="cursor-pointer text-gray-600 dark:text-dark-300">{{ t('version.releaseNotes') }}</summary>
      <p class="mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap break-words text-gray-500 dark:text-dark-400">{{ source.release_info.body }}</p>
    </details>
    <a
      v-if="releaseURL"
      :href="releaseURL"
      target="_blank"
      rel="noopener noreferrer"
      class="mt-2 inline-block text-xs text-primary-600 hover:underline dark:text-primary-400"
    >{{ t('version.viewRelease') }}</a>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VersionSourceInfo } from '@/api/admin/system'

const props = withDefaults(defineProps<{
  title: string
  source?: VersionSourceInfo | null
  hint?: string
  kind?: 'local' | 'ranxi'
}>(), { kind: 'local' })
const { t } = useI18n()
const latestVersion = computed(() => props.source?.warning && !props.source.cached ? '' : props.source?.latest_version)
const hasUpdate = computed(() => !!props.source?.has_update && !!latestVersion.value)
const updateColors = computed(() => props.kind === 'ranxi' ? {
  panel: 'border-orange-400 bg-orange-50/80 dark:border-orange-500/70 dark:bg-orange-950/30',
  text: 'text-orange-800 dark:text-orange-300',
  badge: 'bg-orange-100 dark:bg-orange-900/40'
} : {
  panel: 'border-cyan-400 bg-cyan-50/80 dark:border-cyan-500/70 dark:bg-cyan-950/30',
  text: 'text-cyan-800 dark:text-cyan-300',
  badge: 'bg-cyan-100 dark:bg-cyan-900/40'
})
const warning = computed(() => {
  if (!props.source?.warning) return ''
  return t(props.source.cached ? 'version.cachedWarning' : 'version.checkFailed')
})
const releaseURL = computed(() => {
  const value = props.source?.release_info?.html_url
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && url.hostname === 'github.com' ? url.href : ''
  } catch {
    return ''
  }
})
function formatVersion(value?: string): string {
  return value ? 'v' + value : '—'
}
</script>
