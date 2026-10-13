<template>
  <div class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm">
    <!-- 标题行 -->
    <div class="flex flex-wrap items-center justify-between gap-2 pb-3 border-b border-slate-100 dark:border-dark-700/60">
      <div class="flex items-center gap-2">
        <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-teal-500/10 text-teal-500">
          <Icon name="key" size="xs" />
        </span>
        <h3 class="text-xs font-semibold text-slate-800 dark:text-white">API 密钥用量统计</h3>
        <span class="inline-flex items-center rounded-full bg-teal-500/10 px-2 py-0.5 text-[10px] font-semibold text-teal-600 dark:text-teal-400">
          {{ rangeLabel }}
        </span>
      </div>
      <div class="flex items-center gap-2">
        <!-- 时间范围快捷切换 -->
        <div class="flex items-center rounded-lg bg-slate-100 dark:bg-dark-700/60 p-0.5 text-[11px]">
          <button
            v-for="opt in rangeOptions"
            :key="opt.value"
            class="px-2.5 py-1 rounded-md font-medium transition-colors"
            :class="range === opt.value
              ? 'bg-white dark:bg-dark-600 text-teal-600 dark:text-teal-400 shadow-sm'
              : 'text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'"
            @click="setRange(opt.value)"
          >
            {{ opt.label }}
          </button>
        </div>
        <span class="text-[10px] text-slate-400">按 Token 排序 · 共 {{ formatNumber(items.length) }} 个密钥</span>
      </div>
    </div>

    <!-- 加载/错误/空态 -->
    <div v-if="loading" class="flex items-center justify-center py-5">
      <LoadingSpinner size="sm" />
    </div>
    <div v-else-if="error" class="mt-3 rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-2.5 text-xs text-slate-500 dark:text-slate-400">
      {{ error }}
    </div>
    <div v-else-if="items.length === 0" class="mt-3 rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-3 text-center text-xs text-slate-400">
      {{ rangeLabel }}暂无 API 密钥调用数据
    </div>

    <!-- 密钥表 -->
    <div v-else class="mt-3 overflow-hidden rounded-xl border border-slate-200 dark:border-dark-700">
      <div class="overflow-x-auto">
        <table class="min-w-full text-left text-xs">
          <thead class="bg-slate-50 dark:bg-dark-700/60">
            <tr class="border-b border-slate-200 text-slate-500 dark:border-dark-700 dark:text-slate-400">
              <th class="w-8 px-2 py-2"></th>
              <th class="px-3 py-2 font-semibold">API 密钥 / 归属用户</th>
              <th class="px-3 py-2 text-right font-semibold">请求数</th>
              <th class="px-3 py-2 text-right font-semibold">总 Token</th>
              <th class="px-3 py-2 text-right font-semibold">输入 / 输出</th>
              <th class="px-3 py-2 text-right font-semibold">缓存命中率</th>
              <th class="px-3 py-2 text-right font-semibold">平均 Tokens/秒</th>
              <th class="w-6 px-2 py-2"></th>
            </tr>
          </thead>
          <tbody>
            <template v-for="row in items" :key="row.api_key_id">
              <tr
                class="border-b border-slate-100 text-slate-700 dark:border-dark-800 dark:text-slate-200 cursor-pointer hover:bg-slate-50/70 dark:hover:bg-dark-700/40 transition-colors"
                :class="expanded.has(row.api_key_id) ? 'bg-slate-50/60 dark:bg-dark-700/30' : ''"
                @click="toggleExpand(row.api_key_id)"
              >
                <td class="px-2 py-1.5 align-middle text-slate-400">
                  <svg
                    class="mx-auto transition-transform"
                    :class="expanded.has(row.api_key_id) ? 'rotate-90' : ''"
                    width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"
                  >
                    <path d="m9 18 6-6-6-6" />
                  </svg>
                </td>
                <td class="px-3 py-1.5">
                  <div class="flex items-center gap-2 min-w-0">
                    <span
                      class="h-2 w-2 rounded-full shrink-0"
                      :class="cacheHitRate(row) >= 90 ? 'bg-cyan-400' : cacheHitRate(row) >= 70 ? 'bg-amber-500' : 'bg-slate-400'"
                      :title="`缓存命中率: ${cacheHitRate(row)}%`"
                    ></span>
                    <div class="min-w-0">
                      <div class="flex items-center gap-1.5 min-w-0">
                        <span class="font-medium text-slate-800 dark:text-slate-100 truncate" :title="row.name">{{ row.name || `密钥 #${row.api_key_id}` }}</span>
                        <span
                          v-if="row.username"
                          class="shrink-0 rounded bg-slate-100 dark:bg-dark-700 px-1 py-px text-[10px] text-slate-500 dark:text-slate-400"
                        >{{ row.username }}</span>
                      </div>
                    </div>
                  </div>
                </td>
                <td class="px-3 py-1.5 text-right tabular-nums">{{ formatNumber(row.request_count) }}</td>
                <td class="px-3 py-1.5 text-right">
                  <div class="font-bold tabular-nums text-slate-900 dark:text-white">{{ formatTokens(row.total_tokens) }}</div>
                  <div class="sharebar mt-1 ml-auto w-20">
                    <i :style="{ width: sharePct(row) + '%', background: '#14b8a6' }"></i>
                  </div>
                </td>
                <td class="px-3 py-1.5 text-right tabular-nums whitespace-nowrap">
                  <span class="text-sky-600 dark:text-sky-400">{{ formatTokens(row.input_tokens) }}</span>
                  <span class="text-slate-300 dark:text-dark-600 mx-0.5">/</span>
                  <span class="text-violet-600 dark:text-violet-400">{{ formatTokens(row.output_tokens) }}</span>
                </td>
                <td class="px-3 py-1.5 text-right">
                  <span
                    class="rounded px-1.5 py-0.5 text-[10px] font-medium tabular-nums"
                    :class="cacheHitRate(row) > 0
                      ? 'bg-cyan-50 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
                      : 'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500'"
                  >⚡ {{ cacheHitRate(row) }}%</span>
                </td>
                <td class="px-3 py-1.5 text-right tabular-nums">{{ formatRate(row.avg_tokens_per_sec) }}</td>
                <td class="px-2 py-1.5"></td>
              </tr>

              <!-- 展开行：该密钥各模型明细 -->
              <tr v-if="expanded.has(row.api_key_id)" class="border-b border-slate-100 dark:border-dark-800">
                <td colspan="8" class="px-0 py-0">
                  <div class="bg-slate-50/80 dark:bg-dark-900/60 border-t border-dashed border-slate-200 dark:border-dark-700/70 px-8 py-2.5">
                    <div class="flex items-center gap-1.5 mb-1.5">
                      <span class="text-[10px] font-semibold text-slate-500 dark:text-slate-400">模型用量明细 · {{ row.name || `密钥 #${row.api_key_id}` }}</span>
                      <span class="text-[9px] text-slate-400">速度 / 用量 / 次数 / 缓存命中率</span>
                    </div>
                    <div v-if="row.models.length === 0" class="py-2 text-[11px] text-slate-400">
                      暂无模型明细数据
                    </div>
                    <table v-else class="min-w-full text-left text-[11px]">
                      <thead>
                        <tr class="text-slate-400 border-b border-slate-200/70 dark:border-dark-700/70">
                          <th class="py-1 pr-3 font-medium">模型</th>
                          <th class="py-1 px-3 text-right font-medium">请求数</th>
                          <th class="py-1 px-3 text-right font-medium">Tokens/秒</th>
                          <th class="py-1 px-3 text-right font-medium">总 Token</th>
                          <th class="py-1 px-3 text-right font-medium">输入</th>
                          <th class="py-1 px-3 text-right font-medium">输出</th>
                          <th class="py-1 px-3 text-right font-medium">缓存命中率</th>
                          <th class="py-1 pl-3 text-right font-medium">平均时长(ms)</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr
                          v-for="m in row.models"
                          :key="m.model"
                          class="border-b border-slate-100/70 dark:border-dark-800/60 last:border-b-0 text-slate-600 dark:text-slate-300"
                        >
                          <td class="py-1 pr-3">
                            <div class="flex items-center gap-1.5">
                              <span class="h-1.5 w-1.5 rounded-full shrink-0 bg-slate-400"></span>
                              <span class="font-medium truncate" :title="m.model">{{ m.model }}</span>
                            </div>
                          </td>
                          <td class="py-1 px-3 text-right tabular-nums">{{ formatNumber(m.request_count) }}</td>
                          <td class="py-1 px-3 text-right tabular-nums">{{ formatRate(m.avg_tokens_per_sec) }}</td>
                          <td class="py-1 px-3 text-right font-semibold tabular-nums text-slate-800 dark:text-slate-100">{{ formatTokens(m.total_tokens) }}</td>
                          <td class="py-1 px-3 text-right tabular-nums text-sky-600 dark:text-sky-400">{{ formatTokens(m.input_tokens) }}</td>
                          <td class="py-1 px-3 text-right tabular-nums text-violet-600 dark:text-violet-400">{{ formatTokens(m.output_tokens) }}</td>
                          <td class="py-1 px-3 text-right">
                            <span class="text-cyan-600 dark:text-cyan-400 tabular-nums">⚡ {{ modelCacheHitRate(m) }}%</span>
                          </td>
                          <td class="py-1 pl-3 text-right tabular-nums">{{ formatNumber(m.avg_duration_ms) }}</td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <div v-if="items.length > 0" class="mt-2 flex items-center justify-between">
      <span class="text-[10px] text-slate-400">点击行展开，查看该密钥各模型的速度 / 用量 / 次数 / 缓存命中率 明细</span>
      <button class="text-[11px] text-teal-600 dark:text-teal-400 hover:underline" @click="toggleExpandAll">
        {{ allExpanded ? '收起全部 ▴' : '展开全部 ▾' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adminAPI } from '@/api/admin'
import type { ApiKeyUsageStatItem, ApiKeyModelUsageStat } from '@/api/admin/dashboard'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'

type RangeValue = 'today' | 'yesterday' | '7d' | '30d'

const rangeOptions: { value: RangeValue; label: string }[] = [
  { value: 'today', label: '今天' },
  { value: 'yesterday', label: '昨天' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' }
]

const range = ref<RangeValue>('today')
const rangeLabel = computed(() => rangeOptions.find((o) => o.value === range.value)?.label ?? '今天')

const items = ref<ApiKeyUsageStatItem[]>([])
const loading = ref(false)
const error = ref('')
const expanded = ref(new Set<number>())

const allExpanded = computed(() => items.value.length > 0 && expanded.value.size >= items.value.length)

const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getRangeDates = (value: RangeValue): { start: string; end: string } => {
  const today = new Date()
  const fmt = (d: Date) => formatLocalDate(d)
  if (value === 'today') {
    return { start: fmt(today), end: fmt(today) }
  }
  const shifted = new Date(today)
  if (value === 'yesterday') {
    shifted.setDate(today.getDate() - 1)
    return { start: fmt(shifted), end: fmt(shifted) }
  }
  shifted.setDate(today.getDate() - (value === '7d' ? 6 : 29))
  return { start: fmt(shifted), end: fmt(today) }
}

const loadStats = async () => {
  loading.value = true
  error.value = ''
  expanded.value = new Set()
  try {
    const dates = getRangeDates(range.value)
    if (typeof adminAPI?.dashboard?.getApiKeyUsageStats !== 'function') {
      throw new Error('API 不可用')
    }
    const res = await adminAPI.dashboard.getApiKeyUsageStats({
      start_date: dates.start,
      end_date: dates.end,
      limit: 50
    })
    items.value = res?.items || []
  } catch (err: any) {
    console.error('Error loading API key usage stats:', err)
    items.value = []
    error.value = err?.message || '加载 API 密钥用量统计失败'
  } finally {
    loading.value = false
  }
}

const setRange = (value: RangeValue) => {
  if (range.value === value) return
  range.value = value
  loadStats()
}

const toggleExpand = (apiKeyId: number) => {
  const next = new Set(expanded.value)
  if (next.has(apiKeyId)) {
    next.delete(apiKeyId)
  } else {
    next.add(apiKeyId)
  }
  expanded.value = next
}

const toggleExpandAll = () => {
  expanded.value = allExpanded.value ? new Set<number>() : new Set(items.value.map((row) => row.api_key_id))
}

// 缓存命中率口径与 todayCacheHitRate 一致: cache_read / (input + cache_read)
const cacheHitRateOf = (input: number, cacheRead: number): number => {
  const promptTokens = input + cacheRead
  if (promptTokens <= 0) return 0
  return Math.min(100, Math.round((cacheRead / promptTokens) * 100))
}
const cacheHitRate = (row: ApiKeyUsageStatItem): number => cacheHitRateOf(row.input_tokens, row.cache_read_tokens)
const modelCacheHitRate = (m: ApiKeyModelUsageStat): number => cacheHitRateOf(m.input_tokens, m.cache_read_tokens)

const maxTotalTokens = computed(() => items.value.reduce((max, row) => Math.max(max, row.total_tokens), 0))
const sharePct = (row: ApiKeyUsageStatItem): number => {
  if (maxTotalTokens.value <= 0) return 0
  return Math.max(4, Math.round((row.total_tokens / maxTotalTokens.value) * 100))
}

const toFiniteNumber = (value: unknown): number => {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

const formatTokens = (value: number | null | undefined): string => {
  const v = toFiniteNumber(value)
  if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(2)}B`
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`
  if (v >= 1_000) return `${(v / 1_000).toFixed(2)}K`
  return v.toLocaleString()
}

const formatNumber = (value: number | null | undefined): string => toFiniteNumber(value).toLocaleString()

const formatRate = (value: number | null | undefined): string => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return value.toFixed(2)
}

onMounted(() => {
  loadStats()
})

defineExpose({ loadStats })
</script>

<style scoped>
.sharebar {
  height: 4px;
  border-radius: 2px;
  background: rgba(148, 163, 184, 0.25);
  overflow: hidden;
}
.sharebar > i {
  display: block;
  height: 100%;
  border-radius: 2px;
}
</style>
