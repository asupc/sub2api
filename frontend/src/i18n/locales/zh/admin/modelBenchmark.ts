export default {
  modelBenchmark: {
    title: '模型能力测评',
    description: '覆盖九类能力，按相同试卷和预算比较模型，分开展示正确率、完成率、格式合规与成本',
    loadFailed: '加载模型能力测评数据失败',
    refresh: '刷新',
    items: '题',
    fields: {
      name: '计划名称', model: '模型', account: '账号', cron: 'Cron', samples: '采样次数', maxTokens: '单题 max tokens',
      timeout: '单题超时（秒）', enabled: '启用计划', status: '状态', nextRun: '下次运行', intelligence: '能力分',
      efficiency: '效率分', composite: '综合评级', outputTokens: '输出 Token', totalTokens: '总 Token', latency: '平均耗时',
      cost: '估算成本', usage: 'Usage', lastRun: '最近运行', created: '创建时间', category: '类别', score: '得分',
      tokens: '输入 / 输出（推理）', reason: '判定原因', comparisonKey: '公共试卷批次', coverage: '有效完成率', formatCompliance: '格式合规率', budgetExhausted: '预算耗尽题数', rankingEligibility: '排名资格', tokenBudget: '输出预算', samplesBudget: '独立试卷数'
    },
    stats: {
      latestIntelligence: '最新能力分', bestModel: '当前最佳模型', outputTokens: '输出 Token', latency: '平均耗时',
      activePlans: '启用计划', upstreamUsage: '上游 usage', usageMissing: 'usage 缺失', serialRunner: '计划并行，题目依次执行',
      runs: '执行次数', managePlans: '管理测评计划'
    },
    category: { math: '数学组合', algorithms: '算法复杂度', logic: '形式逻辑', code: '代码缺陷', planning: '规划图论', instruction: '精确指令', abstraction: '抽象归纳', understanding: '材料与数据', causal: '因果与概率' },
    rankings: { title: '模型排行', hint: '同试卷、同版本、同预算 · 完成率≥90%、各领域≥80% · 至少3次执行', empty: '暂无符合当前试卷、预算和完成率要求的排名；至少需要3次合格执行' },
    categories: { title: '能力拆解', empty: '暂无成功测评数据' },
    history: { title: '历史曲线', hint: '能力分、效率分、Token、耗时与成本分开呈现，避免综合分掩盖问题', allModels: '全部模型', empty: '暂无历史数据' },
    plans: {
      title: '测评计划', create: '新建测评计划', edit: '编辑测评计划', namePlaceholder: '例如：旗舰模型每周硬测评', empty: '暂无测评计划',
      modelSearchPlaceholder: '搜索模型 ID 或显示名...', modelSearchEmpty: '无匹配模型',
      unsupportedModel: '当前模型暂无可测评账号',
      actions: '操作', runNow: '手动执行', enable: '启用', disable: '停用',
      costWarning: '每次运行约发起 {count} × {samples} 个上游请求，请确认额度与成本。', deleteConfirm: '确定删除计划「{name}」及其历史记录吗？',
      runConfirmTitle: '确认开始执行测评',
      runConfirmMessage: '即将对「{name}」（{model}）发起约 {count} × {samples} 个上游请求。执行期间请勿关闭服务，结果可在「模型能力测评」页的最近执行中查看。',
      runQueued: '已开始执行：{name}，可在最近执行中查看进度。',
      runFailed: '手动执行失败：{error}',
      queued: '已排队', pageTitle: '测评计划', pageDescription: '管理模型能力测评计划：创建、启停与手动执行一次',
      loading: '加载中...', comparisonHelp: '参与比较的模型使用相同批次、独立试卷数和预算。更换批次会生成新题；重复执行同一批次主要反映稳定性。'
    },
    runs: { title: '最近执行', empty: '暂无执行记录' },
    runDetail: { title: 'Run 详情' },
    comparison: {
      apply: '应用比较条件',
      hint: '题库难度尚待真实模型校准；分数表示这些任务上的表现。当前提供112题，每增加一份独立试卷会增加112次模型请求。',
      providerBudget: '由上游管理', eligible: '可参与排名', diagnostic: '仅作诊断',
      providerReason: '上游未接受统一输出预算，保留诊断分，不混入固定预算排行。',
      coverageReason: '有效完成率或某领域覆盖不足，不参与排名。',
      legacyReason: '旧版结果缺少完整比较条件，请按新题库重新测评。',
      spread: '同批次重复执行的标准差，表示观测波动，不是置信区间'
    },
    runStatus: { running: '执行中', success: '成功', partial: '部分完成', failed: '失败', canceled: '已取消' },
    status: { enabled: '已启用', disabled: '已停用' }
  }
}
