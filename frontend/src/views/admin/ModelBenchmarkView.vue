<template>
  <AppLayout>
    <div class="min-w-0 space-y-4">
      <!-- 页面顶部导航栏与快捷操作 -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <span
            v-if="suite"
            class="rounded-lg border border-primary-200 bg-primary-50 px-2.5 py-1 text-xs font-semibold text-primary-700 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300"
          >
            {{ suite.id }} · D{{ suite.min_difficulty }}-D{{ suite.max_difficulty }} · {{ suite.item_count }} {{ t('admin.modelBenchmark.items') }}
          </span>
        </div>

        <div class="flex items-center gap-2">
          <RouterLink to="/admin/benchmark-plans" class="btn btn-secondary btn-sm flex items-center gap-1.5">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
            </svg>
            <span>{{ t('admin.modelBenchmark.stats.managePlans') }}</span>
          </RouterLink>
          <button type="button" class="btn btn-secondary btn-sm flex items-center gap-1.5" @click="loadAll">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            <span>{{ t('admin.modelBenchmark.refresh') }}</span>
          </button>
        </div>
      </div>

      <!-- 错误警告条 -->
      <div v-if="loadError" class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-900/20 dark:text-rose-300">
        {{ loadError }}
      </div>
      <div v-if="actionError" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ actionError }}
      </div>

      <!-- 概览指标 KPI 卡片 -->
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div v-for="card in summaryCards" :key="card.label" class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <div class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ card.label }}</div>
          <div class="mt-2 break-words text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ card.value }}</div>
          <div class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ card.hint }}</div>
        </div>
      </div>

      <!-- 视角切换 Tab -->
      <div class="flex items-center gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          type="button"
          class="flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-semibold transition"
          :class="
            activeTab === 'leaderboard'
              ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
          "
          @click="activeTab = 'leaderboard'"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
          </svg>
          <span>{{ t('admin.modelBenchmark.tabs.leaderboard') }}</span>
        </button>

        <button
          type="button"
          class="flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-semibold transition"
          :class="
            activeTab === 'runs'
              ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
          "
          @click="activeTab = 'runs'"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
          </svg>
          <span>{{ t('admin.modelBenchmark.tabs.runs') }}</span>
        </button>
      </div>

      <!-- Tab 1: 模型能力中心（天梯榜单与全景画像） -->
      <div v-show="activeTab === 'leaderboard'">
        <ModelLeaderboard
          :rankings="rankings"
          :suite-key="rankingSuiteKey"
          :suite-options="rankingSuiteOptions"
          @select-model="openModelDrawer"
          @update:suite-key="onSuiteChange"
        />
      </div>

      <!-- Tab 2: 评测执行记录流水 -->
      <div v-show="activeTab === 'runs'">
        <RunsTable
          :runs="runs"
          :suite-version="suite?.version"
          @open-run="openRunModal"
          @refresh="loadRuns"
        />
      </div>
    </div>

    <!-- 模型能力透视全景抽屉 -->
    <ModelDetailDrawer
      :show="showDrawer"
      :model-id="drawerModelId"
      @close="showDrawer = false"
      @view-run="openRunModal"
      @triggered="loadAll"
    />

    <!-- 单次 Run 答题与思维链详情弹窗 -->
    <RunDetailModal
      :show="showRunModal"
      :run="selectedRun"
      :items="runItems"
      @close="closeRunModal"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import adminAPI from '@/api/admin'
import type {
  ModelBenchmarkRanking,
  ModelBenchmarkRankingSuite,
  ModelBenchmarkRun,
  ModelBenchmarkRunItem,
  ModelBenchmarkSuite
} from '@/api/admin/modelBenchmarks'
import ModelLeaderboard from './components/benchmark/ModelLeaderboard.vue'
import RunsTable from './components/benchmark/RunsTable.vue'
import ModelDetailDrawer from './components/benchmark/ModelDetailDrawer.vue'
import RunDetailModal from './components/benchmark/RunDetailModal.vue'

const { t } = useI18n()

const loadError = ref('')
const actionError = ref('')
const activeTab = ref<'leaderboard' | 'runs'>('leaderboard')

const suites = ref<ModelBenchmarkSuite[]>([])
const rankings = ref<ModelBenchmarkRanking[]>([])
const rankingSuites = ref<ModelBenchmarkRankingSuite[]>([])
const rankingSuiteKey = ref('')
const runs = ref<ModelBenchmarkRun[]>([])

