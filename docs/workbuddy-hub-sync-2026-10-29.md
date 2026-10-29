# WorkBuddy 移植源切换 workbuddy2api-hub：全面对照差距分析与实施记录

- 日期：2026-10-29
- 范围：workbuddy 平台集成（`backend/internal/pkg/workbuddy/`、`service/openai_gateway_workbuddy*.go`、`service/workbuddy_*`、运营任务链路）
- 移植源：`/vol2/1000/code/workbuddy2api-hub`（Python，ardeyouxipianyi/workbuddy2api-hub，v1.9.0 @ `a6a967e`）
- 原移植源：workbuddy2api-panel（Go，`73a7560e`，2026-10-21 起停止维护，仓库已移至 `/vol2/1000/code/workbuddy2api-panel/original/`）
- 用户拍板：**全面对照移植**（含运营行为更新与其余全部差异）；**PORTER-BASE workbuddy 行换成 hub**。
- 维持既有用户裁定（2026-10-22，"不再建议"）：sanitize 品牌词缩减（`DeepSeek Harness`→`DeepSeek`）与积分到期取最早**继续跳过**。

## 一、对照方法

以 hub 的 CHANGELOG（v1.4.5→v1.9.0）+ 代码为清单，逐条对照 sub2api-so 现有 Go 实现
（`pkg/workbuddy` 7926 行 + 网关服务层），分为：已有等价 / 待移植 / 维持不移植（原因）。
hub 的看板、用量聚合、更新器、便携包、OpenRouter 估价、本地 webtools 等纯面板/部署功能
属「自有等价」（sub2api 有自己的管理台/统计/部署形态），不列入对照。

## 二、待移植清单（本次实施）

### 批次 1：出站协议保活（pkg/workbuddy 请求体层）

| # | hub 来源 | 内容 | 现状 |
|---|---------|------|------|
| 1 | #533/#537/#539/#550/#574 | 通用身份句正则（`You are … powered by/built on/built by/created by/developed by/made by/trained by`，冠词后 0~3 修饰词、名词可省），整句替换为 `You are a helpful AI assistant.`；作用域 system/developer/assistant（#574：assistant 历史同句稳定 400） | Go 只有 5 条固定句改写，无通用正则、无作用域拆分 |
| 2 | #579（v1.7.25） | DSH GUI 描述段：`… whose URL the runtime context names.` → `… whose URL is named in the runtime context.`（只调句尾语序，保留 `DeepSeek Harness` 全名） | 无（grep `DSH` 零命中） |
| 3 | #683（835da5a，2026-10-28） | DSH 文件引用段：`Tokens prefixed with @ are paths the user explicitly referenced.` → `…paths that the user explicitly referenced.`（负向前瞻幂等） | 无 |
| 4 | #62（v1.5.9） | OmO 子代理指纹：`Sisyphus-Junior - Focused executor from OhMyOpenCode` → `Sisyphus-Junior - Focused executor`（全角色） | 无 |
| 5 | #446 | `tool_calls[].function.arguments` 非字符串 → JSON 字符串（上游 Go struct 只认 string，dict 触发 11101）；null → `{}`；args 用 identity=False 清洗 | Go 只处理 string 形态 |
| 6 | v1.5.7（#57） | `tool_choice="none"` **保留 tools/functions 声明**（删声明会让模型把调用降级成 DSML 文本、Agent 原地空转；上游不遵守 none 也是走正常 tool_calls 通道更好） | Go 删 tools（旧行为） |
| 7 | v1.7.0（#…stop） | `normalize_stop`：单字符串 → 单元素数组；数组过滤空串/非串；空值删字段（上游 `[]string`，字符串触发 11101） | 无 |
| 8 | #451（11129） | 函数参数 Schema 规整 `_normalize_tool_parameters` + `_deep_normalize_schema`：补 `type:"object"` / `properties:{}`、required 清洗、类型别名（str/int/dict…）归一、null 元数据字段剔除、items/`$defs`/组合关键字递归；`normalize_tools`：`functions`→`tools` 转换、顶层 name 工具包装 | 无（仅 Grok 通道有局部修复） |
| 9 | v1.4.9/#208 | `merge_adjacent_tool_calls`（相邻 assistant 拆条合并）+ `repack_tool_result_blocks`（结果块移回所属批次，含「插队消息移到整批结果之后」#420 修复）+ `drop_truncated_tool_calls`（半截 JSON 参数的调用剔除） | Go 只有孤儿清理；合并/repack 原「评估未移植」（deepseek-only），hub 后续实测证明 DeepSeek 主力模型确会产出拆条/插队报文（#420 现场），重新评估为**移植** |
| 10 | #121（v1.6.11） | `max_tokens` 缺省补目录 `maxOutputTokens`（Codex Responses 缺省触发上游默认 32k 截断） | 无（仅 max_completion_tokens 翻译） |

