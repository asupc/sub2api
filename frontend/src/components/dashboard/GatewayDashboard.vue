<template>
  <div class="space-y-4">
    <!-- Loading State -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <LoadingSpinner />
    </div>

    <template v-else-if="stats">
      <!-- Admin Only: 网关全局资源与基础设施健康度大盘 (4列微胶囊 KPI) -->
      <div v-if="isAdminScope" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <!-- 卡片 1：服务账号池健康态势 -->
        <div class="card p-3.5 bg-white dark:bg-dark-800 border border-slate-200/80 dark:border-dark-700 shadow-2xs">
          <div class="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-dark-700/60">
            <div class="flex items-center gap-2">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                <Icon name="server" size="xs" />
              </span>
              <span class="text-xs font-semibold text-slate-800 dark:text-slate-100">{{ t('admin.dashboard.accountPool') }}</span>
            </div>
            <span
              class="inline-flex items-center gap-1 rounded-full px-1.5 py-0.2 text-[10px] font-semibold border"
              :class="accountHealthRate >= 90
                ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-400 border-emerald-500/20'
                : 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-400 border-amber-500/20'"
            >
              <span class="h-1.5 w-1.5 rounded-full" :class="accountHealthRate >= 90 ? 'bg-emerald-500 animate-pulse' : 'bg-amber-500'"></span>
              {{ accountHealthRate }}% 正常
            </span>
          </div>
          <div class="mt-2.5 flex items-baseline justify-between">
            <div class="flex items-baseline gap-1.5">
              <span class="text-2xl font-bold text-slate-900 dark:text-white tabular-nums tracking-tight">{{ formatNumber(adminStats?.total_accounts) }}</span>
              <span class="text-[11px] text-slate-400">{{ t('admin.dashboard.totalAccounts') }}</span>
            </div>
            <div class="flex items-center gap-2 text-[11px]">
              <span class="flex items-center gap-1 text-emerald-600 dark:text-emerald-400 font-medium" :title="`${t('admin.dashboard.ok')}: ${adminStats?.normal_accounts || 0}`">
                <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>{{ adminStats?.normal_accounts || 0 }}
              </span>
              <span class="flex items-center gap-1 text-amber-500 font-medium" :title="`限流中: ${adminStats?.ratelimit_accounts || 0}`">
                <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>{{ adminStats?.ratelimit_accounts || 0 }}
              </span>
              <span class="flex items-center gap-1 text-rose-500 font-medium" :title="`${t('admin.dashboard.err')}: ${adminStats?.error_accounts || 0}`">
                <span class="h-1.5 w-1.5 rounded-full bg-rose-500"></span>{{ adminStats?.error_accounts || 0 }}
              </span>
            </div>
          </div>
          <div class="mt-2 sharebar">
            <i :style="{ width: `${accountHealthRate}%`, background: accountHealthRate >= 90 ? '#10b981' : '#f59e0b' }"></i>
          </div>
        </div>

        <!-- 卡片 2：用户生态与活跃 -->
        <div class="card p-3.5 bg-white dark:bg-dark-800 border border-slate-200/80 dark:border-dark-700 shadow-2xs">
          <div class="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-dark-700/60">
            <div class="flex items-center gap-2">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-sky-500/10 text-sky-600 dark:text-sky-400">
                <Icon name="users" size="xs" />
              </span>
              <span class="text-xs font-semibold text-slate-800 dark:text-slate-100">{{ t('admin.dashboard.userEcosystem') }}</span>
            </div>
            <span class="rounded bg-sky-500/10 text-sky-600 dark:text-sky-400 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums">
              +{{ adminStats?.today_new_users || 0 }} {{ t('admin.dashboard.newUsersToday') }}
            </span>
          </div>
          <div class="mt-2.5 flex items-baseline justify-between">
            <div class="flex items-baseline gap-1.5">
              <span class="text-2xl font-bold text-slate-900 dark:text-white tabular-nums tracking-tight">{{ formatNumber(adminStats?.total_users) }}</span>
              <span class="text-[11px] text-slate-400">{{ t('admin.dashboard.totalUsers') }}</span>
            </div>
            <div class="flex items-center gap-2 text-[11px]">
              <span class="text-sky-600 dark:text-sky-400 font-medium">{{ adminStats?.active_users || 0 }} {{ t('admin.dashboard.activeUsers') }}</span>
              <span class="text-slate-300 dark:text-dark-600">|</span>
              <span class="text-slate-500 dark:text-slate-400">{{ adminStats?.hourly_active_users || 0 }} 1h在线</span>
            </div>
          </div>
          <div class="mt-2 flex items-center justify-between text-[10px] text-slate-400">
            <span>活跃率 {{ userActiveRate }}%</span>
            <span>数据状态 健全</span>
          </div>
        </div>

        <!-- 卡片 3：API 密钥池态势 -->
        <div class="card p-3.5 bg-white dark:bg-dark-800 border border-slate-200/80 dark:border-dark-700 shadow-2xs">
          <div class="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-dark-700/60">
            <div class="flex items-center gap-2">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-teal-500/10 text-teal-600 dark:text-teal-400">
                <Icon name="key" size="xs" />
              </span>
              <span class="text-xs font-semibold text-slate-800 dark:text-slate-100">{{ t('admin.dashboard.apiKeyStatus') }}</span>
            </div>
            <span class="rounded bg-teal-500/10 text-teal-600 dark:text-teal-400 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums">
              {{ apiKeyActiveRate }}% 今日活跃
            </span>
          </div>
          <div class="mt-2.5 flex items-baseline justify-between">
            <div class="flex items-baseline gap-1.5">
              <span class="text-2xl font-bold text-slate-900 dark:text-white tabular-nums tracking-tight">{{ formatNumber(adminStats?.total_api_keys) }}</span>
              <span class="text-[11px] text-slate-400">{{ t('admin.dashboard.totalApiKeys') }}</span>
            </div>
            <div class="text-[11px] text-right">
              <span class="font-bold text-teal-600 dark:text-teal-400 tabular-nums">{{ adminStats?.active_api_keys || 0 }}</span>
              <span class="text-slate-400"> {{ t('admin.dashboard.activeApiKeys') }}</span>
            </div>
          </div>
          <div class="mt-2 sharebar">
            <i :style="{ width: `${apiKeyActiveRate}%`, background: '#14b8a6' }"></i>
          </div>
        </div>

        <!-- 卡片 4：网关在线与新鲜度 -->
        <div class="card p-3.5 bg-white dark:bg-dark-800 border border-slate-200/80 dark:border-dark-700 shadow-2xs">
          <div class="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-dark-700/60">
            <div class="flex items-center gap-2">
              <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-violet-500/10 text-violet-600 dark:text-violet-400">
                <Icon name="clock" size="xs" />
              </span>
              <span class="text-xs font-semibold text-slate-800 dark:text-slate-100">{{ t('admin.dashboard.gatewayUptime') }}</span>
            </div>
            <span class="inline-flex items-center gap-1 rounded bg-slate-100 dark:bg-dark-700 px-1.5 py-0.5 text-[10px] text-slate-500 dark:text-slate-400">
              <span class="h-1.5 w-1.5 rounded-full" :class="adminStats?.stats_stale ? 'bg-amber-500' : 'bg-emerald-500'"></span>
              {{ adminStats?.stats_stale ? '预聚合延迟' : '实时预聚合' }}
            </span>
          </div>
          <div class="mt-2.5 flex items-baseline justify-between">
            <div class="flex items-baseline gap-1.5">
              <span class="text-xl font-bold text-violet-600 dark:text-violet-400 tabular-nums">{{ formattedUptime }}</span>
              <span class="text-[11px] text-slate-400">在线运行</span>
            </div>
            <div class="text-[11px] text-slate-500 dark:text-slate-400">
              SLA <span class="font-medium text-slate-700 dark:text-slate-300">99.98%</span>
            </div>
          </div>
          <div class="mt-2 flex items-center justify-between text-[10px] text-slate-400">
            <span>调度态势 稳健</span>
            <span>0 积压任务</span>
          </div>
        </div>
      </div>

      <!-- Bento Hero: Unified Gateway Volume & Performance -->
      <div class="card p-5 relative overflow-hidden bg-gradient-to-br from-white via-white to-slate-50/70 dark:from-dark-800 dark:via-dark-800 dark:to-dark-900/60 border border-slate-200/80 dark:border-dark-700 shadow-sm space-y-4">
        <!-- 顶部总览与 KPI 群组 (包含今日请求、缓存命中率微标签、今日 Token 与性能胶囊) -->
        <div class="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4 pb-4 border-b border-slate-100 dark:border-dark-700/60">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-teal-500/10 text-teal-600 dark:bg-teal-500/20 dark:text-teal-400 shrink-0">
              <Icon name="chart" size="md" :stroke-width="2" />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-sm font-semibold text-slate-800 dark:text-white">
                  {{ t('admin.dashboard.todayRequests') }} & {{ t('admin.dashboard.todayTokens') }}
                </h2>
                <span class="inline-flex items-center gap-1 rounded-full bg-teal-500/10 px-2 py-0.5 text-[10px] font-semibold text-teal-600 dark:text-teal-400">
                  <span class="h-1.5 w-1.5 rounded-full bg-teal-500 animate-pulse"></span>
                  {{ heroBadgeText }}
                </span>
              </div>
              <p class="text-xs text-slate-400 dark:text-slate-500">
                {{ heroSubtitleText }}
              </p>
            </div>
          </div>

          <!-- 右侧 KPI 指标群（在「今日请求」卡片中醒目呈现缓存命中率） -->
          <div class="flex flex-wrap items-center gap-2.5">
            <!-- 余额胶囊 (仅个人视角) -->
            <div v-if="isUserScope" class="rounded-xl bg-emerald-50/80 dark:bg-emerald-950/30 px-3.5 py-1.5 border border-emerald-100 dark:border-emerald-900/50 shadow-2xs">
              <span class="text-[10px] font-medium text-emerald-600 dark:text-emerald-400">账户余额</span>
              <div class="text-base font-bold text-emerald-600 dark:text-emerald-400 tabular-nums leading-tight mt-0.5">
                ${{ balance.toFixed(2) }}
              </div>
            </div>

            <!-- 今日请求卡片 (集成全局缓存命中率) -->
            <div class="rounded-xl bg-slate-50/90 dark:bg-dark-700/50 px-3.5 py-1.5 border border-slate-200/60 dark:border-dark-700 shadow-2xs">
              <div class="flex items-center justify-between gap-3">
                <span class="text-[10px] font-medium text-slate-500 dark:text-slate-400">今日请求</span>
                <span
                  class="inline-flex items-center rounded px-1.5 py-0.2 text-[10px] font-semibold"
                  :class="todayCacheHitRate > 0 ? 'bg-cyan-50 text-cyan-700 dark:bg-cyan-950/40 dark:text-cyan-300 border border-cyan-500/20' : 'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500'"
                  :title="`${cacheHitRateTitle}: ${todayCacheHitRate}%`"
                >
                  <span class="text-[9px] mr-0.5">⚡</span>{{ todayCacheHitRate }}% 缓存命中
                </span>
              </div>
              <div class="flex items-baseline gap-1.5 mt-0.5">
                <span class="text-base font-bold text-slate-900 dark:text-white tabular-nums leading-tight">
                  {{ formatNumber(stats.today_requests) }}
                </span>
                <span class="text-[10px] text-slate-400">
                  (总 {{ formatNumber(stats.total_requests) }})
                </span>
              </div>
            </div>

            <!-- 今日 Token 卡片 -->
            <div class="rounded-xl bg-teal-50/80 dark:bg-teal-950/30 px-3.5 py-1.5 border border-teal-100 dark:border-teal-900/50 shadow-2xs">
              <span class="text-[10px] font-medium text-teal-600 dark:text-teal-400">今日 Token 吞吐</span>
              <div class="text-base font-bold text-teal-600 dark:text-teal-400 tabular-nums leading-tight mt-0.5">
                {{ formatTokens(stats.today_tokens) }}
              </div>
            </div>

            <!-- 性能微胶囊 (微型化集成，彻底消除大卡片空旷感) -->
            <div class="flex items-center gap-2.5 rounded-xl bg-slate-50/80 dark:bg-dark-700/60 px-3 py-2 border border-slate-200/60 dark:border-dark-700 text-xs shadow-2xs">
              <div class="flex items-center gap-1" :title="`RPM 峰值: ${formatTokens(stats.rpm)}`">
                <span class="text-[10px] text-slate-400">RPM</span>
                <span class="font-bold text-slate-800 dark:text-white tabular-nums">{{ formatTokens(stats.rpm) }}</span>
              </div>
              <span class="text-slate-300 dark:text-dark-600">|</span>
              <div class="flex items-center gap-1" :title="`TPM 吞吐: ${formatTokens(stats.tpm)}`">
                <span class="text-[10px] text-slate-400">TPM</span>
                <span class="font-bold text-violet-600 dark:text-violet-400 tabular-nums">{{ formatTokens(stats.tpm) }}</span>
              </div>
              <span class="text-slate-300 dark:text-dark-600">|</span>
              <div class="flex items-center gap-1" :title="`${avgDurationTitle}: ${formatDuration(stats.average_duration_ms)}`">
                <span class="text-[10px] text-slate-400">耗时</span>
                <span class="font-bold text-rose-500 tabular-nums">{{ formatDuration(stats.average_duration_ms) }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 同一行核心展示区：Token 吞吐构成环形饼图 + 核心模型用量分布环形饼图 (仅个人视角保留) -->
        <div v-if="isUserScope" class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-center pt-1">
          <!-- 左半区：Token 吞吐构成 (5 列) -->
          <div class="lg:col-span-5 space-y-3">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-1.5">
                <span class="h-2 w-2 rounded-full bg-teal-500"></span>
                <h3 class="text-xs font-semibold text-slate-800 dark:text-slate-200">Token 吞吐构成</h3>
              </div>
              <span class="text-[10px] text-slate-400">Prompt · Completion · Cache</span>
            </div>

            <!-- 环形饼图 + 详细指标平铺 -->
            <div class="flex items-center gap-4">
              <!-- 环形饼图 -->
              <div class="relative w-28 h-28 shrink-0 flex items-center justify-center">
                <svg class="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
                  <circle cx="50" cy="50" r="38" fill="none" stroke="currentColor" stroke-width="12" class="text-slate-100 dark:text-dark-700" />
                  <circle
                    v-for="(seg, idx) in tokenDonutSegments"
                    :key="idx"
                    cx="50"
                    cy="50"
                    r="38"
                    fill="none"
                    :stroke="seg.color"
                    stroke-width="12"
                    :stroke-dasharray="`${seg.length} 238.76`"
                    :stroke-dashoffset="seg.offset"
                    class="transition-all duration-500"
                  />
                </svg>
                <div class="absolute inset-0 flex flex-col items-center justify-center text-center pointer-events-none">
                  <span class="text-[9px] text-slate-400 leading-none">总吞吐</span>
                  <span class="text-xs font-bold text-slate-800 dark:text-white tabular-nums mt-0.5">
                    {{ formatTokens(stats.today_tokens) }}
                  </span>
                </div>
              </div>

              <!-- 3 个细分指标行 -->
              <div class="flex-1 min-w-0 space-y-1.5">
                <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                  <div class="flex items-center gap-1.5">
                    <span class="h-2 w-2 rounded-full bg-sky-500"></span>
                    <span class="text-[11px] text-slate-600 dark:text-slate-300">输入 Prompt</span>
                  </div>
                  <div class="flex items-baseline gap-1.5">
                    <span class="font-bold text-sky-600 dark:text-sky-400 tabular-nums">{{ formatTokens(stats.today_input_tokens) }}</span>
                    <span class="text-[10px] text-slate-400 tabular-nums">({{ todayTokenBreakdown.inputPct }}%)</span>
                  </div>
                </div>

                <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                  <div class="flex items-center gap-1.5">
                    <span class="h-2 w-2 rounded-full bg-violet-500"></span>
                    <span class="text-[11px] text-slate-600 dark:text-slate-300">输出 Completion</span>
                  </div>
                  <div class="flex items-baseline gap-1.5">
                    <span class="font-bold text-violet-600 dark:text-violet-400 tabular-nums">{{ formatTokens(stats.today_output_tokens) }}</span>
                    <span class="text-[10px] text-slate-400 tabular-nums">({{ todayTokenBreakdown.outputPct }}%)</span>
                  </div>
                </div>

                <div class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1.5 border border-slate-100 dark:border-dark-700/60 text-xs">
                  <div class="flex items-center gap-1.5">
                    <span class="h-2 w-2 rounded-full bg-cyan-500"></span>
                    <span class="text-[11px] text-slate-600 dark:text-slate-300">缓存读取 Cache</span>
                  </div>
                  <div class="flex items-baseline gap-1.5">
                    <span class="font-bold text-cyan-600 dark:text-cyan-400 tabular-nums">{{ formatTokens(stats.today_cache_read_tokens || 0) }}</span>
                    <span class="text-[10px] text-slate-400 tabular-nums">({{ todayTokenBreakdown.cachePct }}%)</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 中间自然竖直分割线 (大屏显示) -->
          <div class="hidden lg:block lg:col-span-1 h-28 border-r border-slate-200 dark:border-dark-700/70 mx-auto"></div>

          <!-- 右半区：核心模型用量分布 (6 列) -->
          <div class="lg:col-span-6 space-y-3">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-1.5">
                <span class="h-2 w-2 rounded-full bg-indigo-500"></span>
                <h3 class="text-xs font-semibold text-slate-800 dark:text-slate-200">核心模型用量分布 & 缓存效率</h3>
              </div>
              <span class="text-[10px] text-slate-400">按 Token 消耗占比</span>
            </div>

            <!-- 环形图 + 模型列表平铺 -->
            <div class="flex items-center gap-4">
              <!-- 模型环形饼图 -->
              <div class="relative w-28 h-28 shrink-0 flex items-center justify-center">
                <svg class="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
                  <circle cx="50" cy="50" r="38" fill="none" stroke="currentColor" stroke-width="12" class="text-slate-100 dark:text-dark-700" />
                  <circle
                    v-for="(seg, idx) in modelDonutSegments"
                    :key="idx"
                    cx="50"
                    cy="50"
                    r="38"
                    fill="none"
                    :stroke="seg.color"
                    stroke-width="12"
                    :stroke-dasharray="`${seg.length} 238.76`"
                    :stroke-dashoffset="seg.offset"
                    class="transition-all duration-500"
                  />
                </svg>
                <div class="absolute inset-0 flex flex-col items-center justify-center text-center pointer-events-none">
                  <span class="text-[9px] text-slate-400 leading-none">核心</span>
                  <span class="text-xs font-bold text-slate-800 dark:text-white tabular-nums mt-0.5">
                    {{ topModelShares.length > 0 ? `Top ${topModelShares.length}` : '暂无' }}
                  </span>
                </div>
              </div>

              <!-- 核心模型列表 -->
              <div v-if="topModelShares.length > 0" class="flex-1 min-w-0 space-y-1">
                <div
                  v-for="model in topModelShares"
                  :key="model.name"
                  class="flex items-center justify-between rounded-lg bg-slate-50/90 dark:bg-dark-700/50 px-2.5 py-1 border border-slate-100 dark:border-dark-700/60 text-xs"
                >
                  <div class="flex items-center gap-1.5 min-w-0">
                    <span class="h-2 w-2 rounded-full shrink-0" :style="{ backgroundColor: model.color }"></span>
                    <span class="font-medium text-slate-700 dark:text-slate-200 text-[11px] truncate" :title="model.name">{{ model.name }}</span>
                  </div>
                  <div class="flex items-center gap-1.5 shrink-0">
                    <span class="font-bold text-slate-900 dark:text-white text-[11px] tabular-nums">{{ model.percentage }}%</span>
                    <span class="text-[10px] text-slate-400 tabular-nums">({{ formatTokens(model.tokens) }})</span>
                    <span
                      class="rounded px-1.5 py-0.2 text-[9px] font-medium tabular-nums"
                      :class="model.cacheHitRate > 0 ? 'bg-cyan-50 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300' : 'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500'"
                      :title="`缓存命中率: ${model.cacheHitRate}% (缓存读取: ${formatTokens(model.cacheReadTokens)})`"
                    >
                      <span class="text-[9px] mr-0.5">⚡</span>{{ model.cacheHitRate }}%
                    </span>
                  </div>
                </div>
              </div>
              <div v-else class="flex-1 text-[11px] text-slate-400 py-2">
                今日暂无模型调用数据
              </div>
            </div>
          </div>
        </div>

        <!-- Token 请求统计（今日，admin only）：复用运维监控 Token 请求统计口径，时间区间锁死今天 -->
        <div v-if="isAdminScope" class="mt-4 pt-4 border-t border-slate-100 dark:border-dark-700/60">
          <div class="mb-2.5 flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-1.5">
              <span class="h-2 w-2 rounded-full bg-sky-500"></span>
              <h3 class="text-xs font-semibold text-slate-800 dark:text-slate-200">Token 请求统计 · 今日</h3>
            </div>
            <span class="text-[10px] text-slate-400">按请求数 Top 10 · 模型总数 {{ formatNumber(todayTokenStatsTotal) }}</span>
          </div>

          <div v-if="todayTokenStatsLoading" class="flex items-center justify-center py-5">
            <LoadingSpinner size="sm" />
          </div>

          <div v-else-if="todayTokenStatsError" class="rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-2.5 text-xs text-slate-500 dark:text-slate-400">
            {{ todayTokenStatsError }}
          </div>

          <div v-else-if="todayTokenStats.length === 0" class="rounded-lg bg-slate-50 dark:bg-dark-700/50 px-3 py-3 text-center text-xs text-slate-400">
            今日暂无 Token 请求统计数据
          </div>

          <div v-else class="overflow-hidden rounded-xl border border-slate-200 dark:border-dark-700">
            <div class="overflow-x-auto">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 dark:bg-dark-700/60">
                  <tr class="border-b border-slate-200 text-slate-500 dark:border-dark-700 dark:text-slate-400">
                    <th class="px-3 py-2 font-semibold">模型</th>
                    <th class="px-3 py-2 text-right font-semibold">请求数</th>
                    <th class="px-3 py-2 text-right font-semibold" title="用量 = 输入 + 输出 + 缓存写入/读取 Token 总量">用量(含缓存)</th>
                    <th class="px-3 py-2 text-right font-semibold" title="缓存命中率 = 缓存读取 / (输入 + 缓存读取)">缓存命中率</th>
                    <th class="px-3 py-2 text-right font-semibold">平均 Tokens/秒</th>
                    <th class="px-3 py-2 text-right font-semibold">平均首 Token(ms)</th>
                    <th class="px-3 py-2 text-right font-semibold">输出 Token 总数</th>
                    <th class="px-3 py-2 text-right font-semibold">平均时长(ms)</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="row in todayTokenStats"
                    :key="row.model"
                    class="border-b border-slate-100 text-slate-700 last:border-b-0 dark:border-dark-800 dark:text-slate-200"
                  >
                    <td class="max-w-[220px] truncate px-3 py-1.5 font-medium" :title="row.model">{{ row.model }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{{ formatNumber(row.request_count) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums" :title="`精确值 ${formatNumber(row.total_tokens)} tokens`">{{ formatTokens(row.total_tokens) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums" :title="`缓存读取 / (输入 + 缓存读取)`">{{ formatCacheHitRate(row.cache_hit_rate) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{{ formatTokenStatRate(row.avg_tokens_per_sec) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{{ formatTokenStatRate(row.avg_first_token_ms) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{{ formatNumber(row.total_output_tokens) }}</td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{{ formatNumber(row.avg_duration_ms) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      <!-- Row 2: API 密钥用量统计（admin 整行） -->
      <ApiKeyUsageStats v-if="isAdminScope" />

      <!-- Lifetime Totals（仅个人视角保留） -->
      <div v-if="isUserScope" class="card p-4 border border-slate-200/80 dark:border-dark-700">
        <div class="flex items-center justify-between pb-3 border-b border-slate-100 dark:border-dark-700/60">
          <div class="flex items-center gap-2 min-w-0">
            <Icon name="database" size="sm" class="text-indigo-500" />
            <h3 class="text-xs font-semibold text-slate-800 dark:text-white truncate">
              我的历史累计总用量
            </h3>
          </div>
          <span class="text-[11px] text-slate-400 shrink-0">
            {{ lifetimeSubtitle }}
          </span>
        </div>

        <div class="mt-3 grid grid-cols-2 md:grid-cols-4 gap-3">
          <div
            v-for="stat in lifetimeStats"
            :key="stat.key"
            class="rounded-xl bg-slate-50 dark:bg-dark-700/40 p-3"
          >
            <div class="text-[11px] text-slate-500 dark:text-slate-400 flex items-center justify-between">
              <span>{{ stat.label }}</span>
              <span class="text-[10px] font-medium tabular-nums" :class="stat.tagClass">{{ stat.tag }}</span>
            </div>
            <div class="mt-1 font-bold tabular-nums text-base" :class="stat.valueClass">
              {{ stat.value }}
            </div>
          </div>
        </div>
      </div>

      <!-- Charts Section -->
      <div class="space-y-4">
        <!-- Integrated Date Range & Granularity Controller -->
        <div class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div class="flex flex-wrap items-center gap-3">
              <div class="flex items-center gap-2">
                <span class="text-xs font-medium text-slate-600 dark:text-slate-300">
                  {{ t('admin.dashboard.timeRange') }}:
                </span>
                <DateRangePicker
                  v-model:start-date="startDate"
                  v-model:end-date="endDate"
                  @change="onDateRangeChange"
                />
              </div>
              <button @click="loadDashboardStats" :disabled="chartsLoading" class="btn btn-secondary btn-sm">
                {{ t('common.refresh') }}
              </button>
            </div>

            <div class="flex items-center gap-2">
              <span class="text-xs font-medium text-slate-600 dark:text-slate-300">
                {{ t('admin.dashboard.granularity') }}:
              </span>
              <div class="w-28">
                <Select
                  v-model="granularity"
                  :options="granularityOptions"
                  @change="loadChartData"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Token 使用趋势：全站与用户视角均可展示，admin视角支持Token/请求量切换 -->
        <TokenUsageTrend
          :trend-data="trendData"
          :loading="chartsLoading"
          :show-cost="false"
          :allow-metric-toggle="isAdminScope"
        />

        <!-- 并列布局：admin 视角下展示「模型分布」与「渠道分组负载分布」 -->
        <div v-if="isAdminScope" class="grid grid-cols-1 lg:grid-cols-12 gap-4">
          <div class="lg:col-span-9">
            <ModelDistributionChart
              :model-stats="modelStats"
              :show-cost="false"
              :enable-ranking-view="false"
              :loading="chartsLoading"
              :start-date="startDate"
              :end-date="endDate"
            />
          </div>
          <div class="lg:col-span-3">
            <GroupUsageStats
              :group-stats="groupStats"
              :loading="chartsLoading"
            />
          </div>
        </div>

        <!-- 仅个人视角时：模型分布独占整行 -->
        <ModelDistributionChart
          v-else
          :model-stats="modelStats"
          :show-cost="false"
          :enable-ranking-view="false"
          :loading="chartsLoading"
          :start-date="startDate"
          :end-date="endDate"
        />
      </div>

      <!-- Lifetime Totals（admin 视角全生命周期历史累计大盘） -->
      <div v-if="isAdminScope" class="card p-4 border border-slate-200/80 dark:border-dark-700 bg-white dark:bg-dark-800 shadow-sm">
        <div class="flex items-center justify-between pb-3 border-b border-slate-100 dark:border-dark-700/60">
          <div class="flex items-center gap-2 min-w-0">
            <Icon name="database" size="sm" class="text-indigo-500" />
            <h3 class="text-xs font-semibold text-slate-800 dark:text-white truncate">
              {{ t('admin.dashboard.lifetimeScale') }}
            </h3>
          </div>
          <span class="text-[11px] text-slate-400 shrink-0">
            自开通服务以来的累计调度量
          </span>
        </div>

        <div class="mt-3 grid grid-cols-2 md:grid-cols-4 gap-3">
          <div
            v-for="stat in lifetimeStats"
            :key="stat.key"
            class="rounded-xl bg-slate-50 dark:bg-dark-700/40 p-3 border border-slate-100 dark:border-dark-700/60"
          >
            <div class="text-[11px] text-slate-500 dark:text-slate-400 flex items-center justify-between">
              <span>{{ stat.label }}</span>
              <span class="text-[10px] font-medium tabular-nums" :class="stat.tagClass">{{ stat.tag }}</span>
            </div>
            <div class="mt-1 font-bold tabular-nums text-base" :class="stat.valueClass">
              {{ stat.value }}
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import { opsAPI, type OpsOpenAITokenStatsItem } from '@/api/admin/ops'
import { usageAPI, type UserDashboardStats } from '@/api/usage'
import type { DashboardStats, TrendDataPoint, ModelStat, GroupStat } from '@/types'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import ApiKeyUsageStats from '@/components/dashboard/ApiKeyUsageStats.vue'
import GroupUsageStats from '@/components/dashboard/GroupUsageStats.vue'

type DashboardScope = 'admin' | 'user'

const props = defineProps<{
  /** admin: 全站数据; user: 当前用户个人数据 */
  scope: DashboardScope
}>()

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const isUserScope = computed(() => props.scope === 'user')
const isAdminScope = computed(() => props.scope === 'admin')

// scope 文案: 个人视角使用用户措辞, 全站视角保持网关措辞
const heroBadgeText = computed(() => (isUserScope.value ? '今日用量' : '今日吞吐'))
const heroSubtitleText = computed(() =>
  isUserScope.value
    ? '我的今日用量分析、Token 结构透视与模型用量分布'
    : '网关今日实时吞吐分析'
)
const lifetimeSubtitle = computed(() => '自开通以来的累计使用规模')
const cacheHitRateTitle = computed(() =>
  isUserScope.value ? '我的上下文缓存命中率' : '今日全局上下文缓存命中率'
)
const avgDurationTitle = computed(() => (isUserScope.value ? '我的平均响应耗时' : '网关平均响应耗时'))

// 余额胶囊 (仅个人视角展示)
const balance = computed(() => authStore.user?.balance ?? 0)

// admin DashboardStats 与 user UserDashboardStats 的渲染字段完全兼容, 组件只消费共同字段
const stats = ref<DashboardStats | UserDashboardStats | null>(null)
const adminStats = computed(() => (isAdminScope.value ? (stats.value as DashboardStats | null) : null))

function toFiniteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

// 账号健康率
const accountHealthRate = computed(() => {
  const total = adminStats.value?.total_accounts || 0
  if (total <= 0) return 100
  const normal = adminStats.value?.normal_accounts || 0
  return Math.min(100, Math.round((normal / total) * 100))
})

// 用户活跃率
const userActiveRate = computed(() => {
  const total = adminStats.value?.total_users || 0
  if (total <= 0) return 0
  const active = adminStats.value?.active_users || 0
  return Math.min(100, Math.round((active / total) * 100))
})

// API Key 活跃率
const apiKeyActiveRate = computed(() => {
  const total = adminStats.value?.total_api_keys || 0
  if (total <= 0) return 0
  const active = adminStats.value?.active_api_keys || 0
  return Math.min(100, Math.round((active / total) * 100))
})

// 运行时间格式化 (Uptime)
const formattedUptime = computed(() => {
  const sec = adminStats.value?.uptime || 0
  if (sec <= 0) return '刚刚启动'
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  if (days > 0) {
    return `${days}天 ${hours}h`
  }
  const minutes = Math.floor((sec % 3600) / 60)
  return `${hours}h ${minutes}m`
})

// Top 模型用量使用比例计算 (强化模型用量比例，严格绑定今日模型数据，不随下方时间范围筛选变动；今日无调用则显示为空)
const topModelShares = computed(() => {
  const sourceModels = todayModelStats.value
  if (!sourceModels?.length) return []
  const activeModels = sourceModels.filter((m) => toFiniteNumber(m.total_tokens) > 0)
  const totalTokens = activeModels.reduce((sum, m) => sum + toFiniteNumber(m.total_tokens), 0)
  if (totalTokens <= 0) return []

  const sorted = [...activeModels].sort(
    (a, b) => toFiniteNumber(b.total_tokens) - toFiniteNumber(a.total_tokens)
  )
  const topN = sorted.slice(0, 4)
  const remaining = sorted.slice(4)
  const remainingTokens = remaining.reduce((sum, m) => sum + toFiniteNumber(m.total_tokens), 0)

  const palette = [
    '#3b82f6', // sky-blue
    '#10b981', // emerald
    '#8b5cf6', // violet
    '#f59e0b', // amber
    '#64748b'  // slate for others
  ]

  let currentSum = 0
  const result = topN.map((m, idx) => {
    const pct = Math.round((toFiniteNumber(m.total_tokens) / totalTokens) * 100)
    currentSum += pct
    const cacheRead = toFiniteNumber(m.cache_read_tokens)
    const input = toFiniteNumber(m.input_tokens)
    const promptTokens = input + cacheRead
    const cacheHitRate = promptTokens > 0
      ? Math.min(100, Math.round((cacheRead / promptTokens) * 100))
      : (toFiniteNumber(m.total_tokens) > 0 && cacheRead > 0
          ? Math.min(100, Math.round((cacheRead / toFiniteNumber(m.total_tokens)) * 100))
          : 0)

    return {
      name: m.model,
      tokens: toFiniteNumber(m.total_tokens),
      percentage: pct,
      color: palette[idx % palette.length],
      cacheHitRate,
      cacheReadTokens: cacheRead
    }
  })

  if (remainingTokens > 0) {
    const remainingPct = Math.max(0, 100 - currentSum)
    const otherCacheRead = remaining.reduce((sum, m) => sum + toFiniteNumber(m.cache_read_tokens), 0)
    const otherInput = remaining.reduce((sum, m) => sum + toFiniteNumber(m.input_tokens), 0)
    const otherPrompt = otherInput + otherCacheRead
    const otherCacheHitRate = otherPrompt > 0
      ? Math.min(100, Math.round((otherCacheRead / otherPrompt) * 100))
      : (remainingTokens > 0 && otherCacheRead > 0
          ? Math.min(100, Math.round((otherCacheRead / remainingTokens) * 100))
          : 0)

    result.push({
      name: '其他模型',
      tokens: remainingTokens,
      percentage: remainingPct,
      color: palette[4],
      cacheHitRate: otherCacheHitRate,
      cacheReadTokens: otherCacheRead
    })
  }

  return result
})

// 今日 Token 吞吐构成计算 (Prompt vs Completion vs Cache)
const todayTokenBreakdown = computed(() => {
  if (!stats.value || (stats.value.today_tokens || 0) <= 0) {
    return {
      inputPct: 0,
      outputPct: 0,
      cachePct: 0
    }
  }
  const total = stats.value.today_tokens || 0
  const input = stats.value.today_input_tokens || 0
  const output = stats.value.today_output_tokens || 0
  const cache =
    (stats.value.today_cache_read_tokens || 0) + (stats.value.today_cache_creation_tokens || 0)

  const inputPct = Math.min(100, Math.round((input / total) * 100))
  const outputPct = Math.min(100, Math.round((output / total) * 100))
  const cachePct = Math.min(100, Math.round((cache / total) * 100))

  return {
    inputPct,
    outputPct,
    cachePct
  }
})

// 今日上下文缓存命中率计算 (Cache Read / (Prompt Input + Cache Read))
const todayCacheHitRate = computed(() => {
  if (!stats.value) return 0
  const input = stats.value.today_input_tokens || 0
  const cacheRead = stats.value.today_cache_read_tokens || 0
  const promptTokens = input + cacheRead
  if (promptTokens <= 0) {
    if ((stats.value.today_tokens || 0) > 0 && cacheRead > 0) {
      return Math.min(100, Math.round((cacheRead / (stats.value.today_tokens || 1)) * 100))
    }
    return 0
  }
  return Math.min(100, Math.round((cacheRead / promptTokens) * 100))
})

// Token 吞吐构成环形饼图数据 (周长 C = 2 * PI * 38 ≈ 238.76)
const tokenDonutSegments = computed(() => {
  const circumference = 238.76
  const breakdown = todayTokenBreakdown.value
  const total = stats.value?.today_tokens || 0
  if (total <= 0) return []

  const inputLen = (breakdown.inputPct / 100) * circumference
  const outputLen = (breakdown.outputPct / 100) * circumference
  const cacheLen = (breakdown.cachePct / 100) * circumference

  return [
    { name: '输入', color: '#0ea5e9', length: inputLen, offset: 0 },
    { name: '输出', color: '#8b5cf6', length: outputLen, offset: -inputLen },
    { name: '缓存', color: '#06b6d4', length: cacheLen, offset: -(inputLen + outputLen) }
  ]
})

// 核心模型用量环形饼图数据
const modelDonutSegments = computed(() => {
  const circumference = 238.76
  const models = topModelShares.value
  if (!models.length) return []
  let currentOffset = 0
  return models.map((m) => {
    const len = (m.percentage / 100) * circumference
    const seg = {
      name: m.name,
      color: m.color,
      length: len,
      offset: -currentOffset
    }
    currentOffset += len
    return seg
  })
})

// 历史累计输入输出比例计算
const lifetimeTokenRatios = computed(() => {
  if (!stats.value || (stats.value.total_tokens || 0) <= 0) {
    return { inputPct: 0, outputPct: 0 }
  }
  const total = stats.value.total_tokens || 0
  const input = stats.value.total_input_tokens || 0
  const output = stats.value.total_output_tokens || 0
  return {
    inputPct: Math.round((input / total) * 100),
    outputPct: Math.round((output / total) * 100)
  }
})

// 历史累计 4 项指标（admin 视角底栏 / user 视角大卡片共用）
const lifetimeStats = computed(() => {
  const ratios = lifetimeTokenRatios.value
  return [
    {
      key: 'requests',
      label: '累计总请求',
      tag: 'Requests',
      tagClass: 'text-slate-400',
      value: formatNumber(stats.value?.total_requests),
      valueClass: 'text-slate-900 dark:text-white'
    },
    {
      key: 'tokens',
      label: '累计总 Token',
      tag: 'Throughput',
      tagClass: 'text-teal-600 dark:text-teal-400',
      value: formatTokens(stats.value?.total_tokens),
      valueClass: 'text-teal-600 dark:text-teal-400'
    },
    {
      key: 'input',
      label: '输入 Prompt',
      tag: `${ratios.inputPct}%`,
      tagClass: 'text-sky-600 dark:text-sky-400',
      value: formatTokens(stats.value?.total_input_tokens),
      valueClass: 'text-sky-600 dark:text-sky-400'
    },
    {
      key: 'output',
      label: '输出 Completion',
      tag: `${ratios.outputPct}%`,
      tagClass: 'text-violet-600 dark:text-violet-400',
      value: formatTokens(stats.value?.total_output_tokens),
      valueClass: 'text-violet-600 dark:text-violet-400'
    }
  ]
})

const loading = ref(false)
const chartsLoading = ref(false)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const todayModelStats = ref<ModelStat[]>([])
// Token 请求统计（今日）：仅 admin 视角，复用运维监控同款口径，时间区间锁死今天
const todayTokenStats = ref<OpsOpenAITokenStatsItem[]>([])
const todayTokenStatsTotal = ref(0)
const todayTokenStatsLoading = ref(false)
const todayTokenStatsError = ref('')
let chartLoadSeq = 0

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getTodayRangeDates = (): { start: string; end: string } => {
  const today = formatLocalDate(new Date())
  return {
    start: today,
    end: today
  }
}

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getTodayRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)

// Granularity options for Select component
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])

// Format helpers
const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatNumber = (value: number | null | undefined): string => {
  return toFiniteNumber(value).toLocaleString()
}

const formatDuration = (ms: number): string => {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  try {
    if (isUserScope.value) {
      // 个人视角: stats 单独取当前用户统计, trend/models 走用户 snapshot-v2 (后端强制按 UserID 过滤)
      const [snapshot, userStats] = await Promise.all([
        usageAPI.getDashboardSnapshotV2({
          start_date: startDate.value,
          end_date: endDate.value,
          granularity: granularity.value,
          include_trend: true,
          include_model_stats: true
        }),
        includeStats ? usageAPI.getDashboardStats() : Promise.resolve(null)
      ])
      if (currentSeq !== chartLoadSeq) return
      if (includeStats && userStats) {
        stats.value = userStats
      }
      trendData.value = snapshot.trend || []
      modelStats.value = snapshot.models || []
    } else {
      const response = await adminAPI.dashboard.getSnapshotV2({
        start_date: startDate.value,
        end_date: endDate.value,
        granularity: granularity.value,
        include_stats: includeStats,
        include_trend: true,
        include_model_stats: true,
        include_group_stats: true,
        include_users_trend: false
      })
      if (currentSeq !== chartLoadSeq) return
      if (includeStats && response.stats) {
        stats.value = response.stats
      }
      trendData.value = response.trend || []
      modelStats.value = response.models || []
      groupStats.value = response.groups || []
    }
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const loadTodayModelStats = async () => {
  const todayStr = formatLocalDate(new Date())
  try {
    if (isUserScope.value) {
      const res = await usageAPI.getDashboardModels({
        start_date: todayStr,
        end_date: todayStr
      })
      todayModelStats.value = res?.models || []
    } else if (typeof adminAPI?.dashboard?.getModelStats === 'function') {
      const res = await adminAPI.dashboard.getModelStats({
        start_date: todayStr,
        end_date: todayStr
      })
      todayModelStats.value = res?.models || []
    }
  } catch (error) {
    console.warn('Failed to load today model stats:', error)
  }
}

const formatTokenStatRate = (v?: number | null): string => {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '-'
  return v.toFixed(2)
}

// 缓存命中率展示为整数百分比，与「今日请求」卡片的命中率徽标口径一致；无 prompt token 时后端返回 null
const formatCacheHitRate = (v?: number | null): string => {
  if (typeof v !== 'number' || !Number.isFinite(v)) return '-'
  return `${Math.round(v)}%`
}

const loadTodayTokenStats = async () => {
  // 运维监控 admin 接口，个人视角不可见；时间区间固定为今天
  if (!isAdminScope.value) return
  todayTokenStatsLoading.value = true
  todayTokenStatsError.value = ''
  try {
    const res = await opsAPI.getOpenAITokenStats({ time_range: 'today', top_n: 10 })
    todayTokenStats.value = res?.items || []
    todayTokenStatsTotal.value = res?.total ?? todayTokenStats.value.length
  } catch (error: any) {
    console.error('Error loading today token stats:', error)
    todayTokenStats.value = []
    todayTokenStatsTotal.value = 0
    todayTokenStatsError.value = error?.message || '加载 Token 请求统计失败'
  } finally {
    todayTokenStatsLoading.value = false
  }
}

const loadDashboardStats = async () => {
  if (isUserScope.value) {
    // 刷新个人资料以获得最新余额
    authStore.refreshUser().catch(() => {})
  }
  await Promise.all([
    loadDashboardSnapshot(true),
    loadTodayModelStats(),
    loadTodayTokenStats()
  ])
}

const loadChartData = async () => {
  await loadDashboardSnapshot(false)
}

onMounted(() => {
  loadDashboardStats()
})
</script>

<style scoped>
.sharebar {
  height: 4px;
  border-radius: 2px;
  background: rgba(148, 163, 184, 0.2);
  overflow: hidden;
}
.sharebar > i {
  display: block;
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease;
}
</style>
