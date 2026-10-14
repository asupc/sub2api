<template>
  <div class="flex items-center gap-1.5">
    <!-- WorkBuddy：积分余额 remain/total + 最早到期 -->
    <template v-if="platform === 'workbuddy' && wb">
      <span class="inline-flex items-center rounded border border-slate-200/80 bg-white px-1.5 py-0.5 font-mono text-[10px] font-medium text-slate-700 select-none dark:border-dark-700/80 dark:bg-dark-800 dark:text-slate-200">
        {{ t('admin.ops.quota.remain') }} {{ wb.remain }}<template v-if="wb.total > 0">/{{ wb.total }}</template>
      </span>
      <span
        v-if="wbExpiringLabel"
        class="rounded bg-amber-50 px-1 py-0.5 text-[10px] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
        :title="t('admin.ops.quota.earliestExpire')"
      >
        {{ wbExpiringLabel }}
      </span>
      <span v-if="wb.enterprise" class="rounded bg-sky-50 px-1 py-0.5 text-[10px] text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
        {{ t('admin.ops.quota.enterprise') }}
      </span>
    </template>

    <!-- Qoder：credits remain/total + 套餐 -->
    <template v-else-if="platform === 'qoder' && qd">
      <span class="inline-flex items-center rounded border border-slate-200/80 bg-white px-1.5 py-0.5 font-mono text-[10px] font-medium text-slate-700 select-none dark:border-dark-700/80 dark:bg-dark-800 dark:text-slate-200">
        {{ t('admin.ops.quota.credits') }} {{ qd.remain }}<template v-if="qd.total > 0">/{{ qd.total }}</template>
      </span>
      <span
        v-if="qd.exceeded"
        class="rounded bg-red-50 px-1 py-0.5 text-[10px] text-red-600 dark:bg-red-900/30 dark:text-red-300"
      >
        {{ t('admin.ops.quota.exceeded') }}
      </span>
      <span
        v-else-if="qd.plan"
        class="rounded bg-gray-100 px-1 py-0.5 text-[10px] text-gray-600 dark:bg-gray-800 dark:text-gray-300"
      >
        {{ qd.plan }}
      </span>
    </template>

    <!-- Trae：积分总量 + 最早到期 + 临期告警 -->
    <template v-else-if="platform === 'trae' && trae">
      <span class="inline-flex items-center rounded border border-slate-200/80 bg-white px-1.5 py-0.5 font-mono text-[10px] font-medium text-slate-700 select-none dark:border-dark-700/80 dark:bg-dark-800 dark:text-slate-200">
        {{ t('admin.ops.quota.credits') }} {{ formatCredits(trae.total) }}
      </span>
      <span
        v-if="trae.expiring_soon"
        class="rounded bg-amber-50 px-1 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
        :title="t('admin.ops.quota.expiringSoonDesc')"
      >
        {{ t('admin.ops.quota.expiringSoon') }}
      </span>
      <span
        v-else-if="traeEarliestLabel"
        class="rounded bg-gray-100 px-1 py-0.5 text-[10px] text-gray-600 dark:bg-gray-800 dark:text-gray-300"
      >
        {{ traeEarliestLabel }}
      </span>
    </template>

    <!-- 无快照 -->
    <div v-else class="text-xs text-gray-400">-</div>

    <!-- 签到状态（二开：qoder/workbuddy，读 extra.ops_state.checkin 按账号时区判当日三态） -->
    <span v-if="checkin" :class="checkin.cls" :title="checkin.title">
      {{ checkin.label }}
    </span>

    <!-- 今日积分消耗（二开：qoder/workbuddy，来源上游 usage 积分字段日累计） -->
    <span
      v-if="todayCredits !== null && todayCredits !== undefined"
      class="rounded bg-indigo-50 px-1 py-0.5 font-mono text-[10px] text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300"
      :title="t('admin.ops.quota.todayCreditsDesc')"
    >
      {{ t('admin.ops.quota.todayCredits') }} {{ formatTodayCredits(todayCredits) }}
    </span>

    <!-- 手动查询按钮：调平台 quota 端点，响应即最新快照（后端已落 extra） -->
    <button
      type="button"
      class="shrink-0 rounded border border-primary-200/60 p-1 text-primary-600 transition-all active:scale-95 hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-teal-800/40 dark:text-teal-400 dark:hover:bg-teal-950/50"
      :disabled="loading"
      :title="t('admin.ops.quota.probe')"
      @click="handleQuery"
    >
      <svg
        class="h-3 w-3"
        :class="{ 'animate-spin': loading }"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
        />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import type {
  WorkbuddyQuotaSnapshot,
  QoderQuotaSnapshot,
  TraeQuotaSnapshot,
  PlatformOpsStateEntry
} from '@/api/admin/platformOps'

/**
 * 三平台（WorkBuddy / Qoder / Trae）额度快照单元格：
 * 渲染账号 extra 中的手动/定时任务落下的快照，并提供手动查询按钮
 * （调各平台 quota 端点，后端返回最新快照并已写回 extra）。
 */
