<template>
  <div
    v-if="isShowcaseVariant"
    class="auth-showcase"
    :class="isDark ? 'auth-showcase--night' : 'auth-showcase--day'"
  >
    <div class="auth-showcase__frame">
      <section class="auth-showcase__brand" aria-labelledby="auth-hero-title">
        <div>
          <div class="auth-brand">
            <div class="auth-brand__mark">
              <img :src="siteLogo || '/logo.png'" alt="Logo" />
            </div>
            <div class="auth-brand__copy">
              <div class="auth-brand__name">{{ siteName }}</div>
              <div class="auth-brand__subtitle">{{ siteSubtitle }}</div>
            </div>
          </div>

          <h1 id="auth-hero-title" class="auth-hero__title">
            <span v-for="line in heroTitleLines" :key="line">{{ line }}</span>
          </h1>
          <p class="auth-hero__description">
            {{ heroCopy.description }}
          </p>

          <div class="auth-metrics" aria-hidden="true">
            <div v-for="metric in heroMetrics" :key="metric.label" class="auth-metric">
              <strong>{{ metric.value }}</strong>
              <span>{{ metric.label }}</span>
            </div>
          </div>
        </div>

        <div class="auth-actions">
          <div class="auth-qq" tabindex="0" role="group" :aria-label="qqAriaLabel">
            <div class="auth-qq__popover">
              <img :src="qqQrCode" :alt="t('auth.loginHero.qqQrAlt')" />
              <span>{{ t('auth.loginHero.qqScan') }}</span>
            </div>
            <div class="auth-qq__bar">
              <span>{{ t('auth.loginHero.qqGroup', { number: qqGroupNumber }) }}</span>
              <Icon name="grid" size="sm" :stroke-width="2" class="auth-qq__icon" aria-hidden="true" />
            </div>
          </div>

          <a
            class="auth-codex-link"
            :href="codexDownloadUrl"
            target="_blank"
            rel="noopener noreferrer"
          >
            <span>{{ t('auth.loginHero.codexDownload') }}</span>
            <Icon name="externalLink" size="sm" :stroke-width="2" aria-hidden="true" />
          </a>
        </div>
      </section>

      <section class="auth-showcase__form" :aria-label="authFormLabel">
        <div class="auth-card">
          <div class="auth-card__toolbar">
            <button
              type="button"
              class="auth-theme-toggle"
              :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
              :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')"
              @click="toggleTheme"
            >
              <span class="auth-theme-toggle__track" aria-hidden="true">
                <span class="auth-theme-toggle__thumb"></span>
              </span>
              <Icon v-if="isDark" name="sun" size="sm" />
              <Icon v-else name="moon" size="sm" />
              <span>{{ heroCopy.toggleLabel }}</span>
            </button>
          </div>

          <div class="auth-card__content">
            <slot />
          </div>
        </div>

        <div class="auth-showcase__footer">
          <slot name="footer" />
        </div>

        <div class="auth-showcase__copyright">
          &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
        </div>
      </section>
    </div>
  </div>

  <div v-else class="app-backdrop relative flex min-h-screen items-center justify-center overflow-hidden p-4">
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
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(
  defineProps<{
    variant?: 'default' | 'login' | 'showcase'
    formLabel?: string
  }>(),
  {
    variant: 'default',
    formLabel: ''
  }
)

const { t } = useI18n()
const appStore = useAppStore()
const isDark = ref(false)
const qqGroupNumber = '1009439039'
const qqQrCode = '/qq-group-1009439039.png'
const codexDownloadUrl = 'https://codex.download.icodett.xyz/'

let themeObserver: MutationObserver | null = null

const isShowcaseVariant = computed(() => props.variant === 'login' || props.variant === 'showcase')
const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const currentYear = computed(() => new Date().getFullYear())
const authFormLabel = computed(() => props.formLabel || t('auth.loginPanelTitle'))

const heroCopy = computed(() => {
  const themeKey = isDark.value ? 'night' : 'day'
  return {
    title: t(`auth.loginHero.${themeKey}.title`),
    description: t(`auth.loginHero.${themeKey}.description`),
    toggleLabel: t(`auth.loginHero.${themeKey}.toggleLabel`)
  }
})

const heroMetrics = computed(() => [
  { value: '99.9%', label: t('auth.loginHero.metrics.uptime') },
  { value: '128ms', label: t('auth.loginHero.metrics.latency') },
  { value: '24/7', label: t('auth.loginHero.metrics.watch') }
])

