<template>
  <div class="space-y-3">
    <!-- 区域单选 -->
    <div>
      <label class="input-label">{{ t('admin.accounts.oauth.workbuddy.realm') }}</label>
      <div class="mt-2 grid grid-cols-2 gap-1.5">
        <button
          v-for="option in realmOptions"
          :key="option.value"
          type="button"
          @click="realm = option.value"
          :class="[
            'flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left transition-all',
            realm === option.value
              ? 'border-sky-500/60 bg-sky-50 dark:bg-sky-900/20'
              : 'border-gray-200 hover:border-sky-300 dark:border-dark-600 dark:hover:border-sky-700'
          ]"
        >
          <Icon
            name="globe"
            size="sm"
            :class="realm === option.value ? 'shrink-0 text-sky-500' : 'shrink-0 text-gray-400 dark:text-gray-500'"
          />
          <div class="min-w-0">
            <span class="block truncate text-xs font-medium text-gray-900 dark:text-white">{{ option.label }}</span>
            <span class="block truncate text-[11px] text-gray-500 dark:text-gray-400">{{ option.desc }}</span>
          </div>
        </button>
      </div>
    </div>

    <!-- 导入方式 -->
    <div class="flex gap-4">
      <label class="flex cursor-pointer items-center gap-2">
        <input v-model="importMode" type="radio" value="device" class="text-primary-600 focus:ring-primary-500" />
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.workbuddy.deviceAuth') }}</span>
      </label>
      <label class="flex cursor-pointer items-center gap-2">
        <input v-model="importMode" type="radio" value="json" class="text-primary-600 focus:ring-primary-500" />
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.workbuddy.jsonImport') }}</span>
      </label>
    </div>

    <!-- 设备授权流 -->
    <DeviceAuthFlow
      v-if="importMode === 'device'"
      :title="t('admin.accounts.oauth.workbuddy.flowTitle')"
      :description="t('admin.accounts.oauth.workbuddy.flowDesc')"
      :start="startLogin"
      :poll="pollLogin"
      :success-hint="authorizedHint"
      @success="handleAuthSuccess"
    />

    <!-- JSON 凭据手动导入 -->
    <div v-else class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-700 dark:bg-blue-900/30">
      <h4 class="mb-1 font-semibold text-blue-900 dark:text-blue-200">
        {{ t('admin.accounts.oauth.workbuddy.jsonTitle') }}
      </h4>
      <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">
        {{ t('admin.accounts.oauth.workbuddy.jsonDesc') }}
      </p>
      <textarea
        v-model="jsonInput"
        rows="6"
        class="input w-full resize-y font-mono text-xs"
        :placeholder="t('admin.accounts.oauth.workbuddy.jsonPlaceholder')"
        spellcheck="false"
      ></textarea>
      <p v-if="jsonParseError" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ jsonParseError }}</p>
    </div>

    <!-- 版本覆盖高级项（可选） -->
    <div>
      <button
        type="button"
        class="flex items-center gap-1 text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
        @click="showAdvanced = !showAdvanced"
      >
        <Icon :name="showAdvanced ? 'chevronUp' : 'chevronDown'" size="xs" />
        {{ t('admin.accounts.oauth.workbuddy.advanced') }}
      </button>
      <div v-if="showAdvanced" class="mt-2 grid gap-3 sm:grid-cols-3">
        <div>
          <label class="input-label">client_version</label>
          <input v-model="clientVersion" type="text" class="input font-mono text-xs" placeholder="1.9.0" />
        </div>
        <div>
          <label class="input-label">cli_version</label>
          <input v-model="cliVersion" type="text" class="input font-mono text-xs" placeholder="0.0.1" />
        </div>
        <div>
          <label class="input-label">user_agent</label>
          <input v-model="userAgent" type="text" class="input font-mono text-xs" placeholder="Mozilla/5.0 ..." />
        </div>
      </div>
    </div>

    <!-- 模型白名单（可选；授权/导入完成后可同步上游模型） -->
    <div v-if="whitelistCredentials" class="border-t border-gray-200 pt-3 dark:border-dark-600">
      <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>
      <ModelWhitelistSelector
        v-model="selectedModels"
        platform="workbuddy"
        :sync-credentials="whitelistCredentials"
      />
    </div>

    <!-- 创建按钮 -->
    <button
      type="button"
      :disabled="!canCreate || creating"
      class="btn btn-primary w-full"
      @click="handleCreate"
    >
      <span v-if="creating" class="mr-2 inline-block h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
      <Icon v-else name="sparkles" size="sm" class="mr-2" />
      {{ creating ? t('admin.accounts.creating') : t('admin.accounts.oauth.workbuddy.create') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { DeviceLoginPollResponse } from '@/api/admin/platformOps'
import DeviceAuthFlow from '../DeviceAuthFlow.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { Icon } from '@/components/icons'

/**
 * WorkBuddy 建号表单：区域单选 + 设备授权流（或 JSON 凭据手动导入）+
 * 可选版本覆盖（client_version / cli_version / user_agent）+
 * 可选模型白名单（授权完成后可「同步上游支持的模型」）。
 * 设备流完成且无版本覆盖时以 state 建号；有覆盖或手动导入时以 credentials 建号。
 */
const props = defineProps<{
  name: string
  notes: string | null
  groupIds: number[]
  concurrency?: number
  priority?: number
}>()

const emit = defineEmits<{ created: [] }>()

const { t } = useI18n()
const appStore = useAppStore()

const realm = ref<'cn' | 'global'>('cn')
const importMode = ref<'device' | 'json'>('device')
const jsonInput = ref('')
const showAdvanced = ref(false)
const clientVersion = ref('')
const cliVersion = ref('')
const userAgent = ref('')
const creating = ref(false)

const deviceState = ref<{ state: string; credentials?: Record<string, unknown> } | null>(null)

// 模型白名单（可选）：选中模型落 credentials.model_mapping（{模型: 模型}）。
const selectedModels = ref<string[]>([])

// 授权/导入完成后凭据在手，白名单区与「同步上游模型」随之可用。
const whitelistCredentials = computed(() => {
  if (importMode.value === 'json' && parsedJson.value) {
    return { platform: 'workbuddy', type: 'oauth', credentials: parsedJson.value }
  }
  if (deviceState.value?.credentials) {
    return { platform: 'workbuddy', type: 'oauth', credentials: deviceState.value.credentials }
  }
  return null
})

const realmOptions = computed(() => [
  { value: 'cn' as const, label: t('admin.accounts.oauth.workbuddy.realmCN'), desc: t('admin.accounts.oauth.workbuddy.realmCNDesc') },
  { value: 'global' as const, label: t('admin.accounts.oauth.workbuddy.realmGlobal'), desc: t('admin.accounts.oauth.workbuddy.realmGlobalDesc') }
])

const parsedJson = computed<Record<string, unknown> | null>(() => {
  const raw = jsonInput.value.trim()
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
    return null
  } catch {
    return null
  }
})

const jsonParseError = computed(() => {
  const raw = jsonInput.value.trim()
  if (!raw || parsedJson.value) return ''
  return t('admin.accounts.oauth.workbuddy.jsonInvalid')
})

// 版本覆盖项（仅填写了才合并进 credentials）。
const versionOverrides = computed(() => {
  const overrides: Record<string, unknown> = {}
  if (clientVersion.value.trim()) overrides.client_version = clientVersion.value.trim()
  if (cliVersion.value.trim()) overrides.cli_version = cliVersion.value.trim()
  if (userAgent.value.trim()) overrides.user_agent = userAgent.value.trim()
  return overrides
})

const hasOverrides = computed(() => Object.keys(versionOverrides.value).length > 0)

const canCreate = computed(() => {
  if (creating.value) return false
  if (importMode.value === 'json') return !!parsedJson.value
  return !!deviceState.value
})

const authorizedHint = computed(() =>
  deviceState.value ? t('admin.accounts.oauth.workbuddy.authorizedHint') : ''
)

const startLogin = () => adminAPI.platformOps.workbuddyLoginStart(realm.value)
const pollLogin = (state: string): Promise<DeviceLoginPollResponse> =>
  adminAPI.platformOps.workbuddyLoginPoll(state)

const handleAuthSuccess = (payload: { state: string; credentials?: Record<string, unknown> }) => {
  deviceState.value = { state: payload.state, credentials: payload.credentials }
}

const handleCreate = async () => {
  if (!canCreate.value) return
  creating.value = true
  try {
    let payload: {
      state?: string
      credentials?: Record<string, unknown>
      name?: string
      notes?: string | null
      group_ids?: number[]
      concurrency?: number
      priority?: number
      model_mapping?: Record<string, unknown>
    } = {
      name: props.name.trim() || undefined,
      notes: props.notes,
      group_ids: props.groupIds,
      concurrency: props.concurrency ?? 10,
      priority: props.priority ?? 10
    }
    if (importMode.value === 'json' && parsedJson.value) {
      payload.credentials = { ...parsedJson.value, ...versionOverrides.value }
    } else if (deviceState.value) {
      // 无版本覆盖时以 state 建号（后端从会话幂等取回凭证）；
      // 有覆盖时必须走 credentials，把覆盖项合并进去。
      if (hasOverrides.value && deviceState.value.credentials) {
        payload.credentials = { ...deviceState.value.credentials, ...versionOverrides.value }
      } else if (hasOverrides.value) {
        appStore.showError(t('admin.accounts.oauth.workbuddy.overridesNeedCredentials'))
        return
      } else {
        payload.state = deviceState.value.state
      }
    }
    if (selectedModels.value.length > 0) {
      const mapping: Record<string, unknown> = {}
      for (const model of selectedModels.value) {
        mapping[model] = model
      }
      payload.model_mapping = mapping
    }
    await adminAPI.platformOps.workbuddyCreateFromOAuth(payload)
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.accounts.oauth.workbuddy.createFailed'))
  } finally {
    creating.value = false
  }
}
</script>
