<template>
  <div class="space-y-3">
    <!-- 区域单选 -->
    <div>
      <label class="input-label">{{ t('admin.accounts.oauth.qoder.realm') }}</label>
      <div class="mt-2 grid grid-cols-2 gap-1.5">
        <button
          v-for="option in realmOptions"
          :key="option.value"
          type="button"
          @click="realm = option.value"
          :class="[
            'flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left transition-all',
            realm === option.value
              ? 'border-violet-500/60 bg-violet-50 dark:bg-violet-900/20'
              : 'border-gray-200 hover:border-violet-300 dark:border-dark-600 dark:hover:border-violet-700'
          ]"
        >
          <Icon
            name="globe"
            size="sm"
            :class="realm === option.value ? 'shrink-0 text-violet-500' : 'shrink-0 text-gray-400 dark:text-gray-500'"
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
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.qoder.deviceAuth') }}</span>
      </label>
      <label class="flex cursor-pointer items-center gap-2">
        <input v-model="importMode" type="radio" value="pat" class="text-primary-600 focus:ring-primary-500" />
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.oauth.qoder.patImport') }}</span>
      </label>
    </div>

    <!-- 设备授权流 -->
    <DeviceAuthFlow
      v-if="importMode === 'device'"
      :title="t('admin.accounts.oauth.qoder.flowTitle')"
      :description="t('admin.accounts.oauth.qoder.flowDesc')"
      :start="startLogin"
      :poll="pollLogin"
      :success-hint="authorizedHint"
      @success="handleAuthSuccess"
    />

    <!-- PAT 导入 -->
    <div v-else class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-700 dark:bg-blue-900/30">
      <h4 class="mb-1 font-semibold text-blue-900 dark:text-blue-200">
        {{ t('admin.accounts.oauth.qoder.patTitle') }}
      </h4>
      <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">
        {{ t('admin.accounts.oauth.qoder.patDesc') }}
      </p>
      <input
        v-model="patInput"
        type="password"
        class="input w-full font-mono text-xs"
        :placeholder="t('admin.accounts.oauth.qoder.patPlaceholder')"
        autocomplete="off"
        spellcheck="false"
      />
      <p v-if="patError" class="mt-1 text-xs text-red-600 dark:text-red-400">{{ patError }}</p>
      <button
        type="button"
        :disabled="!patInput.trim().startsWith('pt-') || exchanging"
        class="btn btn-primary mt-3 w-full text-sm"
        @click="handleImportPAT"
      >
        <span v-if="exchanging" class="mr-2 inline-block h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
        <Icon v-else name="key" size="sm" class="mr-2" />
        {{ exchanging ? t('admin.accounts.oauth.qoder.exchanging') : t('admin.accounts.oauth.qoder.exchange') }}
      </button>
    </div>

    <!-- 模型白名单（可选；授权/PAT 导入完成后可同步上游模型） -->
    <div v-if="credentials" class="border-t border-gray-200 pt-3 dark:border-dark-600">
      <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>
      <ModelWhitelistSelector
        v-model="selectedModels"
        platform="qoder"
        :sync-credentials="{ platform: 'qoder', type: 'oauth', credentials }"
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
      {{ creating ? t('admin.accounts.creating') : t('admin.accounts.oauth.qoder.create') }}
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
 * Qoder 建号表单：区域单选 + 设备授权流或 pt- PAT 导入 + 可选模型白名单。
 * 后端没有 create-from-oauth 端点：两种方式拿到 credentials 后统一走
 * 通用建号 API（platform=qoder, type=oauth）。
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

const realm = ref<'cn' | 'intl'>('cn')
const importMode = ref<'device' | 'pat'>('device')
const patInput = ref('')
const exchanging = ref(false)
const creating = ref(false)
const patError = ref('')
const credentials = ref<Record<string, unknown> | null>(null)

// 模型白名单（可选）：选中模型落 credentials.model_mapping（{模型: 模型}）。
const selectedModels = ref<string[]>([])

const realmOptions = computed(() => [
  { value: 'cn' as const, label: t('admin.accounts.oauth.qoder.realmCN'), desc: t('admin.accounts.oauth.qoder.realmCNDesc') },
  { value: 'intl' as const, label: t('admin.accounts.oauth.qoder.realmIntl'), desc: t('admin.accounts.oauth.qoder.realmIntlDesc') }
])

const canCreate = computed(() => !!credentials.value && !creating.value)

const authorizedHint = computed(() =>
  credentials.value ? t('admin.accounts.oauth.qoder.authorizedHint') : ''
)

const startLogin = () => adminAPI.platformOps.qoderLoginStart(realm.value)
const pollLogin = (state: string): Promise<DeviceLoginPollResponse> =>
  adminAPI.platformOps.qoderLoginPoll(state)

const handleAuthSuccess = (payload: { credentials?: Record<string, unknown> }) => {
  credentials.value = payload.credentials ?? null
}

const handleImportPAT = async () => {
  const pat = patInput.value.trim()
  if (!pat.startsWith('pt-')) return
  exchanging.value = true
  patError.value = ''
  try {
    const result = await adminAPI.platformOps.qoderImportPAT(pat, realm.value)
    credentials.value = result.credentials ?? null
    if (!credentials.value) {
      patError.value = t('admin.accounts.oauth.qoder.noCredentials')
    }
  } catch (err) {
    patError.value = (err as { message?: string })?.message || t('admin.accounts.oauth.qoder.patFailed')
  } finally {
    exchanging.value = false
  }
}

const handleCreate = async () => {
  if (!canCreate.value || !credentials.value) return
  creating.value = true
  try {
    const accountCredentials: Record<string, unknown> = { ...credentials.value }
    if (selectedModels.value.length > 0) {
      const mapping: Record<string, unknown> = {}
      for (const model of selectedModels.value) {
        mapping[model] = model
      }
      accountCredentials.model_mapping = mapping
    }
    await adminAPI.accounts.create({
      // 通用建号对空 name 无兜底，这里用 PAT/设备流凭据里的昵称兜底。
      name: props.name.trim() || inferAccountName(credentials.value),
      notes: props.notes,
      platform: 'qoder',
      type: 'oauth',
      credentials: accountCredentials,
      concurrency: props.concurrency ?? 10,
      priority: props.priority ?? 10,
      group_ids: props.groupIds
    })
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.accounts.oauth.qoder.createFailed'))
  } finally {
    creating.value = false
  }
}

const inferAccountName = (creds: Record<string, unknown>): string => {
  const nickname = typeof creds.nickname === 'string' ? creds.nickname.trim() : ''
  if (nickname) return nickname
  const uid = typeof creds.uid === 'string' ? creds.uid.trim() : ''
  return uid ? `Qoder ${uid}` : 'Qoder OAuth Account'
}
</script>
