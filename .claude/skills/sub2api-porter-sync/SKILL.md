---
name: sub2api-porter-sync
description: 检测三个平台移植源（workbuddy2api-panel / qoder2api-hub / trae-workbuddy-switch）的更新，评估哪些变更值得移植到 D:\my-sub2api，给出清单供用户选择并完成跨语言移植。用户提到“移植源更新”“检查平台源/三平台有没有更新”“qoder/trae/workbuddy 源码更新了”“跟进移植源”“平台协议是不是变了”“平台源同步”时使用。不用于 Wei-Shaw/sub2api 上游同步（那是 sub2api-upstream-sync）。
---

# 平台移植源同步（porter sync）

跟踪三个平台移植源，把值得同步的变更**按功能语义重写**进 `sub2api-so`（`D:\my-sub2api`）。源仓库是独立项目（Python / Rust / Go），二开侧是另一套 Go 网关架构，不能逐行照搬。**评估“值不值得移植”是本流程的核心**：宁可多读代码，也不要凭提交标题拍脑袋。

与 `sub2api-upstream-sync` 的区别：本 skill 只碰 workbuddy / qoder / trae 三个移植源，**永远不改 `UPSTREAM-BASE`**；上游同步只碰 Wei-Shaw/sub2api。两套基准独立推进。

## 固定路径与事实源

| 路径 | 用途 | 约束 |
|---|---|---|
| `D:\my-sub2api` | 二开仓库（master），代码改动只发生在这里 | — |
| `D:\qoder2api-hub` | Qoder 移植源（Python，main） | **只读**；仅允许 `pull --ff-only` |
| `D:\trae-workbuddy-switch` | Trae 移植源（Rust + TS，main） | **只读**；仅允许 `pull --ff-only` |
| `D:\workbuddy2api-panel` | WorkBuddy 权威源（Go，main，`misakano7545/workbuddy2api-panel`） | **只读**；仅允许 `pull --ff-only`；见下方 WorkBuddy 源说明 |
| `D:\my-sub2api\PORTER-BASE` | 各源“已评估到哪”的基准，每行 `仓库名 完整哈希` | 按仓库独立推进 |
| `D:\my-sub2api\CHANGELOG-SO.md` | 移植记录唯一登记处；含「平台移植源同步」「评估未移植」「用户明确跳过」清单 | 新记录追加到末尾 |
| `D:\my-sub2api\docs\workbuddy-hub-sync-2026-10-29.md` | WorkBuddy hub 时代的源切换与逐项差距分析 | 协议/功能面历史参考（源已于 2026-11-17 改为 panel） |
| `D:\my-sub2api\docs\platform-ops-tasks.md` | 三平台运营任务清单与状态键 | 做 ops 变更前必读 |
| `D:\my-sub2api\UPSTREAM-BASE` | 上游 Wei-Shaw/sub2api 的基准 | **禁止修改**（不属本 skill） |

### WorkBuddy 源说明（2026-11-17 起）

WorkBuddy 权威移植源已由 `workbuddy2api-hub`（Python）改为 **`workbuddy2api-panel`（Go）**，本机路径 `D:\workbuddy2api-panel`。

1. panel 是 `linguo2625469/workbuddy2api-panel` 于 2026-10-21 停更后，由 `misakano7545` 自维护的增强 fork，仍在持续更新；源语言是 Go，与二开侧路径最接近，但仍是独立实现，**按语义重写、不要整文件复制**。
2. 该克隆的 git 历史在 2026-09-12 被重新发布（根提交 `fd64aed`），与旧 panel / hub 不共享祖先。**CHANGELOG 中 2026-11-17 之前出现的 panel / hub 哈希（如 `73a7560e`、`535e19e`、`6e1e548`）在本克隆里不存在**，只能按「提交日期 + 标题」定位当时的改动，不能拿来做 diff 基准。
3. panel README 披露其 issue/PR 处置与部分代码由 AI agent 辅助完成，提交质量参差：**必须读实际代码**再下“建议/不建议”结论，不要凭提交标题判断。
4. 目录不存在或工作区有本地改动 → 停下问用户；**不要**用 `D:\workbuddy2api-panel` 之外的旧克隆或记忆替代评估，也不要声称“workbuddy 无更新”。
5. `PORTER-BASE` 的 workbuddy 行必须命名为 `workbuddy2api-panel`（脚本用目录 basename 反查基准行）；换源时同步迁移该行，否则基准查询会落空。

## 不变量

1. **检测阶段只读**：第 1–3 步对 `D:\my-sub2api` 的唯一写入是首次创建 `PORTER-BASE`；其余只允许读。
2. **用户选择后才改代码**：未选中的项不实现；“不建议”项只列在表里供用户推翻。
3. **不碰 `UPSTREAM-BASE`**，不顺手修与本次评估无关的代码。
4. 先读 `CHANGELOG-SO.md` 的「平台移植源同步」「评估未移植（原因，勿重复评估）」「用户明确跳过（不再建议）」；已被用户裁定跳过的功能**不要再提**。
5. 指纹 / 正则字面量必须**用脚本从源文件字节级提取**生成 Go 字面量，禁止人工转写（终端渲染会改写弯引号、异体连字符等）。
6. `PORTER-BASE` 三行**独立推进**：某仓库有悬而未决项就不动该行，不影响其他仓库。

