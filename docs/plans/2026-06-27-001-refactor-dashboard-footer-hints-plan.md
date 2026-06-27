---
title: "refactor: 增强 Dashboard footer 显示动态操作提示"
created: 2026-06-27
plan_type: refactor
status: proposed
---

# refactor: 增强 Dashboard footer 显示动态操作提示

## Problem Frame

当前 Dashboard 的 footer 只显示通用的操作提示 `"←/→: 切换 Tab | q: 退出"`，没有根据当前活动的 tab 显示该 tab 特有的操作提示。用户需要在不同 tab 间切换时查看对应的快捷键，增加了认知负担。

## Requirements

- Header 保持现状，只渲染 tabs（当前 tab 已有高亮显示）
- Footer 根据当前活动的 tab，显示该 tab 特有的键盘操作提示

## Key Technical Decisions

1. **数据存储方式**：在 `model.go` 中添加 `tabHelpTips` map，键为 tab ID，值为对应的操作提示字符串
2. **渲染逻辑**：`renderFooter()` 方法根据 `m.activeTab` 从 map 中获取对应提示

## Implementation Units

### U1. 添加 tabHelpTips map 并修改 renderFooter

**Goal:** 在 footer 中显示当前 tab 的动态操作提示

**Requirements:** 满足 Problem Frame 和 Requirements 中定义的需求

**Files:**
- `internal/tui/dashboard/model.go`

**Approach:**
1. 在 `model.go` 中定义 `tabHelpTips` map，存储各 tab ID 到操作提示字符串的映射
2. 修改 `renderFooter()` 方法，根据 `m.activeTab` 返回对应的提示

**Patterns to follow:**
- 使用现有的 `TabOrder` 和 `TabNames` 的 map 模式
- 使用现有的 lipgloss 样式（从 `renderFooter()` 中继承）

**Test scenarios:**
- 切换到 logs tab 时，footer 显示 `j/k: 上下移动 | enter: 详情 | esc: 返回 | c: 复制 | ←/→: 切换 Tab | q: 退出`
- 切换到 stats tab 时，footer 显示 `j/k: 上下移动 | tab: 切换视图 | ←/→: 切换 Tab | q: 退出`
- 切换到 team_rank/models/teams 时，显示对应的操作提示
- 切换到 keyinfo/login 时，显示通用提示

## Open Questions

无

## Deferred to Follow-Up Work

无