export default {
  modelBenchmark: {
    title: '模型能力测评',
    description: '高难度题集自动评分，持续跟踪能力分、Token 用量、耗时、成本与历史曲线',
    loadFailed: '加载模型能力测评数据失败',
    refresh: '刷新',
    items: '题',
    fields: {
      name: '计划名称', model: '模型', account: '账号', cron: 'Cron', samples: '采样次数', maxTokens: '单题 max tokens',
      timeout: '单题超时（秒）', enabled: '启用计划', status: '状态', nextRun: '下次运行', intelligence: '能力分',
      efficiency: '效率分', composite: '综合评级', outputTokens: '输出 Token', totalTokens: '总 Token', latency: '平均耗时',
      cost: '估算成本', usage: 'Usage', lastRun: '最近运行', created: '创建时间', category: '类别', score: '得分',
      tokens: '输入 / 输出（推理）', reason: '判定原因'
    },
    stats: {
      latestIntelligence: '最新能力分', bestModel: '当前最佳模型', outputTokens: '输出 Token', latency: '平均耗时',
      activePlans: '启用计划', upstreamUsage: '上游 usage', usageMissing: 'usage 缺失', serialRunner: '全局串行执行'
    },
    category: { math: '数学组合', algorithms: '算法复杂度', logic: '形式逻辑', code: '代码缺陷', planning: '规划图论', instruction: '精确指令' },
    rankings: { title: '模型排行', hint: '相同套件与评分版本 · 至少 3 次成功 run', empty: '暂无可比排名；完成至少 3 次成功测评后展示' },
    categories: { title: '能力拆解', empty: '暂无成功测评数据' },
    history: { title: '历史曲线', hint: '能力分、效率分、Token、耗时与成本分开呈现，避免综合分掩盖问题', allModels: '全部模型', empty: '暂无历史数据' },
    plans: {
      title: '测评计划', create: '新建测评计划', namePlaceholder: '例如：旗舰模型每周硬测评', empty: '暂无测评计划',
      modelSearch: '模型搜索', modelSearchPlaceholder: '搜索模型 ID 或显示名...', platformFilter: '平台', allPlatforms: '全部平台',
      unsupportedModel: '当前模型暂无可测评账号',
      actions: '操作', runNow: '手动排队', enable: '启用', disable: '停用',
      costWarning: '每次运行约发起 {count} × {samples} 个上游请求，请确认额度与成本。', deleteConfirm: '确定删除计划「{name}」及其历史记录吗？'
    },
    runs: { title: '最近执行', empty: '暂无执行记录' },
    runDetail: { title: 'Run 详情' },
    runStatus: { running: '执行中', success: '成功', partial: '部分完成', failed: '失败', canceled: '已取消' },
    status: { enabled: '已启用', disabled: '已停用' }
  }
}
