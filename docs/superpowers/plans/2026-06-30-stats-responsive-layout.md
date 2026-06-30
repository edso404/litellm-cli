# TUI 响应式布局修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 stats 视图和 logs 详情视图在极扁窗口下 header/counter 被挤出屏幕的问题

**Architecture:** 采用动态高度传递方案 - 每个渲染函数返回实际占用行数，上游函数根据剩余高度动态调整。stats 视图先改，logs 详情视图后验证。

**Tech Stack:** Go, BubbleTea, lipgloss

---

## 文件结构

- 修改: `internal/tui/stats/model.go` - View() 方法和 renderCounterContent()
- 修改: `internal/tui/logs/model.go` - 详情视图高度处理（验证是否需要修改）
- 测试: 手动测试各种窗口尺寸

---

## 任务 1: 修改 stats/model.go - View() 方法

**Files:**
- Modify: `internal/tui/stats/model.go:223-300` (View 方法)

- [ ] **Step 1: 添加最小高度保护常量**

在文件顶部添加常量定义（在 `TimeRangeWeek` 等常量附近）:
```go
const (
    MinWindowHeight     = 10  // 最小窗口高度
    MinBarHeight        = 3   // 最小柱状图高度
    CounterHeaderLines  = 2   // header 占用行数
    CounterFooterLines  = 1   // footer 占用行数
)
```

- [ ] **Step 2: 修改 View() 方法中的高度计算逻辑**

找到当前代码（约 line 273-295）:
```go
// 固定行数：当 showHeader=true 时额外占用 header(2) 和 footer(1)
// 在 dashboard 中运行时：时间选择器(1) + 卡片(2) + 分隔线(1, 大屏) = 4-5 行
fixedLines := 1 + 2 + 1 // 时间选择器(1) + 卡片(2) + 分隔线(1)
if !isLargeScreen {
    fixedLines--
}
if m.showHeader {
    fixedLines += 3 // header(2) + footer(1)
}
maxBarLines := m.height - fixedLines
if maxBarLines < 3 {
    maxBarLines = 3
}
```

替换为:
```go
// 动态计算可用高度
availableHeight := m.height
if m.showHeader {
    availableHeight -= CounterHeaderLines + CounterFooterLines // header(2) + footer(1)
}
if availableHeight < MinWindowHeight {
    availableHeight = MinWindowHeight
}

// 时间选择器固定 1 行，分隔线 1 行
fixedContentLines := 1 + 1 // 时间选择器(1) + 分隔线(1)
if !isLargeScreen {
    fixedContentLines-- // 窄屏无分隔线
}

// 计算 counter 最大行数（基于列数和指标数）
// 6 个指标: 3列=2行, 2列=3行, 1列=6行
metricsCount := 6
cols := 3
if counterWidth < 80 {
    cols = 2
}
if counterWidth < 40 {
    cols = 1
}
counterMaxRows := (metricsCount + cols - 1) / cols // 向上取整

// counter 最大可用高度 = 可用高度 - 时间选择器 - 分隔线 - 最小 bar 高度
counterMaxHeight := availableHeight - fixedContentLines - MinBarHeight
if counterMaxRows > counterMaxHeight {
    counterMaxRows = counterMaxHeight
}
if counterMaxRows < 1 {
    counterMaxRows = 1
}

// 传递 maxRows 给 counter 渲染，获取实际渲染行数
counterActualRows := m.renderCounterContent(counterWidth, counterMaxRows)

// 计算 bar 可用高度
barAvailableHeight := availableHeight - fixedContentLines - counterActualRows
if barAvailableHeight < MinBarHeight {
    barAvailableHeight = MinBarHeight
}
```

- [ ] **Step 3: 修改 renderBarContent 调用**

找到当前调用（约 line 297）:
```go
sb.WriteString(m.renderBarContent(barWidth, maxBarLines))
```

替换为:
```go
sb.WriteString(m.renderBarContent(barWidth, barAvailableHeight))
```

- [ ] **Step 4: 提交更改**

```bash
git add internal/tui/stats/model.go
git commit -m "fix(stats): 添加响应式布局动态高度计算"
```

---

## 任务 2: 修改 renderCounterContent() 支持动态行数

**Files:**
- Modify: `internal/tui/stats/model.go:370-430` (renderCounterContent 方法)

- [ ] **Step 1: 修改函数签名**

找到当前函数签名（约 line 370）:
```go
func (m *Model) renderCounterContent(width int) string {
```

