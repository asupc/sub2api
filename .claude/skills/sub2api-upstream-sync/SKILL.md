---
name: sub2api-upstream-sync
description: 把上游 Wei-Shaw/sub2api 的新提交同步进二开仓库 D:\my-sub2api，保留全部二开改动并前移版本基准。用户提到“同步上游”“上游更新/有新版本”“跟进上游”“sub2api 官方发版”“sync upstream”时使用。只处理 Wei-Shaw/sub2api；workbuddy / qoder / trae 移植源用 sub2api-porter-sync。
---

# 上游同步（Wei-Shaw/sub2api → D:\my-sub2api）

把上游 `D:\sub2api`（main）的新提交选择性同步进二开仓库 `D:\my-sub2api`（master）。目标不是“合并上游分支”，而是**逐个变更判定后落盘**：该覆盖的覆盖、该三路合并的合并、该裁剪的跳过，并把版本基准和记录一起推进。

## 固定路径

| 路径 | 用途 | 约束 |
|---|---|---|
| `D:\my-sub2api` | 二开仓库（master），所有写入只发生在这里 | — |
| `D:\sub2api` | 上游完整克隆（main） | **只读**；仅允许 `pull --ff-only` / `fetch --tags` 改工作区 |
| `D:\my-sub2api\UPSTREAM-BASE` | 当前已完整同步到的上游 commit（单行完整哈希） | 只在完整同步后前移 |
| `D:\my-sub2api\backend\cmd\server\VERSION` | 对外版本号（单行，如 `0.2.21`） | 与 `UPSTREAM-BASE` 对应的上游 tag 一致 |
| `D:\my-sub2api\backend\scripts\resolve-version.sh` | 版本推导：优先精确 tag，取不到才回落 VERSION 文件 | 构建与镜像 tag 依赖它 |
| `D:\my-sub2api\CHANGELOG-SO.md` | 二开变更唯一记录处；含裁剪范围、高冲突面、同步历史 | 新记录追加到文件末尾 |
| `D:\my-sub2api\CLAUDE.md` | 命令、构建/部署约束、永久裁剪清单 | 与本 skill 冲突时以 CLAUDE.md 为准 |

以下命令片段在 **Git Bash** 中执行（用到 `$(...)`、`tr`、`sed`）；PowerShell 需逐条换算。

## 不变量

1. **只有完整上游同步**才同时前移 `UPSTREAM-BASE` 与 `backend/cmd/server/VERSION`；二者必须对应同一个上游 tag。手动移植、热修、二开功能只记 `CHANGELOG-SO.md`，不动这两个文件。
2. **不 merge 上游分支**。按 `BASE..NEW` 逐提交/逐文件选择性同步，保留本地二开语义。
3. 所有改动（含“跳过”决定）都要追加到 `CHANGELOG-SO.md` 末尾。
4. 永久跳过：支付链路、en 语言包、前端已裁剪入口。动高冲突文件前，先读 `CHANGELOG-SO.md` 的「同步口径与高冲突面」和「前端裁剪范围」。
5. 先读后写：先看上游提交和文件，再看二开现状，最后落盘。

## 流程

### 1. 拉取上游并解析版本

```bash
SO=D:/my-sub2api
UP=D:/sub2api

git -C "$UP" pull --ff-only origin main      # 失败处理见下
git -C "$UP" fetch --tags                    # 不能省：pull 不保证取回新 tag
NEW=$(git -C "$UP" rev-parse main)
TAG=$(git -C "$UP" describe --tags --abbrev=0 --match 'v*' "$NEW")
UPVER=$(git -C "$UP" show "$NEW:backend/cmd/server/VERSION")
echo "NEW=$NEW TAG=$TAG VERSION=$UPVER"
```

- `--ff-only` 失败（本地与上游分叉）→ 停下告知用户，**不要强行 merge**。上游历史上 force push 过，先核对再决定是否 `reset --hard`。
- 网络失败 → 用本地已有提交继续，并明确告知“结果可能不是最新”。
- tag 缺失会让二进制版本号滞后一档；**`fetch --tags` 必跑**。
- `TAG` 去掉 `v` 应与 `UPVER` 一致。不一致时以 tag 为准，并在最终记录中说明（上游 CI 更新 VERSION 可能落后一档）。

### 2. 读取当前基准

```bash
BASE=$(tr -d '[:space:]' < "$SO/UPSTREAM-BASE")
git -C "$UP" describe --tags --abbrev=0 --match 'v*' "$BASE"
git -C "$UP" log --oneline "$BASE..$NEW" | head -50
```

