# 账号管理 UI 视觉规范与排版规格说明书

本文档系统记录「账号管理」模块在紧凑高屏效重构后的完整视觉设计规范，涵盖**外围视口留白、查询条件栏、操作工具栏、表格表头与表体、单元格微胶囊排版、内嵌式分页底栏**以及**全局递进式圆角与字号层级规范**。

---

## 1. 核心设计理念与对齐基准

在重构前，界面存在明显的「大头娃娃」与上下层割裂现象：搜索框与下拉器高达 42px（14px 字号），而表格数据行和表头却已被压缩至紧凑高屏效风格，且存在 16px 甚至更大的气泡圆角。

本次重构确立了**三大硬性对齐基准**：
1. **32px 控件绝对基准线**：工具栏与查询栏中所有的输入框（Input）、下拉选择器（Select）、常规操作按钮（Button）高度严格锁定为 **32px (`h-8`)**，字号统一为 **12px (`text-xs`)**；
2. **现代 SaaS 递进式圆角系统**：**容器 12px → 控件 8px → 内部微徽章 6px/4px**，杜绝大圆角泡泡感；
3. **100% 视口无溢出闭合**：外围 padding 统一为 `1rem` (16px)，容器高度动态闭合，确保全屏展现无整页纵向滚动。

---

## 2. 外围视口与主容器规范 (Main & Viewport)

| 区域 / 元素 | 类名 / 样式规则 | 尺寸数值 | 设计意图 |
|---|---|---|---|
| **`<main>` 内容区域** | `p-4` | 上下左右各 **`1rem` (16px)** | 消除桌面端原先 `lg:p-8` (32px) 的巨大留白浪费，将有效可视宽度与高度最大化留给业务数据 |
| **页面布局组件** | `TablePageLayout` | `height: calc(100vh - 64px - 2rem)` | 减去顶栏 64px + main 上下各 1rem (2rem)，让表格卡片精准撑满视口，绝不产生浏览器窗口滚动条 |
| **紧凑外层间距** | `.table-page-layout.is-dense` | `gap-2.5 sm:gap-3` (10px~12px) | 压缩查询栏与表格卡片之间的纵向缝隙 |

---

## 3. 查询条件与工具栏规格 (Filters & Toolbar)

查询栏位于表格上方，采用 `items-center` 垂直居中对齐，形成一条干净利落的 32px 水平视觉中轴线。

### 3.1 查询条件输入框 (`SearchInput.vue`)
- **规格等级**：`size="sm"`
- **外框尺寸**：高度 **32px (`h-8`)**，宽度在移动端为 `w-full`，桌面端收拢为 **`sm:w-56` (224px)**
- **内边距与字号**：`input-sm pl-8 text-xs`（左内边距 32px 容纳小图标，字号 12px）
- **圆角**：`rounded-lg` (8px)
- **搜索图标**：`size="sm"` (16px)，左侧绝对居中偏移 `pl-2.5` (10px)

### 3.2 筛选下拉框 (`Select.vue`)
- **规格等级**：`size="sm"`
- **触发器尺寸**：高度 **32px (`h-8`, `min-h-[32px]`)**
- **各下拉框宽度规划**：
  - 平台筛选（Platform）：`w-32` (128px)
  - 状态筛选（Status）：`w-32` (128px)
  - 分组筛选（Group）：`w-36` (144px)
- **内边距与字号**：`px-2.5 py-1.5 text-xs` (12px)
- **圆角**：`rounded-lg` (8px)
- **图标**：
  - 下拉箭头（chevronDown）：`size="sm"` (16px)
  - 清空图标（x）：`size="xs"` (12px)
- **自适应下拉菜单 (`.select-dropdown-sm`)**：
  - 最小宽度：由默认的 200px 缩减为 **`140px`**，避免窄下拉框展开超大浮层；
  - 菜单项：`px-3 py-1.5 text-xs` (12px)；
  - 菜单圆角：`rounded-lg` (8px)。

### 3.3 工具栏与操作按钮
- **「新建账号」按钮**：`btn btn-primary btn-sm`
  - 高度：**32px (`h-8`)**
  - 内边距与字号：`px-3 py-1.5 text-xs font-medium`
  - 圆角：`rounded-lg` (8px)
- **「更多操作」按钮**：`btn btn-secondary btn-sm px-2.5`
  - 高度：**32px (`h-8`)**
  - 图标：`more` (16px) + `chevronDown` (12px)
  - 圆角：`rounded-lg` (8px)
