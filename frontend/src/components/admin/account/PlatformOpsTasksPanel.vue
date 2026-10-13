<template>
  <div class="py-1" data-testid="platform-ops-panel">
    <div class="flex items-center justify-between px-3 pb-1 pt-1.5">
      <div class="flex items-center gap-1">
        <button
          type="button"
          class="rounded px-1.5 py-0.5 text-[11px] font-semibold uppercase tracking-wide"
          :class="view === 'tasks'
            ? 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'
            : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300'"
          @click="view = 'tasks'"
        >
          {{ t('admin.ops.tasks.title') }}
        </button>
        <span class="text-[11px] text-gray-300 dark:text-gray-600">|</span>
        <button
          type="button"
          data-testid="ops-config-tab"
          class="rounded px-1.5 py-0.5 text-[11px] font-semibold uppercase tracking-wide"
          :class="view === 'config'
            ? 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'
            : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300'"
          @click="openConfig"
        >
          {{ t('admin.ops.tasks.configTab') }}
        </button>
      </div>
      <button
        v-if="view === 'tasks'"
        type="button"
        class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-primary-600 hover:bg-primary-50 dark:text-teal-400 dark:hover:bg-teal-950/50"
        :disabled="loading"
        @click="load"
      >
        <span v-if="loading" class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-primary-200 border-t-primary-600"></span>
        <Icon v-else name="refresh" size="xs" />
        {{ t('admin.ops.tasks.refresh') }}
      </button>
    </div>

    <!-- 加载骨架 -->
    <div v-if="loading && !tasks.length" class="space-y-1.5 px-3 py-2">
      <div v-for="i in 3" :key="i" class="h-8 animate-pulse rounded bg-gray-100 dark:bg-dark-700"></div>
    </div>

    <!-- 加载失败 -->
    <div v-else-if="error" class="px-3 py-2 text-xs text-red-500">
      {{ error }}
    </div>

    <!-- 任务列表视图 -->
    <template v-else-if="view === 'tasks'">
      <button
        v-for="task in tasks"
        :key="task.id"
        type="button"
        class="flex w-full flex-col gap-1 px-3 py-1.5 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
        :data-testid="`ops-task-${task.id}`"
        :disabled="runningTask === task.id"
        @click="run(task)"
      >
        <div class="flex w-full items-center justify-between gap-2">
          <span class="flex items-center gap-1.5 text-xs font-medium text-gray-700 dark:text-gray-200">
            <span
              v-if="runningTask === task.id"
              class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-primary-200 border-t-primary-600"
            ></span>
            <Icon v-else name="play" size="xs" class="text-green-500" :stroke-width="2" />
            {{ task.name }}
          </span>
          <span
            class="max-w-[130px] shrink-0 truncate rounded px-1.5 py-0.5 text-[10px] font-medium"
            :class="statusClass(task)"
            :title="statusTitle(task)"
          >
            {{ statusLabel(task) }}
          </span>
        </div>
        <div v-if="task.state.last_err && isFailing(task)" class="w-full truncate text-[10px] text-red-400 dark:text-red-500" :title="task.state.last_err">
          {{ task.state.last_err }}
        </div>
      </button>
      <p class="px-3 pb-1.5 pt-1 text-[10px] text-gray-400 dark:text-gray-500">
        {{ t('admin.ops.tasks.hint') }}
      </p>
    </template>

    <!-- 调度设置视图 -->
    <template v-else>
      <div v-if="configLoading" class="space-y-1.5 px-3 py-2">
        <div v-for="i in 3" :key="i" class="h-10 animate-pulse rounded bg-gray-100 dark:bg-dark-700"></div>
      </div>
      <template v-else>
        <div
          v-for="row in configRows"
          :key="row.id"
          class="flex flex-col gap-1 px-3 py-1.5"
          :data-testid="`ops-config-${row.id}`"
        >
          <div class="flex w-full items-center justify-between gap-2">
            <span class="truncate text-xs font-medium text-gray-700 dark:text-gray-200">{{ row.name }}</span>
            <label class="flex shrink-0 cursor-pointer items-center gap-1">
              <input
                type="checkbox"
                class="h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
                :checked="row.enabled"
                :data-testid="`ops-config-enabled-${row.id}`"
                @change="row.enabled = ($event.target as HTMLInputElement).checked"
              />
              <span class="text-[10px] text-gray-500 dark:text-gray-400">{{ t('admin.ops.tasks.autoEnabled') }}</span>
            </label>
          </div>
          <div v-if="row.kind === 'hours'" class="flex items-center gap-1.5">
            <span class="shrink-0 text-[10px] text-gray-400 dark:text-gray-500">{{ t('admin.ops.tasks.hoursLabel') }}</span>
            <input
              type="text"
              class="input flex-1 px-2 py-1 font-mono text-[11px]"
              :placeholder="row.defaultHours.join(',')"
              :value="row.hoursText"
              :data-testid="`ops-config-hours-${row.id}`"
              @input="row.hoursText = ($event.target as HTMLInputElement).value"
            />
          </div>
          <div v-else-if="row.kind === 'interval'" class="flex items-center gap-1.5">
            <span class="shrink-0 text-[10px] text-gray-400 dark:text-gray-500">{{ t('admin.ops.tasks.intervalLabel') }}</span>
            <input
              type="number"
              min="1"
              max="168"
              class="input w-16 px-2 py-1 text-[11px]"
              :placeholder="String(row.defaultIntervalHours)"
              :value="row.intervalText"
              :data-testid="`ops-config-interval-${row.id}`"
              @input="row.intervalText = ($event.target as HTMLInputElement).value"
            />
            <span class="text-[10px] text-gray-400 dark:text-gray-500">{{ t('admin.ops.tasks.intervalUnit') }}</span>
          </div>
        </div>
        <div class="flex items-center justify-between px-3 pb-1.5 pt-1">
          <p class="text-[10px] text-gray-400 dark:text-gray-500">{{ t('admin.ops.tasks.configHint') }}</p>
          <button
            type="button"
            data-testid="ops-config-save"
            class="btn btn-primary shrink-0 px-2.5 py-1 text-[11px]"
            :disabled="configSaving"
            @click="saveConfig"
          >
            <span v-if="configSaving" class="mr-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
            {{ t('admin.ops.tasks.saveConfig') }}
          </button>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account } from '@/types'