const heroTitleLines = computed(() =>
  heroCopy.value.title.split('\n').map((line) => line.trim()).filter(Boolean)
)

const qqAriaLabel = computed(() =>
  `${t('auth.loginHero.qqGroup', { number: qqGroupNumber })}，${t('auth.loginHero.qqScan')}`
)

function syncThemeState(): void {
  isDark.value = document.documentElement.classList.contains('dark')
}

function toggleTheme(): void {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

onMounted(() => {
  syncThemeState()
  themeObserver = new MutationObserver(syncThemeState)
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class']
  })

  appStore.fetchPublicSettings()
})

onUnmounted(() => {
  themeObserver?.disconnect()
  themeObserver = null
})
</script>

<style scoped>
.auth-showcase {
  position: relative;
  min-height: 100vh;
  overflow-x: hidden;
  color: var(--auth-text);
  background:
    linear-gradient(var(--auth-grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--auth-grid) 1px, transparent 1px),
    linear-gradient(135deg, var(--auth-bg-start), var(--auth-bg-mid) 48%, var(--auth-bg-end));
  background-size: 36px 36px, 36px 36px, auto;
  transition:
    background-color 0.28s ease,
    color 0.28s ease;
}

.auth-showcase--night {
  --auth-bg-start: #04070c;
  --auth-bg-mid: #07111a;
  --auth-bg-end: #0a1019;
  --auth-grid: rgba(108, 240, 255, 0.16);
  --auth-text: #edfaff;
  --auth-muted: rgba(229, 249, 255, 0.7);
  --auth-soft: rgba(229, 249, 255, 0.52);
  --auth-line: rgba(132, 245, 255, 0.32);
  --auth-panel: rgba(10, 24, 38, 0.9);
  --auth-panel-border: rgba(132, 245, 255, 0.34);
  --auth-field: rgba(2, 11, 20, 0.72);
  --auth-field-border: rgba(116, 232, 255, 0.28);
  --auth-primary: #89f6ff;
  --auth-secondary: #52e2b7;
  --auth-button-text: #031017;
  --auth-shadow: rgba(0, 0, 0, 0.42);
}

.auth-showcase--day {
  --auth-bg-start: #fbfdff;
  --auth-bg-mid: #edf8ff;
  --auth-bg-end: #f7fbff;
  --auth-grid: rgba(16, 94, 128, 0.14);
  --auth-text: #132337;
  --auth-muted: rgba(31, 54, 82, 0.72);
  --auth-soft: rgba(31, 54, 82, 0.56);
  --auth-line: rgba(14, 116, 214, 0.26);
  --auth-panel: rgba(255, 255, 255, 0.82);
  --auth-panel-border: rgba(55, 105, 145, 0.24);
  --auth-field: rgba(247, 251, 255, 0.94);
  --auth-field-border: rgba(41, 86, 124, 0.2);
  --auth-primary: #0ea5e9;
  --auth-secondary: #10b981;
  --auth-button-text: #ffffff;
  --auth-shadow: rgba(60, 90, 125, 0.16);
}

.auth-showcase::before {
  content: '';
  position: absolute;
  inset: 54px;
  pointer-events: none;
  border: 1px solid var(--auth-line);
  border-radius: 8px;
}

.auth-showcase__frame {
  position: relative;
  z-index: 1;
  display: grid;
  box-sizing: border-box;
  min-height: 100vh;
  grid-template-columns: minmax(0, 1.05fr) minmax(380px, 440px);
  gap: clamp(2rem, 5vw, 5.5rem);
  align-items: center;
  width: min(1248px, calc(100% - 64px));
  margin: 0 auto;
  padding: clamp(32px, 5vh, 52px) 0;
}

.auth-showcase__brand {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 626px;
  flex-direction: column;
  justify-content: center;
  gap: 44px;
}

.auth-showcase__brand::after {
  content: '';
  position: absolute;
  top: 180px;
  right: clamp(0px, 8vw, 130px);
  width: 340px;
  height: 220px;
  pointer-events: none;
  border: 1px solid transparent;
  border-top-color: var(--auth-line);
  border-radius: 50%;
  opacity: 0.85;
  transform: rotate(-8deg);
}

.auth-brand {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 58px;
}

