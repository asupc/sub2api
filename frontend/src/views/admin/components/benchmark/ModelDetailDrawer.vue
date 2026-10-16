<template>
  <!-- 遮罩与抽屉容器 -->
  <div v-if="show" class="fixed inset-0 z-50 overflow-hidden">
    <!-- 浅灰遮罩背景 -->
    <div
      class="fixed inset-0 bg-gray-900/40 backdrop-blur-xs transition-opacity duration-300"
      @click="$emit('close')"
    ></div>

    <div class="fixed inset-y-0 right-0 flex max-w-full pl-10">
      <div class="w-screen max-w-2xl transform bg-white shadow-2xl transition duration-300 ease-in-out dark:bg-dark-900 flex flex-col">
        <!-- 抽屉顶部头部 -->
        <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-800">
          <div class="min-w-0 pr-4">
            <div class="flex items-center gap-2">
              <h2 class="text-lg font-bold text-gray-900 dark:text-white truncate">
                {{ modelId }}
              </h2>
              <span v-if="overview?.ranking" class="rounded-full bg-primary-50 px-2 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                #{{ overview.ranking.run_count }} 次评测
              </span>
            </div>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.modelBenchmark.drawer.subtitle') }}
            </p>
          </div>

          <div class="flex items-center gap-2 shrink-0">
            <!-- 快捷立即评测按钮 -->
            <button
              v-if="primaryPlan"
              type="button"
              class="btn btn-primary btn-sm flex items-center gap-1.5"
              :disabled="queueing || primaryPlan.running_status === 'running'"
              @click="handleTriggerRun"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polygon points="5 3 19 12 5 21 5 3"></polygon>
              </svg>
              <span>{{ t('admin.modelBenchmark.drawer.quickRun') }}</span>
            </button>

            <!-- 关闭按钮 -->
            <button
              type="button"
              class="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-800"
              @click="$emit('close')"
            >
              <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        <!-- 抽屉主体内容滚动区 -->
        <div v-if="loading" class="flex-1 flex items-center justify-center">
          <div class="text-center text-sm text-gray-400">
            <div class="inline-block h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
            <p class="mt-2">{{ t('admin.modelBenchmark.plans.loading') }}</p>
          </div>
        </div>

        <div v-else class="flex-1 overflow-y-auto p-6 space-y-6">
          <!-- 1. 核心能力得分指标 KPI 卡片 -->
          <div>
            <h3 class="text-xs font-bold uppercase tracking-wider text-gray-400 mb-3">
              {{ t('admin.modelBenchmark.drawer.latestMetrics') }}
            </h3>
            <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div class="rounded-xl border border-gray-100 bg-gray-50/70 p-3 dark:border-dark-800 dark:bg-dark-800/40">
                <div class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.fields.intelligence') }}</div>
                <div class="mt-1 text-2xl font-bold tabular-nums text-primary-600 dark:text-primary-400">
                  {{ formatScore(overview?.ranking?.intelligence_score ?? overview?.latest_run?.intelligence_score) }}
                </div>
              </div>
              <div class="rounded-xl border border-gray-100 bg-gray-50/70 p-3 dark:border-dark-800 dark:bg-dark-800/40">
                <div class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.fields.composite') }}</div>
                <div class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ formatScore(overview?.ranking?.composite_score ?? overview?.latest_run?.composite_score) }}
                </div>
              </div>
              <div class="rounded-xl border border-gray-100 bg-gray-50/70 p-3 dark:border-dark-800 dark:bg-dark-800/40">
                <div class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.fields.efficiency') }}</div>
                <div class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ formatScore(overview?.ranking?.efficiency_score ?? overview?.latest_run?.efficiency_score) }}
                </div>
              </div>
              <div class="rounded-xl border border-gray-100 bg-gray-50/70 p-3 dark:border-dark-800 dark:bg-dark-800/40">
                <div class="text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.fields.latency') }}</div>
                <div class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ formatLatency(overview?.ranking?.avg_latency_ms ?? overview?.latest_run?.avg_latency_ms) }}
                </div>
              </div>
            </div>
          </div>

          <!-- 2. 9 维能力画像雷达图 -->
          <div class="rounded-xl border border-gray-100 bg-white p-4 shadow-xs dark:border-dark-800 dark:bg-dark-900">
            <h3 class="text-xs font-bold text-gray-900 dark:text-white mb-2">
              {{ t('admin.modelBenchmark.drawer.radarTitle') }}
            </h3>
            <RadarChart
              v-if="latestCategoryScores && Object.keys(latestCategoryScores).length > 0"
              :scores="latestCategoryScores"
            />
            <div v-else class="py-12 text-center text-xs text-gray-400">
              {{ t('admin.modelBenchmark.drawer.radarEmpty') }}
            </div>
          </div>

          <!-- 3. 历史走势折线图 (多指标切换) -->
          <div class="rounded-xl border border-gray-100 bg-white p-4 shadow-xs dark:border-dark-800 dark:bg-dark-900">
            <h3 class="text-xs font-bold text-gray-900 dark:text-white mb-3">
              {{ t('admin.modelBenchmark.drawer.trendTitle') }}
            </h3>
            <TrendLineChart :history="overview?.history ?? []" />
          </div>

          <!-- 4. 该模型专属评测记录列表 -->
          <div class="rounded-xl border border-gray-100 bg-white p-4 shadow-xs dark:border-dark-800 dark:bg-dark-900">
            <div class="flex items-center justify-between mb-3">
              <h3 class="text-xs font-bold text-gray-900 dark:text-white">
                {{ t('admin.modelBenchmark.drawer.runsTitle') }}
              </h3>
              <span class="text-[11px] text-gray-400">
                {{ overview?.recent_runs.length ?? 0 }} 条记录
              </span>
            </div>

            <div class="overflow-x-auto">
              <table class="min-w-full text-xs">
                <thead>
                  <tr class="border-b border-gray-100 text-left text-gray-400 dark:border-dark-800">
                    <th class="py-2 px-2">Run ID</th>
                    <th class="py-2 px-2">试卷批次</th>
                    <th class="py-2 px-2">状态</th>
                    <th class="py-2 px-2">能力分</th>
                    <th class="py-2 px-2">耗时</th>
                    <th class="py-2 px-2">时间</th>
                    <th class="py-2 px-2 text-right">操作</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-50 dark:divide-dark-800/60">
                  <tr
                    v-for="run in overview?.recent_runs"
                    :key="run.id"
                    class="hover:bg-gray-50/50 dark:hover:bg-dark-800/30 transition"
                  >
                    <td class="py-2 px-2 font-mono text-gray-700 dark:text-dark-300">#{{ run.id }}</td>
                    <td class="py-2 px-2 text-gray-500 font-mono text-[11px]">{{ run.comparison_key || '—' }}</td>
                    <td class="py-2 px-2">
                      <span :class="statusBadgeClass(run.status)">
                        {{ t(`admin.modelBenchmark.runStatus.${run.status}`) }}
                      </span>
                    </td>
                    <td class="py-2 px-2 font-bold tabular-nums text-primary-600 dark:text-primary-400">
                      {{ formatScore(run.intelligence_score) }}
                    </td>
                    <td class="py-2 px-2 tabular-nums text-gray-500">{{ formatLatency(run.avg_latency_ms) }}</td>
                    <td class="py-2 px-2 text-gray-400 text-[11px]">{{ formatTime(run.created_at) }}</td>
                    <td class="py-2 px-2 text-right">
                      <button
                        type="button"
                        class="text-xs text-primary-600 hover:text-primary-700 font-medium"
                        @click="$emit('view-run', run.id)"
                      >
                        答题明细
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!overview?.recent_runs || overview.recent_runs.length === 0">
                    <td colspan="7" class="py-6 text-center text-xs text-gray-400">
                      {{ t('admin.modelBenchmark.drawer.runsEmpty') }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import adminAPI from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ModelBenchmarkModelOverview } from '@/api/admin/modelBenchmarks'
