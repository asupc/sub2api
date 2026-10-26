<template>
  <div class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm">
    <!-- 标题行 -->
    <div class="flex flex-wrap items-center justify-between gap-2 pb-3 border-b border-slate-100 dark:border-dark-700/60">
      <div class="flex items-center gap-2">
        <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-teal-500/10 text-teal-500">
          <Icon name="key" size="xs" />
        </span>
        <h3 class="text-xs font-semibold text-slate-800 dark:text-white">{{ t('usage.apiKeyUsage') }}</h3>
        <span class="inline-flex items-center rounded-full bg-teal-500/10 px-2 py-0.5 text-[10px] font-semibold text-teal-600 dark:text-teal-400">
          {{ rangeLabel }}
        </span>
      </div>
      <span class="text-[10px] text-slate-400">{{ hintText }}</span>
    </div>

    <ApiKeyUsageTable
      :items="items"
      :loading="loading"
      :error="error"
      :empty-text="emptyText"
      :show-username="scope === 'admin'"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { usageAPI } from '@/api'
import type { ApiKeyUsageStatItem } from '@/types'
import ApiKeyUsageTable from './ApiKeyUsageTable.vue'
import Icon from '@/components/icons/Icon.vue'

type UsageScope = 'admin' | 'user'

const props = defineProps<{
  /** admin: 全站密钥；user: 当前用户自己的密钥 */
  scope: UsageScope
  startDate: string
  endDate: string
  /** 父页面刷新计数：递增即触发重新加载（与日期变化同一通道，避免重复请求） */
  reloadKey: number
}>()

const { t } = useI18n()

const items = ref<ApiKeyUsageStatItem[]>([])
const loading = ref(false)
const error = ref('')
let reqSeq = 0

/** 单次请求的密钥条数上限（与管理端接口默认值一致） */
const STATS_LIMIT = 50

const rangeLabel = computed(() => `${props.startDate} ~ ${props.endDate}`)
const emptyText = computed(() => t('usage.noApiKeyUsage', { range: rangeLabel.value }))

const loadStats = async () => {
  const seq = ++reqSeq
  loading.value = true
  error.value = ''
  try {
    const params = { start_date: props.startDate, end_date: props.endDate, limit: STATS_LIMIT }
    const res = props.scope === 'admin'
      ? await adminAPI.dashboard.getApiKeyUsageStats(params)
      : await usageAPI.getDashboardApiKeysUsageStats(params)
    if (seq !== reqSeq) return
    items.value = res?.items || []
  } catch (err: any) {
    if (seq !== reqSeq) return
    console.error('Error loading API key usage stats:', err)
    items.value = []
    error.value = err?.message || t('usage.loadApiKeyUsageFailed')
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

const toFiniteNumber = (value: unknown): number => {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

const formatNumber = (value: number | null | undefined): string => toFiniteNumber(value).toLocaleString()

/** 达到单次上限时说明只展示前 N 个，避免把返回条数误报成密钥总数 */
const hintText = computed(() => {
  const count = formatNumber(items.value.length)
  return items.value.length >= STATS_LIMIT
    ? t('usage.apiKeyUsageHintTruncated', { count })
    : t('usage.apiKeyUsageHint', { count })
})

watch(() => [props.scope, props.startDate, props.endDate, props.reloadKey], () => {
  void loadStats()
})

onMounted(() => {
  void loadStats()
})
</script>
