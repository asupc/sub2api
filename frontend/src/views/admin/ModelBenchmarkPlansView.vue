<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-xl font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.plans.pageTitle') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.pageDescription') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <span v-if="suite" class="rounded-lg border border-primary-200 bg-primary-50 px-2.5 py-1 text-xs font-semibold text-primary-700 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300">
            {{ suite.id }} · D{{ suite.min_difficulty }}-D{{ suite.max_difficulty }} · {{ suite.item_count }} {{ t('admin.modelBenchmark.items') }}
          </span>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadAll">{{ t('admin.modelBenchmark.refresh') }}</button>
        </div>
      </div>
      <div v-if="loadError" class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-900/20 dark:text-rose-300">{{ loadError }}</div>

      <div class="grid gap-4 xl:grid-cols-[minmax(360px,.42fr)_minmax(0,.58fr)]">
        <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.plans.create') }}</h2>
          <form class="mt-4 space-y-3" @submit.prevent="handleCreate">
            <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.name') }}</label><input v-model.trim="form.name" required class="input input-sm w-full" :placeholder="t('admin.modelBenchmark.plans.namePlaceholder')" /></div>
            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.model') }}</label>
                <Select
                  v-model="form.model_id"
                  size="sm"
                  class="w-full"
                  :options="modelSelectOptions"
                  :search-placeholder="t('admin.modelBenchmark.plans.modelSearchPlaceholder')"
                  :empty-text="t('admin.modelBenchmark.plans.modelSearchEmpty')"
                  @change="onModelChange"
                />
                <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ selectedModelPlatforms }}</p>
              </div>
              <div>
                <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.account') }}</label>
                <Select
                  v-model="form.account_id"
                  size="sm"
                  class="w-full"
                  :options="accountSelectOptions"
                  :disabled="!selectedModelSupported"
                  :placeholder="selectedModelSupported ? t('common.selectOption') : t('admin.modelBenchmark.plans.unsupportedModel')"
                  :empty-text="t('admin.modelBenchmark.plans.unsupportedModel')"
                />
              </div>
              <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.cron') }}</label><input v-model.trim="form.cron_expression" required class="input input-sm w-full font-mono" placeholder="0 4 * * 1" /></div>
              <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.samples') }}</label><input v-model.number="form.sample_count" type="number" min="1" max="3" class="input input-sm w-full" /></div>
              <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.maxTokens') }}</label><input v-model.number="form.max_output_tokens" type="number" min="128" max="4096" class="input input-sm w-full" /></div>
              <div><label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.timeout') }}</label><input v-model.number="form.timeout_seconds" type="number" min="10" max="600" class="input input-sm w-full" /></div>
            </div>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200"><input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />{{ t('admin.modelBenchmark.fields.enabled') }}</label>
            <p class="rounded-lg bg-amber-50 p-2.5 text-[11px] text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.modelBenchmark.plans.costWarning', { count: suite?.item_count ?? 48, samples: form.sample_count || 1 }) }}</p>
            <button type="submit" class="btn btn-primary btn-sm w-full" :disabled="creating || !selectedModelSupported">{{ creating ? t('common.saving') : t('admin.modelBenchmark.plans.create') }}</button>
          </form>
        </div>

        <div class="rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
          <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-800"><h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.plans.title') }}</h2></div>
          <div class="overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead>
                <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">
                  <th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.name') }}</th>
                  <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th>
                  <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.account') }}</th>
                  <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.status') }}</th>
                  <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.nextRun') }}</th>
                  <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.lastRun') }}</th>
                  <th class="px-4 py-3 text-right">{{ t('admin.modelBenchmark.plans.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="plan in plans" :key="plan.id" class="border-b border-gray-50 last:border-0 dark:border-dark-800/70">
                  <td class="max-w-[180px] truncate px-4 py-3 font-medium text-gray-900 dark:text-white">{{ plan.name }}</td>
                  <td class="max-w-[160px] truncate px-3 py-3 text-gray-600 dark:text-dark-300">{{ plan.model_id }}</td>
                  <td class="px-3 py-3 text-gray-600 dark:text-dark-300">#{{ plan.account_id }}</td>
                  <td class="px-3 py-3">
                    <span v-if="plan.running_status === 'running' || plan.manual_trigger_at" class="badge badge-info">{{ t('admin.modelBenchmark.plans.queued') }}</span>
                    <span v-else :class="plan.enabled ? 'badge badge-success' : 'badge badge-secondary'">{{ plan.enabled ? t('admin.modelBenchmark.status.enabled') : t('admin.modelBenchmark.status.disabled') }}</span>
                  </td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ plan.enabled ? formatTime(plan.next_run_at) : '—' }}</td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatTime(plan.last_run_at) }}</td>
                  <td class="px-4 py-3 text-right">
                    <div class="flex justify-end gap-1.5">
                      <button type="button" class="btn btn-secondary btn-xs" :disabled="plan.running_status === 'running' || queueingId === plan.id" @click="requestQueue(plan)">{{ t('admin.modelBenchmark.plans.runNow') }}</button>
                      <button type="button" class="btn btn-secondary btn-xs" @click="togglePlan(plan)">{{ plan.enabled ? t('admin.modelBenchmark.plans.disable') : t('admin.modelBenchmark.plans.enable') }}</button>
                      <button type="button" class="btn btn-danger btn-xs" @click="requestDelete(plan)">{{ t('common.delete') }}</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="plans.length === 0"><td colspan="7" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <!-- 手动执行确认：明确提示即将发起的上游请求规模 -->
    <ConfirmDialog
      :show="queueTarget !== null"
      :title="t('admin.modelBenchmark.plans.runConfirmTitle')"
      :message="queueConfirmMessage"
      :confirm-text="t('admin.modelBenchmark.plans.runNow')"
      @confirm="confirmQueue"
      @cancel="queueTarget = null"
    />
    <ConfirmDialog
      :show="deleteTarget !== null"
      :title="t('common.delete')"
      :message="deleteTarget ? t('admin.modelBenchmark.plans.deleteConfirm', { name: deleteTarget.name }) : ''"
      danger
      @confirm="confirmDelete"
      @cancel="deleteTarget = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import adminAPI from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ModelBenchmarkPlan, ModelBenchmarkSuite } from '@/api/admin/modelBenchmarks'
