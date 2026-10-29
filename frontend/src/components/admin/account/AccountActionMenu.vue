<template>
  <Teleport to="body">
    <div v-if="show && anchorRect">
      <!-- Backdrop: click anywhere outside to close -->
      <div class="fixed inset-0 z-[9998]" @click="emit('close')"></div>
      <div
        ref="menuRef"
        class="action-menu-content fixed z-[9999] overflow-y-auto overscroll-contain rounded-lg bg-white shadow-lg ring-1 ring-black/5 dark:bg-dark-800"
        :class="view === 'ops' ? 'w-80' : 'w-52'"
        :style="menuStyle"
        @click.stop
      >
        <!-- 运营任务子视图（WorkBuddy / Qoder / Trae） -->
        <template v-if="view === 'ops' && account">
          <div class="flex items-center gap-2 border-b border-gray-100 px-3 py-1.5 dark:border-dark-700">
            <button
              type="button"
              class="flex items-center gap-1 rounded px-1 py-0.5 text-xs text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              @click="view = 'main'"
            >
              <Icon name="chevronLeft" size="xs" />
              {{ t('common.back') }}
            </button>
            <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{{ t('admin.ops.tasks.menuEntry') }}</span>
          </div>
          <PlatformOpsTasksPanel :account="account" />
        </template>

        <div v-else class="py-1">
          <template v-if="account">
            <button @click="$emit('test', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="play" size="sm" class="text-green-500" :stroke-width="2" />
              {{ t('admin.accounts.testConnection') }}
            </button>
            <button @click="$emit('stats', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="chart" size="sm" class="text-indigo-500" />
              {{ t('admin.accounts.viewStats') }}
            </button>
            <button @click="$emit('schedule', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="clock" size="sm" class="text-orange-500" />
              {{ t('admin.scheduledTests.schedule') }}
            </button>
            <button v-if="canDuplicate" @click="$emit('duplicate', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="copy" size="sm" class="text-sky-500" />
              {{ t('admin.accounts.duplicateAccount') }}
            </button>
            <!-- 影子账号不持凭据:重授权/刷新 token 对其无效(后端拒绝),故隐藏(外审 G4)。 -->
            <template v-if="(account.type === 'oauth' || account.type === 'setup-token') && !isShadow && !isSourceIntegratedPlatform">
              <button @click="$emit('reauth', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-blue-600 hover:bg-gray-100 dark:hover:bg-dark-700">
                <Icon name="link" size="sm" />
                {{ t('admin.accounts.reAuthorize') }}
              </button>
              <button @click="$emit('refresh-token', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-purple-600 hover:bg-gray-100 dark:hover:bg-dark-700">
                <Icon name="refresh" size="sm" />
                {{ t('admin.accounts.refreshToken') }}
              </button>
            </template>
            <button v-if="isOpenAIOAuthParent" @click="$emit('create-spark-shadow', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-amber-600 hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="sparkles" size="sm" />
              {{ t('admin.accounts.createSparkShadow') }}
            </button>
            <button v-if="supportsPrivacy" @click="$emit('set-privacy', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-emerald-600 hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="shield" size="sm" />
              {{ t('admin.accounts.setPrivacy') }}
            </button>
            <div v-if="hasRecoverableState" class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
            <button v-if="hasRecoverableState" @click="$emit('recover-state', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-emerald-600 hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="sync" size="sm" />
              {{ t('admin.accounts.recoverState') }}
            </button>
            <button v-if="hasQuotaLimit" @click="$emit('reset-quota', account); $emit('close')" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-teal-600 hover:bg-gray-100 dark:hover:bg-dark-700">
              <Icon name="refresh" size="sm" />
              {{ t('admin.accounts.resetQuota') }}
            </button>
            <!-- 三平台运营任务（签到 / 积分刷新等）：切换到子视图查看与手动执行 -->
            <template v-if="isOpsPlatform">
              <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
              <button @click="view = 'ops'" class="flex w-full items-center gap-2 px-3.5 py-1.5 text-xs text-cyan-600 hover:bg-gray-100 dark:hover:bg-dark-700" data-testid="ops-tasks-entry">
                <Icon name="bolt" size="sm" />
                {{ t('admin.ops.tasks.menuEntry') }}
              </button>
            </template>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useResizeObserver, useWindowSize } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import PlatformOpsTasksPanel from './PlatformOpsTasksPanel.vue'
import type { Account } from '@/types'
import { supportsAccountQuota } from '@/utils/accountQuota'

