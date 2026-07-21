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
    class="home-showcase"
    :class="isDark ? 'home-showcase--night' : 'home-showcase--day'"
  >
    <div class="home-showcase__frame"></div>

    <div class="home-shell">
      <header class="home-topbar">
        <router-link to="/home" class="home-brand" :aria-label="siteName">
          <span class="home-brand__logo">
            <img :src="siteLogo || '/logo.svg'" :alt="siteName" />
          </span>
          <span class="home-brand__text">
            <span class="home-brand__name">{{ siteName }}</span>
            <span class="home-brand__subtitle">{{ siteSubtitle }}</span>
          </span>
        </router-link>

        <nav class="home-nav" :aria-label="t('home.redesign.primaryNav')">
          <LocaleSwitcher class="home-nav__locale" />

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="home-icon-button"
            :title="t('home.viewDocs')"
            :aria-label="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>

          <button
            type="button"
            class="home-theme-toggle"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <span class="home-theme-toggle__track">
              <span class="home-theme-toggle__thumb">
                <Icon v-if="isDark" name="sun" size="xs" />
                <Icon v-else name="moon" size="xs" />
              </span>
            </span>
            <span class="home-theme-toggle__label">
              {{ isDark ? t('home.redesign.lightsOn') : t('home.redesign.lightsOff') }}
            </span>
          </button>

          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="home-button home-button--primary home-button--nav"
          >
            <span class="home-user-dot">{{ userInitial }}</span>
            <span>{{ t('home.dashboard') }}</span>
          </router-link>
          <router-link
            v-else
            to="/login"
            class="home-button home-button--primary home-button--nav"
          >
            {{ t('home.login') }}
          </router-link>
        </nav>
      </header>

      <main class="home-main">
        <section class="home-hero" :aria-labelledby="heroTitleId">
          <div class="home-hero__copy">
            <div class="home-chip-row">
              <span class="home-chip">{{ t('home.redesign.chips.stableHub') }}</span>
              <span class="home-chip">{{ t('home.redesign.chips.smartRoute') }}</span>
              <span class="home-chip">{{ t('home.redesign.chips.usageBilling') }}</span>
            </div>

            <h1 :id="heroTitleId" class="home-hero__title">
              <span v-for="line in heroTitleLines" :key="line">{{ line }}</span>
            </h1>

            <p class="home-hero__description">
              {{ heroDescription }}
            </p>

            <div class="home-actions">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="home-button home-button--primary home-button--large"
              >
                <span>{{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}</span>
                <Icon name="arrowRight" size="sm" :stroke-width="2" />
              </router-link>

              <a
                v-if="docUrl"
                :href="docUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="home-button home-button--ghost home-button--large"
              >
                {{ t('home.docs') }}
              </a>
            </div>

            <div class="home-status-grid" :aria-label="t('home.redesign.statusTitle')">
              <div class="home-status-card">
                <strong>99.9%</strong>
                <span>{{ t('home.redesign.status.routeUptime') }}</span>
              </div>
              <div class="home-status-card">
                <strong>128ms</strong>
                <span>{{ t('home.redesign.status.avgLatency') }}</span>
              </div>
              <div class="home-status-card">
                <strong>24/7</strong>
                <span>{{ t('home.redesign.status.edgeWatch') }}</span>
              </div>
            </div>
          </div>

          <aside class="command-center" :aria-label="t('home.redesign.commandCenter')">
            <div class="command-center__header">
              <div>
                <p class="command-center__kicker">{{ t('home.redesign.commandCenter') }}</p>
                <h2>{{ panelTitle }}</h2>
              </div>
              <span class="command-center__live">Live</span>
            </div>

            <div class="command-center__mesh">
              <div class="command-node">
                <span class="command-node__label">ROUTE</span>
                <strong class="command-node__value command-node__value--cyan">healthy</strong>
                <span class="command-node__sub">4 upstreams</span>
              </div>
              <div class="command-node">
                <span class="command-node__label">BILLING</span>
                <strong class="command-node__value command-node__value--green">$0.0023</strong>
                <span class="command-node__sub">per request</span>
              </div>
              <div class="command-node">
                <span class="command-node__label">MODEL</span>
                <strong class="command-node__value command-node__value--amber">Claude</strong>
                <span class="command-node__sub">auto selected</span>
              </div>
            </div>

            <div class="home-terminal">
              <div class="home-terminal__top">
                <span class="home-terminal__dots" aria-hidden="true">
                  <i></i>
                  <i></i>
                  <i></i>
                </span>
                <span class="home-terminal__title">gateway.runtime</span>
              </div>
              <div class="home-terminal__body">
                <div>
                  <span class="home-code--green">$</span>
                  curl
                  <span class="home-code--amber">-X POST</span>
                  <span class="home-code--cyan">/v1/messages</span>
                </div>
                <div class="home-code--muted"># {{ t('home.redesign.terminalComment') }}</div>
                <div>
                  <span class="home-code--green">200 OK</span>
                  <span class="home-code--muted">{ "provider": "best-route" }</span>
                </div>
                <div>
                  <span class="home-code--green">$</span>
                  <span class="home-terminal__cursor"></span>
                </div>
              </div>
            </div>
          </aside>
        </section>

        <section class="home-section" :aria-labelledby="featuresTitleId">
          <div class="home-section__head">
            <div>
              <h2 :id="featuresTitleId">{{ t('home.redesign.featureSectionTitle') }}</h2>
              <p>{{ t('home.redesign.featureSectionSubtitle') }}</p>
            </div>

            <router-link
              :to="isAuthenticated ? dashboardPath : '/login'"
              class="home-button home-button--ghost"
            >
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
            </router-link>
          </div>

          <div class="home-feature-grid">
            <article class="home-feature-card">
              <span class="home-feature-card__icon">
                <Icon name="swap" size="md" />
              </span>
              <h3>{{ t('home.features.unifiedGateway') }}</h3>
              <p>{{ t('home.features.unifiedGatewayDesc') }}</p>
            </article>

            <article class="home-feature-card">
              <span class="home-feature-card__icon">
                <Icon name="shield" size="md" />
              </span>
              <h3>{{ t('home.features.multiAccount') }}</h3>
              <p>{{ t('home.features.multiAccountDesc') }}</p>
            </article>

            <article class="home-feature-card">
              <span class="home-feature-card__icon">
                <Icon name="dollar" size="md" />
              </span>
              <h3>{{ t('home.features.balanceQuota') }}</h3>
              <p>{{ t('home.features.balanceQuotaDesc') }}</p>
            </article>
          </div>
        </section>

        <section class="home-section" :aria-labelledby="providersTitleId">
          <div class="home-section__head home-section__head--compact">
            <div>
              <h2 :id="providersTitleId">{{ t('home.providers.title') }}</h2>
              <p>{{ t('home.redesign.providerDescription') }}</p>
            </div>
          </div>

          <div class="home-model-grid">
            <article class="home-model-card">
              <span class="home-model-card__badge home-model-card__badge--claude">C</span>
              <div>
                <h3>{{ t('home.providers.claude') }}</h3>
                <p>{{ t('home.providers.supported') }}</p>
              </div>
            </article>

            <article class="home-model-card">
              <span class="home-model-card__badge home-model-card__badge--gpt">G</span>
              <div>
                <h3>GPT</h3>
                <p>{{ t('home.providers.supported') }}</p>
              </div>
            </article>

            <article class="home-model-card">
              <span class="home-model-card__badge home-model-card__badge--gemini">G</span>
              <div>
                <h3>{{ t('home.providers.gemini') }}</h3>
                <p>{{ t('home.providers.supported') }}</p>
              </div>
            </article>

            <article class="home-model-card">
              <span class="home-model-card__badge home-model-card__badge--antigravity">A</span>
              <div>
                <h3>{{ t('home.providers.antigravity') }}</h3>
                <p>{{ t('home.providers.supported') }}</p>
              </div>
            </article>

            <article class="home-model-card home-model-card--muted">
              <span class="home-model-card__badge home-model-card__badge--more">+</span>
              <div>
                <h3>{{ t('home.providers.more') }}</h3>
                <p>{{ t('home.providers.soon') }}</p>
              </div>
            </article>
          </div>
        </section>
      </main>

      <footer class="home-footer">
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <div class="home-footer__links">
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">
            {{ t('home.docs') }}
          </a>
          <router-link :to="isAuthenticated ? dashboardPath : '/login'">
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
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

const heroTitleId = 'home-hero-title'
const featuresTitleId = 'home-features-title'
const providersTitleId = 'home-providers-title'

const panelTitle = computed(() =>
  t('home.redesign.panelTitle')
)

const heroTitleLines = computed(() =>
  (isDark.value ? t('auth.loginHero.night.title') : t('auth.loginHero.day.title')).split('\n')
)

const heroDescription = computed(() =>
  isDark.value ? t('auth.loginHero.night.description') : t('auth.loginHero.day.description')
)

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
  const shouldUseDark =
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)

  isDark.value = shouldUseDark
  document.documentElement.classList.toggle('dark', shouldUseDark)
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
.home-showcase {
  --home-bg-start: #04070c;
  --home-bg-mid: #07111a;
  --home-bg-end: #0a1019;
  --home-grid: rgba(108, 240, 255, 0.14);
  --home-text: #edfaff;
  --home-muted: rgba(229, 249, 255, 0.72);
  --home-soft: rgba(229, 249, 255, 0.52);
  --home-line: rgba(132, 245, 255, 0.32);
  --home-panel: rgba(10, 24, 38, 0.9);
  --home-panel-soft: rgba(10, 24, 38, 0.62);
  --home-field: rgba(2, 11, 20, 0.72);
  --home-primary: #89f6ff;
  --home-secondary: #52e2b7;
  --home-accent: #f7c56a;
  --home-danger: #f2779a;
  --home-button-text: #031017;
  --home-shadow: rgba(0, 0, 0, 0.42);
  --home-terminal-bg: rgba(2, 8, 15, 0.86);
  position: relative;
  min-height: 100vh;
  overflow: hidden;
  color: var(--home-text);
  background:
    linear-gradient(var(--home-grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--home-grid) 1px, transparent 1px),
    linear-gradient(135deg, var(--home-bg-start), var(--home-bg-mid) 46%, var(--home-bg-end));
  background-size: 36px 36px, 36px 36px, auto;
}