import type { ModelManifestItem } from '@/api/admin/models'

const { t } = useI18n()
const appStore = useAppStore()
const creating = ref(false)
const loadError = ref('')
const suites = ref<ModelBenchmarkSuite[]>([])
const manifestModels = ref<ModelManifestItem[]>([])
const targets = ref<ModelManifestItem[]>([])
const plans = ref<ModelBenchmarkPlan[]>([])
const queueTarget = ref<ModelBenchmarkPlan | null>(null)
const deleteTarget = ref<ModelBenchmarkPlan | null>(null)
const queueingId = ref<number | null>(null)
const form = reactive({ name: '', model_id: '', account_id: 0, cron_expression: '0 4 * * 1', enabled: false, sample_count: 1, max_output_tokens: 768, timeout_seconds: 90, max_results: 50 })

const suite = computed(() => suites.value[0] ?? null)
const benchmarkTargetByID = computed(() => new Map(targets.value.map(model => [model.id, model])))
const selectedBenchmarkTarget = computed(() => benchmarkTargetByID.value.get(form.model_id))
const selectedModelAccounts = computed(() => selectedBenchmarkTarget.value?.accounts ?? [])
const selectedModelSupported = computed(() => selectedModelAccounts.value.length > 0)
const selectedModelPlatforms = computed(() => {
  const model = manifestModels.value.find(item => item.id === form.model_id)
  return model?.platforms?.length ? model.platforms.join(' / ') : '—'
})
// 模型下拉直接读取全量模型清单，搜索由 Select 组件内建（含显示名匹配）。
const modelSelectOptions = computed(() => manifestModels.value.map(model => ({
  value: model.id,
  label: model.display_name ? `${model.id} (${model.display_name})` : model.id
})))
const accountSelectOptions = computed(() => selectedModelAccounts.value.map(account => ({
  value: account.id,
  label: `#${account.id} ${account.name} · ${account.platform}`
})))
const queueConfirmMessage = computed(() => {
  const plan = queueTarget.value
  if (!plan) return ''
  return t('admin.modelBenchmark.plans.runConfirmMessage', {
    name: plan.name,
    model: plan.model_id,
    count: suite.value?.item_count ?? 48,
    samples: plan.sample_count || 1
  })
})

