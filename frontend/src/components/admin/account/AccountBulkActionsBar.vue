<template>
  <div
    v-show="selectedIds.length > 0"
    class="mb-2.5 flex flex-wrap items-center justify-between gap-2 rounded-lg bg-primary-50 px-3 py-2 dark:bg-primary-900/20"
  >
    <div class="flex flex-wrap items-center gap-2">
      <span v-if="allResultsSelected" class="text-xs font-semibold text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkActions.selectedAll', { count: selectedIds.length }) }}
      </span>
      <span v-else class="text-xs font-semibold text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkActions.selected', { count: selectedIds.length }) }}
      </span>
      <button
        @click="$emit('select-page')"
        class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
      >
        {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
      </button>
      <template v-if="!allResultsSelected && totalResults > selectedIds.length">
        <span class="text-gray-300 dark:text-primary-800">•</span>
        <button
          :disabled="selectingAll"
          @click="$emit('select-all-results')"
          class="text-xs font-medium text-primary-700 hover:text-primary-800 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-300 dark:hover:text-primary-200"
        >
          {{
            selectingAll
              ? t('admin.accounts.bulkActions.selectingAll')
              : t('admin.accounts.bulkActions.selectAllResults', { count: totalResults })
          }}
        </button>
      </template>
      <span class="text-gray-300 dark:text-primary-800">•</span>
      <button
        @click="$emit('clear')"
        class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
      >
        {{ t('admin.accounts.bulkActions.clear') }}
      </button>
    </div>
    <div class="flex flex-wrap items-center gap-1.5">
      <button @click="$emit('delete')" class="btn btn-danger btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.delete') }}</button>
      <button @click="$emit('reset-status')" class="btn btn-secondary btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.resetStatus') }}</button>
      <button @click="$emit('refresh-token')" class="btn btn-secondary btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.refreshToken') }}</button>
      <button @click="$emit('probe-upstream-billing')" class="btn btn-secondary btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.probeUpstreamBilling') }}</button>
      <button @click="$emit('toggle-schedulable', true)" class="btn btn-success btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.enableScheduling') }}</button>
      <button @click="$emit('toggle-schedulable', false)" class="btn btn-warning btn-xs sm:btn-sm">{{ t('admin.accounts.bulkActions.disableScheduling') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  selectedIds: number[]
  totalResults: number
  selectingAll: boolean
  allResultsSelected: boolean
}>()

defineEmits([
  'delete',
  'clear',
  'select-page',
  'select-all-results',
  'toggle-schedulable',
  'reset-status',
  'refresh-token',
  'probe-upstream-billing'
])

const { t } = useI18n()
</script>