.home-showcase--day {
  --home-bg-start: #fbfdff;
  --home-bg-mid: #edf8ff;
  --home-bg-end: #f7fbff;
  --home-grid: rgba(16, 94, 128, 0.13);
  --home-text: #132337;
  --home-muted: rgba(31, 54, 82, 0.76);
  --home-soft: rgba(31, 54, 82, 0.56);
  --home-line: rgba(14, 116, 214, 0.25);
  --home-panel: rgba(255, 255, 255, 0.84);
  --home-panel-soft: rgba(255, 255, 255, 0.64);
  --home-field: rgba(247, 251, 255, 0.94);
  --home-primary: #0ea5e9;
  --home-secondary: #10b981;
  --home-accent: #b7791f;
  --home-danger: #be4264;
  --home-button-text: #ffffff;
  --home-shadow: rgba(60, 90, 125, 0.18);
  --home-terminal-bg: rgba(250, 254, 255, 0.92);
}

.home-showcase__frame {
  position: absolute;
  inset: 42px;
  pointer-events: none;
  border: 1px solid var(--home-line);
  border-radius: 8px;
}

.home-shell {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 100vh;
  flex-direction: column;
  width: min(100%, 1480px);
  margin: 0 auto;
  padding: 34px 42px 28px;
}

.home-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  min-height: 72px;
  padding: 12px 14px;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-panel), transparent 10%);
  box-shadow: 0 18px 52px var(--home-shadow);
  backdrop-filter: blur(18px);
}