// 抽屉与详情弹窗状态
const showDrawer = ref(false)
const drawerModelId = ref('')
const showRunModal = ref(false)
const selectedRun = ref<ModelBenchmarkRun | null>(null)
const runItems = ref<ModelBenchmarkRunItem[]>([])

const suite = computed(() => suites.value[0] ?? null)

// 排名题库选项
const rankingSuiteOptions = computed(() => {
  if (!suite.value) return []
  const builtinKey = `${suite.value.id}|${suite.value.version}|${suite.value.scoring_version}`
  const options = [
    {
      key: builtinKey,
      label: `v${suite.value.version} · ${t('admin.modelBenchmark.rankings.latest')}`,
      params: {
        suite_id: suite.value.id,
        suite_version: suite.value.version,
        scoring_version: suite.value.scoring_version
      }
    }
  ]
  for (const item of rankingSuites.value) {
    const key = `${item.suite_id}|${item.suite_version}|${item.scoring_version}`
    if (key === builtinKey) continue
    options.push({
      key,
      label: `v${item.suite_version} · 评分 v${item.scoring_version}`,
      params: {
        suite_id: item.suite_id,
        suite_version: item.suite_version,
        scoring_version: item.scoring_version
      }
    })
  }
  return options
})

const selectedRankingParams = computed(() => {
  return rankingSuiteOptions.value.find(option => option.key === rankingSuiteKey.value)?.params ?? {}
})

// 顶部概览指标卡片
const summaryCards = computed(() => {
  const best = rankings.value[0]
  const fastest = [...rankings.value].sort((a, b) => (a.avg_latency_ms ?? 999999) - (b.avg_latency_ms ?? 999999))[0]
  return [
    {
      label: '已测评模型总数',
      value: String(rankings.value.length),
      hint: `${rankingSuites.value.length} 个题库套件`
    },
    {
      label: t('admin.modelBenchmark.stats.bestModel'),
      value: best?.model_id ?? '—',
      hint: best?.intelligence_score != null ? `${best.intelligence_score.toFixed(1)} 分` : '—'
    },
    {
      label: '响应速度优选',
      value: fastest?.model_id ?? '—',
      hint: fastest?.avg_latency_ms != null ? formatLatency(fastest.avg_latency_ms) : '—'
    },
    {
      label: t('admin.modelBenchmark.stats.runs'),
      value: String(runs.value.length),
      hint: t('admin.modelBenchmark.stats.serialRunner')
    }
  ]
})

async function loadAll() {
  loadError.value = ''
  try {
    const [suiteData, rankingSuiteData, rankingData, runData] = await Promise.all([
      adminAPI.modelBenchmarks.listSuites(),
      adminAPI.modelBenchmarks.listRankingSuites(),
      adminAPI.modelBenchmarks.listRankings(100, selectedRankingParams.value),
      adminAPI.modelBenchmarks.listRuns({ limit: 100 })
    ])
    suites.value = suiteData
    rankingSuites.value = rankingSuiteData
    if (!rankingSuiteKey.value) {
      rankingSuiteKey.value = rankingSuiteOptions.value[0]?.key ?? ''
    }
    rankings.value = rankingData
    runs.value = runData
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : t('admin.modelBenchmark.loadFailed')
  }
}

async function onSuiteChange(newKey: string) {
  rankingSuiteKey.value = newKey
  try {
    rankings.value = await adminAPI.modelBenchmarks.listRankings(100, selectedRankingParams.value)
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : String(error)
  }
}

async function loadRuns() {
  try {
    runs.value = await adminAPI.modelBenchmarks.listRuns({ limit: 100 })
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : String(error)
  }
}

function openModelDrawer(modelId: string) {
  drawerModelId.value = modelId
  showDrawer.value = true
}

async function openRunModal(id: number) {
  try {
    const [run, items] = await Promise.all([
      adminAPI.modelBenchmarks.getRun(id),
      adminAPI.modelBenchmarks.listRunItems(id)
    ])
    selectedRun.value = run
    runItems.value = items
    showRunModal.value = true
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : String(error)
  }
}

function closeRunModal() {
  showRunModal.value = false
  selectedRun.value = null
  runItems.value = []
}

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

onMounted(loadAll)
</script>
