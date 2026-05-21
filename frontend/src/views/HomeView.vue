<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    class="app-backdrop relative flex min-h-screen flex-col overflow-hidden"
  >
    <header class="relative z-20 px-6 py-4">
      <nav class="card mx-auto flex max-w-6xl items-center justify-between px-4 py-3 sm:px-6">
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center overflow-hidden rounded-xl border border-gray-200/80 bg-white/80 dark:border-dark-700 dark:bg-dark-800/80">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <div class="hidden sm:block">
            <div class="text-sm font-semibold text-gray-900 dark:text-white">{{ siteName }}</div>
            <div class="text-xs text-gray-500 dark:text-dark-400">{{ siteSubtitle }}</div>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <LocaleSwitcher />

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-ghost btn-icon"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>

          <button
            @click="toggleTheme"
            class="btn btn-ghost btn-icon"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="btn btn-primary btn-sm"
          >
            <span class="flex h-5 w-5 items-center justify-center rounded-full bg-white/18 text-[10px] font-semibold text-white">
              {{ userInitial }}
            </span>
            <span>{{ t('home.dashboard') }}</span>
            <Icon name="arrowRight" size="sm" :stroke-width="2" />
          </router-link>
          <router-link
            v-else
            to="/login"
            class="btn btn-primary btn-sm"
          >
            {{ t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="relative z-10 flex-1 px-6 pb-16 pt-10">
      <div class="mx-auto max-w-6xl">
        <section class="grid gap-8 lg:grid-cols-[minmax(0,1.05fr)_minmax(420px,0.95fr)]">
          <div class="flex flex-col justify-center">
            <div class="mb-5 flex flex-wrap items-center gap-3">
              <span class="section-chip">{{ t('home.features.unifiedGateway') }}</span>
              <span class="section-chip">{{ t('home.tags.realtimeBilling') }}</span>
            </div>

            <h1 class="mb-4 text-4xl font-bold text-gray-900 dark:text-white md:text-5xl lg:text-6xl">
              {{ siteName }}
            </h1>
            <p class="mb-8 max-w-2xl text-lg leading-8 text-gray-600 dark:text-dark-300 md:text-xl">
              {{ siteSubtitle }}
            </p>

            <div class="mb-8 flex flex-wrap items-center gap-3">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary btn-lg"
              >
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="md" :stroke-width="2" />
              </router-link>
              <a
                v-if="docUrl"
                :href="docUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="btn btn-secondary btn-lg"
              >
                {{ t('home.docs') }}
              </a>
            </div>

            <div class="mb-8 flex flex-wrap items-center gap-4">
              <div class="section-chip">
                <Icon name="swap" size="sm" class="text-primary-600 dark:text-primary-300" />
                <span>{{ t('home.tags.subscriptionToApi') }}</span>
              </div>
              <div class="section-chip">
                <Icon name="shield" size="sm" class="text-primary-600 dark:text-primary-300" />
                <span>{{ t('home.tags.stickySession') }}</span>
              </div>
              <div class="section-chip">
                <Icon name="chart" size="sm" class="text-primary-600 dark:text-primary-300" />
                <span>{{ t('home.tags.realtimeBilling') }}</span>
              </div>
            </div>

            <div class="grid gap-4 md:grid-cols-3">
              <div class="card p-5">
                <div class="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-xl bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                  <Icon name="server" size="md" />
                </div>
                <h3 class="mb-2 text-base font-semibold text-gray-900 dark:text-white">
                  {{ t('home.features.unifiedGateway') }}
                </h3>
                <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">
                  {{ t('home.features.unifiedGatewayDesc') }}
                </p>
              </div>
              <div class="card p-5">
                <div class="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-xl bg-accent-100 text-accent-700 dark:bg-accent-900/30 dark:text-accent-300">
                  <svg
                    class="h-5 w-5"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M18 18.72a9.094 9.094 0 003.741-.479 3 3 0 00-4.682-2.72m.94 3.198l.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0112 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 016 18.719m12 0a5.971 5.971 0 00-.941-3.197m0 0A5.995 5.995 0 0012 12.75a5.995 5.995 0 00-5.058 2.772m0 0a3 3 0 00-4.681 2.72 8.986 8.986 0 003.74.477m.94-3.197a5.971 5.971 0 00-.94 3.197M15 6.75a3 3 0 11-6 0 3 3 0 016 0zm6 3a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0zm-13.5 0a2.25 2.25 0 11-4.5 0 2.25 2.25 0 014.5 0z"
                    />
                  </svg>
                </div>
                <h3 class="mb-2 text-base font-semibold text-gray-900 dark:text-white">
                  {{ t('home.features.multiAccount') }}
                </h3>
                <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">
                  {{ t('home.features.multiAccountDesc') }}
                </p>
              </div>
              <div class="card p-5">
                <div class="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-xl bg-gray-900 text-white dark:bg-dark-700">
                  <svg
                    class="h-5 w-5"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z"
                    />
                  </svg>
                </div>
                <h3 class="mb-2 text-base font-semibold text-gray-900 dark:text-white">
                  {{ t('home.features.balanceQuota') }}
                </h3>
                <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">
                  {{ t('home.features.balanceQuotaDesc') }}
                </p>
              </div>
            </div>
          </div>

          <div class="card overflow-hidden">
            <div class="border-b border-gray-200/80 px-5 py-4 dark:border-dark-700/80">
              <div class="flex items-center justify-between gap-3">
                <div>
                  <div class="text-sm font-semibold text-gray-900 dark:text-white">
                    {{ t('home.tags.subscriptionToApi') }}
                  </div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">
                    {{ t('home.features.unifiedGatewayDesc') }}
                  </div>
                </div>
                <span class="section-chip">Live</span>
              </div>
            </div>

            <div class="console-window">
              <div class="console-header">
                <div class="terminal-buttons">
                  <span class="btn-close"></span>
                  <span class="btn-minimize"></span>
                  <span class="btn-maximize"></span>
                </div>
                <span class="console-title">gateway.runtime</span>
              </div>

              <div class="console-body">
                <div class="code-line line-1">
                  <span class="code-prompt">$</span>
                  <span class="code-cmd">curl</span>
                  <span class="code-flag">-X POST</span>
                  <span class="code-url">/v1/messages</span>
                </div>
                <div class="code-line line-2">
                  <span class="code-comment"># {{ t('home.tags.stickySession') }}</span>
                </div>
                <div class="code-line line-3">
                  <span class="code-success">200 OK</span>
                  <span class="code-response">{ "content": "Hello!" }</span>
                </div>
                <div class="code-line line-4">
                  <span class="code-prompt">$</span>
                  <span class="cursor"></span>
                </div>
              </div>

              <div class="console-metrics">
                <div class="metric-item">
                  <span class="metric-label">{{ t('home.tags.realtimeBilling') }}</span>
                  <span class="metric-value">$0.0023</span>
                </div>
                <div class="metric-item">
                  <span class="metric-label">{{ t('home.features.multiAccount') }}</span>
                  <span class="metric-value">4 upstreams</span>
                </div>
                <div class="metric-item">
                  <span class="metric-label">{{ t('home.features.balanceQuota') }}</span>
                  <span class="metric-value">healthy</span>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section class="mt-14">
          <div class="mb-8">
            <h2 class="mb-3 text-2xl font-bold text-gray-900 dark:text-white">
              {{ t('home.providers.title') }}
            </h2>
            <p class="max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-400">
              {{ t('home.providers.description') }}
            </p>
          </div>

          <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
            <div class="card flex items-center gap-3 p-4">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#D97745] text-sm font-bold text-white">
                C
              </div>
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ t('home.providers.claude') }}
                </div>
                <div class="text-xs text-primary-700 dark:text-primary-300">
                  {{ t('home.providers.supported') }}
                </div>
              </div>
            </div>

            <div class="card flex items-center gap-3 p-4">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#3f6854] text-sm font-bold text-white">
                G
              </div>
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">GPT</div>
                <div class="text-xs text-primary-700 dark:text-primary-300">
                  {{ t('home.providers.supported') }}
                </div>
              </div>
            </div>

            <div class="card flex items-center gap-3 p-4">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#4f7ac8] text-sm font-bold text-white">
                G
              </div>
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ t('home.providers.gemini') }}
                </div>
                <div class="text-xs text-primary-700 dark:text-primary-300">
                  {{ t('home.providers.supported') }}
                </div>
              </div>
            </div>

            <div class="card flex items-center gap-3 p-4">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#b75872] text-sm font-bold text-white">
                A
              </div>
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ t('home.providers.antigravity') }}
                </div>
                <div class="text-xs text-primary-700 dark:text-primary-300">
                  {{ t('home.providers.supported') }}
                </div>
              </div>
            </div>

            <div class="card flex items-center gap-3 p-4 opacity-75">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-gray-300 text-sm font-bold text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                +
              </div>
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ t('home.providers.more') }}
                </div>
                <div class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('home.providers.soon') }}
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </main>

    <footer class="relative z-10 border-t border-gray-200/70 px-6 py-8 dark:border-dark-800/70">
      <div
        class="mx-auto flex max-w-6xl flex-col items-center justify-between gap-4 text-center sm:flex-row sm:text-left"
      >
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >
            {{ t('home.docs') }}
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
.console-window {
  background:
    linear-gradient(180deg, rgba(22, 23, 20, 0.98), rgba(17, 18, 16, 0.98)),
    linear-gradient(135deg, rgba(63, 104, 84, 0.16), transparent 45%);
}

.console-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(255, 255, 255, 0.03);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}
.btn-minimize {
  background: #eab308;
}
.btn-maximize {
  background: #22c55e;
}

.console-title {
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #8b8a81;
}

.console-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}
.line-2 {
  animation-delay: 1s;
}
.line-3 {
  animation-delay: 1.8s;
}
.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}

.code-cmd {
  color: #f4f3ef;
}

.code-flag {
  color: #d4a15a;
}

.code-url {
  color: #7eb492;
}

.code-comment {
  color: #8b8a81;
  font-style: italic;
}

.code-success {
  color: #7eb492;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 9999px;
  font-weight: 600;
}

.code-response {
  color: #f3b49f;
}

.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

.console-metrics {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 16px 20px 20px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.metric-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.04);
}

.metric-label {
  font-size: 11px;
  color: #a7a59b;
}

.metric-value {
  font-size: 13px;
  font-weight: 600;
  color: #f4f3ef;
}

@media (max-width: 1024px) {
  .console-metrics {
    grid-template-columns: 1fr;
  }
}
</style>