.auth-brand__mark {
  display: grid;
  width: 48px;
  height: 48px;
  flex: 0 0 auto;
  place-items: center;
  overflow: hidden;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.88);
  background: rgba(255, 255, 255, 0.96);
  box-shadow:
    0 16px 34px color-mix(in srgb, var(--auth-shadow), transparent 18%),
    0 0 0 1px color-mix(in srgb, var(--auth-panel-border), transparent 48%);
}

.auth-brand__mark img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.auth-brand__name {
  font-size: 1.125rem;
  font-weight: 800;
  line-height: 1.2;
}

.auth-brand__subtitle {
  margin-top: 4px;
  color: var(--auth-soft);
  font-size: 0.75rem;
  line-height: 1.4;
}

.auth-hero__title {
  max-width: 720px;
  margin: 0;
  font-size: clamp(3rem, 4vw, 4.15rem);
  font-weight: 900;
  line-height: 1.12;
  letter-spacing: 0;
  word-break: keep-all;
}

.auth-hero__title span {
  display: block;
}

.auth-hero__title span + span {
  margin-top: 0.08em;
}

.auth-hero__description {
  max-width: 560px;
  margin: 26px 0 0;
  color: var(--auth-muted);
  font-size: 1rem;
  line-height: 1.85;
}

.auth-metrics {
  display: grid;
  width: min(100%, 416px);
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 64px;
}

.auth-metric {
  min-height: 78px;
  padding: 14px;
  border: 1px solid var(--auth-panel-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--auth-panel), transparent 16%);
  box-shadow: inset 0 0 28px rgba(255, 255, 255, 0.02);
}

.auth-metric strong {
  display: block;
  font-size: 1.375rem;
  line-height: 1.1;
}

.auth-metric span {
  display: block;
  margin-top: 10px;
  color: var(--auth-soft);
  font-size: 0.6875rem;
  line-height: 1.35;
}

.auth-actions {
  display: flex;
  width: min(100%, 372px);
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.auth-qq {
  position: relative;
  width: min(100%, 216px);
  margin-left: 0;
  opacity: 0.78;
  outline: none;
  transition: opacity 0.18s ease;
}

.auth-qq:hover,
.auth-qq:focus,
.auth-qq:focus-within {
  opacity: 1;
}

.auth-qq__bar {
  display: flex;
  height: 40px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 12px;
  border: 1px solid var(--auth-panel-border);
  border-radius: 8px;
  color: var(--auth-soft);
  background: color-mix(in srgb, var(--auth-panel), transparent 36%);
  font-size: 0.75rem;
}

.auth-qq__icon {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  color: color-mix(in srgb, var(--auth-primary), transparent 18%);
}

.auth-qq__popover {
  position: absolute;
  z-index: 5;
  bottom: 58px;
  left: 92px;
  width: 124px;
  padding: 9px;
  border: 1px solid var(--auth-panel-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--auth-panel), transparent 2%);
  box-shadow: 0 18px 40px var(--auth-shadow);
  opacity: 0;
  transform: translateY(8px);
  visibility: hidden;
  transition:
    opacity 0.18s ease,
    transform 0.18s ease,
    visibility 0.18s ease;
}

.auth-qq:hover .auth-qq__popover,
.auth-qq:focus-within .auth-qq__popover,
.auth-qq:focus .auth-qq__popover {
  opacity: 1;
  transform: translateY(0);
  visibility: visible;
}

.auth-qq__popover img {
  display: block;
  width: 104px;
  height: 104px;
  border-radius: 6px;
  background: #fff;
  object-fit: contain;
}

.auth-qq__popover span {
  display: block;
  margin-top: 7px;
  color: var(--auth-soft);
  font-size: 0.625rem;
  line-height: 1.2;
  text-align: center;
}

.auth-codex-link {
  display: inline-flex;
  height: 40px;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  border: 1px solid var(--auth-panel-border);
  border-radius: 8px;
  color: var(--auth-soft);
  background: color-mix(in srgb, var(--auth-panel), transparent 48%);
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1;
  text-decoration: none;
  opacity: 0.78;
  transition:
    opacity 0.18s ease,
    transform 0.18s ease,
    border-color 0.18s ease,
    color 0.18s ease;
}

.auth-codex-link:hover {
  color: var(--auth-primary);
  opacity: 1;
  transform: translateY(-1px);
}