async function loadAll() {
  loadError.value = ''
  try {
    const [suiteData, manifestData, targetData, planData] = await Promise.all([
      adminAPI.modelBenchmarks.listSuites(),
      adminAPI.models.getModelManifest(),
      adminAPI.modelBenchmarks.listTargets(),
      adminAPI.modelBenchmarks.listPlans()
    ])
    suites.value = suiteData
    manifestModels.value = manifestData.models ?? []
    targets.value = targetData
    plans.value = planData
    if (!form.model_id) {
      applyPreferredModel()
    }
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : t('admin.modelBenchmark.loadFailed')
  }
}

function applyPreferredModel() {
  // 优先选中当前确有可测评账号的模型，避免默认落到空账号的模型上。
  const preferred = manifestModels.value.find(model => benchmarkTargetByID.value.get(model.id)?.accounts.length) ?? manifestModels.value[0]
  form.model_id = preferred?.id ?? ''
  form.account_id = benchmarkTargetByID.value.get(form.model_id)?.accounts[0]?.id ?? 0
}

function onModelChange() {
  form.account_id = selectedModelAccounts.value[0]?.id ?? 0
}

watch(() => manifestModels.value.length, () => {
  if (!form.model_id) {
    applyPreferredModel()
  }
})

async function handleCreate() {
  if (!form.name || !form.model_id || !form.account_id) return
  creating.value = true
  loadError.value = ''
  try {
    await adminAPI.modelBenchmarks.createPlan({ ...form })
    form.name = ''
    form.enabled = false
    await loadAll()
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : String(error)
  } finally {
    creating.value = false
  }
}

async function togglePlan(plan: ModelBenchmarkPlan) {
  try {
    await adminAPI.modelBenchmarks.updatePlan(plan.id, { name: plan.name, model_id: plan.model_id, account_id: plan.account_id, cron_expression: plan.cron_expression, enabled: !plan.enabled, sample_count: plan.sample_count, max_output_tokens: plan.max_output_tokens, timeout_seconds: plan.timeout_seconds, max_results: plan.max_results })
    await loadAll()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : String(error))
  }
}

function requestQueue(plan: ModelBenchmarkPlan) {
  queueTarget.value = plan
}

async function confirmQueue() {
  const plan = queueTarget.value
  if (!plan) return
  queueingId.value = plan.id
  try {
    await adminAPI.modelBenchmarks.queueRun(plan.id)
    appStore.showSuccess(t('admin.modelBenchmark.plans.runQueued', { name: plan.name }))
    queueTarget.value = null
    await loadAll()
  } catch (error) {
    appStore.showError(t('admin.modelBenchmark.plans.runFailed', { error: error instanceof Error ? error.message : String(error) }))
  } finally {
    queueingId.value = null
  }
}

function requestDelete(plan: ModelBenchmarkPlan) {
  deleteTarget.value = plan
}

async function confirmDelete() {
  const plan = deleteTarget.value
  if (!plan) return
  try {
    await adminAPI.modelBenchmarks.delete(plan.id)
    deleteTarget.value = null
    await loadAll()
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : String(error))
  }
}

function formatTime(value?: string | null) { return value ? new Date(value).toLocaleString() : '—' }
onMounted(loadAll)
</script>