.home-brand {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 14px;
  text-decoration: none;
}

.home-brand__logo {
  display: grid;
  width: 48px;
  height: 48px;
  flex: 0 0 auto;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.96);
  box-shadow:
    0 14px 30px color-mix(in srgb, var(--home-shadow), transparent 24%),
    0 0 0 1px color-mix(in srgb, var(--home-line), transparent 48%);
}

.home-brand__logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.home-brand__text {
  min-width: 0;
}

.home-brand__name {
  display: block;
  overflow: hidden;
  color: var(--home-text);
  font-size: 17px;
  font-weight: 800;
  line-height: 1.2;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.home-brand__subtitle {
  display: block;
  overflow: hidden;
  margin-top: 4px;
  color: var(--home-soft);
  font-size: 12px;
  line-height: 1.2;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.home-nav {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.home-nav__locale {
  color: var(--home-soft);
}

.home-nav__locale :deep(button) {
  color: var(--home-soft);
}

.home-nav__locale :deep(button:hover) {
  color: var(--home-text);
  background: color-mix(in srgb, var(--home-panel), transparent 46%);
}

.home-icon-button,
.home-theme-toggle,
.home-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  text-decoration: none;
  white-space: nowrap;
  transition:
    transform 0.18s ease,
    border-color 0.18s ease,
    background-color 0.18s ease,
    box-shadow 0.18s ease,
    color 0.18s ease;
}

.home-icon-button {
  width: 38px;
  height: 38px;
  border: 1px solid var(--home-line);
  background: color-mix(in srgb, var(--home-panel), transparent 48%);
  color: var(--home-primary);
}

.home-icon-button:hover,
.home-theme-toggle:hover,
.home-button:hover {
  transform: translateY(-1px);
}

.home-icon-button:focus-visible,
.home-theme-toggle:focus-visible,
.home-button:focus-visible,
.home-brand:focus-visible {
  outline: 2px solid var(--home-primary);
  outline-offset: 3px;
}

.home-theme-toggle {
  height: 38px;
  gap: 8px;
  padding: 0 12px;
  border: 1px solid color-mix(in srgb, var(--home-primary), transparent 58%);
  background: color-mix(in srgb, var(--home-primary), transparent 88%);
  color: var(--home-primary);
  font-size: 13px;
  font-weight: 800;
}

.home-theme-toggle__track {
  position: relative;
  width: 36px;
  height: 18px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--home-primary), transparent 70%);
}

