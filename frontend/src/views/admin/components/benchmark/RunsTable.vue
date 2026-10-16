<template>
  <div class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
    <!-- 筛选过滤操作栏 -->
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-4 dark:border-dark-800">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-bold text-gray-900 dark:text-white">
          {{ t('admin.modelBenchmark.runs.title') }}
        </h2>
        <span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-800 dark:text-dark-300">
          {{ filteredRuns.length }} 条记录
        </span>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <!-- 模型模糊搜索过滤 -->
        <input
          v-model.trim="filterModel"
          type="text"
          placeholder="按模型筛选..."
          class="input input-sm w-36 text-xs"
        />

        <!-- 状态过滤下拉 -->
        <select
          v-model="filterStatus"
          class="h-8 rounded-lg border border-gray-300 bg-white px-2.5 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200"
        >
          <option value="">全部状态</option>
          <option value="success">{{ t('admin.modelBenchmark.runStatus.success') }}</option>
          <option value="partial">{{ t('admin.modelBenchmark.runStatus.partial') }}</option>
          <option value="running">{{ t('admin.modelBenchmark.runStatus.running') }}</option>
          <option value="failed">{{ t('admin.modelBenchmark.runStatus.failed') }}</option>
          <option value="canceled">{{ t('admin.modelBenchmark.runStatus.canceled') }}</option>
        </select>

        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('refresh')">
          {{ t('admin.modelBenchmark.refresh') }}
        </button>
      </div>
    </div>

    <!-- 执行记录表格 -->
    <div class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead>
          <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">
            <th class="px-4 py-3">Run ID</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.status') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.intelligence') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.outputTokens') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.latency') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.coverage') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.rankingEligibility') }}</th>
            <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.created') }}</th>
            <th class="px-4 py-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-50 dark:divide-dark-800/60">
          <tr
            v-for="run in filteredRuns"
            :key="run.id"
            class="hover:bg-gray-50/60 dark:hover:bg-dark-800/40 cursor-pointer transition"
            @click="$emit('open-run', run.id)"
          >
            <td class="px-4 py-3 font-mono font-medium text-gray-800 dark:text-dark-200">#{{ run.id }}</td>
            <td class="px-3 py-3 max-w-[200px]">
              <div class="truncate font-semibold text-gray-900 dark:text-white">{{ run.model_id }}</div>
              <div class="mt-0.5 text-[11px] text-gray-400 font-mono">
                {{ run.comparison_key || '—' }} · {{ run.max_output_tokens > 0 ? `${run.max_output_tokens} tokens` : '上游预算' }}
              </div>
            </td>
            <td class="px-3 py-3">
              <span :class="statusBadgeClass(run.status)">
                {{ t(`admin.modelBenchmark.runStatus.${run.status}`) }}
              </span>
            </td>
            <td class="px-3 py-3 font-bold tabular-nums text-primary-600 dark:text-primary-400">
              {{ formatScore(run.intelligence_score) }}
            </td>
            <td class="px-3 py-3 tabular-nums text-gray-600 dark:text-dark-300">
              {{ formatTokens(run.output_tokens) }}
            </td>
            <td class="px-3 py-3 tabular-nums text-gray-600 dark:text-dark-300">
              {{ formatLatency(run.avg_latency_ms) }}
            </td>
            <td class="px-3 py-3 tabular-nums text-gray-600 dark:text-dark-300">
              {{ run.quality ? `${run.quality.completion_rate.toFixed(1)}%` : '—' }}
            </td>
            <td class="px-3 py-3">
              <span
                :class="run.quality?.ranking_eligible ? 'badge badge-success' : 'badge badge-warning'"
                :title="qualityReason(run)"
              >
                {{ t(run.quality?.ranking_eligible ? 'admin.modelBenchmark.comparison.eligible' : 'admin.modelBenchmark.comparison.diagnostic') }}
              </span>
            </td>
            <td class="px-3 py-3 text-xs text-gray-400">
              {{ formatTime(run.created_at) }}
            </td>
            <td class="px-4 py-3 text-right">
              <button
                type="button"
                class="btn btn-secondary btn-xs"
                @click.stop="$emit('open-run', run.id)"
              >
                查看明细
              </button>
            </td>
          </tr>

          <tr v-if="filteredRuns.length === 0">
            <td colspan="10" class="px-4 py-12 text-center text-sm text-gray-400">
              {{ t('admin.modelBenchmark.runs.empty') }}
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
import type { ModelBenchmarkRun } from '@/api/admin/modelBenchmarks'

const props = defineProps<{
  runs: ModelBenchmarkRun[]
  suiteVersion?: string
}>()

defineEmits<{
  (e: 'open-run', id: number): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const filterModel = ref('')
const filterStatus = ref('')

const filteredRuns = computed(() => {
  return props.runs.filter(r => {
    if (filterStatus.value && r.status !== filterStatus.value) return false
    if (filterModel.value && !r.model_id.toLowerCase().includes(filterModel.value.toLowerCase())) return false
    return true
  })
})

function statusBadgeClass(status: string) {
  if (status === 'success') return 'badge badge-success'
  if (status === 'partial') return 'badge badge-warning'
  if (status === 'running') return 'badge badge-info'
  return 'badge badge-danger'
}

function qualityReason(run: ModelBenchmarkRun) {
  if (!run.quality || run.suite_version !== props.suiteVersion || !Object.keys(run.quality).length) {
    return t('admin.modelBenchmark.comparison.legacyReason')
  }
  if (run.quality.ranking_eligible) {
    return t('admin.modelBenchmark.comparison.eligible')
  }
  return t(
    run.quality.ranking_reason === 'provider_managed_budget'
      ? 'admin.modelBenchmark.comparison.providerReason'
      : 'admin.modelBenchmark.comparison.coverageReason'
  )
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
  return str ? new Date(str).toLocaleString() : '—'
}
</script>
