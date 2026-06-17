<template>
  <div class="card p-4">
    <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
      <h3 class="min-w-0 text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('admin.dashboard.accountUsageTrend') }}
      </h3>
      <div class="flex shrink-0 rounded-lg border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-600 dark:bg-dark-800">
        <button
          v-for="option in metricOptions"
          :key="option.value"
          type="button"
          class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors"
          :class="metric === option.value
            ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400'
            : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
          @click="metric = option.value"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <div v-if="loading" class="flex h-64 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="chartData" class="h-64">
      <Line :data="chartData" :options="lineOptions" />
    </div>
    <div
      v-else
      class="flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { AccountUsageTrendPoint } from '@/types'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const { t } = useI18n()

const props = defineProps<{
  trendData: AccountUsageTrendPoint[]
  loading?: boolean
}>()

type AccountTrendMetric = 'tokens' | 'requests' | 'actual_cost' | 'account_cost'

const metric = ref<AccountTrendMetric>('tokens')
const metricOptions = computed<Array<{ value: AccountTrendMetric; label: string }>>(() => [
  { value: 'tokens', label: t('admin.dashboard.tokens') },
  { value: 'requests', label: t('admin.dashboard.requests') },
  { value: 'actual_cost', label: t('admin.dashboard.actual') },
  { value: 'account_cost', label: t('admin.dashboard.accountCost') }
])

const colors = [
  '#2563eb',
  '#059669',
  '#d97706',
  '#dc2626',
  '#7c3aed',
  '#db2777',
  '#0d9488',
  '#ea580c',
  '#4f46e5',
  '#65a30d'
]

const isDarkMode = computed(() => document.documentElement.classList.contains('dark'))

const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

const accountLabel = (point: AccountUsageTrendPoint): string => {
  const name = point.account_name?.trim()
  return name || `#${point.account_id}`
}

const metricValue = (point: AccountUsageTrendPoint): number => {
  if (metric.value === 'requests') return point.requests
  if (metric.value === 'actual_cost') return point.actual_cost
  if (metric.value === 'account_cost') return point.account_cost
  return point.tokens
}

const chartData = computed(() => {
  if (!props.trendData?.length) return null

  const dates = new Set<string>()
  const accounts = new Map<number, { label: string; data: Map<string, number> }>()
  props.trendData.forEach((point) => {
    dates.add(point.date)
    if (!accounts.has(point.account_id)) {
      accounts.set(point.account_id, { label: accountLabel(point), data: new Map() })
    }
    accounts.get(point.account_id)!.data.set(point.date, metricValue(point))
  })

  const sortedDates = Array.from(dates).sort()
  const datasets = Array.from(accounts.values()).map((account, index) => ({
    label: account.label,
    data: sortedDates.map((date) => account.data.get(date) || 0),
    borderColor: colors[index % colors.length],
    backgroundColor: `${colors[index % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}

const formatMetric = (value: number): string => {
  if (metric.value === 'actual_cost' || metric.value === 'account_cost') {
    return `$${formatCost(value)}`
  }
  return formatTokens(value)
}

const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 12,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.dataset.label}: ${formatMetric(Number(context.raw || 0))}`
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatMetric(Number(value))
      }
    }
  }
}))
</script>
