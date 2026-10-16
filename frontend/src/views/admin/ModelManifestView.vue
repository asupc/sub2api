<template>
  <AppLayout>
    <div class="space-y-4">
    <!-- ==================== 顶部指标卡片 ==================== -->
    <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="mb-1 flex items-center justify-between text-gray-500 dark:text-dark-400">
          <span class="text-xs font-medium">{{ t('admin.modelManifest.stats.totalModels') }}</span>
          <span class="flex h-6 w-6 items-center justify-center rounded-md bg-blue-500/10 text-blue-500">
            <Icon name="grid" size="xs" />
          </span>
        </div>
        <div class="text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ summary?.total_models ?? '—' }}</div>
        <div class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">
          {{ t('admin.modelManifest.stats.acrossPlatforms', { count: platformOptions.length }) }}
        </div>
      </div>

      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="mb-1 flex items-center justify-between text-gray-500 dark:text-dark-400">
          <span class="text-xs font-medium">{{ t('admin.modelManifest.stats.healthyModels') }}</span>
          <span class="flex h-6 w-6 items-center justify-center rounded-md bg-emerald-500/10 text-emerald-500">
            <Icon name="checkCircle" size="xs" />
          </span>
        </div>
        <div class="text-2xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">{{ summary?.healthy_models ?? '—' }}</div>
        <div class="mt-1 text-[11px] text-emerald-600 dark:text-emerald-400">
          {{ t('admin.modelManifest.stats.healthyHint') }}
        </div>
      </div>

      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="mb-1 flex items-center justify-between text-gray-500 dark:text-dark-400">
          <span class="text-xs font-medium">{{ t('admin.modelManifest.stats.dangerModels') }}</span>
          <span class="flex h-6 w-6 items-center justify-center rounded-md bg-rose-500/10 text-rose-500">
            <Icon name="exclamationTriangle" size="xs" />
          </span>
        </div>
        <div class="text-2xl font-bold tabular-nums" :class="dangerCount > 0 ? 'text-rose-600 dark:text-rose-400' : 'text-gray-900 dark:text-white'">
          {{ summary?.danger_models ?? '—' }}
        </div>
        <div class="mt-1 text-[11px] text-rose-600 dark:text-rose-400">
          {{ t('admin.modelManifest.stats.dangerHint') }}
        </div>
      </div>

      <div class="rounded-xl border border-gray-200 bg-white p-4 shadow-xs dark:border-dark-700 dark:bg-dark-900">
        <div class="mb-1 flex items-center justify-between text-gray-500 dark:text-dark-400">
          <span class="text-xs font-medium">{{ t('admin.modelManifest.stats.mountedAccounts') }}</span>
          <span class="flex h-6 w-6 items-center justify-center rounded-md bg-purple-500/10 text-purple-500">
            <Icon name="users" size="xs" />
          </span>
        </div>
        <div class="text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ summary?.total_accounts ?? '—' }}</div>
        <div class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">
          {{ t('admin.modelManifest.stats.totalConcurrency', { count: totalSchedulableConcurrency }) }}
        </div>
      </div>
    </div>

    <!-- ==================== 控制栏：视角切换 + 搜索 + 状态 + 平台胶囊 ==================== -->
    <div class="space-y-3 rounded-xl border border-gray-200 bg-white p-3 shadow-xs dark:border-dark-700 dark:bg-dark-900 sm:p-4">
      <div class="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
        <!-- 视角切换器 -->
        <div class="inline-flex self-start rounded-xl border border-gray-200 bg-gray-100 p-1 dark:border-dark-700 dark:bg-dark-800">
          <button
            type="button"
            :class="[
              'flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-xs font-semibold transition',
              viewMode === 'model'
                ? 'bg-white text-blue-600 shadow-xs dark:bg-dark-700 dark:text-blue-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-dark-400 dark:hover:text-white'
            ]"
            @click="viewMode = 'model'"
          >
            <Icon name="cube" size="xs" />
            {{ t('admin.modelManifest.tabs.byModel') }}
          </button>
          <button
            type="button"
            :class="[
              'flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-xs font-semibold transition',
              viewMode === 'account'
                ? 'bg-white text-blue-600 shadow-xs dark:bg-dark-700 dark:text-blue-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-dark-400 dark:hover:text-white'
            ]"
            @click="viewMode = 'account'"
          >
            <Icon name="user" size="xs" />
            {{ t('admin.modelManifest.tabs.byAccount') }}
          </button>
        </div>

        <!-- 搜索 + 状态筛选 + 刷新 -->
        <div class="flex flex-wrap items-center gap-2.5">
          <SearchInput
            v-model="search"
            size="sm"
            class="w-full sm:w-64"
            :placeholder="t('admin.modelManifest.searchPlaceholder')"
          />
          <Select
            v-model="statusFilter"
            size="sm"
            class="w-36"
            :options="statusOptions"
          />
          <!-- 模型视角专用：按最近测评分数降序（未测评的排最后） -->
          <Select
            v-if="viewMode === 'model'"
            v-model="sortMode"
            size="sm"
            class="w-36"
            :options="sortOptions"
          />
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            @click="load"
          >
            <Icon name="refresh" size="xs" :class="loading && 'animate-spin'" />
            {{ t('admin.modelManifest.refresh') }}
          </button>
        </div>
      </div>

      <!-- 平台横向胶囊 -->
      <div class="flex items-center gap-1.5 overflow-x-auto border-t border-gray-100 pt-1 dark:border-dark-800 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <span class="mr-1 shrink-0 text-[11px] font-medium text-gray-400">{{ t('admin.modelManifest.platformLabel') }}</span>
        <button
          type="button"
          :class="[
            'shrink-0 rounded-lg px-2.5 py-1 text-xs font-medium transition',
            platformFilter === 'all'
              ? 'border border-blue-200 bg-blue-50 text-blue-600 dark:border-blue-800 dark:bg-blue-950/60 dark:text-blue-400'
              : 'text-gray-600 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800'
          ]"
          @click="platformFilter = 'all'"
        >
          {{ t('admin.modelManifest.platformAll') }} ({{ allModels.length }})
        </button>
        <button
          v-for="option in platformOptions"
          :key="option.value"
          type="button"
          :class="[
            'shrink-0 rounded-lg px-2.5 py-1 text-xs font-medium transition',
            platformFilter === option.value
              ? 'border border-blue-200 bg-blue-50 text-blue-600 dark:border-blue-800 dark:bg-blue-950/60 dark:text-blue-400'
              : 'text-gray-600 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800'
          ]"
          @click="platformFilter = option.value"
        >
          {{ option.label }} ({{ option.count }})
        </button>
      </div>
    </div>

    <!-- ==================== 加载 / 错误 / 空态 ==================== -->
    <div v-if="loading && !manifest" class="flex items-center justify-center rounded-xl border border-gray-200 bg-white py-16 dark:border-dark-700 dark:bg-dark-900">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="loadError" class="rounded-xl border border-rose-200 bg-rose-50 p-6 text-center dark:border-rose-900/60 dark:bg-rose-950/20">
      <p class="text-sm text-rose-600 dark:text-rose-400">{{ loadError }}</p>
      <button type="button" class="btn btn-secondary btn-sm mt-3" @click="load">{{ t('admin.modelManifest.refresh') }}</button>
    </div>
    <div v-else-if="filteredModels.length === 0" class="rounded-xl border border-dashed border-gray-300 p-12 text-center dark:border-dark-700">
      <Icon name="search" size="lg" class="mx-auto text-gray-400" />
      <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.modelManifest.empty') }}</p>
    </div>

    <!-- ==================== 视图 A：模型视角（折叠卡片） ==================== -->
    <div v-else-if="viewMode === 'model'" class="space-y-3">
      <div
        v-for="model in filteredModels"
        :key="model.id"
        :class="[
          'overflow-hidden rounded-xl border shadow-xs transition-all',
          model.has_warning
            ? 'border-rose-300 bg-rose-50/20 dark:border-rose-900/60 dark:bg-rose-950/10'
            : 'border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900'
        ]"
      >
        <!-- 主信息栏 -->
        <button
          type="button"
          class="flex w-full flex-wrap items-center justify-between gap-3 p-3.5 text-left transition hover:bg-gray-50/70 dark:hover:bg-dark-800/40 sm:p-4"
          @click="toggleExpand(model.id)"
        >
          <div class="flex min-w-0 items-center gap-3">
            <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <Icon
                name="chevronRight"
                size="xs"
                :class="['transition-transform duration-200', expanded.has(model.id) && 'rotate-90']"
              />
            </span>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm font-bold text-gray-900 dark:text-white">{{ model.id }}</span>
                <span v-if="model.display_name" class="hidden text-xs text-gray-500 dark:text-dark-400 sm:inline">({{ model.display_name }})</span>
                <span
                  class="cursor-pointer rounded p-1 text-gray-400 transition hover:bg-gray-100 hover:text-blue-500 dark:hover:bg-dark-800"
                  :title="t('admin.modelManifest.copyId')"
                  :aria-label="t('admin.modelManifest.copyId')"
                  @click.stop="copyModelId(model.id)"
                >
                  <Icon name="copy" size="xs" />
                </span>
              </div>
              <div class="mt-1 flex flex-wrap items-center gap-1.5">
                <span
                  v-for="platform in model.platforms"
                  :key="platform"
                  :class="['inline-flex items-center rounded border px-2 py-0.5 text-[10px] font-medium', platformBadgeClass(platform)]"
                >
                  {{ platformLabel(platform) }}
                </span>
              </div>
            </div>
          </div>

          <!-- 右侧统计胶囊与状态 -->
          <div class="ml-auto flex items-center gap-3 sm:gap-6">
            <div class="text-right">
              <div class="text-[11px] text-gray-400">{{ t('admin.modelManifest.benchmark.label') }}</div>
              <div
                v-if="model.benchmark_score != null"
                class="text-xs font-semibold tabular-nums text-indigo-600 dark:text-indigo-400"
                :title="benchmarkTooltip(model)"
              >
                {{ model.benchmark_score.toFixed(1) }}
              </div>
              <div v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('admin.modelManifest.benchmark.untested') }}</div>
            </div>
            <div class="text-right">
              <div class="text-[11px] text-gray-400">{{ t('admin.modelManifest.availableAccounts') }}</div>
              <div
                :class="[
                  'text-xs font-semibold tabular-nums',
                  model.has_warning ? 'font-bold text-rose-600 dark:text-rose-400' : 'text-gray-800 dark:text-dark-200'
                ]"
              >
                {{ model.active_accounts }} / {{ model.total_accounts }}
              </div>
            </div>
            <div class="hidden text-right sm:block">
              <div class="text-[11px] text-gray-400">{{ t('admin.modelManifest.concurrencyPool') }}</div>
              <div class="text-xs font-semibold tabular-nums text-gray-800 dark:text-dark-200">{{ model.total_concurrency }}</div>
            </div>
            <span
              :class="[
                'inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-medium',
                model.has_warning
                  ? 'border-rose-500/20 bg-rose-500/10 text-rose-600 dark:text-rose-400'
                  : 'border-emerald-500/20 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
              ]"
            >
              <span :class="['h-1.5 w-1.5 rounded-full', model.has_warning ? 'animate-pulse bg-rose-500' : 'bg-emerald-500']" />
              {{ model.has_warning ? t('admin.modelManifest.dangerTag') : t('admin.modelManifest.healthyTag') }}
            </span>
          </div>
        </button>

        <!-- 展开后的挂载账号明细 -->
        <div v-if="expanded.has(model.id)" class="border-t border-gray-100 bg-gray-50/50 p-4 dark:border-dark-800 dark:bg-dark-950/40">
          <div
            v-if="model.has_warning"
            class="mb-3 flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-2.5 text-xs text-rose-600 dark:text-rose-400"
          >
            <Icon name="exclamationTriangle" size="xs" />
            <span>{{ t('admin.modelManifest.dangerAlert') }}</span>
          </div>

          <div class="overflow-x-auto">
            <table class="w-full text-left text-xs">
              <thead>
                <tr class="border-b border-gray-200/60 font-medium text-gray-400 dark:border-dark-700">
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.account') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.type') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.status') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.concurrency') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.priority') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.groups') }}</th>
                  <th class="px-3 py-2">{{ t('admin.modelManifest.columns.target') }}</th>
                  <th class="px-3 py-2 text-right">{{ t('admin.modelManifest.columns.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr
                  v-for="account in model.accounts"
                  :key="account.id"
                  class="transition hover:bg-gray-100/50 dark:hover:bg-dark-800/40"
                >
                  <td class="px-3 py-2.5">
                    <div class="flex items-center gap-1.5 font-medium text-gray-900 dark:text-white">
                      <span class="font-mono text-[11px] text-gray-400">#{{ account.id }}</span>
                      <span>{{ account.name }}</span>
                    </div>
                    <div class="mt-1">
                      <span :class="['inline-flex items-center rounded border px-2 py-0.5 text-[10px] font-medium', platformBadgeClass(account.platform)]">
                        {{ platformLabel(account.platform) }}
                      </span>
                    </div>
                  </td>
                  <td class="px-3 py-2.5">
                    <span class="rounded bg-gray-200/60 px-1.5 py-0.5 font-mono text-[10px] uppercase text-gray-700 dark:bg-dark-700 dark:text-dark-300">
                      {{ account.type }}
                    </span>
                  </td>
                  <td class="px-3 py-2.5">
                    <span :class="['inline-flex items-center gap-1 text-[11px] font-medium', accountStateClass(account)]">
                      <span :class="['h-1.5 w-1.5 rounded-full', accountStateDotClass(account)]" />
                      {{ accountStateLabel(account) }}
                    </span>
                  </td>
                  <td class="px-3 py-2.5 font-semibold tabular-nums">{{ account.concurrency }}</td>
                  <td class="px-3 py-2.5 tabular-nums">{{ account.priority }}</td>
                  <td class="px-3 py-2.5">
                    <div class="flex flex-wrap gap-1">
                      <span
                        v-for="group in account.groups"
                        :key="group"
                        class="rounded bg-gray-200/70 px-1.5 py-0.5 text-[10px] text-gray-600 dark:bg-dark-700 dark:text-dark-300"
                      >
                        {{ group }}
                      </span>
                      <span v-if="account.groups.length === 0" class="text-[10px] text-gray-400">—</span>
                    </div>
                  </td>
                  <td class="px-3 py-2.5 font-mono text-[11px] text-gray-500 dark:text-dark-400">
                    {{ account.target || t('admin.modelManifest.directPassThrough') }}
                  </td>
                  <td class="px-3 py-2.5 text-right">
                    <RouterLink
                      :to="`/admin/accounts/p/${account.platform}`"
                      class="font-medium text-blue-600 hover:underline dark:text-blue-400"
                    >
                      {{ t('admin.modelManifest.configure') }}
                    </RouterLink>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <!-- ==================== 视图 B：账号视角（紧凑表格） ==================== -->
    <div v-else class="overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xs dark:border-dark-700 dark:bg-dark-900">
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead>
            <tr class="border-b border-gray-200 bg-gray-50/50 font-medium text-gray-400 dark:border-dark-700 dark:bg-dark-800/40">
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.account') }}</th>
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.type') }}</th>
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.status') }}</th>
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.concurrency') }}</th>
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.modelCount') }}</th>
              <th class="px-4 py-3">{{ t('admin.modelManifest.columns.models') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.modelManifest.columns.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr
              v-for="row in accountRows"
              :key="row.id"
              class="transition hover:bg-gray-50/60 dark:hover:bg-dark-800/40"
            >
              <td class="px-4 py-3">
                <div class="flex items-center gap-2">
                  <span class="font-mono text-gray-400">#{{ row.id }}</span>
                  <strong class="text-gray-900 dark:text-white">{{ row.name }}</strong>
                </div>
                <div class="mt-1">
                  <span :class="['inline-flex items-center rounded border px-2 py-0.5 text-[10px] font-medium', platformBadgeClass(row.platform)]">
                    {{ platformLabel(row.platform) }}
                  </span>
                </div>
              </td>
              <td class="px-4 py-3">
                <span class="rounded border border-gray-200 bg-gray-100 px-2 py-0.5 font-mono text-[10px] uppercase text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300">
                  {{ row.type }}
                </span>
              </td>
              <td class="px-4 py-3">
                <span :class="['inline-flex items-center gap-1 text-[11px] font-medium', accountStateClass(row)]">
                  <span :class="['h-1.5 w-1.5 rounded-full', accountStateDotClass(row)]" />
                  {{ accountStateLabel(row) }}
                </span>
              </td>
              <td class="px-4 py-3 font-semibold tabular-nums">{{ row.concurrency }}</td>
              <td class="px-4 py-3 font-bold tabular-nums text-gray-800 dark:text-dark-200">
                {{ t('admin.modelManifest.modelCountValue', { count: row.models.length }) }}
              </td>
              <td class="px-4 py-3">
                <div class="flex max-w-xl flex-wrap gap-1.5">
                  <span
                    v-for="model in row.models"
                    :key="model"
                    class="inline-flex cursor-pointer items-center gap-1 rounded border border-gray-200 bg-gray-100 px-2 py-0.5 font-mono text-[11px] text-gray-700 transition hover:border-blue-300 hover:text-blue-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300 dark:hover:text-blue-400"
                    :title="t('admin.modelManifest.copyId')"
                    @click="copyModelId(model)"
                  >
                    {{ model }}
                  </span>
                </div>
              </td>
              <td class="px-4 py-3 text-right">
                <RouterLink
                  :to="`/admin/accounts/p/${row.platform}`"
                  class="font-medium text-blue-600 hover:underline dark:text-blue-400"
                >
                  {{ t('admin.modelManifest.configure') }}
                </RouterLink>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { adminAPI } from '@/api/admin'
import type {
  ModelManifestAccountItem,
  ModelManifestItem,
  ModelManifestResponse
} from '@/api/admin'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { useAppStore } from '@/stores'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

const { t } = useI18n()
const appStore = useAppStore()

// ==================== 数据加载 ====================
const manifest = ref<ModelManifestResponse | null>(null)
const loading = ref(false)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    manifest.value = await adminAPI.models.getModelManifest()
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : t('admin.modelManifest.loadFailed')
  } finally {
    loading.value = false
  }
}

onMounted(load)

// ==================== 交互状态 ====================
const viewMode = ref<'model' | 'account'>('model')
const platformFilter = ref('all')
// Select 组件回写宽类型（string），取值约束在 all/healthy/danger
const statusFilter = ref('all')
const search = ref('')
const expanded = ref(new Set<string>())

function toggleExpand(modelId: string) {
  const next = new Set(expanded.value)
  if (next.has(modelId)) {
    next.delete(modelId)
  } else {
    next.add(modelId)
  }
  expanded.value = next
}

const sortMode = ref<'default' | 'score'>('score')
const sortOptions = computed(() => [
  { value: 'default', label: t('admin.modelManifest.benchmark.sortDefault') },
  { value: 'score', label: t('admin.modelManifest.benchmark.sortScore') }
])

const statusOptions = computed(() => [
  { value: 'all', label: t('admin.modelManifest.statusAll') },
  { value: 'healthy', label: t('admin.modelManifest.statusHealthy') },
  { value: 'danger', label: t('admin.modelManifest.statusDanger') }
])

// ==================== 派生数据 ====================
const summary = computed(() => manifest.value?.summary ?? null)
const allModels = computed<ModelManifestItem[]>(() => manifest.value?.models ?? [])

// 客户端过滤：与后端同口径（模型 ID / 显示名 / 挂载账号名模糊匹配）
const filteredModels = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const filtered = allModels.value.filter(model => {
    if (platformFilter.value !== 'all' && !model.platforms.includes(platformFilter.value)) return false
    if (statusFilter.value === 'healthy' && model.has_warning) return false
    if (statusFilter.value === 'danger' && !model.has_warning) return false
    if (keyword) {
      const matchModel =
        model.id.toLowerCase().includes(keyword) || model.display_name.toLowerCase().includes(keyword)
      const matchAccount = model.accounts.some(account => account.name.toLowerCase().includes(keyword))
      if (!matchModel && !matchAccount) return false
    }
    return true
  })
  if (sortMode.value === 'score') {
    // 最近测评分数降序；未测评的排最后（同分保持既有顺序）。
    return [...filtered].sort((a, b) => {
      const left = a.benchmark_score
      const right = b.benchmark_score
      if (left == null && right == null) return 0
      if (left == null) return 1
      if (right == null) return -1
      return right - left
    })
  }
  return filtered
})

function benchmarkTooltip(model: ModelManifestItem): string {
  const date = model.benchmark_scored_at ? new Date(model.benchmark_scored_at).toLocaleString() : '—'
  if (model.benchmark_suite_version) {
    return t('admin.modelManifest.benchmark.tooltip', { date, version: model.benchmark_suite_version })
  }
  return t('admin.modelManifest.benchmark.tooltipNoVersion', { date })
}

// 平台胶囊：从数据中动态统计（含各平台模型数），保证与真实账号对齐
const platformOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const model of allModels.value) {
    for (const platform of model.platforms) {
      counts.set(platform, (counts.get(platform) ?? 0) + 1)
    }
  }
  const labels = new Map(CONCRETE_PLATFORM_OPTIONS.map(option => [option.value, option.label]))
  return Array.from(counts.entries())
    .map(([value, count]) => ({ value, count, label: labels.get(value) ?? value }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
})

