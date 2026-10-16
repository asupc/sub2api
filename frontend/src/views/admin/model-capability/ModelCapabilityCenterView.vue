<template>
  <AppLayout>
    <div class="min-w-0 space-y-4">
      <!-- 头部 -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-bold text-gray-900 dark:text-white">模型能力中心</h1>
          <p class="text-xs text-gray-500 dark:text-dark-400">将模型清单、测评计划与模型能力测评统一在一个页面管理</p>
        </div>
      </div>

      <!-- Tab 导航 -->
      <div class="flex items-center gap-1 border-b border-gray-200 dark:border-dark-700" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          class="flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-semibold transition"
          :class="
            activeTab === tab.key
              ? 'border-primary-600 text-primary-600 dark:border-primary-400 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
          "
          role="tab"
          :aria-selected="activeTab === tab.key"
          @click="setActiveTab(tab.key)"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- Tab 内容：全部挂载，用 v-show 保留状态 -->
      <div v-show="activeTab === 'overview'">
        <OverviewTab />
      </div>
      <div v-show="activeTab === 'manifest'">
        <ManifestTab />
      </div>
      <div v-show="activeTab === 'plans'">
        <PlansTab />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AppLayout from '@/components/layout/AppLayout.vue'
import OverviewTab from './tabs/OverviewTab.vue'
import ManifestTab from './tabs/ManifestTab.vue'
import PlansTab from './tabs/PlansTab.vue'

const route = useRoute()
const router = useRouter()

const tabs = [
  { key: 'overview', label: '模型能力测评' },
  { key: 'manifest', label: '模型清单' },
  { key: 'plans', label: '测评计划' },
]

function normalizeTab(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value
  const candidate = typeof raw === 'string' ? raw : ''
  return tabs.some(tab => tab.key === candidate) ? candidate : 'overview'
}

const activeTab = ref(normalizeTab(route.query.tab))

function setActiveTab(key: string) {
  activeTab.value = key
  const query = { ...route.query, tab: key }
  void router.replace({ query })
}

watch(() => route.query.tab, (value) => {
  activeTab.value = normalizeTab(value)
})

onMounted(() => {
  activeTab.value = normalizeTab(route.query.tab)
})
</script>
