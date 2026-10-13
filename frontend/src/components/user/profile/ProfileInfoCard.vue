<template>
  <div class="space-y-4">
    <section
      data-testid="profile-overview-hero"
      class="card overflow-hidden border border-primary-100/80 bg-gradient-to-br from-primary-50 via-white to-amber-50/70 dark:border-primary-900/40 dark:from-primary-950/40 dark:via-dark-900 dark:to-dark-950"
    >
      <div class="px-4 py-4 md:px-5">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-start">
          <div
            class="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-[1.75rem] bg-gradient-to-br from-primary-500 to-primary-600 text-2xl font-bold text-white shadow-lg shadow-primary-500/20"
          >
            <img
              v-if="avatarUrl"
              :src="avatarUrl"
              :alt="displayName"
              class="h-full w-full object-cover"
            >
            <span v-else>{{ avatarInitial }}</span>
          </div>

          <div class="min-w-0 flex-1 space-y-4">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="truncate text-2xl font-semibold text-gray-900 dark:text-white">
                  {{ displayName }}
                </h2>
                <span :class="['badge', user?.role === 'admin' ? 'badge-primary' : 'badge-gray']">
                  {{ user?.role === 'admin' ? t('profile.administrator') : t('profile.user') }}
                </span>
                <span
                  :class="['badge', user?.status === 'active' ? 'badge-success' : 'badge-danger']"
                >
                  {{
                    user?.status === 'active'
                      ? t('common.active')
                      : t('common.disabled')
                  }}
                </span>
              </div>

              <div class="space-y-1">
                <p class="truncate text-sm text-gray-600 dark:text-gray-300">
                  {{ primaryEmailDisplay }}
                </p>
              </div>
            </div>

            <div class="grid gap-3 sm:grid-cols-3">
              <div
                data-testid="profile-overview-metric-balance"
                class="rounded-2xl bg-white/85 px-4 py-3 shadow-sm ring-1 ring-white/70 dark:bg-dark-900/60 dark:ring-dark-700"
              >
                <p class="text-xs font-medium uppercase tracking-[0.16em] text-gray-400 dark:text-gray-500">
                  {{ t('profile.accountBalance') }}
                </p>
                <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
                  {{ formatCurrency(user?.balance || 0) }}
                </p>
              </div>
              <div
                data-testid="profile-overview-metric-concurrency"
                class="rounded-2xl bg-white/85 px-4 py-3 shadow-sm ring-1 ring-white/70 dark:bg-dark-900/60 dark:ring-dark-700"
              >
                <p class="text-xs font-medium uppercase tracking-[0.16em] text-gray-400 dark:text-gray-500">
                  {{ t('profile.concurrencyLimit') }}
                </p>
                <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
                  {{ user?.concurrency || 0 }}
                </p>
              </div>
              <div
                data-testid="profile-overview-metric-member-since"
                class="rounded-2xl bg-white/85 px-4 py-3 shadow-sm ring-1 ring-white/70 dark:bg-dark-900/60 dark:ring-dark-700"
              >
                <p class="text-xs font-medium uppercase tracking-[0.16em] text-gray-400 dark:text-gray-500">
                  {{ t('profile.memberSince') }}
                </p>
                <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
                  {{ memberSinceLabel }}
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <div class="space-y-4">
      <div data-testid="profile-main-column" class="space-y-4">
        <section
          data-testid="profile-basics-panel"
          class="card border border-gray-100 bg-white/90 p-4 dark:border-dark-700 dark:bg-dark-900/50"
        >
          <div class="mb-4 flex items-start justify-between gap-4">
            <div>
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('profile.basicsTitle') }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('profile.basicsDescription') }}
              </p>
            </div>
          </div>

          <div class="grid gap-4 sm:grid-cols-1 md:grid-cols-2">
            <div class="rounded-3xl border border-gray-100 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-900/30">
              <ProfileAvatarCard
                :user="user"
                embedded
              />
            </div>

            <div class="rounded-3xl border border-gray-100 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-900/30">
              <ProfileEditForm
                :initial-username="user?.username || ''"
                embedded
              />
            </div>
          </div>
        </section>

      </div>

      <div data-testid="profile-side-column" class="space-y-4">
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ProfileAvatarCard from '@/components/user/profile/ProfileAvatarCard.vue'
import ProfileEditForm from '@/components/user/profile/ProfileEditForm.vue'
import type { User } from '@/types'

const props = defineProps<{
  user: User | null
}>()

const { t } = useI18n()

const avatarUrl = computed(() => props.user?.avatar_url?.trim() || '')
const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || t('profile.user'))
const primaryEmailDisplay = computed(() => props.user?.email?.trim() || '')
const avatarInitial = computed(() => displayName.value.charAt(0).toUpperCase() || 'U')
const memberSinceLabel = computed(() => {
  const raw = props.user?.created_at?.trim()
  if (!raw) {
    return '-'
  }

  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }

  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
  }).format(date)
})

function formatCurrency(value: number): string {
  return `$${value.toFixed(2)}`
}
</script>
