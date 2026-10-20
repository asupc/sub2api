<template>
  <BaseDialog
    :show="show"
    :title="`${t('admin.modelBenchmark.runDetail.title')} #${run?.id ?? ''}`"
    width="wide"
    @close="$emit('close')"
  >
    <div v-if="run" class="space-y-4">
      <!-- 元数据基本信息栏 -->
      <div class="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-800/60 dark:text-dark-300">
        <div>
          <span class="font-bold text-gray-900 dark:text-white">{{ run.model_id }}</span>
          <span v-if="run.upstream_model_id" class="ml-2 font-mono text-[11px] text-gray-400">({{ run.upstream_model_id }})</span>
        </div>
        <div class="flex items-center gap-2">
          <span :class="statusBadgeClass(run.status)">{{ t(`admin.modelBenchmark.runStatus.${run.status}`) }}</span>
          <span class="text-gray-400">{{ formatTime(run.created_at) }}</span>
        </div>
      </div>

      <!-- 核心指标网格 -->
      <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 md:grid-cols-6">
        <div v-for="metric in metrics" :key="metric.label" class="rounded-lg border border-gray-100 bg-white p-2.5 shadow-2xs dark:border-dark-800 dark:bg-dark-900">
          <div class="text-[11px] text-gray-400 dark:text-dark-400">{{ metric.label }}</div>
          <div class="mt-1 font-bold tabular-nums text-gray-900 dark:text-white">{{ metric.value }}</div>
        </div>
      </div>

      <!-- 题目分类过滤与搜索 -->
      <div class="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-gray-100 dark:border-dark-800">
        <div class="flex items-center gap-2">
          <h4 class="text-xs font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.drawer.itemEvidence') }}</h4>
          <span class="rounded-full bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600 dark:bg-dark-800 dark:text-dark-300">
            {{ itemProgressText }}
          </span>
          <span
            v-if="unansweredCount > 0"
            class="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
            :title="t('admin.modelBenchmark.runDetail.unansweredHint')"
          >
            {{ t('admin.modelBenchmark.runDetail.unansweredBadge', { count: unansweredCount }) }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <select v-model="filterStatus" class="h-7 rounded border border-gray-200 bg-white px-2 text-xs dark:border-dark-700 dark:bg-dark-800">
            <option value="">全部状态</option>
            <option value="correct">正确</option>
            <option value="partial">部分正确</option>
            <option value="incorrect">错误</option>
            <option value="format_error">格式错误</option>
            <option value="execution_failed">执行失败</option>
          </select>
          <input
            v-model.trim="searchQuery"
            type="text"
            placeholder="搜索题目 ID 或类别..."
            class="input input-xs w-40"
          />
        </div>
      </div>

      <!-- 题目列表表格 -->
      <div class="max-h-[460px] overflow-auto rounded-lg border border-gray-100 dark:border-dark-800">
        <table class="min-w-full text-xs">
          <thead class="sticky top-0 bg-gray-50 dark:bg-dark-800 text-left text-gray-500 dark:text-dark-400">
            <tr>
              <th class="px-3 py-2">Item ID</th>
              <th class="px-2 py-2">类别</th>
              <th class="px-2 py-2">难度</th>
              <th class="px-2 py-2">得分</th>
              <th class="px-2 py-2">耗时</th>
              <th class="px-2 py-2">Token</th>
              <th class="px-3 py-2">作答结果 / 判定</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-50 dark:divide-dark-800/70">
            <template v-for="item in filteredItems" :key="item.id">
              <tr
                class="hover:bg-gray-50/60 dark:hover:bg-dark-800/40 cursor-pointer transition"
                @click="expandedItemId = expandedItemId === item.id ? null : item.id"
              >
                <td class="px-3 py-2 font-mono text-gray-900 dark:text-white">
                  <span class="mr-1 inline-block text-[10px] text-gray-400">{{ expandedItemId === item.id ? '▼' : '▶' }}</span>
                  {{ item.item_id }}
                </td>
                <td class="px-2 py-2">{{ t(`admin.modelBenchmark.category.${item.category}`) || item.category }}</td>
                <td class="px-2 py-2 font-mono">D{{ item.difficulty }}</td>
                <td class="px-2 py-2 font-bold tabular-nums">
                  <span :class="scoreColor(item.score)">{{ (item.score * 100).toFixed(0) }}%</span>
                </td>
                <td class="px-2 py-2 tabular-nums text-gray-500">{{ formatLatency(item.latency_ms) }}</td>
                <td class="px-2 py-2 tabular-nums text-gray-500">{{ item.input_tokens ?? '—' }} / {{ item.output_tokens ?? '—' }}</td>
                <td class="max-w-[220px] truncate px-3 py-2 text-gray-500 dark:text-dark-400">
                  <span v-if="item.failure_reason" class="text-rose-600 dark:text-rose-400" :title="item.failure_reason">
                    {{ item.failure_reason }}
                  </span>
                  <span v-else-if="item.status === 'correct'" class="text-emerald-600 dark:text-emerald-400">
                    完全正确
                  </span>
                  <span v-else>{{ item.status }}</span>
                </td>
              </tr>
              <!-- 展开的做题细节与思维链 -->
              <tr v-if="expandedItemId === item.id" class="bg-gray-50/80 dark:bg-dark-800/40">
                <td colspan="7" class="p-3 text-xs space-y-2">
                  <div v-if="item.prompt_snapshot" class="rounded border border-gray-200 bg-white p-2.5 dark:border-dark-700 dark:bg-dark-900">
                    <div class="text-[11px] font-bold text-gray-500 dark:text-dark-400 mb-1">测试 Prompt:</div>
                    <pre class="font-mono text-[11px] whitespace-pre-wrap break-words text-gray-700 dark:text-dark-200 max-h-40 overflow-y-auto">{{ item.prompt_snapshot }}</pre>
                  </div>
                  <div class="rounded border border-gray-200 bg-white p-2.5 dark:border-dark-700 dark:bg-dark-900">
                    <div class="text-[11px] font-bold text-gray-500 dark:text-dark-400 mb-1">模型回答输出 (Response):</div>
                    <pre class="font-mono text-[11px] whitespace-pre-wrap break-words text-gray-700 dark:text-dark-200 max-h-52 overflow-y-auto">{{ item.response_text || '（无响应内容）' }}</pre>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="filteredItems.length === 0">
              <td colspan="7" class="py-8 text-center text-xs text-gray-400">无匹配的测试用例</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { ModelBenchmarkRun, ModelBenchmarkRunItem } from '@/api/admin/modelBenchmarks'

const props = defineProps<{
  show: boolean
  run: ModelBenchmarkRun | null
  items: ModelBenchmarkRunItem[]
}>()

defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()

const filterStatus = ref('')
const searchQuery = ref('')
const expandedItemId = ref<number | null>(null)

const filteredItems = computed(() => {
  return props.items.filter(item => {
    if (filterStatus.value && item.status !== filterStatus.value) return false
    if (searchQuery.value) {
      const q = searchQuery.value.toLowerCase()
      return item.item_id.toLowerCase().includes(q) || item.category.toLowerCase().includes(q)
    }
    return true
  })
})

// 明细行只包含已落库的作答；中断/超时的 run 题数远少于 run.item_count，
// 因此徽标同时给出「已作答/总题数」，避免把「15 / 15」误读为全卷全对。
const unansweredCount = computed(() => {
  const total = props.run?.item_count ?? props.items.length
  const answered = props.run?.answered_count ?? props.items.length
  return Math.max(total - answered, 0)
})

const itemProgressText = computed(() => {
  const total = props.run?.item_count ?? props.items.length
  return t('admin.modelBenchmark.runDetail.itemProgress', {
    answered: props.run?.answered_count ?? props.items.length,
    total
  })
})

const metrics = computed(() => {
  const r = props.run
  if (!r) return []
  return [
    { label: t('admin.modelBenchmark.fields.intelligence'), value: r.intelligence_score != null ? r.intelligence_score.toFixed(1) : '—' },
    { label: t('admin.modelBenchmark.fields.composite'), value: r.composite_score != null ? r.composite_score.toFixed(1) : '—' },
    { label: t('admin.modelBenchmark.fields.efficiency'), value: r.efficiency_score != null ? r.efficiency_score.toFixed(1) : '—' },
    { label: t('admin.modelBenchmark.fields.latency'), value: formatLatency(r.avg_latency_ms) },
    { label: t('admin.modelBenchmark.fields.coverage'), value: r.quality ? `${r.quality.completion_rate.toFixed(1)}%` : '—' },
    { label: t('admin.modelBenchmark.fields.cost'), value: r.estimated_cost != null ? `$${r.estimated_cost.toFixed(4)}` : '—' }
  ]
})

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function formatTime(str?: string | null) {
  return str ? new Date(str).toLocaleString() : '—'
}

function statusBadgeClass(status: string) {
  if (status === 'success') return 'badge badge-success'
  if (status === 'partial') return 'badge badge-warning'
  if (status === 'running') return 'badge badge-info'
  return 'badge badge-danger'
}

function scoreColor(score: number) {
  if (score >= 0.8) return 'text-emerald-600 dark:text-emerald-400'
  if (score >= 0.5) return 'text-amber-600 dark:text-amber-400'
  return 'text-rose-600 dark:text-rose-400'
}
</script>
