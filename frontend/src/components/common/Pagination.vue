<template>
  <div
    class="pagination-bar flex items-center justify-between border-t border-slate-100 bg-slate-50/50 dark:border-dark-700/60 dark:bg-dark-900/40"
    :class="dense ? 'px-3 py-1' : 'px-4 py-1.5'"
  >
    <!-- Mobile pagination: 极简流体微胶囊 -->
    <div class="flex flex-1 items-center justify-between sm:hidden text-xs">
      <button
        @click="goToPage(page - 1)"
        :disabled="page === 1"
        :class="[
          'relative inline-flex items-center gap-1 rounded font-medium transition-all active:scale-95 disabled:cursor-not-allowed disabled:opacity-30',
          'text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-white',
          dense ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-xs'
        ]"
      >
        <Icon name="chevronLeft" size="sm" />
        <span>{{ t('pagination.previous') }}</span>
      </button>

      <span class="text-[11px] font-medium text-slate-500 dark:text-slate-400 tabular-nums">
        {{ t('pagination.pageOf', { page, total: totalPages }) }}
      </span>

      <button
        @click="goToPage(page + 1)"
        :disabled="page === totalPages"
        :class="[
          'relative inline-flex items-center gap-1 rounded font-medium transition-all active:scale-95 disabled:cursor-not-allowed disabled:opacity-30',
          'text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-white',
          dense ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-xs'
        ]"
      >
        <span>{{ t('pagination.next') }}</span>
        <Icon name="chevronRight" size="sm" />
      </button>
    </div>

    <!-- Desktop pagination: 极简微型内嵌底栏 (26-28px 高度，零割裂) -->
    <div class="hidden sm:flex sm:flex-1 sm:items-center sm:justify-between text-xs">
      <!-- Left Info: 单行极简说明，不占地 -->
      <div class="flex items-center space-x-2 text-[11px]">
        <div class="flex items-center gap-1.5 text-slate-500 dark:text-slate-400">
          <span class="inline-flex items-center gap-1 font-semibold text-slate-700 dark:text-slate-300 tabular-nums">
            <span class="h-1.5 w-1.5 rounded-full bg-teal-500"></span>
            {{ t('pagination.of') }} {{ total }} {{ t('pagination.results') }}
          </span>
          <span class="text-slate-300 dark:text-dark-600">·</span>
          <span class="tabular-nums text-slate-400 dark:text-slate-500">
            {{ t('pagination.showing') }} {{ fromItem }}-{{ toItem }}
          </span>
        </div>

        <!-- Page size selector: 极简无边框轻量下拉 -->
        <div v-if="showPageSizeSelector" class="flex items-center space-x-1 pl-1.5 border-l border-slate-200/80 dark:border-dark-700/60">
          <span class="text-slate-400 dark:text-slate-500">{{ t('pagination.perPage') }}</span>
          <div class="page-size-select" :class="dense ? 'w-16 is-dense' : 'w-18'">
            <Select
              :model-value="pageSize"
              :options="pageSizeSelectOptions"
              @update:model-value="handlePageSizeChange"
            />
          </div>
        </div>

        <!-- Inline Jump: 极客闪速跳页 -->
        <div v-if="showJump" class="flex items-center space-x-1 pl-1.5 border-l border-slate-200/80 dark:border-dark-700/60">
          <span class="text-slate-400 dark:text-slate-500">{{ t('pagination.jumpTo') }}</span>
          <input
            v-model="jumpPage"
            type="number"
            min="1"
            :max="totalPages"
            :class="[
              'input font-mono text-center transition-colors focus:ring-1 focus:ring-teal-500',
              dense ? 'w-12 py-0 px-1 text-[11px] h-5 min-h-0' : 'w-14 py-0.5 px-1.5 text-xs h-6 min-h-0'
            ]"
            :placeholder="t('pagination.jumpPlaceholder')"
            @keyup.enter="submitJump"
          />
          <button
            type="button"
            :class="[
              'btn btn-ghost transition-all active:scale-95',
              dense ? 'btn-xs h-5 min-h-0 px-1 text-[11px]' : 'btn-xs h-6 min-h-0 px-1.5 text-xs'
            ]"
            @click="submitJump"
          >
            {{ t('pagination.jumpAction') }}
          </button>
        </div>
      </div>

      <!-- Right Navigation: 微型浮动按键组 (22-24px 高度) -->
      <nav
        class="relative z-0 inline-flex items-center gap-0.5"
        aria-label="Pagination"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="page === 1"
          :class="[
            'relative inline-flex items-center justify-center rounded transition-all active:scale-95',
            'text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 dark:text-slate-400 dark:hover:text-white dark:hover:bg-dark-700',
            'disabled:cursor-not-allowed disabled:opacity-25 disabled:hover:bg-transparent dark:disabled:hover:bg-transparent disabled:hover:text-slate-500 dark:disabled:hover:text-slate-400 disabled:active:scale-100',
            dense ? 'h-6 w-6' : 'h-7 w-7'
          ]"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="sm" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number'"
          :class="[
            'relative inline-flex items-center justify-center font-medium transition-all select-none rounded',
            dense ? 'h-6 min-w-[24px] px-1.5 text-[11px]' : 'h-7 min-w-[28px] px-2 text-xs',
            pageNum === page
              ? 'z-10 bg-teal-500 text-white font-bold shadow-xs shadow-teal-500/20'
              : typeof pageNum === 'number'
                ? 'text-slate-600 hover:bg-slate-200/60 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-dark-700 dark:hover:text-white active:scale-95'
                : 'text-slate-400 dark:text-slate-500 cursor-default tracking-widest px-0.5'
          ]"
          :aria-label="
            typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
          "
          :aria-current="pageNum === page ? 'page' : undefined"
        >
          {{ pageNum }}
        </button>

        <!-- Next button -->
        <button
          @click="goToPage(page + 1)"
          :disabled="page === totalPages"
          :class="[
            'relative inline-flex items-center justify-center rounded transition-all active:scale-95',
            'text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 dark:text-slate-400 dark:hover:text-white dark:hover:bg-dark-700',
            'disabled:cursor-not-allowed disabled:opacity-25 disabled:hover:bg-transparent dark:disabled:hover:bg-transparent disabled:hover:text-slate-500 dark:disabled:hover:text-slate-400 disabled:active:scale-100',
            dense ? 'h-6 w-6' : 'h-7 w-7'
          ]"
          :aria-label="t('pagination.next')"
        >
          <Icon name="chevronRight" size="sm" />
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
  dense?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false,
  dense: false
})

const emit = defineEmits<Emits>()

const totalPages = computed(() => Math.ceil(props.total / props.pageSize))

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    // Show all pages if total is small
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    // Always show first page
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    // Add ellipsis before if needed
    if (start > 2) {
      pages.push('...')
    }

    // Add middle pages
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    // Add ellipsis after if needed
    if (end < total - 1) {
      pages.push('...')
    }

    // Always show last page
    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = String(jumpPage.value ?? '').trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.page-size-select :deep(.select-trigger) {
  @apply px-2.5 py-1 text-xs rounded-lg border-slate-200 dark:border-dark-700 bg-slate-50/80 hover:bg-slate-100 dark:bg-dark-700/60 dark:hover:bg-dark-700 font-semibold text-slate-800 dark:text-slate-200 transition-colors shadow-none;
}
.page-size-select.is-dense :deep(.select-trigger) {
  @apply px-2 py-0.5 text-xs h-6 min-h-0 rounded-md border-slate-200/80 dark:border-dark-700/80 font-medium;
}
</style>
