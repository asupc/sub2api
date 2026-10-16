<template>
  <AppLayout>
    <div class="min-w-0 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <span v-if="suite" class="rounded-lg border border-primary-200 bg-primary-50 px-2.5 py-1 text-xs font-semibold text-primary-700 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300">
            {{ suite.id }} · D{{ suite.min_difficulty }}-D{{ suite.max_difficulty }} · {{ suite.item_count }} {{ t('admin.modelBenchmark.items') }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <RouterLink to="/admin/benchmark-plans" class="btn btn-secondary btn-sm">{{ t('admin.modelBenchmark.stats.managePlans') }}</RouterLink>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadAll">{{ t('admin.modelBenchmark.refresh') }}</button>
        </div>
      </div>
      <form class="flex flex-wrap items-end gap-3 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900" @submit.prevent="loadAll">
        <label class="min-w-[180px] flex-1 text-xs text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.comparisonKey') }}<input v-model.trim="comparisonKey" aria-label="公共试卷批次" required maxlength="64" pattern="[A-Za-z0-9._-]+" class="input input-sm mt-1 w-full" /></label>
        <label class="text-xs text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.tokenBudget') }}<input v-model.number="comparisonTokens" aria-label="输出预算" type="number" min="128" max="4096" required class="input input-sm mt-1 w-28" /></label>
        <label class="text-xs text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.timeout') }}<input v-model.number="comparisonTimeout" aria-label="单题超时" type="number" min="10" max="600" required class="input input-sm mt-1 w-24" /></label>
        <label class="text-xs text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.samplesBudget') }}<input v-model.number="comparisonSamples" aria-label="独立试卷数" type="number" min="1" max="3" required class="input input-sm mt-1 w-20" /></label>
        <button type="submit" class="btn btn-primary btn-sm">{{ t('admin.modelBenchmark.comparison.apply') }}</button>
        <p class="w-full text-xs text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.comparison.hint') }}</p>
      </form>
      <div v-if="loadError" class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-900/20 dark:text-rose-300">{{ loadError }}</div>
      <div v-if="actionError" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ actionError }}</div>

      <div class="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <div v-for="card in summaryCards" :key="card.label" class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <div class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ card.label }}</div>
          <div class="mt-2 break-words text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ card.value }}</div>
          <div class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ card.hint }}</div>
        </div>
      </div>

      <div class="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(360px,.65fr)]">
        <div class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-4 py-3 dark:border-dark-800">
            <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.rankings.title') }}</h2>
            <span class="text-[11px] text-gray-400">{{ t('admin.modelBenchmark.rankings.hint') }}</span>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead><tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">
                <th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.intelligence') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.efficiency') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.composite') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.outputTokens') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.latency') }}</th><th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.lastRun') }}</th>
              </tr></thead>
              <tbody>
                <tr v-for="ranking in rankings" :key="`${ranking.model_id}-${ranking.max_output_tokens}-${ranking.timeout_seconds}-${ranking.sample_count}`" class="border-b border-gray-50 last:border-0 dark:border-dark-800/70">
                  <td class="max-w-[220px] truncate px-4 py-3 font-semibold text-gray-900 dark:text-white">{{ ranking.model_id }}<div class="mt-1 text-[11px] font-normal text-gray-500">{{ ranking.comparison_key }} · {{ ranking.max_output_tokens }} tokens · {{ ranking.timeout_seconds }}s · ×{{ ranking.sample_count }}</div></td>
                  <td class="px-3 py-3 font-bold tabular-nums text-primary-700 dark:text-primary-300">{{ formatScore(ranking.intelligence_score) }}<span v-if="ranking.score_stddev != null" class="ml-1 text-xs font-normal text-gray-500" :title="t('admin.modelBenchmark.comparison.spread')">±{{ ranking.score_stddev.toFixed(1) }}</span></td>
                  <td class="px-3 py-3 tabular-nums">{{ formatScore(ranking.efficiency_score) }}</td><td class="px-3 py-3 tabular-nums">{{ formatScore(ranking.composite_score) }}</td>
                  <td class="px-3 py-3 tabular-nums">{{ formatTokens(ranking.output_tokens) }}</td><td class="px-3 py-3 tabular-nums">{{ formatLatency(ranking.avg_latency_ms) }}</td>
                  <td class="px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatTime(ranking.last_run_at) }}</td>
                </tr>
                <tr v-if="rankings.length === 0"><td colspan="7" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.rankings.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
        </div>
        <div class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.categories.title') }}</h2>
          <div v-if="latestRun" class="mt-4 space-y-3">
            <div v-for="item in categoryBars" :key="item.label" class="space-y-1">
              <div class="flex items-center justify-between text-xs"><span class="font-medium text-gray-700 dark:text-dark-200">{{ item.label }}</span><span class="font-bold tabular-nums text-gray-900 dark:text-white">{{ item.score }}</span></div>
              <div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800"><div class="h-full rounded-full bg-gradient-to-r from-primary-500 to-indigo-500" :style="{ width: `${item.score}%` }"></div></div>
            </div>
          </div>
          <div v-else class="py-12 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.categories.empty') }}</div>
        </div>
      </div>      <div class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div><h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.history.title') }}</h2><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.history.hint') }}</p></div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="historyMetric" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-xs dark:border-dark-700 dark:bg-dark-800">
              <option value="intelligence_score">{{ t('admin.modelBenchmark.fields.intelligence') }}</option><option value="efficiency_score">{{ t('admin.modelBenchmark.fields.efficiency') }}</option><option value="output_tokens">{{ t('admin.modelBenchmark.fields.outputTokens') }}</option><option value="total_tokens">{{ t('admin.modelBenchmark.fields.totalTokens') }}</option><option value="avg_latency_ms">{{ t('admin.modelBenchmark.fields.latency') }}</option><option value="estimated_cost">{{ t('admin.modelBenchmark.fields.cost') }}</option>
            </select>
            <select v-model="historyModel" class="h-9 max-w-[220px] rounded-lg border border-gray-300 bg-white px-2 text-xs dark:border-dark-700 dark:bg-dark-800">
              <option value="">{{ t('admin.modelBenchmark.history.allModels') }}</option><option v-for="model in historyModelOptions" :key="model" :value="model">{{ model }}</option>
            </select>
          </div>
        </div>
        <div class="mt-4 overflow-x-auto">
          <svg viewBox="0 0 1000 320" class="h-[320px] min-w-[760px] w-full" role="img" :aria-label="t('admin.modelBenchmark.history.title')">
            <g v-for="grid in chartGrids" :key="`grid-${grid.y}`"><line x1="54" x2="985" :y1="grid.y" :y2="grid.y" stroke="currentColor" class="text-gray-100 dark:text-dark-800" stroke-width="1" /><text x="44" :y="grid.y + 4" text-anchor="end" class="fill-gray-400 text-[10px]">{{ grid.label }}</text></g>
            <g v-for="point in chartXLabels" :key="`x-${point.x}`"><text :x="point.x" y="312" text-anchor="middle" class="fill-gray-400 text-[10px]">{{ point.label }}</text></g>
            <g v-for="series in chartSeries" :key="series.model"><polyline :points="series.points" fill="none" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" :stroke="series.color" /><circle v-for="point in series.dots" :key="`${series.model}-${point.x}-${point.y}`" :cx="point.x" :cy="point.y" r="3.4" class="fill-white dark:fill-dark-900" :stroke="series.color" stroke-width="2"><title>{{ series.model }} · {{ point.label }} · {{ point.value }}</title></circle></g>
            <g v-if="chartSeries.length === 0"><text x="500" y="160" text-anchor="middle" class="fill-gray-400 text-sm">{{ t('admin.modelBenchmark.history.empty') }}</text></g>
          </svg>
        </div>
        <div class="mt-2 flex flex-wrap gap-3 text-xs text-gray-500 dark:text-dark-400"><span v-for="series in chartSeries" :key="`legend-${series.model}`" class="inline-flex items-center gap-1.5"><span class="h-2 w-2 rounded-full" :style="{ background: series.color }"></span>{{ series.model }}</span></div>
      </div>

      <div class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-800"><h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.runs.title') }}</h2><button type="button" class="btn btn-secondary btn-xs" @click="loadRuns">{{ t('admin.modelBenchmark.refresh') }}</button></div>
        <div class="overflow-x-auto"><table class="min-w-full text-sm"><thead><tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400"><th class="px-4 py-3">Run</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.status') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.intelligence') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.outputTokens') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.latency') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.coverage') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.rankingEligibility') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.usage') }}</th><th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.created') }}</th></tr></thead><tbody>
          <tr v-for="run in runs" :key="run.id" class="cursor-pointer border-b border-gray-50 transition hover:bg-gray-50/70 last:border-0 dark:border-dark-800/70 dark:hover:bg-dark-800/40" @click="openRun(run.id)"><td class="px-4 py-3 font-mono">#{{ run.id }}</td><td class="max-w-[190px] truncate px-3 py-3">{{ run.model_id }}<div class="mt-1 text-[11px] text-gray-500">v{{ run.suite_version }} · {{ run.max_output_tokens > 0 ? `${run.max_output_tokens} tokens` : t('admin.modelBenchmark.comparison.providerBudget') }}</div></td><td class="px-3 py-3"><span :class="statusClass(run.status)">{{ t(`admin.modelBenchmark.runStatus.${run.status}`) }}</span></td><td class="px-3 py-3 font-bold tabular-nums">{{ formatScore(run.intelligence_score) }}</td><td class="px-3 py-3 tabular-nums">{{ formatTokens(run.output_tokens) }}</td><td class="px-3 py-3 tabular-nums">{{ formatLatency(run.avg_latency_ms) }}</td><td class="px-3 py-3 tabular-nums">{{ run.quality ? `${run.quality.completion_rate.toFixed(1)}%` : '—' }}</td><td class="px-3 py-3"><span :class="run.quality?.ranking_eligible ? 'badge badge-success' : 'badge badge-warning'" :title="qualityReason(run)">{{ t(run.quality?.ranking_eligible ? 'admin.modelBenchmark.comparison.eligible' : 'admin.modelBenchmark.comparison.diagnostic') }}</span></td><td class="px-3 py-3"><span :class="run.usage_source === 'upstream' ? 'badge badge-success' : 'badge badge-warning'">{{ run.usage_source }}</span></td><td class="px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatTime(run.created_at) }}</td></tr>
          <tr v-if="runs.length === 0"><td colspan="10" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.runs.empty') }}</td></tr>
        </tbody></table></div>
      </div>
      <div v-if="selectedRun" class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-800"><div><h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.runDetail.title') }} #{{ selectedRun.id }}</h2><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ selectedRun.model_id }} · {{ selectedRun.upstream_model_id || '—' }} · #{{ selectedRun.account_id }}</p></div><button type="button" class="btn btn-secondary btn-xs" @click="closeRun">{{ t('common.close') }}</button></div>
        <div class="grid grid-cols-2 gap-3 border-b border-gray-100 p-4 dark:border-dark-800 md:grid-cols-6"><div v-for="metric in runMetrics" :key="metric.label"><div class="text-[11px] text-gray-500 dark:text-dark-400">{{ metric.label }}</div><div class="mt-1 font-bold tabular-nums text-gray-900 dark:text-white">{{ metric.value }}</div></div></div>
        <p class="border-b border-gray-100 px-4 py-3 text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">{{ selectedRun.comparison_key || 'legacy' }} · {{ selectedRun.max_output_tokens > 0 ? `${selectedRun.max_output_tokens} tokens` : t('admin.modelBenchmark.comparison.providerBudget') }} · {{ selectedRun.timeout_seconds || '—' }}s · ×{{ selectedRun.sample_count || 1 }} · {{ qualityReason(selectedRun) }}</p>
        <div class="max-h-[520px] overflow-auto"><table class="min-w-full text-xs"><thead class="sticky top-0 bg-white dark:bg-dark-900"><tr class="border-b border-gray-100 text-left text-gray-500 dark:border-dark-800 dark:text-dark-400"><th class="px-4 py-3">Item</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.category') }}</th><th class="px-3 py-3">D</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.score') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.tokens') }}</th><th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.latency') }}</th><th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.reason') }}</th></tr></thead><tbody>
          <tr v-for="item in runItems" :key="item.id" class="border-b border-gray-50 last:border-0 dark:border-dark-800/70"><td class="px-4 py-3 font-mono">{{ item.item_id }}</td><td class="px-3 py-3">{{ item.category }}</td><td class="px-3 py-3">{{ item.difficulty }}</td><td class="px-3 py-3 font-bold tabular-nums">{{ (item.score * 100).toFixed(0) }}%</td><td class="px-3 py-3 tabular-nums">{{ item.input_tokens ?? '—' }} / {{ item.output_tokens ?? '—' }}{{ item.reasoning_tokens ? ` (+${item.reasoning_tokens})` : '' }}</td><td class="px-3 py-3 tabular-nums">{{ formatLatency(item.latency_ms) }}</td><td class="max-w-[260px] truncate px-4 py-3 text-gray-500 dark:text-dark-400" :title="item.failure_reason || item.error_message">{{ item.failure_reason || item.error_message || '—' }}</td></tr>
        </tbody></table></div>
      </div>
    </div>
  </AppLayout>