.home-theme-toggle__thumb {
  position: absolute;
  top: 3px;
  left: 3px;
  display: grid;
  width: 12px;
  height: 12px;
  place-items: center;
  border-radius: 999px;
  background: #ffffff;
  color: #0f172a;
  box-shadow: 0 0 12px color-mix(in srgb, var(--home-primary), transparent 30%);
  transition: transform 0.22s ease;
}

.home-showcase--day .home-theme-toggle__thumb {
  transform: translateX(18px);
}

.home-button {
  min-height: 44px;
  gap: 10px;
  padding: 0 18px;
  border: 1px solid transparent;
  font-size: 14px;
  font-weight: 800;
}

.home-button--large {
  min-height: 46px;
  padding: 0 20px;
}

.home-button--nav {
  min-height: 38px;
  padding: 0 14px;
}

.home-button--primary {
  color: var(--home-button-text);
  background: linear-gradient(135deg, var(--home-primary), var(--home-secondary));
  box-shadow: 0 16px 34px color-mix(in srgb, var(--home-primary), transparent 74%);
}

.home-button--ghost {
  border-color: var(--home-line);
  background: color-mix(in srgb, var(--home-panel), transparent 42%);
  color: var(--home-muted);
}

.home-user-dot {
  display: grid;
  width: 22px;
  height: 22px;
  place-items: center;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.22);
  color: var(--home-button-text);
  font-size: 11px;
  font-weight: 900;
}

.home-main {
  flex: 1;
}

.home-hero {
  display: grid;
  grid-template-columns: minmax(0, 0.92fr) minmax(430px, 0.9fr);
  gap: 46px;
  align-items: center;
  min-height: 520px;
  padding: 66px 0 34px;
}

.home-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 24px;
}

.home-chip {
  display: inline-flex;
  min-height: 34px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 0 12px;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-panel), transparent 52%);
  color: var(--home-soft);
  font-size: 12px;
  font-weight: 760;
}

.home-hero__title {
  max-width: 640px;
  margin: 0;
  color: var(--home-text);
  font-size: clamp(3rem, 7vw, 5.25rem);
  font-weight: 900;
  letter-spacing: 0;
  line-height: 1.06;
}

.home-hero__title span {
  display: block;
}

.home-hero__description {
  max-width: 620px;
  margin: 28px 0 0;
  color: var(--home-muted);
  font-size: 19px;
  line-height: 1.85;
}

.home-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 34px;
}

