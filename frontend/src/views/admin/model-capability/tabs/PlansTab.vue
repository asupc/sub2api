<template>
  <div class="min-w-0 space-y-4">
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

      <!-- 测评设置：回环网关认证身份与全局统一的采样次数 / 单题输出预算 -->
      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="min-w-0">
            <h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.modelBenchmark.settings.title') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.settings.help') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <span v-if="settingsKeyId" class="badge badge-success">{{ t('admin.modelBenchmark.settings.configured', { name: settingsKeyName || `#${settingsKeyId}` }) }}</span>
            <span v-else class="badge badge-secondary">{{ t('admin.modelBenchmark.settings.missing') }}</span>
            <Select
              v-model="settingsGroupId"
              size="sm"
              class="w-44"
              :options="groupOptions"
              :placeholder="t('admin.modelBenchmark.settings.groupPlaceholder')"
              @change="onSettingsGroupChange"
            />
            <Select
              v-model="settingsFormKeyId"
              size="sm"
              class="w-52"
              :options="apiKeyOptions"
              :disabled="!settingsGroupId"
              :placeholder="settingsGroupId ? t('admin.modelBenchmark.settings.keyPlaceholder') : t('admin.modelBenchmark.settings.groupPlaceholder')"
            />
            <label class="flex items-center gap-1.5 text-xs text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.samples') }}
              <input v-model.number="settingsSampleCount" type="number" min="1" max="3" class="input input-sm w-16" />
            </label>
            <label class="flex items-center gap-1.5 text-xs text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.maxTokens') }}
              <input v-model.number="settingsMaxTokens" type="number" min="128" max="32768" class="input input-sm w-24" />
            </label>
            <button type="button" class="btn btn-primary btn-sm" :disabled="savingSettings" @click="saveSettings">
              {{ savingSettings ? t('common.saving') : t('common.save') }}
            </button>
          </div>
        </div>
      </div>

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
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.cron') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.status') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.nextRun') }}</th>
                <th class="px-3 py-3">{{ t('admin.modelBenchmark.fields.lastRun') }}</th>
                <th class="px-4 py-3 text-right">{{ t('admin.modelBenchmark.plans.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="plan in plans" :key="plan.id" class="border-b border-gray-50 last:border-0 dark:border-dark-800/70 hover:bg-gray-50/50 dark:hover:bg-dark-800/40 transition">
                <td class="max-w-[180px] px-4 py-3 font-medium text-gray-900 dark:text-white"><div class="truncate">{{ plan.name }}</div><div class="mt-1 text-[11px] font-normal text-gray-500">{{ plan.comparison_key || 'baseline-v2' }} · {{ t('admin.modelBenchmark.fields.samples') }} {{ plan.sample_count }} · {{ t('admin.modelBenchmark.fields.maxTokens') }} {{ plan.max_output_tokens }} · {{ t('admin.modelBenchmark.fields.reasoningEffort') }} {{ plan.reasoning_effort || plan.reasoning_level || '-' }} · {{ t('admin.modelBenchmark.fields.timeout') }} {{ plan.timeout_seconds }}s · max_results {{ plan.max_results || 50 }}</div></td>
                <td class="max-w-[160px] truncate px-3 py-3 text-gray-600 dark:text-dark-300 font-mono text-xs">{{ plan.model_id }}</td>
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
              <tr v-if="plans.length === 0"><td colspan="7" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.empty') }}</td></tr>
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
        <div class="grid gap-3 sm:grid-cols-2">
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
            />
            <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ selectedModelSupported ? selectedModelPlatforms : t('admin.modelBenchmark.plans.unsupportedModel') }}</p>
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
              disabled
              class="input input-sm w-full cursor-not-allowed opacity-70"
            />
            <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.managedBySettings') }}</p>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.maxTokens') }}
            </label>
            <input
              v-model.number="planForm.max_output_tokens"
              type="number"
              disabled
              class="input input-sm w-full cursor-not-allowed opacity-70"
            />
            <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.managedBySettings') }}</p>
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('admin.modelBenchmark.fields.reasoningEffort') }}
            </label>
            <input
              v-model.trim="planForm.reasoning_effort"
              class="input input-sm w-full"
              placeholder="high"
            />
          </div>
          <div>
            <label for="benchmark-comparison-key" class="mb-1 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('admin.modelBenchmark.fields.comparisonKey') }}</label>
            <input id="benchmark-comparison-key" v-model.trim="planForm.comparison_key" required maxlength="64" pattern="[A-Za-z0-9._-]+" class="input input-sm w-full" />
            <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ t('admin.modelBenchmark.plans.comparisonHelp') }}</p>
          </div>
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
          {{ t('admin.modelBenchmark.plans.costWarning', { count: suite?.item_count ?? 48, samples: settingsSampleCount || 1 }) }}
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import adminAPI from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
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
  cron_expression: '0 4 * * 1',
  enabled: false,
  sample_count: 1,
  comparison_key: 'baseline-v2',
  max_output_tokens: 8192,
  timeout_seconds: 90,
  reasoning_effort: '',
  max_results: 50
})