</template><script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import adminAPI from '@/api/admin'
import type { ModelBenchmarkHistoryPoint, ModelBenchmarkRanking, ModelBenchmarkRun, ModelBenchmarkRunItem, ModelBenchmarkSuite } from '@/api/admin/modelBenchmarks'

const { t } = useI18n()
const loadError = ref('')
const actionError = ref('')
const suites = ref<ModelBenchmarkSuite[]>([])
const rankings = ref<ModelBenchmarkRanking[]>([])
const runs = ref<ModelBenchmarkRun[]>([])
const history = ref<ModelBenchmarkHistoryPoint[]>([])
const selectedRun = ref<ModelBenchmarkRun | null>(null)
const runItems = ref<ModelBenchmarkRunItem[]>([])
const historyMetric = ref<'intelligence_score' | 'efficiency_score' | 'output_tokens' | 'total_tokens' | 'avg_latency_ms' | 'estimated_cost'>('intelligence_score')
const historyModel = ref('')
const comparisonKey = ref('baseline-v2')
const comparisonTokens = ref(2048)
const comparisonTimeout = ref(90)
const comparisonSamples = ref(1)
const comparisonParams = computed(() => ({ comparison_key: comparisonKey.value, max_output_tokens: comparisonTokens.value, timeout_seconds: comparisonTimeout.value, sample_count: comparisonSamples.value }))

