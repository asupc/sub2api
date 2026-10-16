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
          <button type="button" class="btn btn-secondary btn-sm" @click="loadAll">{{ t('admin.modelBenchmark.refresh') }}</button>
          <button type="button" class="btn btn-primary btn-sm flex items-center gap-1.5" @click="openCreateModal">
            <Icon name="plus" size="xs" />
            <span>{{ t('admin.modelBenchmark.plans.create') }}</span>
          </button>
        </div>
      </div>
      <div v-if="loadError" class="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-800 dark:bg-rose-900/20 dark:text-rose-300">{{ loadError }}</div>

      <div class="min-w-0 rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-800 flex items-center justify-between">
          <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.plans.title') }}</h2>
          <span class="text-xs text-gray-400 dark:text-dark-400">{{ plans.length }} {{ t('admin.modelBenchmark.plans.title') }}</span>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 text-left text-xs text-gray-500 dark:border-dark-800 dark:text-dark-400">
                <th class="px-4 py-3">{{ t('admin.modelBenchmark.fields.name') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.model') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.account') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.cron') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.status') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.nextRun') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.lastRun') }}</th>
                <th class="px-4 py-3 text-right">{{ t('admin.modelBenchmark.plans.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="plan in plans" :key="plan.id" class="border-b border-gray-50 last:border-0 dark:border-dark-800/70 hover:bg-gray-50/50 dark:hover:bg-dark-800/40 transition">
                <td class="max-w-[180px] px-4 py-3 font-medium text-gray-900 dark:text-white"><div class="truncate">{{ plan.name }}</div><div class="mt-1 text-[11px] font-normal text-gray-500">{{ plan.comparison_key || 'baseline-v2' }} · {{ plan.max_output_tokens }} tokens</div></td>
                <td class="max-w-[160px] truncate px-3 py-3 text-gray-600 dark:text-dark-300 font-mono text-xs">{{ plan.model_id }}</td>
                <td class="px-3 py-3 text-gray-600 dark:text-dark-300">#{{ plan.account_id }}</td>
                <td class="px-3 py-3 text-xs font-mono text-gray-600 dark:text-dark-300">{{ plan.cron_expression }}</td>
                <td class="px-3 py-3">
                  <span v-if="plan.running_status === 'running' || plan.manual_trigger_at" class="badge badge-info">{{ t('admin.modelBenchmark.plans.queued') }}</span>
                  <span v-else :class="plan.enabled ? 'badge badge-success' : 'badge badge-secondary'">{{ plan.enabled ? t('admin.modelBenchmark.status.enabled') : t('admin.modelBenchmark.status.disabled') }}</span>
                </td>
                <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ plan.enabled ? formatTime(plan.next_run_at) : '—' }}</td>
                <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatTime(plan.last_run_at) }}</td>
                <td class="px-4 py-3 text-right">
                  <div class="flex justify-end gap-1.5 whitespace-nowrap">
                    <button type="button" class="btn btn-secondary btn-xs" :disabled="plan.running_status === 'running' || queueingId === plan.id" @click="requestQueue(plan)">{{ t('admin.modelBenchmark.plans.runNow') }}</button>
                    <button type="button" class="btn btn-secondary btn-xs" @click="openEditModal(plan)">{{ t('common.edit') }}</button>
                    <button type="button" class="btn btn-secondary btn-xs" @click="togglePlan(plan)">{{ plan.enabled ? t('admin.modelBenchmark.plans.disable') : t('admin.modelBenchmark.plans.enable') }}</button>
                    <button type="button" class="btn btn-danger btn-xs" @click="requestDelete(plan)">{{ t('common.delete') }}</button>
                  </div>
                </td>
              </tr>
              <tr v-if="plans.length === 0"><td colspan="8" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 新建 / 编辑测评计划模态框 -->
    <BaseDialog
      :show="showPlanModal"
      :title="editingPlanId ? t('admin.modelBenchmark.plans.edit') : t('admin.modelBenchmark.plans.create')"
      width="wide"
      @close="closePlanModal"
    >
      <div v-if="modalError" class="mb-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs text-rose-700 dark:border-rose-800 dark:bg-rose-900/20 dark:text-rose-300">
        {{ modalError }}
      </div>
      <form class="space-y-4" @submit.prevent="handleSavePlan">
        <div>
          <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
            {{ t('admin.modelBenchmark.fields.name') }} <span class="text-rose-500">*</span>
          </label>
          <input
            v-model.trim="planForm.name"
            required
            class="input input-sm w-full"
            :placeholder="t('admin.modelBenchmark.plans.namePlaceholder')"
          />
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.model') }} <span class="text-rose-500">*</span>
            </label>
            <Select
              v-model="planForm.model_id"
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
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.account') }} <span class="text-rose-500">*</span>
            </label>
            <Select
              v-model="planForm.account_id"
              size="sm"
              class="w-full"
              :options="accountSelectOptions"
              :disabled="!selectedModelSupported"
              :placeholder="selectedModelSupported ? t('common.selectOption') : t('admin.modelBenchmark.plans.unsupportedModel')"
              :empty-text="t('admin.modelBenchmark.plans.unsupportedModel')"
            />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.cron') }} <span class="text-rose-500">*</span>
            </label>
            <input
              v-model.trim="planForm.cron_expression"
              required
              class="input input-sm w-full font-mono"
              placeholder="0 4 * * 1"
            />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.samples') }}
            </label>
            <input
              v-model.number="planForm.sample_count"
              type="number"
              min="1"
              max="3"
              class="input input-sm w-full"
            />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.maxTokens') }}
            </label>
            <input
              v-model.number="planForm.max_output_tokens"
              type="number"
              min="128"
              max="4096"
              class="input input-sm w-full"
            />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.timeout') }}
            </label>
            <input
              v-model.number="planForm.timeout_seconds"
              type="number"
              min="10"
              max="600"
              class="input input-sm w-full"
            />
          </div>
        </div>
        <div class="space-y-1.5">
          <label for="benchmark-comparison-key" class="block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.comparisonKey') }}</label>
          <input id="benchmark-comparison-key" v-model.trim="planForm.comparison_key" required maxlength="64" pattern="[A-Za-z0-9._-]+" class="input input-sm w-full" />
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.comparisonHelp') }}</p>
        </div>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
          <input
            v-model="planForm.enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600"
          />
          {{ t('admin.modelBenchmark.fields.enabled') }}
        </label>
        <p class="rounded-lg bg-amber-50 p-2.5 text-[11px] text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
          {{ t('admin.modelBenchmark.plans.costWarning', { count: suite?.item_count ?? 48, samples: planForm.sample_count || 1 }) }}
        </p>
        <div class="flex justify-end gap-2 pt-2 border-t border-gray-100 dark:border-dark-800">
          <button type="button" class="btn btn-secondary btn-sm" @click="closePlanModal">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-sm" :disabled="saving || !selectedModelSupported">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </BaseDialog>

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
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import adminAPI from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ModelBenchmarkPlan, ModelBenchmarkSuite } from '@/api/admin/modelBenchmarks'
import type { ModelManifestItem } from '@/api/admin/models'