### 批次 2：出站身份头与档位

| # | hub 来源 | 内容 | 现状 |
|---|---------|------|------|
| 11 | v1.5.0 | `X-Product: SaaS`（官方值；`WorkBuddy` 为自创组合） | Go 发 `WorkBuddy` |
| 12 | #554（v1.7.19） | 版本常量更新：CN 桌面 5.8.3 / CLI 2.161.0（2026-10-18 Go 版实跑对齐）等，见 `wb_identity.py` | Go 5.5.4 / 2.137.1 |
| 13 | #516 | `X-Private-Data` 数据用途声明可配置（auto/true/false） | 无（未移植 config 开关） |
| 14 | #643（d1a8913） | 桌面 chat 可选协议一致性 `WB_DESKTOP_CHAT_COMPAT=1`：桌面 chat 去浏览器头、补 SDK runtime 头（x-stainless-*）+ B3 parent span；计费 REST/CLI/VSCode 不变；不伪造 device token | 无 |
| 15 | #542（v1.4.9 补全） | effort 档位映射：目录 `canDisableThinking:false` 时 off/none 回落默认档；缺档补目录默认（deepseek 系 thinking 注入带 effort） | Go 仅 global 静态表降级 |

### 批次 3：错误语义与账号处置

| # | hub 来源 | 内容 | 现状 |
|---|---------|------|------|
| 16 | #499 | 模型级 5xx 不熔断整池：5xx body 带业务码 11102/11129/11134/11150/11158 时不罚号（不记 fails/熔断），上游 500 原样透传 | 5xx 一律 KindServer 走通用 failover |
| 17 | #440/#691 | 401 判定收紧：仅 JSON 凭证失效才标 token 失效（中间层 HTML 401 不罚号）；认网关 `WWW-Authenticate` 的 401 为真失效 | 服务层 401/403 一律强制 refresh |
| 18 | #630 | 成功请求解除凭证失效标记（note_success 清 token_expired / session strike 归零） | 部分（签到/余额成功自动恢复）；请求成功不归零 session strike |
| 19 | #288 | 重试循环 5xx 从固定清单放宽为一切 >= 500 | 待核对 failover 链 |

### 批次 4：运营任务行为

| # | hub 来源 | 内容 | 现状 |
|---|---------|------|------|
| 20 | #596 | 签到业务失败不记成功（`200 + code 10001 +「请求处理中，请勿重复操作」`等明确失败回执判失败） | 待核对 |
| 21 | #505（v1.7.27） | 账号档位读计费摘要 `/billing/meter/get-user-resource-summary`（IsPaidUser/SubscriptionPackageName…），失败不写结论 | billing.go 只解析资源/积分 |
| 22 | #649/#645/#623/#635 等 | 国际版每日活跃：11140 持久化停止重试、证书失败降级 `/v2/report`、网页会话跑完即释放（10 条上限清理）、预算+候选序列 | 依现有 ops 实现核对后补差异 |
| 23 | #600 | Token 保活提前量 48h（默认 2h 远小于巡检间隔） | 待核对 |
| 24 | #507 | 成长任务手动领取开关（auto_claim） | 现有自动推进+领奖 |

## 三、维持不移植（含原因，勿重复评估）