import type { PlatformOpsAccountTask } from '@/api/admin/platformOps'
import { Icon } from '@/components/icons'

/**
 * 账号运营任务面板（WorkBuddy / Qoder / Trae）：任务视图列出该账号平台的运营
 * 任务与最近执行状态，点击手动执行；调度设置视图配置账号级任务计划（开关 +
 * 每日定点小时 / 固定间隔，覆盖注册表默认；禁用仅停自动调度，手动执行不受影响）。
 */
const props = defineProps<{ account: Account }>()

const { t } = useI18n()
const appStore = useAppStore()

const view = ref<'tasks' | 'config'>('tasks')
const tasks = ref<PlatformOpsAccountTask[]>([])
const loading = ref(false)
const error = ref('')
const runningTask = ref('')

const configLoading = ref(false)
const configSaving = ref(false)

/** 调度设置的行编辑态（kind 决定展示定点或间隔输入）。 */
interface OpsConfigRow {
  id: string
  name: string
  kind: 'hours' | 'interval' | 'manual'
  enabled: boolean
  hoursText: string
  intervalText: string
  defaultHours: number[]
  defaultIntervalHours: number
}

const configRows = ref<OpsConfigRow[]>([])

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const result = await adminAPI.platformOps.getAccountTasks(props.account.id)
    tasks.value = result.tasks ?? []
  } catch (err) {
    error.value = (err as { message?: string })?.message || t('admin.ops.tasks.loadFailed')
  } finally {
    loading.value = false
  }
}

// 打开调度设置视图：拉默认调度 + 平台级覆盖（配置对该平台全部账号统一生效）。
const openConfig = async () => {
  view.value = 'config'
  if (configRows.value.length > 0) return
  configLoading.value = true
  try {
    const result = await adminAPI.platformOps.getPlatformTaskConfig(props.account.platform)
    configRows.value = (result.tasks ?? []).map(task => {
      const defaultHours = task.hours ?? []
      const defaultIntervalHours = task.interval_seconds ? Math.round(task.interval_seconds / 3600) : 0
      // 生效值优先取账号覆盖，缺省回落注册表默认；输入框空 = 保持默认。
      const effectiveHours = task.config.hours ?? defaultHours
      const effectiveIntervalHours = task.config.interval_hours ?? defaultIntervalHours
      const kind: OpsConfigRow['kind'] =
        defaultHours.length > 0 || task.config.hours?.length ? 'hours' : defaultIntervalHours > 0 || task.config.interval_hours ? 'interval' : 'manual'
      return {
        id: task.id,
        name: task.name,
        kind,
        enabled: task.config.enabled ?? true,
        hoursText: effectiveHours.length ? effectiveHours.join(',') : '',
        intervalText: effectiveIntervalHours > 0 ? String(effectiveIntervalHours) : '',
        defaultHours,
        defaultIntervalHours
      }
    })
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.ops.tasks.loadFailed'))
  } finally {
    configLoading.value = false
  }
}

