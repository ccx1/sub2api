<template>
  <div class="app-backdrop relative flex min-h-screen items-center justify-center overflow-hidden p-4">
    <div class="relative z-10 w-full max-w-md">
      <div class="mb-6 text-center">
        <div
          class="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl border border-gray-200/80 bg-white/85 shadow-card dark:border-dark-700 dark:bg-dark-800/85"
        >
          <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
        </div>
        <div class="mb-3 flex items-center justify-center gap-2">
          <span class="section-chip">Workspace Console</span>
        </div>
        <h1 class="mb-2 text-3xl font-bold text-gray-900 dark:text-white">
          {{ siteName }}
        </h1>
        <p class="text-sm text-gray-600 dark:text-dark-300">
          {{ siteSubtitle }}
        </p>
      </div>

      <div class="card-glass panel-backdrop p-8">
        <slot />
      </div>

      <div class="mt-6 text-center text-sm text-gray-600 dark:text-dark-300">
        <slot name="footer" />
      </div>

      <div class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped></style>
