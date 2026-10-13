<template>
  <div class="space-y-3">
    <!-- 产品线变体 -->
    <div>
      <label class="input-label">{{ t('admin.accounts.oauth.trae.variant') }}</label>
      <select v-model="variant" class="input">
        <option v-for="option in variantOptions" :key="option.value" :value="option.value">
          {{ option.label }} — {{ option.desc }}
        </option>
      </select>
      <p class="input-hint">{{ t('admin.accounts.oauth.trae.variantHint') }}</p>
    </div>

    <!-- 设备授权流（本机回调：粘贴回跳 URL 完成授权） -->
    <DeviceAuthFlow
      :title="t('admin.accounts.oauth.trae.flowTitle')"
      :description="t('admin.accounts.oauth.trae.flowDesc')"
      :start="startLogin"
      :poll="pollLogin"
      needs-callback
      :success-hint="authorizedHint"
      @success="handleAuthSuccess"
    />

    <!-- 模型白名单（可选；trae 无动态目录，同步返回客户端静态清单） -->
    <div v-if="authResult" class="border-t border-gray-200 pt-3 dark:border-dark-600">
      <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>
      <ModelWhitelistSelector
        v-model="selectedModels"
        platform="trae"
        :sync-credentials="{ platform: 'trae', type: 'oauth', credentials: {} }"
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
      {{ creating ? t('admin.accounts.creating') : t('admin.accounts.oauth.trae.create') }}
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
 * Trae 建号表单：产品线变体下拉 + 本机回调设备授权流 + 可选模型白名单。
 * 授权页回跳 http://127.0.0.1:17388 后把回跳 URL 粘贴回来完成交换，
 * 建号走 create-from-oauth（state + callback_query，后端复用会话凭证）。
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

const variant = ref('trae_cn')
const creating = ref(false)
const authResult = ref<{ state: string; callbackQuery?: string } | null>(null)

// 模型白名单（可选）：选中模型落 credentials.model_mapping（{模型: 模型}）。
const selectedModels = ref<string[]>([])

const variantOptions = computed(() => [
  { value: 'trae_cn', label: t('admin.accounts.oauth.trae.variantTraeCN'), desc: t('admin.accounts.oauth.trae.variantTraeCNDesc') },
  { value: 'solo_cn', label: t('admin.accounts.oauth.trae.variantSoloCN'), desc: t('admin.accounts.oauth.trae.variantSoloCNDesc') },
  { value: 'trae_global', label: t('admin.accounts.oauth.trae.variantTraeGlobal'), desc: t('admin.accounts.oauth.trae.variantTraeGlobalDesc') },
  { value: 'solo_global', label: t('admin.accounts.oauth.trae.variantSoloGlobal'), desc: t('admin.accounts.oauth.trae.variantSoloGlobalDesc') }
])

const canCreate = computed(() => !!authResult.value && !creating.value)

const authorizedHint = computed(() =>
  authResult.value ? t('admin.accounts.oauth.trae.authorizedHint') : ''
)

const startLogin = () => adminAPI.platformOps.traeLoginStart(variant.value)
const pollLogin = (state: string, callbackQuery?: string): Promise<DeviceLoginPollResponse> =>
  adminAPI.platformOps.traeLoginPoll(state, callbackQuery)

const handleAuthSuccess = (payload: { state: string; callbackQuery?: string }) => {
  authResult.value = { state: payload.state, callbackQuery: payload.callbackQuery }
}

const handleCreate = async () => {
  if (!canCreate.value || !authResult.value) return
  creating.value = true
  try {
    const mapping =
      selectedModels.value.length > 0
        ? Object.fromEntries(selectedModels.value.map(model => [model, model]))
        : undefined
    await adminAPI.platformOps.traeCreateFromOAuth({
      state: authResult.value.state,
      callback_query: authResult.value.callbackQuery,
      name: props.name.trim() || undefined,
      concurrency: props.concurrency ?? 10,
      priority: props.priority ?? 10,
      group_ids: props.groupIds,
      model_mapping: mapping
    })
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.accounts.oauth.trae.createFailed'))
  } finally {
    creating.value = false
  }
}
</script>
