<template>
  <div>
    <!-- Loading state -->
    <div v-if="props.loading && !props.stats" class="space-y-1">
      <div class="h-2.5 w-16 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
      <div class="h-2.5 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
    </div>

    <!-- Error state -->
    <div v-else-if="props.error && !props.stats" class="text-xs text-red-500">
      {{ props.error }}
    </div>

    <!-- Stats data -->
    <div v-else-if="props.stats" class="flex flex-col gap-0.5 text-[11px] leading-tight">
      <!-- Requests & Tokens -->
      <div class="flex items-center gap-1.5 font-mono text-gray-700 dark:text-gray-300">
        <span class="font-bold">{{ formatNumber(props.stats.requests) }}</span>
        <span class="text-[10px] text-gray-400">req</span>
        <span class="text-gray-300 dark:text-gray-600">·</span>
        <span class="font-bold">{{ formatTokens(props.stats.tokens) }}</span>
      </div>
      <!-- Cost (Account + User) -->
      <div class="flex items-center gap-1.5 font-mono">
        <span class="font-bold text-emerald-600 dark:text-emerald-400" :title="t('usage.accountBilled')">
          ${{ formatCurrency(props.stats.cost) }}
        </span>
        <span v-if="props.stats.user_cost != null" class="text-[10px] text-gray-400 dark:text-gray-500" :title="t('usage.userBilled')">
          (U: ${{ formatCurrency(props.stats.user_cost) }})
        </span>
      </div>
    </div>

    <!-- No data -->
    <div v-else class="text-xs text-gray-400">-</div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { WindowStats } from '@/types'
import { formatNumber, formatCurrency } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    stats?: WindowStats | null
    loading?: boolean
    error?: string | null
  }>(),
  {
    stats: null,
    loading: false,
    error: null
  }
)

const { t } = useI18n()

// Format large token numbers (e.g., 1234567 -> 1.23M)
const formatTokens = (tokens: number): string => {
  if (tokens >= 1000000) {
    return `${(tokens / 1000000).toFixed(2)}M`
  } else if (tokens >= 1000) {
    return `${(tokens / 1000).toFixed(1)}K`
  }
  return tokens.toString()
}
</script>
