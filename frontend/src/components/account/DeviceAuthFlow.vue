<template>
  <div
    class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-700 dark:bg-blue-900/30"
  >
    <div class="flex items-start gap-4">
      <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-blue-500">
        <Icon name="link" size="md" class="text-white" />
      </div>
      <div class="min-w-0 flex-1">
        <h4 class="mb-1 font-semibold text-blue-900 dark:text-blue-200">{{ title }}</h4>
        <p v-if="description" class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ description }}</p>

        <!-- 待开始 -->
        <button
          v-if="phase === 'idle'"
          type="button"
          :disabled="loading"
          class="btn btn-primary text-sm"
          @click="handleStart"
        >
          <span v-if="loading" class="mr-2 inline-block h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
          <Icon v-else name="link" size="sm" class="mr-2" />
          {{ loading ? t('admin.accounts.oauth.deviceFlow.starting') : t('admin.accounts.oauth.deviceFlow.start') }}
        </button>

        <!-- 等待授权 -->
        <div v-else-if="phase === 'waiting'" class="space-y-3">
          <div class="flex items-center gap-2">
            <input
              :value="authUrl"
              readonly
              type="text"
              class="input flex-1 bg-gray-50 font-mono text-xs dark:bg-gray-700"
            />
            <button
              type="button"
              class="btn btn-secondary p-2"
              :title="t('admin.accounts.oauth.deviceFlow.copyUrl')"
              @click="handleCopyUrl"
            >
              <Icon v-if="!copied" name="copy" size="sm" />
              <Icon v-else name="check" size="sm" class="text-green-500" :stroke-width="2" />
            </button>
            <button
              type="button"
              class="btn btn-secondary p-2"
              :title="t('admin.accounts.oauth.deviceFlow.openInNewTab')"
              @click="openInNewTab"
            >
              <Icon name="externalLink" size="sm" />
            </button>
          </div>

          <div class="flex flex-wrap items-center gap-2 text-xs text-blue-700 dark:text-blue-300">
            <span class="inline-flex items-center gap-1.5 rounded-full bg-blue-100 px-2 py-0.5 font-medium dark:bg-blue-900/50">
              <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500"></span>
              {{ t('admin.accounts.oauth.deviceFlow.waiting') }}
            </span>
            <span class="font-mono">{{ countdownLabel }}</span>
            <button
              type="button"
              class="text-blue-600 hover:text-blue-700 dark:text-blue-400"
              @click="reset"
            >
              {{ t('admin.accounts.oauth.deviceFlow.restart') }}
            </button>
          </div>

          <!-- 本机回调平台（Trae）：粘贴回跳 URL 完成授权 -->
          <div v-if="needsCallback" class="rounded-lg border border-blue-300 bg-white/80 p-3 dark:border-blue-600 dark:bg-gray-800/80">
            <label class="mb-1.5 block text-sm font-medium text-blue-900 dark:text-blue-200">
              {{ t('admin.accounts.oauth.deviceFlow.callbackLabel') }}
            </label>
            <p class="mb-2 text-xs text-blue-700 dark:text-blue-300">
              {{ t('admin.accounts.oauth.deviceFlow.callbackDesc') }}
            </p>
            <textarea
              v-model="callbackInput"
              rows="2"
              class="input w-full resize-y font-mono text-xs"
              :placeholder="t('admin.accounts.oauth.deviceFlow.callbackPlaceholder')"
              spellcheck="false"
            ></textarea>
            <button
              type="button"
              :disabled="exchanging || !callbackInput.trim()"
              class="btn btn-primary mt-2 w-full text-sm"
              @click="handleCompleteWithCallback"
            >
              <span v-if="exchanging" class="mr-2 inline-block h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
              <Icon v-else name="check" size="sm" class="mr-2" />
              {{ exchanging ? t('admin.accounts.oauth.deviceFlow.exchanging') : t('admin.accounts.oauth.deviceFlow.completeAuth') }}
            </button>
          </div>
        </div>

        <!-- 授权成功 -->
        <div
          v-else-if="phase === 'success'"
          class="rounded-lg border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-700 dark:bg-emerald-900/30"
        >
          <p class="flex items-center gap-2 text-sm font-medium text-emerald-700 dark:text-emerald-300">
            <Icon name="check" size="sm" :stroke-width="2" />
            {{ successText || t('admin.accounts.oauth.deviceFlow.success') }}
          </p>
          <p v-if="successHint" class="mt-1 text-xs text-emerald-600 dark:text-emerald-400">{{ successHint }}</p>
        </div>

        <!-- 超时 / 失败 -->
        <div
          v-else-if="phase === 'timeout' || phase === 'error'"
          class="rounded-lg border p-3"
          :class="phase === 'timeout'
            ? 'border-amber-200 bg-amber-50 dark:border-amber-700 dark:bg-amber-900/30'
            : 'border-red-200 bg-red-50 dark:border-red-700 dark:bg-red-900/30'"
        >
          <p
            class="text-sm"
            :class="phase === 'timeout'
              ? 'text-amber-700 dark:text-amber-300'
              : 'text-red-600 dark:text-red-400'"
          >
            {{ phase === 'timeout'
              ? t('admin.accounts.oauth.deviceFlow.timeout')
              : errorMessage || t('admin.accounts.oauth.deviceFlow.failed') }}
          </p>
          <button
            type="button"
            class="btn btn-secondary mt-2 text-sm"
            @click="reset"
          >
            <Icon name="refresh" size="sm" class="mr-2" />
            {{ t('admin.accounts.oauth.deviceFlow.restart') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import { Icon } from '@/components/icons'
import type { DeviceLoginPollResponse } from '@/api/admin/platformOps'

/**
 * 通用设备授权流组件（WorkBuddy / Qoder / Trae 复用）：
 * 发起 → 展示授权 URL（复制 / 新窗口）→ 倒计时内每 3s 轮询 → 成功 emit。
 * Trae 为「本机回调」语义（needsCallback）：授权页回跳 localhost 后把回跳
 * URL 查询串粘贴回来，手动触发一次带 callback_query 的 poll 完成交换。
 */
const props = withDefaults(
  defineProps<{
    title: string
    description?: string
    /** 发起授权，返回授权 URL 与 state。 */
    start: () => Promise<{ url: string; state: string }>
    /** 轮询进度；callbackQuery 仅本机回调平台粘贴后传入。 */
    poll: (state: string, callbackQuery?: string) => Promise<DeviceLoginPollResponse>
    /** 授权成功后组件保持成功态，由父组件接管建号。 */
    needsCallback?: boolean
    timeoutSeconds?: number
    pollIntervalMs?: number
    successText?: string
    successHint?: string
  }>(),
  {
    description: '',
    needsCallback: false,
    timeoutSeconds: 900,
    pollIntervalMs: 3000,
    successText: '',
    successHint: ''
  }
)

const emit = defineEmits<{
  /** 授权完成：state 必有；credentials 视后端返回（轮询即取回凭证的平台才有）。 */
  success: [payload: { state: string; credentials?: Record<string, unknown>; callbackQuery?: string }]
}>()

const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()

const phase = ref<'idle' | 'waiting' | 'success' | 'timeout' | 'error'>('idle')
const loading = ref(false)
const exchanging = ref(false)
const errorMessage = ref('')
const authUrl = ref('')
const state = ref('')
const callbackInput = ref('')
const remainingSeconds = ref(0)

let pollTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

const countdownLabel = computed(() => {
  const total = Math.max(0, remainingSeconds.value)
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
})

const stopTimers = () => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (tickTimer) {
    clearInterval(tickTimer)
    tickTimer = null
  }
}

const reset = () => {
  stopTimers()
  phase.value = 'idle'
  loading.value = false
  exchanging.value = false
  errorMessage.value = ''
  authUrl.value = ''
  state.value = ''
  callbackInput.value = ''
  remainingSeconds.value = 0
}

/** 从粘贴内容提取回跳查询串：支持完整 URL、带 ? 的查询串、裸查询串。 */
const normalizeCallbackQuery = (raw: string): string => {
  const trimmed = raw.trim()
  const queryIndex = trimmed.indexOf('?')
  return queryIndex >= 0 ? trimmed.slice(queryIndex + 1) : trimmed
}

const handleStart = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await props.start()
    authUrl.value = result.url
    state.value = result.state
    remainingSeconds.value = props.timeoutSeconds
    phase.value = 'waiting'
    startTimers()
  } catch (err) {
    errorMessage.value = (err as { message?: string })?.message || ''
    phase.value = 'error'
  } finally {
    loading.value = false
  }
}