.home-status-grid {
  display: grid;
  width: min(100%, 468px);
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 46px;
}

.home-status-card {
  min-height: 84px;
  padding: 14px;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-panel), transparent 18%);
}

.home-status-card strong {
  display: block;
  color: var(--home-text);
  font-size: 23px;
  line-height: 1.1;
}

.home-status-card span {
  display: block;
  margin-top: 10px;
  color: var(--home-soft);
  font-size: 12px;
}

.command-center {
  position: relative;
  min-height: 494px;
  overflow: hidden;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: var(--home-panel);
  box-shadow: 0 30px 86px var(--home-shadow);
  backdrop-filter: blur(18px);
}

.command-center::before {
  content: "";
  position: absolute;
  inset: 24px 26px auto auto;
  width: 230px;
  height: 150px;
  pointer-events: none;
  border: 1px solid transparent;
  border-top-color: var(--home-line);
  border-radius: 50%;
  transform: rotate(-8deg);
}

.command-center__header {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 22px 24px;
  border-bottom: 1px solid var(--home-line);
}

.command-center__kicker {
  margin: 0;
  color: var(--home-soft);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.command-center__header h2 {
  margin: 8px 0 0;
  color: var(--home-text);
  font-size: 20px;
  font-weight: 880;
  line-height: 1.35;
}

.command-center__live {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 32px;
  padding: 0 10px;
  border: 1px solid var(--home-line);
  border-radius: 999px;
  color: var(--home-primary);
  font-size: 12px;
  font-weight: 800;
}

.command-center__live::before {
  content: "";
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--home-secondary);
  box-shadow: 0 0 14px var(--home-secondary);
}

.command-center__mesh {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  padding: 24px;
}

.command-node {
  min-height: 96px;
  padding: 14px;
  border: 1px solid color-mix(in srgb, var(--home-line), transparent 18%);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-field), transparent 16%);
}

.command-node__label,
.command-node__sub {
  display: block;
  color: var(--home-soft);
  font-size: 11px;
}

.command-node__label {
  font-weight: 800;
}

.command-node__value {
  display: block;
  margin-top: 12px;
  font-family: "Cascadia Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 18px;
  font-weight: 800;
  line-height: 1.2;
}

.command-node__sub {
  margin-top: 8px;
}

.command-node__value--cyan,
.home-code--cyan {
  color: var(--home-primary);
}

.command-node__value--green,
.home-code--green {
  color: var(--home-secondary);
}

.command-node__value--amber,
.home-code--amber {
  color: var(--home-accent);
}

.home-terminal {
  position: relative;
  z-index: 1;
  margin: 0 24px 24px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--home-line), transparent 18%);
  border-radius: 8px;
  background: var(--home-terminal-bg);
}

.home-terminal__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 42px;
  padding: 0 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--home-line), transparent 18%);
}

.home-terminal__dots {
  display: flex;
  gap: 7px;
}

.home-terminal__dots i {
  width: 10px;
  height: 10px;
  border-radius: 50%;
}

.home-terminal__dots i:nth-child(1) {
  background: #f45f64;
}

.home-terminal__dots i:nth-child(2) {
  background: #f1bd45;
}

.home-terminal__dots i:nth-child(3) {
  background: #35cc6b;
}

.home-terminal__title {
  color: var(--home-soft);
  font-family: "Cascadia Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}

.home-terminal__body {
  padding: 18px;
  color: var(--home-text);
  font-family: "Cascadia Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 14px;
  line-height: 2;
}

.home-code--muted {
  color: var(--home-soft);
}

.home-terminal__cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  border-radius: 1px;
  background: var(--home-primary);
  vertical-align: middle;
  animation: home-cursor 1s step-end infinite;
}

@keyframes home-cursor {
  0%,
  50% {
    opacity: 1;
  }

  51%,
  100% {
    opacity: 0;
  }
}

.home-section {
  margin-top: 34px;
}

.home-section__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 18px;
}

.home-section__head h2 {
  margin: 0;
  color: var(--home-text);
  font-size: 28px;
  line-height: 1.2;
}

.home-section__head p {
  max-width: 560px;
  margin: 8px 0 0;
  color: var(--home-soft);
  font-size: 14px;
  line-height: 1.7;
}