- `UPSTREAM-BASE` 不存在 → 先查 `CHANGELOG-SO.md` 的同步历史；仍无法确定就**询问用户**。禁止猜测基准。
- 先向用户展示：上游版本区间（`BASE` 处 tag → `NEW` 处 tag）、提交数、变更文件数，再开始同步。

### 3. 盘点与逐文件分类

```bash
git -C "$UP" log --oneline --no-merges "$BASE..$NEW"
git -C "$UP" diff --stat "$BASE..$NEW"
git -C "$UP" diff --name-status "$BASE..$NEW"
```

按提交主题归纳中文摘要（新增/修复/重构分开）。发版刚结束时区间内可能只有上游 CI 的 `chore: sync VERSION to x.y.z` 一个提交，这不代表没功能——功能在打 tag 前已进 main。

**分类前统一换行符**：二开工作区可能是 CRLF，`git show BASE:路径` 是 LF；裸 diff 会把整个文件误判为“二开改过”。一律：

```bash
diff --strip-trailing-cr -u <(git -C "$UP" show "$BASE:$path" | tr -d '\r') "$SO/$path"
```

每个文件落入下表之一：

| 二开状态 | 动作 |
|---|---|
| 路径在二开已删除 / 属于裁剪清单 | 跳过，记录 `MISSING_IN_SO` |
| 二开内容与 `BASE` 版一致（去 CRLF） | 直接用 `NEW` 版覆盖（上游删除则同步删除） |
| 二开内容与 `BASE` 版不同 | 三路合并（见第 4 步） |
| 上游新增文件 | 先检查是否 import 二开已裁剪模块，无问题再复制 |
| 依赖文件（`go.mod`/`go.sum`/`package.json`/`pnpm-lock.yaml`） | 同步后在二开目录执行对应安装/整理命令 |
| `backend/cmd/server/VERSION` | 改为上游 `NEW` 的取值（完整同步时） |

### 4. 执行同步

**未改文件覆盖**：

```bash
mkdir -p "$(dirname "$SO/$path")"
git -C "$UP" show "$NEW:$path" > "$SO/$path"
```

**三路合并**（不要肉眼搬 patch）：

```bash
TMP=$(mktemp -d)
git -C "$UP" show "$BASE:$path" | tr -d '\r' > "$TMP/base"
git -C "$UP" show "$NEW:$path"  | tr -d '\r' > "$TMP/theirs"
tr -d '\r' < "$SO/$path" > "$TMP/ours"
git merge-file -p --diff3 "$TMP/ours" "$TMP/base" "$TMP/theirs" > "$TMP/out"
```

- 退出码 `0`：结果直接写回 `$SO/$path`。
- 非 `0`：输出含 `<<<<<<<`，逐处裁决——保留二开语义、融入上游变更；拿不准就列入冲突清单问用户，不要静默取舍。
- 整个上游变更都落在二开已删/已重写的功能块时，可保持二开版不动，并在记录中写明原因。

**批量脚本两个坑**：

- 不要用 `case "$path" in "$MULTI_LINE_VAR")` 匹配多行清单（永远不命中）。用目录通配或 `grep -Fx -f 清单文件` 逐行精确匹配。
- 覆盖/跳过后用 `git -C "$SO" status --porcelain` 复查变更集，确认没有误覆盖支付、en 语言包、已裁剪前端文件。
- 上游新增/修改的测试可能 import 二开已删模块（en 语言包、`ProxySelector`、`utils/pricing`、`AnnouncementBell` 等）。复制前 grep 模块名；命中就改写（删不适用用例、双语收敛为 zh 单语），或整文件跳过。

### 5. 依赖与生成代码

- Go 依赖：`cd "$SO/backend" && go mod tidy`，`go.sum` 一并提交。
- 前端依赖：`package.json` 变更后必须用 **pnpm 9** 重生成 `pnpm-lock.yaml`；`package.json#pnpm.overrides` 与 `pnpm-workspace.yaml#overrides` 必须逐项一致，否则 Docker 的 `--frozen-lockfile` 报 `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`。
- 修改了 `backend/ent/schema/` → `go generate ./ent`，生成文件一并提交。
- 修改 Wire → `go generate ./cmd/server` 后**必须 `git diff` 检查**：本分支 `wire_gen.go` 有手工接线（三平台 provider），无脑覆盖会丢失装配。

### 6. 验证

先探测工具链。本机当前可能没有 `go` / `docker` / `pnpm`；不可用时**明确告知用户“未验证”并给出待执行命令**，不要假装通过。