const planForm = reactive(defaultForm())

const suite = computed(() => suites.value[0] ?? null)
const benchmarkTargetByID = computed(() => new Map(targets.value.map(model => [model.id, model])))
const selectedModelSupported = computed(() => benchmarkTargetByID.value.has(planForm.model_id))
const selectedModelPlatforms = computed(() => {
  const model = manifestModels.value.find(item => item.id === planForm.model_id)
  return model?.platforms?.length ? model.platforms.join(' / ') : '—'
})

// 模型下拉直接读取全量模型清单，搜索由 Select 组件内建（含显示名匹配）。
const modelSelectOptions = computed(() => manifestModels.value.map(model => ({
  value: model.id,
  label: model.display_name ? `${model.id} (${model.display_name})` : model.id
})))

// ---- 测评设置：回环网关认证 Key + 全局统一的采样次数 / 单题输出预算 ----
const settingsKeyId = ref(0)
const settingsKeyName = ref('')
const settingsGroupId = ref<number | ''>('')
const settingsFormKeyId = ref<number | ''>('')
const settingsSampleCount = ref(1)
const settingsMaxTokens = ref(8192)
const savingSettings = ref(false)
const groups = ref<{ id: number; name: string }[]>([])
const groupApiKeys = ref<{ id: number; name: string }[]>([])

const groupOptions = computed(() => groups.value.map(group => ({ value: group.id, label: group.name })))
const apiKeyOptions = computed(() => groupApiKeys.value.map(key => ({ value: key.id, label: `#${key.id} ${key.name}` })))

async function onSettingsGroupChange() {
  settingsFormKeyId.value = ''
  groupApiKeys.value = []
  const groupId = settingsGroupId.value
  if (!groupId) return
  try {
    const data = await adminAPI.groups.getGroupApiKeys(Number(groupId), 1, 100)
    groupApiKeys.value = (data.items ?? []).filter((key: { status?: string }) => key.status === 'active')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error))
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    // 未重新选择 Key 时沿用已配置值，避免把 api_key_id 置 0 清掉现有配置
    const keyId = settingsFormKeyId.value || settingsKeyId.value
    const sampleCount = Math.min(3, Math.max(1, Math.round(Number(settingsSampleCount.value) || 1)))
    const maxTokens = Math.min(32768, Math.max(128, Math.round(Number(settingsMaxTokens.value) || 8192)))
    const settings = await adminAPI.modelBenchmarks.updateSettings({ api_key_id: Number(keyId) || 0, sample_count: sampleCount, max_output_tokens: maxTokens })
    settingsKeyId.value = settings.api_key_id ?? 0
    settingsSampleCount.value = settings.sample_count ?? sampleCount
    settingsMaxTokens.value = settings.max_output_tokens ?? maxTokens
    const selected = groupApiKeys.value.find(key => key.id === Number(keyId))
    if (selected) settingsKeyName.value = selected.name
    appStore.showSuccess(t('common.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error))
  } finally {
    savingSettings.value = false
  }
}

const queueConfirmMessage = computed(() => {
  const plan = queueTarget.value
  if (!plan) return ''
  return t('admin.modelBenchmark.plans.runConfirmMessage', {
    name: plan.name,
    model: plan.model_id,
    count: suite.value?.item_count ?? 112,
    samples: plan.sample_count || settingsSampleCount || 1
  })
})