- **更多操作浮动菜单**：
  - 菜单外框：`rounded-lg border shadow-xl`
  - 分组标题：`text-[11px] font-semibold uppercase tracking-wide`
  - 菜单项 (`.account-tools-menu-item`)：`px-2.5 py-1.5 text-xs gap-2.5 rounded-md`
  - 图标容器 (`.account-tools-menu-icon`)：`h-7 w-7 rounded-md`
  - 列展示勾选列表：`px-2.5 py-1.5 text-xs rounded-md`

---

## 4. 表格数据与单元格呈现规范 (Table & Cells)

### 4.1 表格卡片容器 (`.table-scroll-container`)
- **圆角**：从原来的 `rounded-2xl` (16px) 优化为 **`rounded-xl` (12px)**，消除了大圆角的浮肿感，更显专业 SaaS 质感；
- **边框与阴影**：`border border-gray-200 dark:border-dark-700 shadow-sm`。

### 4.2 表头 (`thead th`)
- **字号与字重**：**`text-[11px] font-semibold uppercase tracking-wider`**；
- **垂直内边距**：`py-1.5`，表头整体高度紧凑维持在 **约 28px**；
- **水平内边距**：`px-3`；
- **背景色**：半透明毛玻璃效果 `bg-gray-50/80 dark:bg-dark-800/80 backdrop-blur-sm`。

### 4.3 表体数据行 (`tbody td`)
- **字号**：**`text-xs` (12px)**；
- **行高与内边距**：标准数据行垂直内边距 `py-1.5`，行高控制在 **32px ~ 36px**；
- **数字格式**：全面启用等宽字体 `tabular-nums font-mono`，确保垂直对齐整齐划一；
- **行操作菜单 (`AccountActionMenu.vue`)**：
  - 外框圆角：`rounded-lg` (8px)；
  - 菜单项：`px-3.5 py-1.5 text-xs` (12px)，间距利落敏捷。

### 4.4 用量窗口（上游 v0.2.15 原版样式）
2026-10-13 起用量窗口列视觉与交互回归上游原版，放弃微胶囊/fixed-grid 栅格与 238px 定宽：
- **容器排版**：`space-y-1` 纵排，每窗口一条 `UsageProgressBar`，列宽由表格自动分配；
- **进度条行 (`UsageProgressBar`)**：彩底标签徽章（`w-[32px]` 定宽居中，indigo/emerald/purple/amber 浅色系）+ `h-1.5 w-8 rounded-full bg-gray-200` 细进度条 + `w-[32px]` 右对齐百分比 + `text-[10px] text-gray-400` 重置倒计时；填充色按 75/90 阈值绿/琥珀/红分级；
- **窗口统计灰胶囊行**：有 `windowStats` 且有用量时渲染在对应 bar 上方——每项 `rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800`（req/tokens/A$/U$，`text-[9px]`）；anthropic 仅 5h 窗口传入（上游原生行为）；
- **操作按钮行**：主动查询（anthropic 经 `ClaudeResetCreditsCell`、OpenAI 经 `OpenAIQuotaResetCell` 的 `#pre-actions` slot）为蓝色文字按钮（`text-blue-600 hover:bg-blue-50 text-[10px]`），与相关按钮同排；
- **Key 账号今日统计**：与统计行同款灰胶囊（`rounded bg-gray-100 px-1.5 py-0.5`，数据来自 `todayStats` prop）。

---

## 5. 一体化内嵌分页组件规范 (Pagination)

分页组件不再是游离于表格底部的独立卡片，而是作为 `.table-scroll-container` 内部底栏一体化内嵌。

