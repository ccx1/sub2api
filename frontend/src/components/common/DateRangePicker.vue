<template>
  <div class="relative" ref="containerRef">
    <button
      ref="triggerRef"
      type="button"
      @click="toggle"
      :class="['date-picker-trigger', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Teleport to="body">
      <Transition name="date-picker-dropdown">
        <div
          v-if="isOpen"
          ref="dropdownRef"
          class="date-picker-dropdown"
          :style="dropdownStyle"
        >
          <!-- Quick presets -->
          <div class="date-picker-presets">
            <button
              v-for="preset in presets"
              :key="preset.value"
              @click="selectPreset(preset)"
              :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
            >
              {{ t(preset.labelKey) }}
            </button>
          </div>

          <div class="date-picker-divider"></div>

          <!-- Custom date range inputs -->
          <div class="date-picker-custom">
            <div class="date-picker-field">
              <label class="date-picker-label">{{ t('dates.startDate') }}</label>
              <input
                type="date"
                v-model="localStartDate"
                :max="localEndDate || tomorrow"
                class="date-picker-input"
                @change="onDateChange"
              />
            </div>
            <div class="date-picker-separator">
              <Icon name="arrowRight" size="sm" class="text-gray-400" />
            </div>
            <div class="date-picker-field">
              <label class="date-picker-label">{{ t('dates.endDate') }}</label>
              <input
                type="date"
                v-model="localEndDate"
                :min="localStartDate"
                :max="tomorrow"
                class="date-picker-input"
                @change="onDateChange"
              />
            </div>
          </div>

          <!-- Apply button -->
          <div class="date-picker-actions">
            <button @click="apply" class="date-picker-apply">
              {{ t('dates.apply') }}
            </button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

interface Props {
  startDate: string
  endDate: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownStyle = ref<Record<string, string>>({ top: '0px', left: '0px' })
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>('last24Hours')

const today = computed(() => {
  // Use local timezone to avoid UTC timezone issues
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
})

// Tomorrow's date - used for max date to handle timezone differences
// When user is in a timezone behind the server, "today" on server might be "tomorrow" locally
const tomorrow = computed(() => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatDateToString(d)
})

// Helper function to format date to YYYY-MM-DD using local timezone
const formatDateToString = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today.value
      return { start: t, end: t }
    }
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatDateToString(d)
      return { start: yesterday, end: yesterday }
    }
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    getRange: () => {
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return {
        start: formatDateToString(start),
        end: formatDateToString(end)
      }
    }
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today.value }
    }
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    }
  }
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr + 'T00:00:00')
  const dateLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
}

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
}

const onDateChange = () => {
  // Check if current dates match any preset
  activePreset.value = null
  for (const preset of presets) {
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      break
    }
  }
}

const toggle = () => {
  if (isOpen.value) {
    isOpen.value = false
    return
  }

  updateDropdownPosition()
  isOpen.value = true
}

const apply = () => {
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value
  })
  isOpen.value = false
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as Node
  const insideTrigger = containerRef.value?.contains(target)
  const insideDropdown = dropdownRef.value?.contains(target)
  if (!insideTrigger && !insideDropdown) {
    isOpen.value = false
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
  }
}