import RadarChart from './RadarChart.vue'
import TrendLineChart from './TrendLineChart.vue'

const props = defineProps<{
  show: boolean
  modelId: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'view-run', runId: number): void
  (e: 'triggered'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const queueing = ref(false)
const overview = ref<ModelBenchmarkModelOverview | null>(null)

const primaryPlan = computed(() => {
  return overview.value?.plans[0] ?? null
})

const latestCategoryScores = computed(() => {
  return (
    overview.value?.ranking?.category_scores ??
    overview.value?.latest_run?.category_scores ??
    null
  )
})

watch(
  () => [props.show, props.modelId],
  async ([show, id]) => {
    if (show && id) {
      await loadOverview(id as string)
    } else {
      overview.value = null
    }
  },
  { immediate: true }
)

async function loadOverview(id: string) {
  loading.value = true
  try {
    overview.value = await adminAPI.modelBenchmarks.getModelOverview(id)
  } catch (err) {
    appStore.showError(err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

async function handleTriggerRun() {
  if (!primaryPlan.value) return
  queueing.value = true
  try {
    await adminAPI.modelBenchmarks.queueRun(primaryPlan.value.id)
    appStore.showSuccess(t('admin.modelBenchmark.drawer.quickRunSuccess'))
    emit('triggered')
    // 刷新概览
    if (props.modelId) {
      await loadOverview(props.modelId)
    }
  } catch (err) {
    appStore.showError(err instanceof Error ? err.message : String(err))
  } finally {
    queueing.value = false
  }
}

function formatScore(val?: number | null) {
  return val == null ? '—' : val.toFixed(1)
}

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function formatTime(str?: string | null) {
  return str ? new Date(str).toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  }) : '—'
}

function statusBadgeClass(status: string) {
  if (status === 'success') return 'badge badge-success'
  if (status === 'partial') return 'badge badge-warning'
  if (status === 'running') return 'badge badge-info'
  return 'badge badge-danger'
}
</script>
