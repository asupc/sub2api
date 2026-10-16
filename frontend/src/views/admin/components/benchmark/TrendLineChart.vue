<template>
  <div class="space-y-3">
    <!-- 指标切换工具栏 -->
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="opt in metricOptions"
          :key="opt.key"
          type="button"
          class="px-2.5 py-1 text-xs rounded-lg font-medium transition"
          :class="
            selectedMetric === opt.key
              ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300 font-semibold shadow-xs'
              : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'
          "
          @click="selectedMetric = opt.key"
        >
          {{ opt.label }}
        </button>
      </div>

      <span v-if="hoveredPoint" class="text-xs font-mono text-primary-600 dark:text-primary-400">
        {{ hoveredPoint.time }} · {{ hoveredPoint.formattedValue }}
      </span>
    </div>

    <!-- SVG 折线走势图 -->
    <div v-if="historyPoints.length > 0" class="relative w-full overflow-x-auto rounded-lg border border-gray-100 bg-gray-50/50 p-2 dark:border-dark-800 dark:bg-dark-900/50">
      <svg
        viewBox="0 0 600 200"
        class="w-full h-[180px] min-w-[360px] overflow-visible"
        role="img"
        @mouseleave="hoveredPoint = null"
      >
        <!-- 背景横向刻度线 -->
        <g v-for="grid in gridLines" :key="`grid-${grid.y}`">
          <line
            x1="45"
            x2="590"
            :y1="grid.y"
            :y2="grid.y"
            stroke="currentColor"
            class="text-gray-200 dark:text-dark-800"
            stroke-width="1"
            stroke-dasharray="2 3"
          />
          <text
            x="38"
            :y="grid.y + 3.5"
            text-anchor="end"
            class="fill-gray-400 text-[10px] tabular-nums font-mono select-none"
          >
            {{ grid.label }}
          </text>
        </g>

        <!-- 折线区域渐变填充 -->
        <defs>
          <linearGradient id="trendGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="rgb(59 130 246)" stop-opacity="0.3" />
            <stop offset="100%" stop-color="rgb(59 130 246)" stop-opacity="0.0" />
          </linearGradient>
        </defs>

        <!-- 折线下方填充面积 -->
        <polygon
          v-if="areaPoints"
          :points="areaPoints"
          fill="url(#trendGradient)"
        />

        <!-- 主折线 -->
        <polyline
          :points="polylinePoints"
          fill="none"
          stroke="rgb(59 130 246)"
          stroke-width="2.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />

        <!-- 悬停指示参考竖线 -->
        <line
          v-if="hoveredPoint"
          :x1="hoveredPoint.x"
          :x2="hoveredPoint.x"
          y1="15"
          y2="175"
          stroke="currentColor"
          class="text-primary-400"
          stroke-width="1"
          stroke-dasharray="3 3"
        />

        <!-- 数据节点点阵 -->
        <g v-for="(p, idx) in chartCoordinates" :key="`pt-${idx}`">
          <circle
            :cx="p.x"
            :cy="p.y"
            :r="hoveredPoint?.index === idx ? 5 : 3.5"
            class="fill-white stroke-primary-600 dark:fill-dark-900 dark:stroke-primary-400 cursor-pointer transition-all"
            :stroke-width="hoveredPoint?.index === idx ? 2.5 : 1.8"
            @mouseenter="onHoverPoint(p, idx)"
          />
        </g>
      </svg>
    </div>

    <div v-else class="py-10 text-center text-xs text-gray-400 dark:text-dark-500">
      {{ t('admin.modelBenchmark.drawer.trendEmpty') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelBenchmarkHistoryPoint } from '@/api/admin/modelBenchmarks'

const props = withDefaults(
  defineProps<{
    history?: ModelBenchmarkHistoryPoint[]
  }>(),
  {
    history: () => []
  }
)

const { t } = useI18n()

type MetricKey = 'intelligence_score' | 'composite_score' | 'efficiency_score' | 'avg_latency_ms' | 'output_tokens'

const selectedMetric = ref<MetricKey>('intelligence_score')

const metricOptions = computed(() => [
  { key: 'intelligence_score' as MetricKey, label: t('admin.modelBenchmark.fields.intelligence') },
  { key: 'composite_score' as MetricKey, label: t('admin.modelBenchmark.fields.composite') },
  { key: 'efficiency_score' as MetricKey, label: t('admin.modelBenchmark.fields.efficiency') },
  { key: 'avg_latency_ms' as MetricKey, label: t('admin.modelBenchmark.fields.latency') },
  { key: 'output_tokens' as MetricKey, label: t('admin.modelBenchmark.fields.outputTokens') }
])

const historyPoints = computed(() => {
  return [...props.history]
    .filter(p => p[selectedMetric.value] != null)
    .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
})

const metricValues = computed(() => {
  return historyPoints.value.map(p => Number(p[selectedMetric.value] ?? 0))
})

const minVal = computed(() => {
  if (!metricValues.value.length) return 0
  const min = Math.min(...metricValues.value)
  return selectedMetric.value.includes('score') ? Math.max(0, Math.floor(min - 5)) : Math.floor(min * 0.9)
})

const maxVal = computed(() => {
  if (!metricValues.value.length) return 100
  const max = Math.max(...metricValues.value)
  return selectedMetric.value.includes('score') ? Math.min(100, Math.ceil(max + 5)) : Math.ceil(max * 1.1)
})

const gridLines = computed(() => {
  const steps = 4
  const delta = (maxVal.value - minVal.value) || 1
  return Array.from({ length: steps + 1 }, (_, i) => {
    const val = maxVal.value - (delta * i) / steps
    const y = 20 + (i * 150) / steps
    return {
      y,
      label: formatValue(val)
    }
  })
})

const chartCoordinates = computed(() => {
  const pts = historyPoints.value
  if (!pts.length) return []
  const delta = (maxVal.value - minVal.value) || 1

  return pts.map((p, idx) => {
    const val = Number(p[selectedMetric.value] ?? 0)
    const x = pts.length === 1 ? 317 : 45 + (idx * (590 - 45)) / (pts.length - 1)
    const y = 170 - ((val - minVal.value) / delta) * 150
    return {
      x,
      y,
      val,
      time: new Date(p.created_at).toLocaleString(),
      runId: p.run_id
    }
  })
})

const polylinePoints = computed(() => {
  return chartCoordinates.value.map(p => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')
})

const areaPoints = computed(() => {
  const coords = chartCoordinates.value
  if (coords.length < 2) return ''
  const first = coords[0]
  const last = coords[coords.length - 1]
  const line = polylinePoints.value
  return `${first.x.toFixed(1)},170 ${line} ${last.x.toFixed(1)},170`
})

interface HoverState {
  index: number
  x: number
  y: number
  time: string
  formattedValue: string
  runId: number
}

const hoveredPoint = ref<HoverState | null>(null)

function onHoverPoint(p: { x: number; y: number; val: number; time: string; runId: number }, index: number) {
  hoveredPoint.value = {
    index,
    x: p.x,
    y: p.y,
    time: p.time,
    formattedValue: formatValue(p.val),
    runId: p.runId
  }
}

function formatValue(val: number): string {
  if (selectedMetric.value.includes('score')) {
    return val.toFixed(1)
  }
  if (selectedMetric.value === 'avg_latency_ms') {
    return val >= 1000 ? `${(val / 1000).toFixed(1)}s` : `${Math.round(val)}ms`
  }
  if (selectedMetric.value === 'output_tokens') {
    return val >= 1000 ? `${(val / 1000).toFixed(1)}k` : String(Math.round(val))
  }
  return val.toFixed(1)
}
</script>