const { t } = useI18n()
const appStore = useAppStore()
const saving = ref(false)
const loadError = ref('')
const modalError = ref('')
const showPlanModal = ref(false)
const editingPlanId = ref<number | null>(null)

const suites = ref<ModelBenchmarkSuite[]>([])
const manifestModels = ref<ModelManifestItem[]>([])
const targets = ref<ModelManifestItem[]>([])
const plans = ref<ModelBenchmarkPlan[]>([])
const queueTarget = ref<ModelBenchmarkPlan | null>(null)
const deleteTarget = ref<ModelBenchmarkPlan | null>(null)
const queueingId = ref<number | null>(null)

const defaultForm = () => ({
  name: '',
  model_id: '',
  account_id: 0,
  cron_expression: '0 4 * * 1',
  enabled: false,
  sample_count: 1,
  comparison_key: 'baseline-v2',
  max_output_tokens: 2048,
  timeout_seconds: 90,
  max_results: 50
})

const planForm = reactive(defaultForm())

const suite = computed(() => suites.value[0] ?? null)
const benchmarkTargetByID = computed(() => new Map(targets.value.map(model => [model.id, model])))
const selectedBenchmarkTarget = computed(() => benchmarkTargetByID.value.get(planForm.model_id))
const selectedModelAccounts = computed(() => selectedBenchmarkTarget.value?.accounts ?? [])
const selectedModelSupported = computed(() => selectedModelAccounts.value.length > 0)
const selectedModelPlatforms = computed(() => {
  const model = manifestModels.value.find(item => item.id === planForm.model_id)
  return model?.platforms?.length ? model.platforms.join(' / ') : '—'
})