async function loadAll() {
  loadError.value = ''
  try {
    const [suiteData, manifestData, targetData, planData, settingsData, groupData] = await Promise.all([
      adminAPI.modelBenchmarks.listSuites(),
      adminAPI.models.getModelManifest(),
      adminAPI.modelBenchmarks.listTargets(),
      adminAPI.modelBenchmarks.listPlans(),
      adminAPI.modelBenchmarks.getSettings().catch(() => ({ api_key_id: 0, sample_count: 1, max_output_tokens: 8192 })),
      adminAPI.groups.getAll().catch(() => [])
    ])
    suites.value = suiteData
    manifestModels.value = manifestData.models ?? []
    targets.value = targetData
    plans.value = planData
    settingsKeyId.value = settingsData.api_key_id ?? 0
    settingsSampleCount.value = settingsData.sample_count ?? 1
    settingsMaxTokens.value = settingsData.max_output_tokens ?? 8192
    groups.value = (groupData ?? []).map(group => ({ id: group.id, name: group.name }))

    // 回显测评设置：定位已配置 Key 所属分组与 Key，并回填右侧选择器
    settingsGroupId.value = ''
    settingsFormKeyId.value = ''
    groupApiKeys.value = []
    settingsKeyName.value = ''
    const configuredKeyId = settingsKeyId.value
    if (configuredKeyId) {
      for (const group of groups.value) {
        try {
          const data = await adminAPI.groups.getGroupApiKeys(Number(group.id), 1, 100)
          const keys = (data.items ?? []).filter((key: { status?: string }) => key.status === 'active')
          const found = keys.find((key: { id: number }) => key.id === configuredKeyId)
          if (found) {
            settingsGroupId.value = group.id
            groupApiKeys.value = keys
            settingsFormKeyId.value = found.id
            settingsKeyName.value = found.name
            break
          }
        } catch {
          // 忽略单个分组拉取失败，继续尝试下一个
        }
      }
    }
  } catch (error) {
    loadError.value = extractApiErrorMessage(error, t('admin.modelBenchmark.loadFailed'))
  }
}

function applyPreferredModel() {
  // 优先选中当前可测评（有可调度账号的文本模型）的模型，避免默认落到不可测评模型上。
  const preferred = manifestModels.value.find(model => benchmarkTargetByID.value.has(model.id)) ?? manifestModels.value[0]
  planForm.model_id = preferred?.id ?? ''
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
  // 采样次数与单题 max tokens 由测评设置统一管理，只读展示全局值
  planForm.sample_count = settingsSampleCount.value
  planForm.max_output_tokens = settingsMaxTokens.value
  applyPreferredModel()
  showPlanModal.value = true
}

function openEditModal(plan: ModelBenchmarkPlan) {
  editingPlanId.value = plan.id
  modalError.value = ''
  planForm.name = plan.name
  planForm.model_id = plan.model_id
  planForm.cron_expression = plan.cron_expression
  planForm.enabled = plan.enabled
  planForm.sample_count = plan.sample_count
  planForm.max_output_tokens = plan.max_output_tokens
  planForm.timeout_seconds = plan.timeout_seconds
  planForm.reasoning_effort = plan.reasoning_effort || plan.reasoning_level || ''
  planForm.max_results = plan.max_results || 50
  planForm.comparison_key = plan.comparison_key || 'baseline-v2'
  showPlanModal.value = true
}

function closePlanModal() {
  showPlanModal.value = false
  modalError.value = ''
}

async function handleSavePlan() {
  if (!planForm.name || !planForm.model_id) return
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
    // API 拦截器 reject 的是普通对象（非 Error 实例），须用统一工具提取 message，否则显示 [object Object]
    modalError.value = extractApiErrorMessage(error)
  } finally {
    saving.value = false
  }
}

async function togglePlan(plan: ModelBenchmarkPlan) {
  try {
    await adminAPI.modelBenchmarks.updatePlan(plan.id, { comparison_key: plan.comparison_key, name: plan.name, model_id: plan.model_id, cron_expression: plan.cron_expression, enabled: !plan.enabled, sample_count: plan.sample_count, max_output_tokens: plan.max_output_tokens, timeout_seconds: plan.timeout_seconds, reasoning_effort: plan.reasoning_effort || plan.reasoning_level || '', max_results: plan.max_results })
    await loadAll()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error))
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
    appStore.showError(t('admin.modelBenchmark.plans.runFailed', { error: extractApiErrorMessage(error) }))
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
    appStore.showError(extractApiErrorMessage(error))
  }
}

function formatTime(value?: string | null) { return value ? new Date(value).toLocaleString() : '—' }
onMounted(loadAll)
</script>