function updateDropdownPosition() {
  const trigger = triggerRef.value
  if (!trigger) return

  const rect = trigger.getBoundingClientRect()
  const margin = 8
  const dropdown = dropdownRef.value
  const dropdownWidth = dropdown?.offsetWidth ?? Math.min(320, window.innerWidth - margin * 2)
  const dropdownHeight = dropdown?.offsetHeight ?? 260
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight

  let top = rect.bottom + margin
  if (top + dropdownHeight > viewportHeight - margin) {
    top = Math.max(margin, rect.top - dropdownHeight - margin)
  }

  let left = rect.left
  if (left + dropdownWidth > viewportWidth - margin) {
    left = Math.max(margin, viewportWidth - dropdownWidth - margin)
  }
  if (left < margin) {
    left = margin
  }

  dropdownStyle.value = {
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`
  }
}

function addPositionListeners() {
  window.addEventListener('scroll', updateDropdownPosition, true)
  window.addEventListener('resize', updateDropdownPosition)
}

function removePositionListeners() {
  window.removeEventListener('scroll', updateDropdownPosition, true)
  window.removeEventListener('resize', updateDropdownPosition)
}

// Sync local state with props
watch(
  () => props.startDate,
  (val) => {
    localStartDate.value = val
    onDateChange()
  }
)

watch(
  () => props.endDate,
  (val) => {
    localEndDate.value = val
    onDateChange()
  }
)

watch(isOpen, async (open) => {
  if (open) {
    await nextTick()
    updateDropdownPosition()
    addPositionListeners()
    return
  }
  removePositionListeners()
})

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
  // Initialize active preset detection
  onDateChange()
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
  removePositionListeners()
})
</script>

<style scoped>
.date-picker-trigger {
  @apply flex items-center gap-2;
  @apply rounded-lg px-3 py-2 text-sm;
  @apply border transition-all duration-200;
  @apply focus:outline-none;
  @apply cursor-pointer;
  border-color: rgba(var(--border-rgb), 0.95);
  background: rgba(var(--surface-rgb), 0.78);
  color: var(--text-muted);
  box-shadow: 0 1px 2px rgba(var(--text-rgb), 0.06);
  backdrop-filter: blur(14px);
}

.date-picker-trigger:hover {
  border-color: rgba(var(--border-strong-rgb), 0.95);
  background: rgba(var(--surface-elevated-rgb), 0.96);
  color: var(--text);
}

.date-picker-trigger:focus-visible {
  box-shadow: var(--shadow-focus);
}

.date-picker-trigger-open {
  border-color: rgba(var(--primary-rgb), 0.56);
  background: rgba(var(--surface-rgb), 0.94);
  color: var(--text);
  box-shadow:
    var(--shadow-focus),
    0 1px 2px rgba(var(--text-rgb), 0.06);
}

.date-picker-icon {
  color: var(--text-soft);
}

.date-picker-value {
  @apply font-medium;
}

.date-picker-chevron {
  color: var(--text-soft);
}

.date-picker-dropdown {
  @apply fixed z-[9999];
  @apply rounded-xl;
  @apply border;
  @apply overflow-hidden;
  border-color: rgba(var(--border-rgb), 0.78);
  background:
    linear-gradient(180deg, rgba(var(--surface-rgb), 0.98), rgba(var(--surface-elevated-rgb), 0.94));
  box-shadow: var(--shadow-panel);
  color: var(--text);
  transform-origin: top left;
  backdrop-filter: blur(18px);
  width: min(320px, calc(100vw - 16px));
  max-width: calc(100vw - 16px);
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply rounded-md px-3 py-1.5 text-xs font-medium;
  @apply transition-colors duration-150;
  color: var(--text-muted);
}

.date-picker-preset:hover {
  background: rgba(var(--surface-strong-rgb), 0.52);
  color: var(--text);
}

.date-picker-preset-active {
  background:
    linear-gradient(135deg, rgba(var(--primary-rgb), 0.14), rgba(var(--primary-strong-rgb), 0.12));
  color: var(--primary);
  box-shadow: inset 0 0 0 1px rgba(var(--primary-rgb), 0.18);
}

.date-picker-divider {
  border-top: 1px solid rgba(var(--border-rgb), 0.66);
}

.date-picker-custom {
  @apply flex items-end gap-2 p-3;
}

.date-picker-field {
  @apply flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium;
  color: var(--text-muted);
}

.date-picker-input {
  @apply w-full rounded-md px-2 py-1.5 text-sm;
  @apply border;
  @apply focus:outline-none;
  border-color: rgba(var(--border-rgb), 0.86);
  background: rgba(var(--surface-rgb), 0.82);
  color: var(--text);
}

.date-picker-input:focus {
  border-color: rgba(var(--primary-rgb), 0.8);
  box-shadow: var(--shadow-focus);
}

.date-picker-input::-webkit-calendar-picker-indicator {
  @apply cursor-pointer opacity-60 hover:opacity-100;
  filter: invert(0.5);
}

:global(.dark) .date-picker-input::-webkit-calendar-picker-indicator {
  filter: invert(0.7);
}

.date-picker-separator {
  @apply flex items-center justify-center pb-1;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply {
  @apply rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply transition-colors duration-150;
  color: #ffffff;
  background: linear-gradient(135deg, var(--primary) 0%, var(--primary-strong) 100%);
  box-shadow: 0 12px 24px rgba(var(--primary-rgb), 0.18);
}

.date-picker-apply:hover {
  box-shadow: 0 14px 28px rgba(var(--primary-rgb), 0.24);
  filter: saturate(1.04);
}

:global(.dark) .date-picker-apply {
  border: 1px solid rgba(var(--primary-rgb), 0.22);
  background: linear-gradient(135deg, rgba(18, 116, 140, 0.92) 0%, rgba(21, 132, 111, 0.9) 100%);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 12px 26px rgba(0, 0, 0, 0.22);
}

:global(.dark) .date-picker-apply:hover {
  background: linear-gradient(135deg, rgba(23, 132, 158, 0.94) 0%, rgba(25, 148, 125, 0.92) 100%);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.1),
    0 14px 28px rgba(0, 0, 0, 0.26);
  filter: none;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: scaleY(0.96);
}
</style>
