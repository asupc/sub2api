export default {
  modelManifest: {
    title: '模型清单',
    description: '透视模型与账号的供给关系，定位断供缺口与模型改写规则',
    loadFailed: '加载模型清单失败',
    refresh: '刷新',
    searchPlaceholder: '搜索模型 ID、账号名称...',
    tabs: {
      byModel: '按模型透视 (谁在提供)',
      byAccount: '按账号透视 (提供哪些模型)'
    },
    stats: {
      totalModels: '系统收录模型',
      acrossPlatforms: '跨 {count} 个平台',
      healthyModels: '健康供给模型',
      healthyHint: '至少有 1 个可调度账号在线',
      dangerModels: '断供告警模型',
      dangerHint: '当前无任何可调度账号能响应',
      mountedAccounts: '挂载账号',
      totalConcurrency: '可调度并发池合计 {count}'
    },
    statusAll: '所有状态',
    statusHealthy: '仅看健康在线',
    statusDanger: '仅看断供告警',
    platformLabel: '平台：',
    platformAll: '全部',
    availableAccounts: '可用 / 总账号',
    concurrencyPool: '可用并发池',
    benchmark: {
      label: '最近测试分数',
      untested: '未测评',
      tooltip: '最近一次模型能力测评的能力分（{date}，套件 {version}）',
      tooltipNoVersion: '最近一次模型能力测评的能力分（{date}）',
      sortLabel: '排序：',
      sortScore: '按最近分数',
      sortDefault: '默认排序',
      scoreHint: '按最近一次测评能力分降序，未测评的排在最后'
    },
    healthyTag: '正常供应',
    dangerTag: '断供告警',
    dangerAlert: '严重警报：当前所有挂载此模型的账号均不可调度，客户请求该模型将无法得到响应！',
    directPassThrough: '原生直通',
    copyId: '复制模型 ID',
    copySuccess: '已复制：{id}',
    copyFailed: '复制失败，请手动复制',
    configure: '配置',
    empty: '没有匹配到相关的模型，请尝试更换筛选条件',
    modelCountValue: '{count} 个模型',
    accountStates: {
      rateLimited: '速率限制中',
      active: '正常',
      error: '异常错误',
      disabled: '已停用'
    },
    columns: {
      account: '账号 ID 与名称',
      type: '认证类型',
      status: '账号状态',
      concurrency: '调度并发',
      priority: '优先级',
      groups: '已挂载分组',
      target: '目标映射',
      actions: '操作',
      modelCount: '提供模型数',
      models: '支持的模型列表'
    }
  }
}