- **sanitize 品牌词缩减、积分到期取最早**：2026-10-22 用户明确跳过（"不再建议"）。
- 既有「评估未移植」清单继续有效（CHANGELOG-SO.md 平台移植源同步节）：models.hidden、裸模型名跨域回退、软冷却 reason/6004 双时钟/缓存告警、按地区轮询/DNS/ProxyFromEnvironment/手机端/账号备注/积分历史、企业判定面板徽标等。
- hub 新增同类项：看板/用量聚合/性能优化全家桶（checkpoint/增量折叠/热力图等）、更新器（便携包/systemd/外部执行者）、OpenRouter 估价、本地 webtools 多引擎、API Key 白名单/并发、账号权重/备注/静态 ck_ Key 账号、跨出口回退、/u/<ref> 定址、限额护栏矩阵（自有平台额度体系）、`/health` 可用数、`/u/` 与余额汇总端点——均有 sub2api 自有等价或与自有调度/Key/计费体系冲突。
- 模型目录实时拉取 /v3/config 过滤规则（v1.6.8/#618/#625）：sub2api 已有 `upstream_models.go` 三平台分支与 model_mapping 体系，目录策略维持自有；仅移植 maxOutputTokens 缺省（#10）。

## 四、实施记录（2026-10-29）

### 已完成（本次提交）

- **批次 1 出站协议保活**（pkg/workbuddy）：
  - `sanitize.go` 重写：作用域拆分（身份句族仅 system/developer/assistant）+ 通用身份句正则（7 动词）+ DSH GUI 段 + DSH 文件引用段 + OmO 指纹 + arguments 序列化（字面量由脚本从源文件字节级提取）；
  - `tool_choice="none"` 保留工具声明（`client.go normalizeToolChoice`）；
  - 新增 `tool_schema.go`：normalizeTools / Schema 递归规整（11129）/ normalizeStop（11101）；
  - `tool_pairing.go` 新增 MergeAdjacentToolCalls / RepackToolResultBlocks，配对修复改三段链 `RepairToolPairing`（merge → repack → cleanup）；
  - `service`：max_tokens 缺省补目录 maxOutputTokens + effort 档位映射（`workbuddyApplyCatalogDefaults`）。
- **批次 2 出站身份头**：X-Product=​`SaaS`；版本常量 realm 化（CN 5.8.3 / global 5.8.4 / CLI 2.161.0）；global UA 平台段 `workbuddy-ai`；X-Private-Data（WB_PRIVATE_DATA 可覆盖）；桌面 chat 兼容档 WB_DESKTOP_CHAT_COMPAT（去浏览器头 + x-stainless-* + B3 parent/b3/traceparent）。
- **批次 3 错误语义**：模型级 5xx 不熔断（`IsModelScopedServerError` → KindModelBlocked 分流）；401 真失效判定（`workbuddyRealCredentialFailure`：WWW-Authenticate / JSON 体才算）；成功清零 session strike（`clearWorkbuddySessionStrikes`）；5xx failover >=500 核实自有等价。
- **批次 4 运营核实**：#596/#505/#600/#507 逐项裁定（已有等价/自有体系/无对应需求）。

验证：`go test -tags=unit ./internal/pkg/workbuddy/` 全绿；`./internal/service/` 仅剩 6 个已知存量失败（与 HEAD 基线一致，见 CHANGELOG-SO 验证基线行）。

### 待续（后续批次）

- **国际版每日活跃网页会话通道**（hub wb_webagent ACP 驱动 + 预算候选序列 + #649 11140 持久化 / #645 证书降级 / #623 会话释放）：独立大功能，需自建 ACP 会话驱动层；
- canDisableThinking 回落（依赖上游目录字段，随模型目录快照体系接入）；
- 产品三身份（WB/VSC/CLI）切换与 VSCode/CLI 头族（hub v1.5.5，需与端点/UA/会话语义一并评估）；
- 静态 API Key（ck_/pt_）双头、企业头族（X-Enterprise-Id/X-Tenant-Id/X-Department-Info）已有部分透传，待全面对照补齐。

### 工作方法记录

- 工作区工具输出对特殊字符存在**渲染失真**（指纹串里的弯引号/异体连字符会被显示成 ASCII）：所有上游指纹字面量必须由脚本从源文件字节级提取（node 读文件 + JSON.stringify 生成 Go 字面量），禁止人工转写；校验用长度/哈希/布尔断言而非肉眼。
- RE2 无 lookahead：hub 正则的环视改捕获组（`$2` 接回分隔符）或直接删除（幂等性不变）；Go 替换串 `$1that` 会被解析为组名 `1that`，需写 `${1}that`。