.auth-codex-link:focus-visible {
  outline: 2px solid var(--auth-primary);
  outline-offset: 3px;
}

.auth-showcase__form {
  min-width: 0;
}

.auth-card {
  position: relative;
  min-height: 626px;
  padding: 28px 30px 30px;
  border: 1px solid var(--auth-panel-border);
  border-radius: 8px;
  background: var(--auth-panel);
  box-shadow: 0 30px 86px var(--auth-shadow);
  backdrop-filter: blur(18px);
}

.auth-card__toolbar {
  display: flex;
  justify-content: flex-start;
  margin-bottom: 28px;
}

.auth-theme-toggle {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  border: 1px solid color-mix(in srgb, var(--auth-primary), transparent 62%);
  border-radius: 999px;
  color: var(--auth-primary);
  background: color-mix(in srgb, var(--auth-primary), transparent 88%);
  font-size: 0.75rem;
  font-weight: 800;
  transition:
    transform 0.18s ease,
    background-color 0.18s ease,
    border-color 0.18s ease;
}

.auth-theme-toggle:hover {
  transform: translateY(-1px);
  border-color: color-mix(in srgb, var(--auth-primary), transparent 40%);
}

.auth-theme-toggle:focus-visible {
  outline: 2px solid var(--auth-primary);
  outline-offset: 3px;
}

.auth-theme-toggle__track {
  position: relative;
  width: 38px;
  height: 20px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--auth-primary), transparent 70%);
}

.auth-theme-toggle__thumb {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 14px;
  height: 14px;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 0 12px color-mix(in srgb, var(--auth-primary), transparent 36%);
  transition: transform 0.18s ease;
}

.auth-showcase--day .auth-theme-toggle__thumb {
  transform: translateX(18px);
}

.auth-card__content :deep(.text-center) {
  text-align: left;
}

.auth-card__content :deep(h2) {
  color: var(--auth-text);
  font-size: 1.75rem;
  line-height: 1.2;
}

.auth-card__content :deep(.input-label) {
  color: var(--auth-muted);
  font-weight: 700;
}

.auth-card__content :deep(.input) {
  min-height: 48px;
  border-color: var(--auth-field-border);
  border-radius: 8px;
  background: var(--auth-field);
  color: var(--auth-text);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.05);
}

.auth-card__content :deep(.input:focus) {
  border-color: color-mix(in srgb, var(--auth-primary), transparent 12%);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--auth-primary), transparent 82%);
}

.auth-card__content :deep(.btn-primary) {
  min-height: 50px;
  border-radius: 8px;
  color: var(--auth-button-text);
  background: linear-gradient(135deg, var(--auth-primary), var(--auth-secondary));
  box-shadow: 0 16px 34px color-mix(in srgb, var(--auth-primary), transparent 74%);
}

.auth-card__content :deep(.btn-secondary),
.auth-card__content :deep(.btn-ghost) {
  border-radius: 8px;
}

.auth-card__content :deep(a) {
  color: var(--auth-primary);
}

.auth-showcase__footer {
  margin-top: 20px;
  color: var(--auth-soft);
  font-size: 0.875rem;
  text-align: center;
}

.auth-showcase__footer :deep(a) {
  color: var(--auth-primary);
  font-weight: 700;
}

.auth-showcase__copyright {
  margin-top: 22px;
  color: var(--auth-soft);
  font-size: 0.75rem;
  text-align: center;
}

@media (max-width: 1024px) {
  .auth-showcase__frame {
    grid-template-columns: 1fr;
    width: min(100% - 32px, 560px);
    gap: 32px;
    padding: 32px 0;
  }

  .auth-showcase__brand {
    display: none;
  }

  .auth-showcase__brand::after {
    display: none;
  }

  .auth-card {
    min-height: auto;
  }
}

@media (max-width: 560px) {
  .auth-showcase::before {
    inset: 16px;
  }

  .auth-showcase__frame {
    width: min(100% - 24px, 440px);
  }

  .auth-metrics {
    grid-template-columns: 1fr;
  }

  .auth-hero__title {
    word-break: normal;
  }

  .auth-qq {
    width: min(100%, 216px);
  }

  .auth-qq__bar {
    flex-wrap: wrap;
    height: auto;
    min-height: 44px;
    padding: 10px 12px;
  }

  .auth-card {
    padding: 22px;
  }
}
</style>
