<template>
  <div class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-teal-500/10 text-teal-600 dark:text-teal-400">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
        </span>
        <h3 class="text-xs font-semibold text-slate-800 dark:text-white">
          {{ allowMetricToggle ? t('admin.dashboard.trafficTrend') : t('admin.dashboard.tokenUsageTrend') }}
        </h3>
      </div>

      <!-- 指标切换器 (仅当 allowMetricToggle 为 true 时展示) -->
      <div v-if="allowMetricToggle" class="flex items-center rounded-lg bg-slate-100 dark:bg-dark-700/60 p-0.5 text-[11px]">
        <button
          type="button"
          class="px-2.5 py-1 rounded-md font-medium transition-colors"
          :class="activeMetric === 'tokens'
            ? 'bg-white dark:bg-dark-600 text-teal-600 dark:text-teal-400 shadow-2xs'
            : 'text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'"
          @click="activeMetric = 'tokens'"
        >
          {{ t('admin.dashboard.tokens') }}
        </button>
        <button
          type="button"
          class="px-2.5 py-1 rounded-md font-medium transition-colors"
          :class="activeMetric === 'requests'
            ? 'bg-white dark:bg-dark-600 text-teal-600 dark:text-teal-400 shadow-2xs'
            : 'text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'"
          @click="activeMetric = 'requests'"
        >
          {{ t('admin.dashboard.requests') }}
        </button>
      </div>
    </div>

    <div v-if="loading" class="flex h-48 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="trendData.length > 0 && chartData" class="h-48">
      <Line :data="chartData" :options="lineOptions" />
    </div>
    <div
      v-else
      class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { TrendDataPoint } from '@/types'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)

const { t } = useI18n()

const props = withDefaults(defineProps<{
  trendData: TrendDataPoint[]
  loading?: boolean
  showCost?: boolean
  allowMetricToggle?: boolean
}>(), {
  loading: false,
  showCost: true,
  allowMetricToggle: false
})

const activeMetric = ref<'tokens' | 'requests'>('tokens')

const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb',
  input: '#3b82f6',
  output: '#10b981',
  cacheCreation: '#f59e0b',
  cacheRead: '#06b6d4',
  cacheHitRate: '#8b5cf6',
  requests: '#14b8a6'
}))

const chartData = computed(() => {
  if (!props.trendData?.length) return null

  const labels = props.trendData.map((d) => d.date)

  if (activeMetric.value === 'requests') {
    return {
      labels,
      datasets: [
        {
          label: t('admin.dashboard.requests'),
          data: props.trendData.map((d) => d.requests),
          borderColor: chartColors.value.requests,
          backgroundColor: `${chartColors.value.requests}20`,
          fill: true,
          tension: 0.35,
          borderWidth: 2,
          pointRadius: 2.5
        }
      ]
    }
  }

  return {
    labels,
    datasets: [
      {
        label: 'Input',
        data: props.trendData.map((d) => d.input_tokens),
        borderColor: chartColors.value.input,
        backgroundColor: `${chartColors.value.input}20`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Output',
        data: props.trendData.map((d) => d.output_tokens),
        borderColor: chartColors.value.output,
        backgroundColor: `${chartColors.value.output}20`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Creation',
        data: props.trendData.map((d) => d.cache_creation_tokens),
        borderColor: chartColors.value.cacheCreation,
        backgroundColor: `${chartColors.value.cacheCreation}20`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Read',
        data: props.trendData.map((d) => d.cache_read_tokens),
        borderColor: chartColors.value.cacheRead,
        backgroundColor: `${chartColors.value.cacheRead}20`,
        fill: true,
        tension: 0.3
      },
      {
        label: 'Cache Hit Rate',
        data: props.trendData.map((d) => {
          const totalPromptTokens = d.input_tokens + d.cache_read_tokens + d.cache_creation_tokens
          return totalPromptTokens > 0 ? (d.cache_read_tokens / totalPromptTokens) * 100 : 0
        }),
        borderColor: chartColors.value.cacheHitRate,
        backgroundColor: `${chartColors.value.cacheHitRate}20`,
        borderDash: [5, 5],
        fill: false,
        tension: 0.3,
        yAxisID: 'yPercent'
      }
    ]
  }
})

const lineOptions = computed(() => {
  const isReq = activeMetric.value === 'requests'

  const scales: any = {
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
        callback: (value: string | number) => (isReq ? Number(value).toLocaleString() : formatTokens(Number(value)))
      }
    }
  }

  if (!isReq) {
    scales.yPercent = {
      position: 'right' as const,
      min: 0,
      max: 100,
      grid: {
        drawOnChartArea: false
      },
      ticks: {
        color: chartColors.value.cacheHitRate,
        font: {
          size: 10
        },
        callback: (value: string | number) => `${value}%`
      }
    }
  }

  return {
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
          padding: 15,
          font: {
            size: 11
          }
        }
      },
      tooltip: {
        callbacks: {
          label: (context: any) => {
            if (context.dataset.yAxisID === 'yPercent') {
              return `${context.dataset.label}: ${context.raw.toFixed(1)}%`
            }
            return isReq
              ? `${context.dataset.label}: ${Number(context.raw).toLocaleString()} 次`
              : `${context.dataset.label}: ${formatTokens(context.raw)}`
          },
          footer: (tooltipItems: any) => {
            if (!props.showCost || isReq) return ''
            const dataIndex = tooltipItems[0]?.dataIndex
            if (dataIndex !== undefined && props.trendData[dataIndex]) {
              const data = props.trendData[dataIndex]
              return `Actual: $${formatCost(data.actual_cost)} | Standard: $${formatCost(data.cost)}`
            }
            return ''
          }
        }
      }
    },
    scales
  }
})

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  if (value >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  } else if (value >= 1) {
    return value.toFixed(2)
  } else if (value >= 0.01) {
    return value.toFixed(3)
  }
  return value.toFixed(4)
}
</script>