// 解析小时输入："9,21" / "9，21" / " 9 21 " → [9,21]；空串 = undefined（沿用默认）。
const parseHoursInput = (text: string): number[] | undefined => {
  const trimmed = text.trim()
  if (!trimmed) return undefined
  const parts = trimmed.split(/[,，\s]+/).filter(Boolean)
  const hours: number[] = []
  for (const part of parts) {
    const h = Number.parseInt(part, 10)
    if (Number.isNaN(h) || h < 0 || h > 23) return []
    hours.push(h)
  }
  return [...new Set(hours)].sort((a, b) => a - b)
}

const saveConfig = async () => {
  if (configSaving.value) return
  const payload: Record<string, { enabled: boolean; hours?: number[]; interval_hours?: number }> = {}
  for (const row of configRows.value) {
    const entry: { enabled: boolean; hours?: number[]; interval_hours?: number } = { enabled: row.enabled }
    if (row.kind === 'hours') {
      const parsed = parseHoursInput(row.hoursText)
      if (parsed && parsed.length === 0) {
        appStore.showError(t('admin.ops.tasks.hoursInvalid', { task: row.name }))
        return
      }
      if (parsed) entry.hours = parsed
    } else if (row.kind === 'interval') {
      const trimmed = row.intervalText.trim()
      if (trimmed) {
        const h = Number.parseInt(trimmed, 10)
        if (Number.isNaN(h) || h < 1 || h > 168) {
          appStore.showError(t('admin.ops.tasks.intervalInvalid', { task: row.name }))
          return
        }
        entry.interval_hours = h
      }
    }
    payload[row.id] = entry
  }
  configSaving.value = true
  try {
    await adminAPI.platformOps.updatePlatformTaskConfig(props.account.platform, payload)
    appStore.showSuccess(t('admin.ops.tasks.configSaved'))
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.ops.tasks.configSaveFailed'))
  } finally {
    configSaving.value = false
  }
}

// 最近一次执行是失败（有 last_err 且晚于 last_ok，或从未成功过）。
const isFailing = (task: PlatformOpsAccountTask): boolean => {
  const { last_err: lastErr, last_ok: lastOk } = task.state
  if (!lastErr) return false
  if (!lastOk) return true
  return new Date(lastErr).getTime() > new Date(lastOk).getTime()
}

const statusClass = (task: PlatformOpsAccountTask): string => {
  if (isFailing(task)) {
    return 'bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300'
  }
  if (task.state.last_ok) {
    return 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  return 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400'
}

const formatShortDate = (value: string | undefined): string => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

const statusLabel = (task: PlatformOpsAccountTask): string => {
  if (isFailing(task)) return t('admin.ops.tasks.failed')
  if (task.state.last_ok) {
    return `${t('admin.ops.tasks.ok')} ${formatShortDate(task.state.last_ok)}`
  }
  return t('admin.ops.tasks.neverRun')
}

const statusTitle = (task: PlatformOpsAccountTask): string => {
  if (isFailing(task)) {
    const time = formatShortDate(task.state.last_run)
    return time ? `${time} ${task.state.last_err ?? ''}` : (task.state.last_err ?? '')
  }
  if (task.state.last_ok) return `${t('admin.ops.tasks.ok')} ${task.state.last_ok}`
  return t('admin.ops.tasks.neverRun')
}

const run = async (task: PlatformOpsAccountTask) => {
  if (runningTask.value) return
  runningTask.value = task.id
  try {
    const result = await adminAPI.platformOps.runAccountTask(props.account.id, task.id)
    if (result.done) {
      appStore.showSuccess(t('admin.ops.tasks.runSuccess', { task: task.name }))
    } else {
      appStore.showError(t('admin.ops.tasks.runFailed', { task: task.name, error: result.error ?? '' }))
    }
  } catch (err) {
    appStore.showError((err as { message?: string })?.message || t('admin.ops.tasks.runFailed', { task: task.name, error: '' }))
  } finally {
    runningTask.value = ''
    // 任务状态（last_run/last_err）已落库，重新拉取展示。
    load()
  }
}

onMounted(load)
</script>