.home-feature-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.home-feature-card {
  min-height: 174px;
  padding: 18px;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-panel), transparent 14%);
  box-shadow: inset 0 0 28px rgba(255, 255, 255, 0.02);
}

.home-feature-card__icon {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-primary), transparent 84%);
  color: var(--home-primary);
}

.home-feature-card h3 {
  margin: 18px 0 8px;
  color: var(--home-text);
  font-size: 17px;
}

.home-feature-card p {
  margin: 0;
  color: var(--home-muted);
  font-size: 13px;
  line-height: 1.7;
}

.home-model-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}

.home-model-card {
  display: flex;
  min-height: 78px;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--home-line);
  border-radius: 8px;
  background: color-mix(in srgb, var(--home-panel), transparent 16%);
}

.home-model-card--muted {
  opacity: 0.82;
}

.home-model-card__badge {
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 8px;
  color: #ffffff;
  font-weight: 900;
}

.home-model-card__badge--claude {
  background: #d97745;
}

.home-model-card__badge--gpt {
  background: #3f6854;
}

.home-model-card__badge--gemini {
  background: #4f7ac8;
}

.home-model-card__badge--antigravity {
  background: #b75872;
}

.home-model-card__badge--more {
  background: #637083;
}

.home-model-card h3 {
  overflow: hidden;
  margin: 0;
  color: var(--home-text);
  font-size: 14px;
  font-weight: 850;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.home-model-card p {
  margin: 4px 0 0;
  color: var(--home-soft);
  font-size: 12px;
}

.home-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-top: auto;
  padding-top: 30px;
  color: var(--home-soft);
  font-size: 12px;
}

.home-footer p {
  margin: 0;
}

.home-footer__links {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 14px;
}

.home-footer a {
  text-decoration: none;
  transition: color 0.18s ease;
}

.home-footer a:hover {
  color: var(--home-text);
}

@media (max-width: 1180px) {
  .home-shell {
    padding: 24px;
  }

  .home-showcase__frame {
    inset: 24px;
  }

  .home-hero {
    grid-template-columns: 1fr;
    gap: 30px;
    padding-top: 44px;
  }

  .command-center {
    min-height: auto;
  }

  .home-model-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 820px) {
  .home-showcase {
    background-size: 28px 28px, 28px 28px, auto;
  }

  .home-showcase__frame {
    display: none;
  }

  .home-shell {
    padding: 16px;
  }

  .home-topbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .home-brand,
  .home-nav {
    width: 100%;
  }

  .home-nav {
    flex-wrap: wrap;
    justify-content: flex-start;
  }

  .home-hero {
    min-height: auto;
    padding: 38px 0 18px;
  }

  .home-hero__title {
    font-size: clamp(2.75rem, 13vw, 4.2rem);
  }

  .home-hero__description {
    font-size: 16px;
    line-height: 1.75;
  }

  .home-status-grid,
  .command-center__mesh,
  .home-feature-grid,
  .home-model-grid {
    grid-template-columns: 1fr;
  }

  .home-status-grid {
    width: 100%;
    margin-top: 30px;
  }

  .home-section__head {
    align-items: flex-start;
    flex-direction: column;
  }

  .home-section__head .home-button {
    width: 100%;
  }

  .home-footer {
    align-items: flex-start;
    flex-direction: column;
  }

  .home-footer__links {
    justify-content: flex-start;
  }
}

@media (max-width: 520px) {
  .home-brand__subtitle,
  .home-theme-toggle__label {
    display: none;
  }

  .home-icon-button,
  .home-theme-toggle {
    width: 38px;
    padding: 0;
  }

  .home-theme-toggle {
    gap: 0;
  }

  .home-button--nav {
    flex: 1 1 120px;
  }

  .home-actions .home-button {
    width: 100%;
  }

  .command-center__header {
    flex-direction: column;
  }

  .home-terminal {
    margin: 0 14px 16px;
  }

  .home-terminal__body {
    overflow-x: auto;
    white-space: nowrap;
  }
}

@media (prefers-reduced-motion: reduce) {
  .home-icon-button,
  .home-theme-toggle,
  .home-button,
  .home-theme-toggle__thumb,
  .home-terminal__cursor {
    transition: none;
    animation: none;
  }
}
</style>