// 视角 B：从模型数据倒推账号 → 模型列表（同一账号跨模型去重）
const accountRows = computed(() => {
  const byAccount = new Map<number, ModelManifestAccountItem & { models: string[] }>()
  for (const model of filteredModels.value) {
    for (const account of model.accounts) {
      const existing = byAccount.get(account.id)
      if (existing) {
        existing.models.push(model.id)
      } else {
        byAccount.set(account.id, { ...account, models: [model.id] })
      }
    }
  }
  return Array.from(byAccount.values()).sort((a, b) => {
    if (a.platform !== b.platform) return a.platform.localeCompare(b.platform)
    return a.id - b.id
  })
})

// 指标卡 4：可调度账号的设计并发合计（按账号去重）
const totalSchedulableConcurrency = computed(() => {
  if (!manifest.value) return 0
  const seen = new Map<number, ModelManifestAccountItem>()
  for (const model of manifest.value.models) {
    for (const account of model.accounts) {
      if (!seen.has(account.id)) seen.set(account.id, account)
    }
  }
  let total = 0
  for (const account of seen.values()) {
    if (account.schedulable && account.concurrency > 0) total += account.concurrency
  }
  return total
})

const dangerCount = computed(() => summary.value?.danger_models ?? 0)

// ==================== 展示辅助 ====================
const PLATFORM_BADGE_CLASSES: Record<string, string> = {
  anthropic: 'bg-amber-500/10 text-amber-600 border-amber-500/20 dark:text-amber-400',
  openai: 'bg-emerald-500/10 text-emerald-600 border-emerald-500/20 dark:text-emerald-400',
  gemini: 'bg-blue-500/10 text-blue-600 border-blue-500/20 dark:text-blue-400',
  antigravity: 'bg-purple-500/10 text-purple-600 border-purple-500/20 dark:text-purple-400',
  grok: 'bg-slate-500/10 text-slate-700 border-slate-500/20 dark:text-dark-300'
}