// 模型下拉直接读取全量模型清单，搜索由 Select 组件内建（含显示名匹配）。
const modelSelectOptions = computed(() => manifestModels.value.map(model => ({
  value: model.id,
  label: model.display_name ? `${model.id} (${model.display_name})` : model.id
})))

const accountSelectOptions = computed(() => {
  const options = selectedModelAccounts.value.map(account => ({
    value: account.id,
    label: `#${account.id} ${account.name} · ${account.platform}`
  }))
  if (planForm.account_id && !options.some(o => o.value === planForm.account_id)) {
    options.unshift({
      value: planForm.account_id,
      label: `#${planForm.account_id}`
    })
  }
  return options
})

const queueConfirmMessage = computed(() => {
  const plan = queueTarget.value
  if (!plan) return ''
  return t('admin.modelBenchmark.plans.runConfirmMessage', {
    name: plan.name,
    model: plan.model_id,
    count: suite.value?.item_count ?? 112,
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
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : t('admin.modelBenchmark.loadFailed')
  }
}

function applyPreferredModel() {
  // 优先选中当前确有可测评账号的模型，避免默认落到空账号的模型上。
  const preferred = manifestModels.value.find(model => benchmarkTargetByID.value.get(model.id)?.accounts.length) ?? manifestModels.value[0]
  planForm.model_id = preferred?.id ?? ''
  planForm.account_id = benchmarkTargetByID.value.get(planForm.model_id)?.accounts[0]?.id ?? 0
}

function onModelChange() {
  planForm.account_id = selectedModelAccounts.value[0]?.id ?? 0
}

watch(() => manifestModels.value.length, () => {
  if (!planForm.model_id) {
    applyPreferredModel()
  }
})

function openCreateModal() {
  editingPlanId.value = null
  modalError.value = ''
  Object.assign(planForm, defaultForm())
  applyPreferredModel()
  showPlanModal.value = true
}

function openEditModal(plan: ModelBenchmarkPlan) {
  editingPlanId.value = plan.id
  modalError.value = ''
  planForm.name = plan.name
  planForm.model_id = plan.model_id
  planForm.account_id = plan.account_id
  planForm.cron_expression = plan.cron_expression
  planForm.enabled = plan.enabled
  planForm.sample_count = plan.sample_count
  planForm.max_output_tokens = plan.max_output_tokens
  planForm.timeout_seconds = plan.timeout_seconds
  planForm.max_results = plan.max_results || 50
  planForm.comparison_key = plan.comparison_key || 'baseline-v2'
  showPlanModal.value = true
}

function closePlanModal() {
  showPlanModal.value = false
  modalError.value = ''
}

async function handleSavePlan() {
  if (!planForm.name || !planForm.model_id || !planForm.account_id) return
  saving.value = true
  modalError.value = ''
  try {
    if (editingPlanId.value) {
      await adminAPI.modelBenchmarks.updatePlan(editingPlanId.value, { ...planForm })
      appStore.showSuccess(t('common.saved'))
    } else {
      await adminAPI.modelBenchmarks.createPlan({ ...planForm })
      appStore.showSuccess(t('common.saved'))
    }
    showPlanModal.value = false
    await loadAll()
  } catch (error) {
    modalError.value = error instanceof Error ? error.message : String(error)
  } finally {
    saving.value = false
  }
}

async function togglePlan(plan: ModelBenchmarkPlan) {
  try {
    await adminAPI.modelBenchmarks.updatePlan(plan.id, { comparison_key: plan.comparison_key, name: plan.name, model_id: plan.model_id, account_id: plan.account_id, cron_expression: plan.cron_expression, enabled: !plan.enabled, sample_count: plan.sample_count, max_output_tokens: plan.max_output_tokens, timeout_seconds: plan.timeout_seconds, max_results: plan.max_results })
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