const suite = computed(() => suites.value[0] ?? null)
const latestRun = computed(() => runs.value.find(run => run.status === 'success' || run.status === 'partial') ?? null)
const summaryCards = computed(() => { const best = rankings.value[0]; const latest = latestRun.value; return [
  { label: t('admin.modelBenchmark.stats.latestIntelligence'), value: formatScore(latest?.intelligence_score), hint: latest ? `#${latest.id}` : '—' },
  { label: t('admin.modelBenchmark.stats.bestModel'), value: best?.model_id ?? '—', hint: formatScore(best?.intelligence_score) },
  { label: t('admin.modelBenchmark.stats.outputTokens'), value: formatTokens(latest?.output_tokens), hint: latest?.usage_source === 'upstream' ? t('admin.modelBenchmark.stats.upstreamUsage') : t('admin.modelBenchmark.stats.usageMissing') },
  { label: t('admin.modelBenchmark.stats.latency'), value: formatLatency(latest?.avg_latency_ms), hint: `P95 ${formatLatency(latest?.p95_latency_ms)}` },
  { label: t('admin.modelBenchmark.stats.runs'), value: runs.value.length, hint: t('admin.modelBenchmark.stats.serialRunner') }
] })
const categoryLabels: Record<string, string> = { math: 'admin.modelBenchmark.category.math', algorithms: 'admin.modelBenchmark.category.algorithms', logic: 'admin.modelBenchmark.category.logic', code: 'admin.modelBenchmark.category.code', planning: 'admin.modelBenchmark.category.planning', instruction: 'admin.modelBenchmark.category.instruction', abstraction: 'admin.modelBenchmark.category.abstraction', understanding: 'admin.modelBenchmark.category.understanding', causal: 'admin.modelBenchmark.category.causal' }
const categoryBars = computed(() => Object.entries(latestRun.value?.category_scores ?? {}).map(([key, value]) => ({ label: t(categoryLabels[key] ?? key), score: Number(value.toFixed(1)) })))
const historyModelOptions = computed(() => Array.from(new Set(history.value.map(point => point.model_id))).sort())
const historyGroups = computed(() => { const groups = new Map<string, ModelBenchmarkHistoryPoint[]>(); for (const point of history.value) { if (historyModel.value && point.model_id !== historyModel.value) continue; groups.set(point.model_id, [...(groups.get(point.model_id) ?? []), point]) } return Array.from(groups.entries()) })
const chartValues = computed(() => historyGroups.value.flatMap(([, points]) => points.map(point => Number(point[historyMetric.value] ?? 0)).filter(value => Number.isFinite(value))))
const chartMin = computed(() => chartValues.value.length ? Math.min(...chartValues.value) : 0)
const chartMax = computed(() => chartValues.value.length ? Math.max(...chartValues.value) : 1)
const chartGrids = computed(() => Array.from({ length: 6 }, (_, index) => ({ y: 20 + index * 52, label: formatChartValue(chartMax.value - (chartMax.value - chartMin.value) * index / 5) })))
const chartXLabels = computed(() => { const count = Math.min(8, Math.max(history.value.length, 1)); return Array.from({ length: count }, (_, index) => { const pointIndex = Math.floor(index * (history.value.length - 1) / Math.max(count - 1, 1)); const point = history.value[pointIndex]; return { x: 54 + index * 931 / Math.max(count - 1, 1), label: point ? new Date(point.created_at).toLocaleDateString() : '' } }) })
const chartColors = ['#2563eb', '#7c3aed', '#059669', '#d97706', '#dc2626', '#0891b2']
const chartSeries = computed(() => historyGroups.value.map(([model, points], groupIndex) => { const color = chartColors[groupIndex % chartColors.length]; const dots = points.map((point, index) => { const x = points.length === 1 ? 520 : 54 + index * 931 / (points.length - 1); const raw = Number(point[historyMetric.value] ?? 0); const y = 280 - (raw - chartMin.value) / Math.max(chartMax.value - chartMin.value, 1) * 260; return { x, y, value: raw, label: new Date(point.created_at).toLocaleString() } }); return { model, color, dots, points: dots.map(dot => `${dot.x},${dot.y}`).join(' ') } }))
const runMetrics = computed(() => { const run = selectedRun.value; if (!run) return []; return [
  { label: t('admin.modelBenchmark.fields.intelligence'), value: formatScore(run.intelligence_score) }, { label: t('admin.modelBenchmark.fields.efficiency'), value: formatScore(run.efficiency_score) }, { label: t('admin.modelBenchmark.fields.composite'), value: formatScore(run.composite_score) },
  { label: t('admin.modelBenchmark.fields.coverage'), value: run.quality ? `${run.quality.completion_rate.toFixed(1)}%` : '—' },
  { label: t('admin.modelBenchmark.fields.formatCompliance'), value: run.quality ? `${run.quality.format_compliance_rate.toFixed(1)}%` : '—' },
  { label: t('admin.modelBenchmark.fields.budgetExhausted'), value: String(run.quality?.budget_exhausted_count ?? '—') },
  { label: t('admin.modelBenchmark.fields.totalTokens'), value: formatTokens(run.total_tokens) }, { label: t('admin.modelBenchmark.fields.latency'), value: formatLatency(run.avg_latency_ms) }, { label: t('admin.modelBenchmark.fields.cost'), value: run.estimated_cost == null ? '—' : `$${run.estimated_cost.toFixed(4)}` }
] })