const startTimers = () => {
  stopTimers()
  pollTimer = setInterval(() => {
    pollOnce().catch(() => {
      // 单次轮询失败不打断流程，等待下一轮；连续失败由超时兜底。
    })
  }, props.pollIntervalMs)
  tickTimer = setInterval(() => {
    remainingSeconds.value -= 1
    if (remainingSeconds.value <= 0) {
      stopTimers()
      phase.value = 'timeout'
    }
  }, 1000)
}

const finish = (result: DeviceLoginPollResponse, callbackQuery?: string) => {
  stopTimers()
  phase.value = 'success'
  emit('success', {
    state: state.value,
    credentials: result.credentials,
    callbackQuery
  })
}

const pollOnce = async (callbackQuery?: string) => {
  if (!state.value || phase.value !== 'waiting') return
  const result = await props.poll(state.value, callbackQuery)
  if (result?.done) {
    finish(result, callbackQuery)
  }
}

const handleCompleteWithCallback = async () => {
  const query = normalizeCallbackQuery(callbackInput.value)
  if (!query) return
  exchanging.value = true
  errorMessage.value = ''
  try {
    await pollOnce(query)
    if (phase.value !== 'success') {
      // 回调参数未能完成交换：保持等待态，提示用户检查粘贴内容。
      errorMessage.value = t('admin.accounts.oauth.deviceFlow.callbackNotReady')
    }
  } catch (err) {
    errorMessage.value = (err as { message?: string })?.message || t('admin.accounts.oauth.deviceFlow.failed')
    phase.value = 'error'
    stopTimers()
  } finally {
    exchanging.value = false
  }
}

const handleCopyUrl = () => {
  if (authUrl.value) {
    copyToClipboard(authUrl.value, t('admin.accounts.oauth.deviceFlow.urlCopied'))
  }
}

const openInNewTab = () => {
  if (authUrl.value) {
    window.open(authUrl.value, '_blank', 'noopener,noreferrer')
  }
}

onUnmounted(stopTimers)
</script>
