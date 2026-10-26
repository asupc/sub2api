<template>
  <!-- 加载/错误/空态 -->
  <div v-if="loading" class="flex items-center justify-center py-5">
    <LoadingSpinner size="sm" />
  </div>
  <div v-else-if="error" class="mt-3 rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-2.5 text-xs text-slate-500 dark:text-slate-400">
    {{ error }}
  </div>
  <div v-else-if="items.length === 0" class="mt-3 rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-3 text-center text-xs text-slate-400">
    {{ emptyText }}
  </div>

  <!-- 密钥表 -->
  <div v-else class="mt-3 overflow-hidden rounded-xl border border-slate-200 dark:border-dark-700">
    <div class="overflow-x-auto">
      <table class="min-w-full text-left text-xs">
        <thead class="bg-slate-50 dark:bg-dark-700/60">
          <tr class="border-b border-slate-200 text-slate-500 dark:border-dark-700 dark:text-slate-400">
            <th class="w-8 px-2 py-2"></th>
            <th class="px-3 py-2 font-semibold">API 密钥{{ showUsername ? ' / 归属用户' : '' }}</th>
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
                        v-if="showUsername && row.username"
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

  <!-- 底部工具条仅在表格正常展示（非加载/错误/空态）时出现 -->
  <div v-if="!loading && !error && items.length > 0" class="mt-2 flex items-center justify-between">
    <span class="text-[10px] text-slate-400">点击行展开，查看该密钥各模型的速度 / 用量 / 次数 / 缓存命中率 明细</span>
    <button class="text-[11px] text-teal-600 dark:text-teal-400 hover:underline" @click="toggleExpandAll">
      {{ allExpanded ? '收起全部 ▴' : '展开全部 ▾' }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ApiKeyModelUsageStat, ApiKeyUsageStatItem } from '@/types'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = withDefaults(defineProps<{
  items: ApiKeyUsageStatItem[]
  loading?: boolean
  error?: string
  emptyText?: string
  /** 是否展示归属用户（管理端展示，用户端自己的密钥无需展示） */
  showUsername?: boolean
}>(), {
  loading: false,
  error: '',
  emptyText: '暂无 API 密钥调用数据',
  showUsername: true
})

const expanded = ref(new Set<number>())

const allExpanded = computed(() => props.items.length > 0 && expanded.value.size >= props.items.length)

// 数据刷新后收起全部展开行（与原 ApiKeyUsageStats 行为一致）
watch(() => props.items, () => {
  expanded.value = new Set<number>()
})

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
  expanded.value = allExpanded.value ? new Set<number>() : new Set(props.items.map((row) => row.api_key_id))
}

// 缓存命中率口径与 todayCacheHitRate 一致: cache_read / (input + cache_read)
const cacheHitRateOf = (input: number, cacheRead: number): number => {
  const promptTokens = input + cacheRead
  if (promptTokens <= 0) return 0
  return Math.min(100, Math.round((cacheRead / promptTokens) * 100))
}
const cacheHitRate = (row: ApiKeyUsageStatItem): number => cacheHitRateOf(row.input_tokens, row.cache_read_tokens)
const modelCacheHitRate = (m: ApiKeyModelUsageStat): number => cacheHitRateOf(m.input_tokens, m.cache_read_tokens)

const maxTotalTokens = computed(() => props.items.reduce((max, row) => Math.max(max, row.total_tokens), 0))
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
