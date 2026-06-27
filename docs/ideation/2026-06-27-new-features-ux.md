---
date: 2026-06-27
topic: new-features-ux
focus: new features & UX for LiteLLM CLI (updated after user feedback)
---

# Ideation: LiteLLM CLI 新功能与 UX 改进

## User Feedback Summary

- **暂时不需要**: Model-Cost Attribution Matrix (#1), API Key-Level Cost Explorer (#2), Live Budget Burn Rate (#3) — 用户只有一个 model 和一个 key，无预算限制
- **语义搜索局限**: Log Semantic Search 只能做客户端已加载日志的模糊匹配，无法实现真正的语义搜索
- **用户提出新想法**: 统一 TUI 面板 — 所有 subcommand 集成到一个统一布局中
- **使用 session_id 关联**: Cross-Request Transcript Stitching 可以用 `proxy_server_request.metadata.user_id` 中的 `session_id` 来关联

## Codebase Context

- **Project shape**: Go CLI using Cobra + Bubble Tea for TUI
- **Top-level layout**: `cmd/` (commands), `internal/api/` (types), `internal/client/` (methods), `internal/config/` (auth), `internal/tui/` (TUI components)
- **Notable patterns**: Commands in `cmd/`, one file per feature; shared logic in `internal/`; Chinese user-facing output
- **Strategy tracks**: 日志查看 (log viewing), 用量统计 (usage stats), 排行榜 (leaderboard)

## Ranked Ideas

### 1. Unified TUI Dashboard with Tab Navigation ⭐
- **Description**: Integrate all subcommand rendering into a unified panel with consistent header/banner/footer/sidebar layout. Top area shows tabs for switching between commands (logs/stats/teams/models), middle area renders the selected command's view (like current logs view), interact via keyboard/mouse.
- **Rationale**: User proposed this idea. Currently each subcommand (`logs`, `stats`, `team`, `models`) is a separate TUI experience launched via `litellm-cli <subcommand>`. A unified dashboard provides: (1) Consistent UX across all views, (2) Quick tab-switching without re-running commands, (3) Shared layout infrastructure reducing code duplication, (4) Browser-like experience — like having multiple "pages" in one app.
- **Downsides**: Requires significant refactoring — each subcommand currently has its own entry point and model; need to handle state management across tabs (e.g., different data refresh intervals for logs vs stats)
- **Confidence**: High (95%)
- **Complexity**: High
- **Axis**: TUI 交互改进
- **Status**: User Priority

### 2. Stats Export to JSON
- **Description**: Add `--export json` flag to stats command that bypasses the TUI and outputs raw JSON data for pipeline integration.
- **Rationale**: User confirmed this is useful. Export functionality was explicitly deferred in the original plan. CLI tools gain superpowers when they can pipe to other tools — enables integration with Grafana, data pipelines, or custom analysis scripts.
- **Downsides**: Need to maintain stable JSON schema for backward compatibility
- **Confidence**: High (95%)
- **Complexity**: Low
- **Axis**: 输出格式与管道
- **Status**: User Priority

### 3. Log Fuzzy Search (Local Filter)
- **Description**: Add fuzzy search across already-loaded log content. Press `/` to open search bar, type to filter displayed logs in real-time. Navigation with `n`/`N` to jump between matches.
- **Rationale**: User noted existing search only supports request_id exact match. This provides client-side filtering of loaded logs — useful for quick lookup within current session. Not true semantic search (which would require API support), but practical for most use cases.
- **Downsides**: Only searches already-loaded logs, not full API history; limited to text matching within current view
- **Confidence**: High (90%)
- **Complexity**: Low
- **Axis**: 日志查看增强
- **Status**: Nice to Have

### 4. Cross-Request Transcript Stitching via session_id
- **Description**: In log detail view, show "Related Requests" section — other logs with same session_id. The session_id is found in `proxy_server_request.metadata.user_id` as a JSON string. Enable navigation between related calls.
- **Rationale**: User pointed out that messages don't have request_id but session_id in metadata can be used for correlation. Debugging LLM apps often requires understanding request chains (system prompt → user query → tool calls → final response). This makes it easy to jump between related requests in a conversation.
- **Downsides**: Requires parsing the metadata JSON to extract session_id; session_id format may vary; may need to load additional logs to find related requests
- **Confidence**: Medium (75%)
- **Complexity**: Medium
- **Axis**: 日志查看增强
- **Status**: Nice to Have

## Rejected (per user feedback)

| Idea | Reason |
|------|--------|
| Model-Cost Attribution Matrix | User has only 1 model — not useful currently |
| API Key-Level Cost Explorer | User has only 1 key — not useful currently |
| Live Budget Burn Rate | No budget limit — not needed |
| True Semantic Search | Requires API support, not feasible with current backend |
| Cross-Request via request_id | Messages don't have request_id (user clarified) |

## Axes Coverage

- **TUI 交互改进**: 1 priority idea (Unified Dashboard)
- **输出格式与管道**: 1 priority idea (JSON Export)
- **日志查看增强**: 2 nice-to-have ideas (Fuzzy Search, Transcript Stitching)

## Notes for Next Steps

The **Unified TUI Dashboard** is the most impactful idea and aligns well with the user's vision. To move forward:
1. Define the tab structure (which subcommands become tabs)
2. Design the shared layout (header, sidebar, content area)
3. Plan the state management (each tab maintains its own data/model)
4. Consider backward compatibility (keep individual command entry points?)

Would you like to brainstorm this Unified Dashboard idea further?