async function loadAll() { loadError.value = ''; try { const [suiteData, rankingData, runData, historyData] = await Promise.all([adminAPI.modelBenchmarks.listSuites(), adminAPI.modelBenchmarks.listRankings(50, comparisonKey.value, { max_output_tokens: comparisonTokens.value, timeout_seconds: comparisonTimeout.value, sample_count: comparisonSamples.value }), adminAPI.modelBenchmarks.listRuns({ limit: 100, comparison_key: comparisonKey.value }), adminAPI.modelBenchmarks.listHistory({ limit: 500, ...comparisonParams.value })]); suites.value = suiteData; rankings.value = rankingData; runs.value = runData; history.value = historyData } catch (error) { loadError.value = error instanceof Error ? error.message : t('admin.modelBenchmark.loadFailed') } }

async function loadRuns() { try { runs.value = await adminAPI.modelBenchmarks.listRuns({ limit: 100, comparison_key: comparisonKey.value }) } catch (error) { actionError.value = error instanceof Error ? error.message : String(error) } }
async function openRun(id: number) { try { const [run, items] = await Promise.all([adminAPI.modelBenchmarks.getRun(id), adminAPI.modelBenchmarks.listRunItems(id)]); selectedRun.value = run; runItems.value = items } catch (error) { actionError.value = error instanceof Error ? error.message : String(error) } }
function closeRun() { selectedRun.value = null; runItems.value = [] }
function qualityReason(run: ModelBenchmarkRun) {
  if (!run.quality || run.suite_version !== suite.value?.version || !Object.keys(run.quality).length) return t('admin.modelBenchmark.comparison.legacyReason')
  if (run.quality.ranking_eligible) return t('admin.modelBenchmark.comparison.eligible')
  return t(run.quality.ranking_reason === 'provider_managed_budget' ? 'admin.modelBenchmark.comparison.providerReason' : 'admin.modelBenchmark.comparison.coverageReason')
}
function formatScore(value?: number | null) { return value == null ? '—' : value.toFixed(1) }
function formatTokens(value?: number | null) { if (value == null) return '—'; return value >= 1000 ? `${(value / 1000).toFixed(1)}k` : String(value) }
function formatLatency(value?: number | null) { return value == null ? '—' : value >= 1000 ? `${(value / 1000).toFixed(1)}s` : `${value}ms` }
function formatTime(value?: string | null) { return value ? new Date(value).toLocaleString() : '—' }
function formatChartValue(value: number) { if (historyMetric.value === 'estimated_cost') return value.toFixed(2); if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`; if (value >= 1000) return `${(value / 1000).toFixed(1)}k`; return value.toFixed(0) }
function statusClass(status: string) { if (status === 'success') return 'badge badge-success'; if (status === 'partial') return 'badge badge-warning'; if (status === 'running') return 'badge badge-info'; return 'badge badge-danger' }
onMounted(loadAll)
</script>