function platformBadgeClass(platform: string): string {
  return PLATFORM_BADGE_CLASSES[platform] ?? 'bg-rose-500/10 text-rose-600 border-rose-500/20 dark:text-rose-400'
}

function platformLabel(platform: string): string {
  const option = CONCRETE_PLATFORM_OPTIONS.find(entry => entry.value === platform)
  return option?.label ?? platform
}

type AccountState = ModelManifestAccountItem | (ModelManifestAccountItem & { models: string[] })

function accountStateLabel(account: AccountState): string {
  if (account.rate_limited) return t('admin.modelManifest.accountStates.rateLimited')
  switch (account.status) {
    case 'active':
      return t('admin.modelManifest.accountStates.active')
    case 'error':
      return t('admin.modelManifest.accountStates.error')
    default:
      return t('admin.modelManifest.accountStates.disabled')
  }
}

function accountStateClass(account: AccountState): string {
  if (account.rate_limited) return 'text-amber-600 dark:text-amber-400'
  switch (account.status) {
    case 'active':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'error':
      return 'text-rose-600 dark:text-rose-400'
    default:
      return 'text-gray-400'
  }
}

function accountStateDotClass(account: AccountState): string {
  if (account.rate_limited) return 'bg-amber-500'
  switch (account.status) {
    case 'active':
      return 'bg-emerald-500'
    case 'error':
      return 'bg-rose-500'
    default:
      return 'bg-gray-400'
  }
}

async function copyModelId(modelId: string) {
  try {
    await navigator.clipboard.writeText(modelId)
    appStore.showSuccess(t('admin.modelManifest.copySuccess', { id: modelId }))
  } catch {
    appStore.showError(t('admin.modelManifest.copyFailed'))
  }
}
</script>
