<template>
  <div class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
    <!-- 头部工具栏：搜索、排序、题库切换 -->
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-4 dark:border-dark-800">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-bold text-gray-900 dark:text-white">
          {{ t('admin.modelBenchmark.rankings.title') }}
        </h2>
        <span class="rounded-full bg-primary-50 px-2 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
          {{ filteredRankings.length }} {{ t('admin.modelBenchmark.fields.model') }}
        </span>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <!-- 搜索过滤输入框 -->
        <div class="relative">
          <input
            v-model.trim="searchQuery"
            type="text"
            class="input input-sm w-44 pl-8 text-xs"
            :placeholder="t('admin.modelBenchmark.leaderboard.searchPlaceholder')"
          />
          <svg
            class="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400 pointer-events-none"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
        </div>

        <!-- 排序下拉 -->
        <select
          v-model="sortBy"
          class="h-8 rounded-lg border border-gray-300 bg-white px-2.5 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200"
        >
          <option value="intelligence">{{ t('admin.modelBenchmark.leaderboard.sortIntelligence') }}</option>
          <option value="composite">{{ t('admin.modelBenchmark.leaderboard.sortComposite') }}</option>
          <option value="efficiency">{{ t('admin.modelBenchmark.leaderboard.sortEfficiency') }}</option>
          <option value="latency">{{ t('admin.modelBenchmark.leaderboard.sortLatency') }}</option>
          <option value="runs">{{ t('admin.modelBenchmark.leaderboard.sortRuns') }}</option>
          <option value="time">{{ t('admin.modelBenchmark.leaderboard.sortTime') }}</option>
        </select>

        <!-- 题库版本下拉 -->
        <select
          :value="suiteKey"
          :aria-label="t('admin.modelBenchmark.rankings.suite')"
          class="h-8 rounded-lg border border-gray-300 bg-white px-2 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200"
          @change="$emit('update:suiteKey', ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="option in suiteOptions" :key="option.key" :value="option.key">
            {{ option.label }}
          </option>
        </select>
      </div>
    </div>

    <!-- 提示条 -->
    <div class="px-4 py-2 bg-gray-50/50 border-b border-gray-100 text-[11px] text-gray-500 flex items-center justify-between dark:bg-dark-800/20 dark:border-dark-800 dark:text-dark-400">
      <span class="flex items-center gap-1.5">
        <svg class="w-3.5 h-3.5 text-primary-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        {{ t('admin.modelBenchmark.leaderboard.perspectiveTip') }}
      </span>
      <span>{{ t('admin.modelBenchmark.rankings.hint') }}</span>
    </div>

    <!-- 排行榜表格 -->
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead>
          <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">
            <th class="px-4 py-3 w-14">#</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.intelligence') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.composite') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.efficiency') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.latency') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.outputTokens') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.lastRun') }}</th>
            <th class="px-4 py-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-50 dark:divide-dark-800/60">
          <tr
            v-for="(ranking, index) in filteredRankings"
            :key="ranking.model_id"
            class="group cursor-pointer hover:bg-primary-50/40 dark:hover:bg-primary-900/10 transition"
            @click="$emit('select-model', ranking.model_id)"
          >
            <!-- 排名勋章/数字 -->
            <td class="px-4 py-3.5">
              <span
                class="inline-flex items-center justify-center w-6 h-6 rounded-full text-xs font-bold"
                :class="rankBadgeClass(index)"
              >
                {{ index + 1 }}
              </span>
            </td>

            <!-- 模型标识与快照说明 -->
            <td class="px-3 py-3.5 max-w-[240px]">
              <div class="font-bold text-gray-900 dark:text-white truncate group-hover:text-primary-600 dark:group-hover:text-primary-400 transition">
                {{ ranking.model_id }}
              </div>
              <div class="mt-0.5 text-[11px] text-gray-400 font-mono">
                {{ ranking.comparison_key }} · {{ ranking.max_output_tokens > 0 ? `${ranking.max_output_tokens} tokens` : t('admin.modelBenchmark.comparison.providerBudget') }}
              </div>
            </td>

            <!-- 智力能力分 (高亮呈现) -->
            <td class="px-3 py-3.5">
              <div class="flex items-center gap-2">
                <span class="text-base font-bold tabular-nums text-primary-600 dark:text-primary-400">
                  {{ formatScore(ranking.intelligence_score) }}
                </span>
                <div class="w-16 h-1.5 rounded-full bg-gray-100 overflow-hidden dark:bg-dark-800 hidden sm:block">
                  <div
                    class="h-full rounded-full bg-primary-500"
                    :style="{ width: `${ranking.intelligence_score ?? 0}%` }"
                  ></div>
                </div>
              </div>
            </td>

            <!-- 综合评分 -->
            <td class="px-3 py-3.5 font-bold tabular-nums text-gray-800 dark:text-dark-100">
              {{ formatScore(ranking.composite_score) }}
            </td>

            <!-- 效率评分 -->
            <td class="px-3 py-3.5 tabular-nums text-gray-600 dark:text-dark-300">
              {{ formatScore(ranking.efficiency_score) }}
            </td>

            <!-- 平均延迟 -->
            <td class="px-3 py-3.5 tabular-nums text-gray-600 dark:text-dark-300">
              {{ formatLatency(ranking.avg_latency_ms) }}
            </td>

            <!-- 输出 Token -->
            <td class="px-3 py-3.5 tabular-nums text-gray-600 dark:text-dark-300">
              {{ formatTokens(ranking.output_tokens) }}
            </td>

            <!-- 最近评测时间 -->
            <td class="px-3 py-3.5 text-xs text-gray-400">
              {{ formatTime(ranking.last_run_at) }}
            </td>

            <!-- 操作列 -->
            <td class="px-4 py-3.5 text-right">
              <button
                type="button"
                class="btn btn-secondary btn-xs inline-flex items-center gap-1 group-hover:btn-primary"
                @click.stop="$emit('select-model', ranking.model_id)"
              >
                <span>{{ t('admin.modelBenchmark.leaderboard.viewPerspective') }}</span>
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
                </svg>
              </button>
            </td>
          </tr>

          <tr v-if="filteredRankings.length === 0">
            <td colspan="9" class="px-4 py-12 text-center text-sm text-gray-400">
              {{ t('admin.modelBenchmark.rankings.empty') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelBenchmarkRanking } from '@/api/admin/modelBenchmarks'

interface SuiteOption {
  key: string
  label: string
}

const props = defineProps<{
  rankings: ModelBenchmarkRanking[]
  suiteKey: string
  suiteOptions: SuiteOption[]
}>()

defineEmits<{
  (e: 'select-model', modelId: string): void
  (e: 'update:suiteKey', key: string): void
}>()

const { t } = useI18n()

const searchQuery = ref('')
const sortBy = ref<'intelligence' | 'composite' | 'efficiency' | 'latency' | 'runs' | 'time'>('intelligence')

const filteredRankings = computed(() => {
  let list = [...props.rankings]

  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(r => r.model_id.toLowerCase().includes(q))
  }

  list.sort((a, b) => {
    switch (sortBy.value) {
      case 'composite':
        return (b.composite_score ?? 0) - (a.composite_score ?? 0)
      case 'efficiency':
        return (b.efficiency_score ?? 0) - (a.efficiency_score ?? 0)
      case 'latency':
        return (a.avg_latency_ms ?? 999999) - (b.avg_latency_ms ?? 999999)
      case 'runs':
        return b.run_count - a.run_count
      case 'time':
        return new Date(b.last_run_at).getTime() - new Date(a.last_run_at).getTime()
      case 'intelligence':
      default:
        return (b.intelligence_score ?? 0) - (a.intelligence_score ?? 0)
    }
  })

  return list
})

function rankBadgeClass(idx: number) {
  if (idx === 0) return 'bg-amber-100 text-amber-800 dark:bg-amber-900/50 dark:text-amber-200 shadow-2xs'
  if (idx === 1) return 'bg-slate-200 text-slate-800 dark:bg-slate-700 dark:text-slate-200'
  if (idx === 2) return 'bg-amber-800/20 text-amber-900 dark:bg-amber-800/40 dark:text-amber-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-400'
}

function formatScore(val?: number | null) {
  return val == null ? '—' : val.toFixed(1)
}

function formatTokens(val?: number | null) {
  if (val == null) return '—'
  return val >= 1000 ? `${(val / 1000).toFixed(1)}k` : String(val)
}

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function formatTime(str?: string | null) {
  return str ? new Date(str).toLocaleDateString() : '—'
}
</script>