```
┌──────────────────────────────────────────────────────────┐
│  .table-scroll-container (rounded-xl, border)            │
│  ┌────────────────────────────────────────────────────┐  │
│  │ <thead> (text-[11px], py-1.5)                      │  │
│  ├────────────────────────────────────────────────────┤  │
│  │ <tbody> (text-xs, py-1.5, tabular-nums)            │  │
│  ├────────────────────────────────────────────────────┤  │
│  │ .table-card-footer (border-t, bg-slate-50/70)      │  │
│  │   ● 共 128 条 · 第 1-20 条 | 每页 20 条 ▾    < 1 2 3 > │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

| 元素 | 紧凑模式 (`is-dense`) 规则 | 说明 |
|---|---|---|
| **外层底栏容器** | `px-2.5 py-0.5 min-h-[28px]` | 边框 `border-t`，整体高度仅 **26px ~ 28px**，将 98% 的纵向高度全归还给数据 |
| **左侧统计流** | `text-[11px] font-medium text-slate-500` | 单行流式文本展示 `● 共 X 条 · 第 A-B 条`，带青绿色呼吸圆点 |
| **每页条数下拉** | `h-6 min-h-0 px-2 py-0.5 text-xs rounded-md` | 嵌入式微型下拉框，无多余阴影 |
| **翻页页码按键** | `h-6 min-w-[24px] px-1 text-[11px] rounded-md` | 紧凑按键，带 `active:scale-95` 弹性微触感 |
| **当前激活页** | `bg-teal-500 text-white font-semibold shadow-xs` | 极客青绿高亮，醒目且不刺眼 |
| **未激活页** | `text-slate-600 hover:bg-slate-100 dark:hover:bg-dark-700` | 扁平 Ghost 态，减少界面视觉噪音 |
| **跳页输入框** | `h-6 w-9 text-[11px] px-1 text-center rounded-md` | 紧凑等宽数字输入框 |

---

## 6. 全局递进式圆角设计系统 (Radius System)

为避免不同层级元素圆角大小冲突，全模块严格遵循以下 3 级递进梯队：

```
Level 1: 容器层 (12px / rounded-xl)
 └── Level 2: 控件层 (8px / rounded-lg)
      └── Level 3: 内嵌层 (6px / rounded-md 或 4px / rounded)
```

1. **L1 容器层 (`rounded-xl` = 12px)**：
   - 表格卡片滚动容器 (`.table-scroll-container`)
   - 仪表盘与各页面主卡片 (`.card`)
2. **L2 交互控件层 (`rounded-lg` = 8px)**：
   - 搜索输入框 (`SearchInput`)
   - 下拉选择框触发器与下拉浮层 (`Select`, `select-dropdown-portal`)
   - 操作按钮 (`btn-sm`, `btn-primary`, `btn-secondary`)
   - 浮动工具菜单面板 (`accountToolsDropdown`)
3. **L3 嵌入微元素层 (`rounded-md` = 6px 或 `rounded` = 4px)**：
   - 菜单子选项 (`.account-tools-menu-item`, 列展示按钮)
   - 用量微胶囊边框卡片 (`UsageProgressBar`)
   - 分页按钮与微型输入框 (`Pagination` 按钮、跳页框)
   - 微型刷新/探测操作按钮 (`h-5` 迷你键)
   - 勾选复选框 (`checkbox`) 与状态指示点

---

## 7. 尺寸与字号速查对照表

| 界面构件 | 高度 / 行高 | 字号 (Font Size) | 内边距 (Padding) | 圆角 (Radius) |
|---|---|---|---|---|
| **`<main>` 外围留白** | — | — | `1rem` (16px) | — |
| **搜索框 (`SearchInput`)** | 32px (`h-8`) | 12px (`text-xs`) | `pl-8 pr-3 py-1.5` | 8px (`rounded-lg`) |
| **下拉选择器 (`Select`)** | 32px (`h-8`) | 12px (`text-xs`) | `px-2.5 py-1.5` | 8px (`rounded-lg`) |
| **常规操作按钮 (`btn-sm`)** | 32px (`h-8`) | 12px (`text-xs`) | `px-3 py-1.5` | 8px (`rounded-lg`) |
| **表格表头 (`th`)** | ~28px | 11px (`text-[11px]`) | `px-3 py-1.5` | — |
| **表格数据行 (`td`)** | ~32px-36px | 12px (`text-xs`) | `px-3 py-1.5` | — |
| **用量微胶囊 (`Usage`)** | ~22px | 11px (`text-[11px]`) | `px-1.5 py-0.5` | 6px (`rounded-md`) |
| **行内探测/查询微按钮** | 20px (`h-5`) | 10px (`text-[10px]`) | `px-1.5 py-0` | 4px (`rounded`) |
| **分页底栏 (`Pagination`)** | 26px-28px | 11px (`text-[11px]`) | `px-2.5 py-0.5` | 12px（由底栏整体包含） |
| **分页页码按钮** | 24px (`h-6`) | 11px (`text-[11px]`) | `px-1` | 6px (`rounded-md`) |