const props = defineProps<{ show: boolean; account: Account | null; anchorRect: DOMRect | null }>()
const emit = defineEmits(['close', 'test', 'stats', 'schedule', 'duplicate', 'reauth', 'refresh-token', 'recover-state', 'reset-quota', 'set-privacy', 'create-spark-shadow'])
const { t } = useI18n()
const menuRef = ref<HTMLElement | null>(null)
const { width: viewportWidth, height: viewportHeight } = useWindowSize()
const viewportPadding = 8
const menuPosition = ref({ top: viewportPadding, left: viewportPadding })
const menuStyle = computed(() => ({
  top: `${menuPosition.value.top}px`,
  left: `${menuPosition.value.left}px`,
  maxWidth: `${Math.max(0, viewportWidth.value - viewportPadding * 2)}px`,
  maxHeight: `${Math.max(0, viewportHeight.value - viewportPadding * 2)}px`
}))

const updatePosition = () => {
  if (!menuRef.value || !props.anchorRect) return

  const { width, height } = menuRef.value.getBoundingClientRect()
  const anchor = props.anchorRect
  const gap = 4
  const maxTop = viewportHeight.value - height - viewportPadding
  const top = anchor.bottom + gap <= maxTop
    ? anchor.bottom + gap
    : anchor.top - height - gap
  const left = viewportWidth.value < 768
    ? anchor.left + anchor.width / 2 - width / 2
    : anchor.right - width

  menuPosition.value.top = Math.max(viewportPadding, Math.min(top, maxTop))
  menuPosition.value.left = Math.max(viewportPadding, Math.min(left, viewportWidth.value - width - viewportPadding))
}

// Measure after rendering; menu items and translated labels can change its size.
watch([menuRef, () => props.anchorRect, viewportWidth, viewportHeight], updatePosition, { flush: 'post' })
useResizeObserver(menuRef, updatePosition)

const canDuplicate = computed(() => {
  if (!props.account || props.account.parent_account_id != null) return false
  return ['apikey', 'upstream', 'bedrock', 'service_account'].includes(props.account.type)
})
const isRateLimited = computed(() => {
  if (props.account?.rate_limit_reset_at && new Date(props.account.rate_limit_reset_at) > new Date()) {
    return true
  }
  const modelLimits = (props.account?.extra as Record<string, unknown> | undefined)?.model_rate_limits as
    | Record<string, { rate_limit_reset_at: string }>
    | undefined
  if (modelLimits) {
    const now = new Date()
    return Object.values(modelLimits).some(info => new Date(info.rate_limit_reset_at) > now)
  }
  return false
})
const isOverloaded = computed(() => props.account?.overload_until && new Date(props.account.overload_until) > new Date())
const isTempUnschedulable = computed(() => props.account?.temp_unschedulable_until && new Date(props.account.temp_unschedulable_until) > new Date())
const hasRecoverableState = computed(() => {
  return props.account?.status === 'error' || Boolean(isRateLimited.value) || Boolean(isOverloaded.value) || Boolean(isTempUnschedulable.value)
})
const isAntigravityOAuth = computed(() => props.account?.platform === 'antigravity' && props.account?.type === 'oauth')
const isOpenAIOAuth = computed(() => props.account?.platform === 'openai' && props.account?.type === 'oauth')
// 三平台（WorkBuddy / Qoder / Trae）有通用运营任务端点，展示「运营任务」子菜单。
const isOpsPlatform = computed(() =>
  props.account?.platform === 'workbuddy' || props.account?.platform === 'qoder' || props.account?.platform === 'trae'
)
// 三平台使用设备登录/OAuth 轮询流程，不支持 ReAuthAccountModal 的通用授权链接，
// 避免错误落回 Claude 授权 URL。
const isSourceIntegratedPlatform = computed(() =>
  props.account?.platform === 'workbuddy' || props.account?.platform === 'qoder' || props.account?.platform === 'trae'
)
// 主视图 / 运营任务子视图；菜单每次打开都回到主视图。
const view = ref<'main' | 'ops'>('main')
// 影子账号(链接型,持 parent_account_id)不持凭据、type 不可变,凭据/隐私类操作对其无效。
const isShadow = computed(() => props.account?.parent_account_id != null)
// A "parent" OpenAI OAuth account is one that is NOT itself a shadow (parent_account_id == null)
const isOpenAIOAuthParent = computed(() => isOpenAIOAuth.value && !isShadow.value)
const supportsPrivacy = computed(() => (isAntigravityOAuth.value || isOpenAIOAuth.value) && !isShadow.value)
const hasQuotaLimit = computed(() => {
  return supportsAccountQuota(props.account?.type) && (
    (props.account?.quota_limit ?? 0) > 0 ||
    (props.account?.quota_daily_limit ?? 0) > 0 ||
    (props.account?.quota_weekly_limit ?? 0) > 0
  )
})

const handleKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') emit('close')
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      view.value = 'main'
      window.addEventListener('keydown', handleKeydown)
    } else {
      window.removeEventListener('keydown', handleKeydown)
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>
