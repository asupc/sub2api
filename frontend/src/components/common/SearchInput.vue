<template>
  <div class="relative w-full">
    <div
      :class="[
        'pointer-events-none absolute inset-y-0 left-0 flex items-center',
        size === 'sm' ? 'pl-2.5' : 'pl-3'
      ]"
    >
      <Icon name="search" :size="size === 'sm' ? 'sm' : 'md'" class="text-gray-400" />
    </div>
    <input
      v-model="searchValue"
      type="text"
      :class="[
        'input',
        size === 'sm' ? 'input-sm pl-5 h-8' : 'pl-6'
      ]"
      :placeholder="placeholder"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  debounceMs?: number
  size?: 'sm' | 'md'
}>(), {
  placeholder: 'Search...',
  debounceMs: 300,
  size: 'md'
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search', value: string): void
}>()

const debouncedEmitSearch = useDebounceFn((value: string) => {
  emit('search', value)
}, props.debounceMs)

const searchValue = computed({
  get: () => props.modelValue,
  set: (value: string) => {
    emit('update:modelValue', value)
    debouncedEmitSearch(value)
  }
})
</script>
