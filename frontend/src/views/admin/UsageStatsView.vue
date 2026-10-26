<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- 时间范围控制条（页面置顶，统计与图表共用） -->
      <div class="card p-3.5">
        <div class="flex flex-wrap items-center justify-between gap-4">
          <div class="flex flex-wrap items-center gap-3">
            <span class="text-xs font-medium text-slate-600 dark:text-slate-300">{{ t('admin.dashboard.timeRange') }}:</span>
            <DateRangePicker
              v-model:start-date="startDate"
              v-model:end-date="endDate"
              @change="onDateRangeChange"
            />
            <button @click="refreshAll" :disabled="loading" class="btn btn-secondary btn-sm">
              {{ t('common.refresh') }}
            </button>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-xs font-medium text-slate-600 dark:text-slate-300">{{ t('admin.dashboard.granularity') }}:</span>
            <div class="w-28">
              <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
            </div>
          </div>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="card flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else>
        <!-- Bento Hero: 与仪表盘同款渐变卡（全站视角，无费用/成本指标） -->
        <div class="card p-5 relative overflow-hidden bg-gradient-to-br from-white via-white to-slate-50/70 dark:from-dark-800 dark:via-dark-800 dark:to-dark-900/60 border border-slate-200/80 dark:border-dark-700 shadow-sm space-y-4">
          <div class="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4 pb-4 border-b border-slate-100 dark:border-dark-700/60">
            <div class="flex items-center gap-3">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-teal-500/10 text-teal-600 dark:bg-teal-500/20 dark:text-teal-400 shrink-0">
                <Icon name="chart" size="md" :stroke-width="2" />
              </div>
              <div>
                <div class="flex items-center gap-2">
                  <h2 class="text-sm font-semibold text-slate-800 dark:text-white">全站用量总览</h2>
                  <span class="inline-flex items-center gap-1 rounded-full bg-teal-500/10 px-2 py-0.5 text-[10px] font-semibold text-teal-600 dark:text-teal-400">
                    <span class="h-1.5 w-1.5 rounded-full bg-teal-500 animate-pulse"></span>
                    {{ rangeBadgeText }} · 全部用户
                  </span>
                </div>
                <p class="text-xs text-slate-400 dark:text-slate-500">全站请求与 Token 汇总</p>
              </div>
            </div>

            <div class="flex flex-wrap items-center gap-2.5">
              <div class="rounded-xl bg-slate-50/90 dark:bg-dark-700/50 px-3.5 py-1.5 border border-slate-200/60 dark:border-dark-700 shadow-2xs">
                <div class="flex items-center justify-between gap-3">
                  <span class="text-[10px] font-medium text-slate-500 dark:text-slate-400">总请求</span>
                  <span
                    class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold"
                    :class="cacheHitRate > 0 ? 'bg-cyan-50 text-cyan-700 dark:bg-cyan-950/40 dark:text-cyan-300 border border-cyan-500/20' : 'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500'"
                    title="缓存命中率 = 缓存读取 / (输入 + 缓存读取)"
                  >
                    <span class="text-[9px] mr-0.5">⚡</span>{{ cacheHitRate }}% 缓存命中
                  </span>
                </div>
                <div class="text-base font-bold text-slate-900 dark:text-white tabular-nums leading-tight mt-0.5">
                  {{ formatNumber(stats?.total_requests) }}
                </div>
              </div>

              <div class="rounded-xl bg-teal-50/80 dark:bg-teal-950/30 px-3.5 py-1.5 border border-teal-100 dark:border-teal-900/50 shadow-2xs">
                <span class="text-[10px] font-medium text-teal-600 dark:text-teal-400">Token 吞吐</span>
                <div class="text-base font-bold text-teal-600 dark:text-teal-400 tabular-nums leading-tight mt-0.5">
                  {{ formatTokens(stats?.total_tokens) }}
                </div>
              </div>

              <div class="flex items-center gap-2.5 rounded-xl bg-slate-50/80 dark:bg-dark-700/60 px-3 py-2 border border-slate-200/60 dark:border-dark-700 text-xs shadow-2xs">
                <div class="flex items-center gap-1" :title="`平均耗时: ${formatDuration(stats?.average_duration_ms)}`">
                  <span class="text-[10px] text-slate-400">平均耗时</span>
                  <span class="font-bold text-rose-500 tabular-nums">{{ formatDuration(stats?.average_duration_ms) }}</span>
                </div>
                <span class="text-slate-300 dark:text-dark-600">|</span>
                <div class="flex items-center gap-1" :title="`平均 RPS: ${avgRps}`">
                  <span class="text-[10px] text-slate-400">RPS</span>
                  <span class="font-bold text-slate-800 dark:text-white tabular-nums">{{ avgRps }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- 双环形区：Token 构成 + 核心模型分布 -->
          <div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-center pt-1">
            <div class="lg:col-span-5 space-y-3">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-1.5">
                  <span class="h-2 w-2 rounded-full bg-teal-500"></span>
                  <h3 class="text-xs font-semibold text-slate-800 dark:text-slate-200">Token 构成</h3>
                </div>
                <span class="text-[10px] text-slate-400">Prompt · Completion · Cache</span>
              </div>
              <div class="flex items-center gap-4">
                <div class="relative w-28 h-28 shrink-0 flex items-center justify-center">
                  <svg class="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
                    <circle cx="50" cy="50" r="38" fill="none" stroke="currentColor" stroke-width="12" class="text-slate-100 dark:text-dark-700" />
                    <circle
                      v-for="(seg, idx) in tokenDonutSegments"
                      :key="idx"
                      cx="50" cy="50" r="38" fill="none"
                      :stroke="seg.color" stroke-width="12"
                      :stroke-dasharray="`${seg.length} 238.76`"
                      :stroke-dashoffset="seg.offset"
                      class="transition-all duration-500"
                    />
                  </svg>
                  <div class="absolute inset-0 flex flex-col items-center justify-center text-center pointer-events-none">
                    <span class="text-[9px] text-slate-400 leading-none">总吞吐</span>
                    <span class="text-xs font-bold text-slate-800 dark:text-white tabular-nums mt-0.5">
                      {{ formatTokens(stats?.total_tokens) }}
                    </span>
                  </div>
                </div>
                <div class="flex-1 min-w-0 space-y-1.5">
                  <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                    <div class="flex items-center gap-1.5">
                      <span class="h-2 w-2 rounded-full bg-sky-500"></span>
                      <span class="text-[11px] text-slate-600 dark:text-slate-300">输入 Prompt</span>
                    </div>
                    <div class="flex items-baseline gap-1.5">
                      <span class="font-bold text-sky-600 dark:text-sky-400 tabular-nums">{{ formatTokens(stats?.total_input_tokens) }}</span>
                      <span class="text-[10px] text-slate-400 tabular-nums">({{ tokenBreakdown.inputPct }}%)</span>
                    </div>
                  </div>
                  <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                    <div class="flex items-center gap-1.5">
                      <span class="h-2 w-2 rounded-full bg-violet-500"></span>
                      <span class="text-[11px] text-slate-600 dark:text-slate-300">输出 Completion</span>
                    </div>
                    <div class="flex items-baseline gap-1.5">
                      <span class="font-bold text-violet-600 dark:text-violet-400 tabular-nums">{{ formatTokens(stats?.total_output_tokens) }}</span>
                      <span class="text-[10px] text-slate-400 tabular-nums">({{ tokenBreakdown.outputPct }}%)</span>
                    </div>
                  </div>
                  <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                    <div class="flex items-center gap-1.5">
                      <span class="h-2 w-2 rounded-full bg-cyan-500"></span>
                      <span class="text-[11px] text-slate-600 dark:text-slate-300">缓存读取 Cache</span>
                    </div>
                    <div class="flex items-baseline gap-1.5">
                      <span class="font-bold text-cyan-600 dark:text-cyan-400 tabular-nums">{{ formatTokens(stats?.total_cache_read_tokens) }}</span>
                      <span class="text-[10px] text-slate-400 tabular-nums">({{ tokenBreakdown.cachePct }}%)</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hidden lg:block lg:col-span-1 h-28 border-r border-slate-200 dark:border-dark-700/70 mx-auto"></div>

            <div class="lg:col-span-6 space-y-3">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-1.5">
                  <span class="h-2 w-2 rounded-full bg-indigo-500"></span>
                  <h3 class="text-xs font-semibold text-slate-800 dark:text-slate-200">核心模型用量分布 &amp; 缓存效率</h3>
                </div>
                <span class="text-[10px] text-slate-400">按 Token 消耗占比</span>
              </div>
              <div class="flex items-center gap-4">
                <div class="relative w-28 h-28 shrink-0 flex items-center justify-center">
                  <svg class="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
                    <circle cx="50" cy="50" r="38" fill="none" stroke="currentColor" stroke-width="12" class="text-slate-100 dark:text-dark-700" />
                    <circle
                      v-for="(seg, idx) in modelDonutSegments"
                      :key="idx"
                      cx="50" cy="50" r="38" fill="none"
                      :stroke="seg.color" stroke-width="12"
                      :stroke-dasharray="`${seg.length} 238.76`"
                      :stroke-dashoffset="seg.offset"
                      class="transition-all duration-500"
                    />
                  </svg>
                  <div class="absolute inset-0 flex flex-col items-center justify-center text-center pointer-events-none">
                    <span class="text-[9px] text-slate-400 leading-none">核心</span>
                    <span class="text-xs font-bold text-slate-800 dark:text-white tabular-nums mt-0.5">
                      {{ topModelShares.length > 0 ? `Top ${topModelShares.length}` : '暂无' }}
                    </span>
                  </div>
                </div>
                <div v-if="topModelShares.length > 0" class="flex-1 min-w-0 space-y-1">
                  <div
                    v-for="model in topModelShares"
                    :key="model.name"
                    class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1 border border-slate-100 dark:border-dark-700/60 text-xs"
                  >
                    <div class="flex items-center gap-1.5 min-w-0">
                      <span class="h-2 w-2 rounded-full shrink-0" :style="{ backgroundColor: model.color }"></span>
                      <span class="font-medium text-slate-700 dark:text-slate-200 text-[11px] truncate" :title="model.name">{{ model.name }}</span>
                    </div>
                    <div class="flex items-center gap-1.5 shrink-0">
                      <span class="font-bold text-slate-900 dark:text-white text-[11px] tabular-nums">{{ model.percentage }}%</span>
                      <span class="text-[10px] text-slate-400 tabular-nums">({{ formatTokens(model.tokens) }})</span>
                      <span
                        class="rounded px-1.5 py-0.2 text-[9px] font-medium tabular-nums"
                        :class="model.cacheHitRate > 0 ? 'bg-cyan-50 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300' : 'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500'"
                        :title="`缓存命中率: ${model.cacheHitRate}% (缓存读取: ${formatTokens(model.cacheReadTokens)})`"
                      >
                        <span class="text-[9px] mr-0.5">⚡</span>{{ model.cacheHitRate }}%
                      </span>
                    </div>
                  </div>
                </div>
                <div v-else class="flex-1 text-[11px] text-slate-400 py-2">所选范围暂无模型调用数据</div>
              </div>
            </div>
          </div>
        </div>

        <!-- Token 使用趋势（整行宽幅） -->
        <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" :show-cost="false" />

        <!-- 模型分布（整行宽幅，仅 Token 口径，支持 requested/upstream/mapping） -->
        <ModelDistributionChart
          v-model:source="modelDistributionSource"
          :model-stats="requestedModelStats"
          :upstream-model-stats="upstreamModelStats"
          :mapping-model-stats="mappingModelStats"
          :loading="modelStatsLoading"
          :show-source-toggle="true"
          :show-metric-toggle="false"
          :enable-breakdown="false"
          :show-account-cost="false"
          :show-cost="false"
          :start-date="startDate"
          :end-date="endDate"
        />

        <!-- 分组分布 + 端点分布（两列，仅 Token 口径） -->
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <GroupDistributionChart
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="false"
            :enable-breakdown="false"
            :show-account-cost="false"
            :show-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="false"
            :enable-breakdown="false"
            :show-cost="false"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
          />
        </div>

        <!-- 按 API 密钥统计用量（整行宽幅，口径与管理控制台一致） -->
        <UsageApiKeyStatsCard
          scope="admin"
          :start-date="startDate"
          :end-date="endDate"
          :reload-key="apiKeyStatsReloadKey"
        />

        <!-- 用户用量排行 Top（仅 Token 口径；完整排行在使用记录页 ranking tab） -->
        <div class="card p-4 border border-slate-200/80 dark:border-dark-700">
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <span class="inline-block h-2 w-2 rounded-full bg-amber-400"></span>
              <h3 class="text-sm font-semibold text-slate-900 dark:text-white">用户用量排行 Top 10</h3>
              <span class="text-[10px] text-slate-400">按 Token 消耗 · 完整排行见使用记录页</span>
            </div>
            <RouterLink :to="{ path: '/admin/usage', query: { start_date: startDate, end_date: endDate } }" class="text-[11px] text-teal-600 dark:text-teal-400 hover:underline">
              查看完整排行 →
            </RouterLink>
          </div>
          <div v-if="rankingLoading" class="flex h-24 items-center justify-center">
            <LoadingSpinner size="md" />
          </div>
          <div v-else-if="topUsers.length > 0" class="space-y-1.5">
            <div
              v-for="(user, idx) in topUsers"
              :key="user.user_id"
              class="flex items-center gap-3 rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-3 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs"
            >
              <span
                class="w-5 text-center text-[10px] font-bold shrink-0"
                :class="idx === 0 ? 'text-amber-500' : idx === 1 ? 'text-slate-400' : idx === 2 ? 'text-amber-700' : 'text-slate-400'"
              >{{ idx + 1 }}</span>
              <span class="w-36 font-medium text-slate-700 dark:text-slate-200 truncate" :title="user.email">{{ user.email || `用户 #${user.user_id}` }}</span>
              <div class="flex-1 h-3.5 min-w-0 bg-slate-200/70 dark:bg-dark-700 rounded overflow-hidden">
                <div
                  class="h-full bg-gradient-to-r from-amber-400 to-amber-300 dark:from-amber-500 dark:to-amber-400"
                  :style="{ width: `${userShareWidth(user)}%` }"
                ></div>
              </div>
              <span class="w-20 text-right font-bold text-slate-800 dark:text-white num text-[11px] tabular-nums shrink-0">{{ formatTokens(user.total_tokens) }}</span>
            </div>
          </div>
          <div v-else class="flex h-24 items-center justify-center text-sm text-slate-400 dark:text-slate-500">
            {{ t('admin.dashboard.noDataAvailable') }}
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { getUserBreakdown } from '@/api/admin/dashboard'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import UsageApiKeyStatsCard from '@/components/dashboard/UsageApiKeyStatsCard.vue'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type {
  EndpointStat,
  GroupStat,
  ModelStat,
  TrendDataPoint,
  UserBreakdownItem,
} from '@/types'

const { t } = useI18n()

type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
type EndpointSource = 'inbound' | 'upstream' | 'path'

const stats = ref<AdminUsageStatsResponse | null>(null)
const trendData = ref<TrendDataPoint[]>([])
const requestedModelStats = ref<ModelStat[]>([])
const upstreamModelStats = ref<ModelStat[]>([])
const mappingModelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const topUsers = ref<UserBreakdownItem[]>([])
const apiKeyStatsReloadKey = ref(0)

const loading = ref(true) // 首屏即加载态：避免子卡片在 refreshAll 前先自行取数导致重复请求
const chartsLoading = ref(false)
const modelStatsLoading = ref(false)
const endpointStatsLoading = ref(false)
const rankingLoading = ref(false)

const modelDistributionSource = ref<ModelDistributionSource>('requested')
const endpointDistributionSource = ref<EndpointSource>('inbound')

let statsReqSeq = 0
let chartReqSeq = 0
let modelStatsReqSeq = 0
let rankingReqSeq = 0

// 已加载过的模型源数据缓存：切换 source 时避免重复请求（口径与 admin UsageView 一致）
const loadedModelSources = ref<Record<ModelDistributionSource, boolean>>({
  requested: false,
  upstream: false,
  mapping: false,
})

function toFiniteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatLocalDate = (date: Date): string =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`

const getTodayRangeDates = () => {
  const today = formatLocalDate(new Date())
  return { start: today, end: today }
}

const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  return Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24)) <= 1 ? 'hour' : 'day'
}

const defaultRange = getTodayRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
const granularity = ref<'day' | 'hour'>(getGranularityForRange(startDate.value, endDate.value))

const granularityOptions = computed<SelectOption[]>(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') },
])

const rangeBadgeText = computed(() =>
  `${startDate.value} ~ ${endDate.value} · ${granularity.value === 'hour' ? '按小时' : '按天'}`
)

// ---- Hero 计算逻辑（口径对齐 GatewayDashboard，数据源为所选时间范围） ----

const cacheHitRate = computed(() => {
  if (!stats.value) return 0
  const input = toFiniteNumber(stats.value.total_input_tokens)
  const cacheRead = toFiniteNumber(stats.value.total_cache_read_tokens)
  const promptTokens = input + cacheRead
  if (promptTokens <= 0) {
    const total = toFiniteNumber(stats.value.total_tokens)
    if (total > 0 && cacheRead > 0) return Math.min(100, Math.round((cacheRead / total) * 100))
    return 0
  }
  return Math.min(100, Math.round((cacheRead / promptTokens) * 100))
})

const avgRps = computed(() => {
  if (!stats.value) return '0'
  const start = new Date(`${startDate.value}T00:00:00`).getTime()
  const end = new Date(`${endDate.value}T00:00:00`).getTime() + 24 * 60 * 60 * 1000
  const seconds = Math.max(1, (end - start) / 1000)
  const rps = toFiniteNumber(stats.value.total_requests) / seconds
  return rps >= 1 ? rps.toFixed(2) : rps.toFixed(3)
})

const tokenBreakdown = computed(() => {
  const total = toFiniteNumber(stats.value?.total_tokens)
  if (total <= 0) return { inputPct: 0, outputPct: 0, cachePct: 0 }
  const cache =
    toFiniteNumber(stats.value?.total_cache_read_tokens) + toFiniteNumber(stats.value?.total_cache_creation_tokens)
  return {
    inputPct: Math.min(100, Math.round((toFiniteNumber(stats.value?.total_input_tokens) / total) * 100)),
    outputPct: Math.min(100, Math.round((toFiniteNumber(stats.value?.total_output_tokens) / total) * 100)),
    cachePct: Math.min(100, Math.round((cache / total) * 100)),
  }
})

const tokenDonutSegments = computed(() => {
  const circumference = 238.76
  const breakdown = tokenBreakdown.value
  const total = toFiniteNumber(stats.value?.total_tokens)
  if (total <= 0) return []
  const inputLen = (breakdown.inputPct / 100) * circumference
  const outputLen = (breakdown.outputPct / 100) * circumference
  const cacheLen = (breakdown.cachePct / 100) * circumference
  return [
    { name: '输入', color: '#0ea5e9', length: inputLen, offset: 0 },
    { name: '输出', color: '#8b5cf6', length: outputLen, offset: -inputLen },
    { name: '缓存', color: '#06b6d4', length: cacheLen, offset: -(inputLen + outputLen) },
  ]
})

const topModelShares = computed(() => {
  const sourceModels = requestedModelStats.value
  if (!sourceModels?.length) return []
  const activeModels = sourceModels.filter((m) => toFiniteNumber(m.total_tokens) > 0)
  const totalTokens = activeModels.reduce((sum, m) => sum + toFiniteNumber(m.total_tokens), 0)
  if (totalTokens <= 0) return []

  const sorted = [...activeModels].sort((a, b) => toFiniteNumber(b.total_tokens) - toFiniteNumber(a.total_tokens))
  const topN = sorted.slice(0, 4)
  const remaining = sorted.slice(4)
  const remainingTokens = remaining.reduce((sum, m) => sum + toFiniteNumber(m.total_tokens), 0)

  const palette = ['#3b82f6', '#10b981', '#8b5cf6', '#f59e0b', '#64748b']

  const calcCacheHitRate = (cacheRead: number, input: number, tokens: number): number => {
    const promptTokens = input + cacheRead
    if (promptTokens > 0) return Math.min(100, Math.round((cacheRead / promptTokens) * 100))
    if (tokens > 0 && cacheRead > 0) return Math.min(100, Math.round((cacheRead / tokens) * 100))
    return 0
  }

  let currentSum = 0
  const result = topN.map((m, idx) => {
    const pct = Math.round((toFiniteNumber(m.total_tokens) / totalTokens) * 100)
    currentSum += pct
    const cacheRead = toFiniteNumber(m.cache_read_tokens)
    return {
      name: m.model,
      tokens: toFiniteNumber(m.total_tokens),
      percentage: pct,
      color: palette[idx % palette.length],
      cacheHitRate: calcCacheHitRate(cacheRead, toFiniteNumber(m.input_tokens), toFiniteNumber(m.total_tokens)),
      cacheReadTokens: cacheRead,
    }
  })

  if (remainingTokens > 0) {
    const otherCacheRead = remaining.reduce((sum, m) => sum + toFiniteNumber(m.cache_read_tokens), 0)
    const otherInput = remaining.reduce((sum, m) => sum + toFiniteNumber(m.input_tokens), 0)
    result.push({
      name: '其他模型',
      tokens: remainingTokens,
      percentage: Math.max(0, 100 - currentSum),
      color: palette[4],
      cacheHitRate: calcCacheHitRate(otherCacheRead, otherInput, remainingTokens),
      cacheReadTokens: otherCacheRead,
    })
  }
  return result
})

const modelDonutSegments = computed(() => {
  const circumference = 238.76
  const models = topModelShares.value
  if (!models.length) return []
  let currentOffset = 0
  return models.map((m) => {
    const len = (m.percentage / 100) * circumference
    const seg = { name: m.name, color: m.color, length: len, offset: -currentOffset }
    currentOffset += len
    return seg
  })
})

const userShareWidth = (user: UserBreakdownItem): number => {
  const max = toFiniteNumber(topUsers.value[0]?.total_tokens)
  if (max <= 0) return 0
  return Math.max(2, Math.round((toFiniteNumber(user.total_tokens) / max) * 100))
}

// ---- 格式化 ----

const formatTokens = (value: number | undefined | null): string => {
  const v = toFiniteNumber(value)
  if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(2)}B`
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`
  if (v >= 1_000) return `${(v / 1_000).toFixed(2)}K`
  return v.toLocaleString()
}

const formatNumber = (value: number | null | undefined): string => toFiniteNumber(value).toLocaleString()

const formatDuration = (ms: number | undefined | null): string => {
  const v = toFiniteNumber(ms)
  return v >= 1000 ? `${(v / 1000).toFixed(2)}s` : `${Math.round(v)}ms`
}

// ---- 数据加载 ----

const rangeParams = computed(() => ({
  start_date: startDate.value,
  end_date: endDate.value,
}))

const loadStats = async (force = false) => {
  const seq = ++statsReqSeq
  endpointStatsLoading.value = true
  loading.value = stats.value === null
  try {
    const res = await adminAPI.usage.getStats({
      ...rangeParams.value,
      ...(force ? { nocache: 1 } : {}),
    })
    if (seq !== statsReqSeq) return
    stats.value = res
    inboundEndpointStats.value = res.endpoints || []
    upstreamEndpointStats.value = res.upstream_endpoints || []
    endpointPathStats.value = res.endpoint_paths || []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load admin usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) {
      endpointStatsLoading.value = false
      loading.value = false
    }
  }
}

const loadModelStats = async (source: ModelDistributionSource, force = false) => {
  if (!force && loadedModelSources.value[source]) return
  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const response = await adminAPI.dashboard.getModelStats({
      ...rangeParams.value,
      model_source: source,
    })
    if (seq !== modelStatsReqSeq) return
    const models = response.models || []
    if (source === 'requested') requestedModelStats.value = models
    else if (source === 'upstream') upstreamModelStats.value = models
    else mappingModelStats.value = models
    loadedModelSources.value[source] = true
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartsLoading.value = true
  try {
    const snapshot = await adminAPI.dashboard.getSnapshotV2({
      ...rangeParams.value,
      granularity: granularity.value,
      include_stats: false,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
      include_users_trend: false,
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) {
    if (seq !== chartReqSeq) return
    console.error('Failed to load chart data:', error)
    trendData.value = []
    groupStats.value = []
  } finally {
    if (seq === chartReqSeq) chartsLoading.value = false
  }
}

const loadRanking = async () => {
  const seq = ++rankingReqSeq
  rankingLoading.value = true
  try {
    const res = await getUserBreakdown({
      ...rangeParams.value,
      sort_by: 'total_tokens',
      limit: 10,
    })
    if (seq !== rankingReqSeq) return
    topUsers.value = (res.users || []).filter((u) => toFiniteNumber(u.total_tokens) > 0)
  } catch (error) {
    if (seq !== rankingReqSeq) return
    console.error('Failed to load user ranking:', error)
    topUsers.value = []
  } finally {
    if (seq === rankingReqSeq) rankingLoading.value = false
  }
}

const invalidateModelStatsCache = () => {
  loadedModelSources.value.requested = false
  loadedModelSources.value.upstream = false
  loadedModelSources.value.mapping = false
}

const refreshAll = () => {
  invalidateModelStatsCache()
  void loadStats()
  void loadModelStats(modelDistributionSource.value)
  void loadChartData()
  void loadRanking()
  apiKeyStatsReloadKey.value++
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  refreshAll()
}

watch(modelDistributionSource, (source) => {
  void loadModelStats(source)
})

onMounted(() => {
  refreshAll()
})
</script>
