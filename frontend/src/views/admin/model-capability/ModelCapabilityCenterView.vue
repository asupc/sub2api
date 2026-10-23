<template>
  <AppLayout>
    <div class="min-w-0 space-y-4">
      <!-- 头部 -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-bold text-gray-900 dark:text-white">模型能力中心</h1>
          <p class="text-xs text-gray-500 dark:text-dark-400">透视模型与账号的供给关系，定位断供缺口与模型改写规则</p>
        </div>
      </div>

      <!-- 模型清单 -->
      <ManifestTab />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
import ManifestTab from './tabs/ManifestTab.vue'
</script>
