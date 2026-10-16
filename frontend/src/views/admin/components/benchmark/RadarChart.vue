<template>
  <div class="relative flex flex-col items-center justify-center p-2">
    <svg :viewBox="`0 0 ${size} ${size}`" class="w-full max-w-[320px] aspect-square overflow-visible">
      <!-- 背景多边形同心圆网格 (25%, 50%, 75%, 100%) -->
      <polygon
        v-for="level in [0.25, 0.5, 0.75, 1.0]"
        :key="`level-${level}`"
        :points="getPolygonPoints(level)"
        class="fill-transparent stroke-gray-200 dark:stroke-dark-700"
        :stroke-width="level === 1 ? '1.5' : '1'"
        :stroke-dasharray="level < 1 ? '3 3' : undefined"
      />

      <!-- 从中心辐射的 9 根轴线 -->
      <line
        v-for="(axis, index) in axes"
        :key="`axis-${axis.key}`"
        :x1="center"
        :y1="center"
        :x2="getAxisEnd(index).x"
        :y2="getAxisEnd(index).y"
        class="stroke-gray-200 dark:stroke-dark-700"
        stroke-width="1"
      />

      <!-- 模型能力得分多边形 (渐变填充 + 描边) -->
      <polygon
        v-if="hasData"
        :points="scorePolygonPoints"
        class="fill-primary-500/25 stroke-primary-600 dark:fill-primary-400/20 dark:stroke-primary-400 transition-all duration-300"
        stroke-width="2.2"
        stroke-linejoin="round"
      />

      <!-- 各维度得分数据点圆圈与悬停交互 -->
      <g v-if="hasData">
        <circle
          v-for="(point, index) in dataPoints"
          :key="`point-${axes[index].key}`"
          :cx="point.x"
          :cy="point.y"
          r="4"
          class="fill-white stroke-primary-600 dark:fill-dark-900 dark:stroke-primary-400 transition-transform hover:scale-125"
          stroke-width="2"
        >
          <title>{{ axes[index].label }}: {{ point.score }}分</title>
        </circle>
      </g>

      <!-- 维度文字标签与得分 -->
      <text
        v-for="(axis, index) in axes"
        :key="`label-${axis.key}`"
        :x="getLabelPos(index).x"
        :y="getLabelPos(index).y"
        text-anchor="middle"
        dominant-baseline="central"
        class="text-[11px] font-medium fill-gray-600 dark:fill-dark-300 select-none"
      >
        {{ axis.label }}
        <tspan v-if="hasData" class="font-bold fill-primary-600 dark:fill-primary-400 ml-0.5">
          {{ scores[axis.key] != null ? Math.round(scores[axis.key]) : '—' }}
        </tspan>
      </text>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    scores?: Record<string, number>
    size?: number
  }>(),
  {
    scores: () => ({}),
    size: 340
  }
)

const { t } = useI18n()

const categoryKeys = [
  'math',
  'algorithms',
  'logic',
  'code',
  'planning',
  'instruction',
  'abstraction',
  'understanding',
  'causal'
] as const

const axes = computed(() =>
  categoryKeys.map(key => ({
    key,
    label: t(`admin.modelBenchmark.category.${key}`)
  }))
)

const center = computed(() => props.size / 2)
const radius = computed(() => props.size * 0.36)
const angleStep = computed(() => (2 * Math.PI) / categoryKeys.length)

const hasData = computed(() => {
  return props.scores && Object.keys(props.scores).length > 0
})

function getAngle(index: number): number {
  // 从正上方 (-PI/2) 开始顺时针旋转
  return -Math.PI / 2 + index * angleStep.value
}

function getPolygonPoints(scale: number): string {
  const r = radius.value * scale
  return axes.value
    .map((_, i) => {
      const angle = getAngle(i)
      const x = center.value + r * Math.cos(angle)
      const y = center.value + r * Math.sin(angle)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

function getAxisEnd(index: number) {
  const angle = getAngle(index)
  return {
    x: center.value + radius.value * Math.cos(angle),
    y: center.value + radius.value * Math.sin(angle)
  }
}

function getLabelPos(index: number) {
  const angle = getAngle(index)
  // 标签稍微超出半径边缘
  const labelRadius = radius.value + 26
  return {
    x: center.value + labelRadius * Math.cos(angle),
    y: center.value + labelRadius * Math.sin(angle)
  }
}

const dataPoints = computed(() => {
  return axes.value.map((axis, i) => {
    const rawScore = props.scores?.[axis.key] ?? 0
    // 归一化得分 0~100 到 0~1
    const normalized = Math.min(Math.max(rawScore / 100, 0), 1)
    const r = radius.value * normalized
    const angle = getAngle(i)
    return {
      x: center.value + r * Math.cos(angle),
      y: center.value + r * Math.sin(angle),
      score: Math.round(rawScore)
    }
  })
})

const scorePolygonPoints = computed(() => {
  return dataPoints.value.map(p => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')
})
</script>