后端（Go 1.27.3；容器镜像 `golang:1.27.3`）：

```bash
cd "$SO/backend"
# 测试前临时挪走会污染 viper 的配置，测后恢复
for f in config.yaml data/config.yaml; do [ -f "$f" ] && mv "$f" "$f.syncbak"; done

go build ./...
go vet ./...
go test -tags=unit ./...
# 或 docker run --rm -v <宿主路径>/backend:/app -w /app golang:1.27.3 go test -tags=unit ./...

for f in config.yaml.syncbak data/config.yaml.syncbak; do [ -f "$f" ] && mv "$f" "${f%.syncbak}"; done
```

已知基线：`go test -tags=unit ./...` 有 6 个存量失败（Gemini thinking variant ×2 子例、复合分组族 ×2、antigravity 目录跳过、effort 透传遗留、prompt-audit 路由覆盖）。**与 HEAD 基线对比**，不要把存量失败算成本次回归。

前端：

```bash
cd "$SO/frontend"
pnpm typecheck
pnpm test:run
pnpm build          # 输出到 backend/internal/web/dist/
```

前端测试失败先甄别归属：上游 `NEW` 版 spec 与二开版一致却失败 → 上游改了实现没改测试，按新行为修 so 侧断言并记录上游提交；两者不一致 → 是本次合并问题，回到第 4 步。

**版本号一致性**：

```bash
git -C "$UP" describe --tags --abbrev=0 --match 'v*' "$NEW" | sed 's/^v//'   # 应为 x.y.z
cat "$SO/backend/cmd/server/VERSION"                                          # 应为同一 x.y.z
bash "$SO/backend/scripts/resolve-version.sh"                                 # 构建实际取到的版本
```

若只是上游 VERSION 文件尚未跟上 tag（上游发版机制固有滞后），按实际情况记录，不要自行改写 VERSION 去“对齐”一个不存在的上游值；若 VERSION 未同步或 tag 未取回，回到第 1/4 步修正。**不要带着滞后的 tag 继续**。

### 7. 记录并推进基准

在 `CHANGELOG-SO.md` **末尾**追加一节，建议格式：

```markdown
### 同步 vX.Y.Z（YYYY-MM-DD）

- 区间与规模：`BASE短哈希 → NEW短哈希`（N 提交 / M 文件，+A/-D）。二开侧：X 覆盖、Y 三路自动合并、Z 冲突手工裁决、W 跳过。
- 同步功能：按主题列新增/修复/重构。
- 冲突裁决：逐文件写保留的二开语义与并入的上游行为。
- 跳过项：文件/功能 + 原因。
- 测试适配：改了哪些上游测试断言及原因。
- 依赖/构建链：go、pnpm、镜像版本变化。
- 验证：后端/前端结果、与 6 项存量失败基线对比、版本号一致性。
- 合并影响：支付链路与 en 语言包零变更的复核结果。
```

然后用 `NEW` 的**完整哈希**覆写 `UPSTREAM-BASE`，并把 `backend/cmd/server/VERSION` 写成上游对应值。两件事必须同批完成。提交信息用 `[同步] 上游 vX.Y → vX.Z（要点）`；用户未要求时不要自行 commit/push。

## 已知坑点

- **CRLF 假差异**：分类与合并前统一 `tr -d '\r'` / `--strip-trailing-cr`。
- **多行变量 case 匹配**：见第 4 步，改用 `grep -Fx -f`。
- **测试 import 已裁模块**：复制上游测试前 grep en 包、`ProxySelector`、`utils/pricing`、`AnnouncementBell` 等。
- **配置污染测试**：`backend/config.yaml`、`backend/data/config.yaml` 会让 viper 读错配置，测试前后挪走/恢复。
- **pnpm overrides 双写**：`package.json` 与 `pnpm-workspace.yaml` 同步改，lock 用 pnpm 9 重生成。
- **Wire 手工接线**：重新生成后必看 diff。
- **ent 生成代码**：不要手改 `backend/ent/`，改 schema 后 `go generate`。
- **上游 force push**：`--ff-only` 失败不要硬合，先核对历史再决定 reset。
- **支付/en 零变更复核**：同步后用 `git status --porcelain` 检查，避免批量脚本误覆盖。

## 输出格式

同步完成后用中文汇报：

1. 上游版本区间（旧 tag/VERSION → 新 tag/VERSION）与新基准完整哈希；
2. 同步的功能点；
3. 冲突与跳过项及原因；
4. 验证结果（含 6 项存量失败基线对比、版本号一致性）；
5. 未执行的验证及原因（工具链缺失、环境限制等）。