## 模块映射（源 → 二开落点）

| 源 | 语言 | 重点源文件 | 二开落点 |
|---|---|---|---|
| workbuddy2api-panel | Go | 出站协议/头族 `internal/upstream/{client,headers,payload,sse}.go`；报文清洗 `sanitize.go`、工具配对 `tool_pairing.go`；模型目录 `model_catalog.go`/`model.json`；指纹 `tlsfingerprint.go`；签到/成长/活动 `tasks.go`、`checkin_activity.go`、`travel.go`、`benefit.go`、`campaign.go`、`streak*.go`；检索/图片/视频 `search.go`/`images.go`/`videos.go`；账号池与调度 `internal/pool/`、`internal/scheduler/`；面板任务 `internal/panel/` | `backend/internal/pkg/workbuddy/`、`service/workbuddy_{ops,oauth,search,usage}.go`、`service/openai_gateway_workbuddy.go` |
| qoder2api-hub | Python | `qoder_sign.py`（COSY 签名）、`qoder_proxy.py`（网关/协议转换）、`qoder_accounts.py`（账号池/OAuth/刷新）、`qoder_activity.py`、`qoder_keepalive.py`、`qoder_session_cache.py`、`qoder_fingerprint.py`、`qoder_routing.py`、`baseprompt.json`、`qoder_catalog_{intl,cn}.json` | `backend/internal/pkg/qoder/`、`service/qoder_{ops,oauth}_service.go`、`service/qoder_keepalive.go`、`service/openai_gateway_qoder.go` |
| trae-workbuddy-switch | Rust | `crates/buddy-switch-core/src/modules/trae/{oauth,payload,sse,model_list,variant,checkin,credits,jwt,device}.rs`；`crates/buddy-switch-gateway/src/trae/{payload,sse,catalog,effort,pool}.rs` | `backend/internal/pkg/trae/`、`service/trae_{ops,oauth}_service.go`、`service/openai_gateway_trae.go` |

前端侧（三平台共用）：`frontend/src/components/account/forms/{WorkBuddy,Qoder,Trae}Form.vue`、`frontend/src/components/account/DeviceAuthFlow.vue`、`frontend/src/api/admin/platformOps.ts`、`frontend/src/api/admin/ops.ts`。

以下命令片段在 **Git Bash** 中执行。

## 流程

### 0. 预检

```bash
SO=D:/my-sub2api
cat "$SO/PORTER-BASE"
git -C "$SO" status --porcelain
```

- 读 `PORTER-BASE`，确认每行仓库名与本地克隆目录 basename 一致（workbuddy 行应为 `workbuddy2api-panel`）。
- 确认 `D:\workbuddy2api-panel` 存在；不存在 → 停下问用户，不要改用它处旧克隆。
- 检查源仓库工作区是否干净；有本地改动 → 停下问用户，不要强拉。
- 读 `CHANGELOG-SO.md` 的相关章节，带上“勿重复评估”清单再开始。

### 1. 拉取并检测增量

对每个可用仓库逐个执行：

```bash
REPO=D:/qoder2api-hub          # 或 D:/trae-workbuddy-switch / D:/workbuddy2api-panel
OLD=$(git -C "$REPO" rev-parse HEAD)
git -C "$REPO" pull --ff-only origin main
NEW=$(git -C "$REPO" rev-parse HEAD)
echo "$OLD -> $NEW"
```

- `--ff-only` 失败（本地分叉）→ 停下告知用户，不要合并。
- 网络失败 → 用本地已有提交继续，并明确告知“结果可能不是最新”。
- **`PORTER-BASE` 不存在（首次运行）**：把 pull 前的本地 `HEAD` 写入，作为初始基准（语义：此前状态视为已评估）。若 CHANGELOG 中该平台最后移植的来源 commit 落后于初始基准，只需提一句“中间有未评估区间”，不展开分析。
- 对比增量：

```bash
BASE=$(awk -v r="$(basename "$REPO")" '$1==r {print $2}' "$SO/PORTER-BASE")
git -C "$REPO" log --oneline "$BASE..$NEW"
git -C "$REPO" diff --stat "$BASE..$NEW"
```

- 三仓库都无新提交 → 告知“三个移植源均无更新”（附各 HEAD 日期），**不写任何文件**，流程结束。

### 2. 分析评估（核心）

对每个有增量的仓库，按提交主题聚类（同主题合并，CI/文档/版本号类归为一条）：

```bash
git -C "$REPO" log --stat "$BASE..$NEW"
git -C "$REPO" show <commit>
```

对照模块映射表，到二开对应落点确认现状后再下结论。判断顺序：