替换为:
```go
func (m *Model) renderCounterContent(width int, maxRows int) int {
```

- [ ] **Step 2: 在函数开头添加高度限制逻辑**

在 `metrics` 数组定义之后（约 line 398），添加:
```go
    // 根据 maxRows 限制实际渲染的指标数量
    visibleMetrics := metrics
    if len(visibleMetrics) > maxRows * cols {
        visibleMetrics = metrics[:maxRows*cols]
    }
    
    actualRows := (len(visibleMetrics) + cols - 1) / cols
    if actualRows < 1 {
        actualRows = 1
    }
```

- [ ] **Step 3: 修改渲染循环使用 visibleMetrics**

找到循环（约 line 419）:
```go
    for row := 0; row < len(metrics); row += cols {
```

替换为:
```go
    for row := 0; row < len(visibleMetrics); row += cols {
```

并在循环内（约 line 424）:
```go
        for col := 0; col < cols && row+col < len(metrics); col++ {
            metric := metrics[row+col]
```

替换为:
```go
        for col := 0; col < cols && row+col < len(visibleMetrics); col++ {
            metric := visibleMetrics[row+col]
```

- [ ] **Step 4: 修改返回语句**

找到返回语句（约 line 433）:
```go
    return sb.String()
```

替换为:
```go
    return actualRows
```

- [ ] **Step 5: 提交更改**

```bash
git add internal/tui/stats/model.go
git commit -m "fix(stats): renderCounterContent 支持动态行数限制"
```

---

## 任务 3: 验证 logs 详情视图

**Files:**
- Modify: `internal/tui/logs/model.go` (验证是否需要修改)

- [ ] **Step 1: 分析 logs 详情视图的高度处理逻辑**

检查 `internal/tui/logs/model.go` 中:
- `renderDetailView()` 函数（约 line 726）
- `availableLines := m.height - 4` 使用处（约 line 928）
- `maxDisplayLines := m.height - headerLines - 1` 使用处（约 line 938）

检查结果:
1. 如果代码已经正确处理了最小高度保护，则**不需要修改**
2. 如果 `m.height` 可能小于 15 导致问题，则需要添加保护

- [ ] **Step 2: 如需修改，添加最小高度保护**

如果发现问题，在 View() 或 renderDetailView() 中添加:
```go
if m.height < 15 {
    // 极简布局：只显示核心信息
    return m.renderDetailViewMinimal()
}
```

- [ ] **Step 3: 提交（如有修改）**

```bash
git add internal/tui/logs/model.go
git commit -m "fix(logs): 添加详情视图最小高度保护"
```

---

## 任务 4: 全面测试

**Files:**
- 测试: 手动测试各种窗口尺寸

- [ ] **Step 1: 测试正常窗口 (120x40)**

运行:
```bash
go run . stats
```
- 验证 header 正常显示
- 验证 counter 卡片 3 列布局
- 验证 bar chart 正常显示

- [ ] **Step 2: 测试极扁窗口 (120x20)**

调整终端窗口高度到约 20 行，重启应用
- 验证 header 不被挤出
- 验证 counter 压缩显示
- 验证 bar chart 可见

- [ ] **Step 3: 测试极限窗口 (120x15)**

调整终端窗口高度到约 15 行，重启应用
- 验证最核心信息仍可见
- 无 panic 或崩溃

- [ ] **Step 4: 测试窄屏 (80x30)**

调整终端窗口宽度到约 80 列，重启应用
- 验证 counter 2 列布局
- 验证无溢出

- [ ] **Step 5: 测试 logs 详情视图**

进入 logs tab，选择一条日志查看详情
- 验证不同窗口尺寸下 header 正常

---

## 任务 5: 运行单元测试

**Files:**
- 测试: `make test`

- [ ] **Step 1: 运行测试**

```bash
make test
```

- [ ] **Step 2: 确认所有测试通过**

预期: 所有测试通过，无 regression

---

## 验证检查清单

- [ ] stats 视图在 120x40 正常显示
- [ ] stats 视图在 120x20 不溢出
- [ ] stats 视图在 120x15 可用
- [ ] logs 详情视图各种尺寸正常
- [ ] `make test` 全部通过
- [ ] 代码风格一致（gofmt）

---

## 自检

完成计划后，检查以下问题:

1. **Spec 覆盖**: 所有设计要求都有对应任务？✓
2. **无占位符**: 无 TBD/TODO？✓
3. **类型一致性**: 方法签名在所有调用处一致？✓
