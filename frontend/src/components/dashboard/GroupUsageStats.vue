<template>
  <div class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm space-y-3">
    <div class="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-dark-700/60">
      <div class="flex items-center gap-2">
        <span class="h-2 w-2 rounded-full bg-teal-500"></span>
        <h3 class="text-xs font-semibold text-slate-800 dark:text-white">
          {{ t('admin.dashboard.groupDistribution') }}
        </h3>
      </div>
      <span class="text-[10px] text-slate-400">
        {{ sortedGroups.length > 0 ? `共 ${sortedGroups.length} 个活跃分组 · 按流量排序` : '暂无分组数据' }}
      </span>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="flex items-center justify-center py-8">
      <LoadingSpinner size="sm" />
    </div>

    <!-- 空数据 -->
    <div v-else-if="sortedGroups.length === 0" class="py-8 text-center text-xs text-slate-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>

    <!-- 分组列表 -->
    <div v-else class="space-y-2 text-xs max-h-[260px] overflow-y-auto pr-1">
      <div
        v-for="group in sortedGroups"
        :key="group.group_id"
        class="p-2.5 rounded-lg bg-slate-50 dark:bg-dark-700/40 border border-slate-100 dark:border-dark-700/60 transition-colors hover:bg-slate-100/70 dark:hover:bg-dark-700/60"
      >
        <div class="flex items-center justify-between mb-1.5">
          <div class="flex items-center gap-1.5 min-w-0">
            <span class="font-semibold text-slate-800 dark:text-slate-100 truncate" :title="group.group_name">
              {{ group.group_name || `分组 #${group.group_id}` }}
            </span>
          </div>
          <div class="flex items-center gap-2 tabular-nums shrink-0">
            <span class="font-bold text-slate-900 dark:text-white">
              {{ formatTokens(group.total_tokens) }} ({{ group.percentage }}%)
            </span>
            <span class="text-[10px] text-slate-400">
              {{ formatNumber(group.requests) }} reqs
            </span>
          </div>
        </div>
        <div class="sharebar">
          <i :style="{ width: group.percentage + '%', background: group.color }"></i>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupStat } from '@/types'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = withDefaults(
  defineProps<{
    groupStats?: GroupStat[]
    loading?: boolean
  }>(),
  {
    groupStats: () => [],
    loading: false
  }
)

const { t } = useI18n()

const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatNumber = (value: number | null | undefined): string => {
  return (value || 0).toLocaleString()
}

const PALETTE = [
  '#14b8a6', // teal
  '#0ea5e9', // sky
  '#8b5cf6', // violet
  '#f59e0b', // amber
  '#ec4899', // pink
  '#10b981', // emerald
  '#6366f1'  // indigo
]

const sortedGroups = computed(() => {
  const groups = props.groupStats || []
  if (!groups.length) return []

  const totalTokens = groups.reduce((sum, g) => sum + (g.total_tokens || 0), 0)

  const active = groups.filter((g) => (g.total_tokens || 0) > 0 || (g.requests || 0) > 0)
  const sorted = [...active].sort((a, b) => (b.total_tokens || 0) - (a.total_tokens || 0))

  return sorted.map((g, idx) => {
    const pct = totalTokens > 0 ? Math.round(((g.total_tokens || 0) / totalTokens) * 100) : 0
    return {
      ...g,
      percentage: pct,
      color: PALETTE[idx % PALETTE.length]
    }
  })
})
</script>

<style scoped>
.sharebar {
  height: 4px;
  border-radius: 2px;
  background: rgba(148, 163, 184, 0.2);
  overflow: hidden;
}
.sharebar > i {
  display: block;
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease;
}
</style>