const props = defineProps<{
  account: Account
  /** 今日积分消耗（二开：仅 qoder/workbuddy 账号有值；null/undefined 不渲染胶囊）。 */
  todayCredits?: number | null
}>()

const emit = defineEmits<{ refreshed: [account: Account] }>()

const { t } = useI18n()

const loading = ref(false)
// 本地覆盖：手动查询成功后立即用响应更新，无需等列表刷新。
const override = ref<WorkbuddyQuotaSnapshot | QoderQuotaSnapshot | TraeQuotaSnapshot | null>(null)

const platform = computed(() => props.account.platform)
const extra = computed(() => (props.account.extra ?? {}) as Record<string, unknown>)

const wb = computed(() => {
  if (platform.value !== 'workbuddy') return null
  return (override.value ?? extra.value.wb_quota ?? null) as WorkbuddyQuotaSnapshot | null
})
const qd = computed(() => {
  if (platform.value !== 'qoder') return null
  return (override.value ?? extra.value.qd_quota ?? null) as QoderQuotaSnapshot | null
})
const trae = computed(() => {
  if (platform.value !== 'trae') return null
  return (override.value ?? extra.value.trae_quota ?? null) as TraeQuotaSnapshot | null
})

// Trae 积分是浮点（按 token 折算），整数直接显示、小数保留 1 位。
const formatCredits = (value: number) =>
  Number.isInteger(value) ? String(value) : value.toFixed(1)

// 今日消耗单次请求常在 0.1 以下（workbuddy 实测 ~0.08/次），1 位小数会把
// 0.27 显示成 0.3——这里保留 2 位并去掉尾随零，整数仍直接显示。
const formatTodayCredits = (value: number) =>
  Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')

const formatShortDate = (value: string | undefined): string => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

// 账号时区的「今天」键（YYYY-MM-DD），与后端 sameLocalDay 切日口径一致；
// 无效时区名（Intl 抛错）回退浏览器本地时区。
const dayKeyInZone = (date: Date, tz: string): string => {
  try {
    return new Intl.DateTimeFormat('en-CA', { timeZone: tz }).format(date)
  } catch {
    return new Intl.DateTimeFormat('en-CA').format(date)
  }
}

type CheckinBadge = {
  kind: 'ok' | 'failed' | 'pending'
  label: string
  title: string
  cls: string
}

// 每日签到状态（workbuddy/qoder，extra.ops_state.checkin）：
// 今日成功（last_ok 为今天）绿 / 今日跑过但未成功（last_run 为今天）红 / 其余灰。
const checkin = computed<CheckinBadge | null>(() => {
  if (platform.value !== 'workbuddy' && platform.value !== 'qoder') return null
  const opsState = extra.value.ops_state as Record<string, PlatformOpsStateEntry> | undefined
  const entry = opsState?.checkin
  if (!entry) {
    return { kind: 'pending', label: t('admin.ops.quota.checkinPending'), title: t('admin.ops.quota.checkinPendingTitle'), cls: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300' }
  }
  const tz = (extra.value.ops_timezone as string) || 'Asia/Shanghai'
  const today = dayKeyInZone(new Date(), tz)
  if (entry.last_ok && dayKeyInZone(new Date(entry.last_ok), tz) === today) {
    const note = entry.note ? `：${entry.note}` : ''
    return {
      kind: 'ok',
      label: t('admin.ops.quota.checkinOk'),
      title: t('admin.ops.quota.checkinOkTitle', { time: formatShortDate(entry.last_ok), note }),
      cls: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    }
  }
  if (entry.last_run && dayKeyInZone(new Date(entry.last_run), tz) === today) {
    return {
      kind: 'failed',
      label: t('admin.ops.quota.checkinFailed'),
      title: t('admin.ops.quota.checkinFailedTitle', { error: entry.last_err ?? '', time: formatShortDate(entry.last_run) }),
      cls: 'bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300'
    }
  }
  return { kind: 'pending', label: t('admin.ops.quota.checkinPending'), title: t('admin.ops.quota.checkinPendingTitle'), cls: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300' }
})

const wbExpiringLabel = computed(() => formatShortDate(wb.value?.expiring_at))
const traeEarliestLabel = computed(() => {
  const snapshot = trae.value
  if (!snapshot?.has_expire || !snapshot.earliest_expire) return ''
  return formatShortDate(new Date(snapshot.earliest_expire * 1000).toISOString())
})

const handleQuery = async () => {
  loading.value = true
  try {
    if (platform.value === 'workbuddy') {
      override.value = await adminAPI.platformOps.workbuddyQueryQuota(props.account.id)
    } else if (platform.value === 'qoder') {
      override.value = await adminAPI.platformOps.qoderQueryQuota(props.account.id)
    } else if (platform.value === 'trae') {
      override.value = await adminAPI.platformOps.traeQueryQuota(props.account.id)
    }
    // 快照已落库；通知父级刷新列表中的账号对象。
    emit('refreshed', props.account)
  } catch {
    // 后端错误经全局拦截器弹出；此处保持原快照展示。
  } finally {
    loading.value = false
  }
}
</script>