1. **协议 / 风控层**（签名算法、请求头族、payload 骨架、SSE 解析、错误码、模型目录、设备指纹/UMID、配额接口）——最高优先。上游平台改协议，不同步就会坏。
2. **配额 / 签到 / 运营逻辑**——中优先。对照 `*_ops_service.go` 的语义差异（接口迁移、冷却策略、任务时刻/状态键），按需移植。
3. **缺陷修复**——先确认二开是否同样存在：架构不同可能天然免疫，或二开时已独立修过（查 CHANGELOG）。
4. **UI / CI / 部署 / 文档**——一般不适用（二开有自己的前端与构建链），除非缺陷落在共用逻辑里。

结论分三档：**建议移植**（协议变更、二开确实存在的缺陷）、**可选**（功能增强，看需求）、**不建议**（不适用/已有等价实现，附一句原因）。

评估时主动核对 CHANGELOG 里的跳过清单；命中“用户明确跳过”或“评估未移植”的，直接归入“不建议”，不再重复论证。

### 3. 建议清单与用户选择

产出对话内表格：

| # | 来源 | 提交 | 变更摘要 | 二开对应现状 | 评估 | 工作量 |
|---|---|---|---|---|---|---|
| 1 | qoder2api-hub | `abc1234` | … | 已读 `pkg/qoder/sse.go`：已有 X，缺 Y | 建议移植 | 中 |

- “对应现状”必须写实：列出读过的二开文件 + 结论（已有等价 / 部分实现缺 X / 无对应）。
- 工作量粗估：小（单文件局部）/ 中（跨文件或需对照重写）/ 大（新模块）。
- 用 `AskUserQuestion`（`multiSelect: true`）让用户选择“建议移植”和“可选”项；“不建议”项只保留在表格里供推翻。
- 全部项都不值得移植 → 不提问，直接报告，并按第 6 步推进基准。

### 4. 移植执行（用户选择后）

1. **先读**：`CHANGELOG-SO.md` 中该平台的历史移植记录、相关 `docs/`、源仓库该提交的完整实现。
2. **按语义重写**为 Go，落点遵循模块映射表与 `handler → service → repository` 分层，匹配既有风格。不是逐行翻译。
3. **字节级提取**指纹/正则：用脚本从源文件读取并生成 Go 字面量，再断言提取结果一致。
4. 补测试：优先放在对应 `pkg/<platform>` 的 `*_test.go`；涉及网关行为时加到 `service/openai_gateway_<platform>_test.go`。
5. 提交信息用 `[模块] 中文描述`；用户未要求时不要自行 push。

### 5. 验证

先探测 `go` / `docker` / `pnpm`。本机当前可能都没有；不可用时**明确告知“未验证”并给出待执行命令**，不要伪造结果。

```bash
cd "$SO/backend"
# 测试前临时挪走会污染 viper 的配置，测后恢复
for f in config.yaml data/config.yaml; do [ -f "$f" ] && mv "$f" "$f.porterbak"; done

gofmt -l internal/pkg/<platform> internal/service
go build ./...
go vet ./...
go test -tags=unit ./internal/pkg/<platform>/... ./internal/service/...
# 条件允许时跑全量：go test -tags=unit ./...
# 或 docker run --rm -v <宿主路径>/backend:/app -w /app golang:1.27.3 go test -tags=unit ./...

for f in config.yaml.porterbak data/config.yaml.porterbak; do [ -f "$f" ] && mv "$f" "${f%.porterbak}"; done
```

- Go 版本以 `backend/go.mod` 为准（当前 **1.27.3**），容器镜像用 `golang:1.27.3`。
- 全量单测有 6 个存量失败（见 `CLAUDE.md`）；与 HEAD 基线对比，不要把存量失败算成本次回归。
- 涉及前端：`cd "$SO/frontend" && pnpm typecheck && pnpm exec vitest run <相关 spec>`；改 lock/overrides 时用 **pnpm 9** 重生成并双写 `package.json` + `pnpm-workspace.yaml`。

### 6. 记录并推进基准

在 `CHANGELOG-SO.md` 末尾追加一条，至少包含：

- 日期、功能描述；
- **来源仓库 + commit 短哈希**（多项分别列）；
- 改动文件 / 模块；
- 验证结果；
- 是否影响与上游合并（对照「同步口径与高冲突面」；三平台文件通常为纯二开，无冲突面）；
- “评估未移植 + 原因”和“用户明确跳过”项，避免下次重复建议。

`PORTER-BASE` 推进规则：

- 某仓库本次全部建议项都有明确结论（已移植，或用户明确不移植并已记录）后，才把该行更新为**新 HEAD 完整哈希**。
- 有项悬而未决（“以后再说”）→ 该行不动，下次重新报告。
- 三行独立推进，互不阻塞。
- 手动补移植基准内已有提交的缺口时，不动基准（在 CHANGELOG 注明“基准不变”）。

## 输出格式

用中文汇报：

1. 各源仓库本次区间与提交数；
2. 建议清单（表格）与用户选择结果；
3. 已移植项：来源 commit、落点文件、关键语义；
4. 未移植/跳过项及原因；
5. 验证结果（含工具链缺失时明确说明未验证）；
6. `PORTER-BASE` 各行的最终状态（推进到哪个哈希 / 为何未动）。
