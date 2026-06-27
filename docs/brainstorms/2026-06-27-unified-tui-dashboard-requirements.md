---
date: 2026-06-27
topic: unified-tui-dashboard
feature: Unified TUI Dashboard
---

# Requirements: Unified TUI Dashboard

## Summary

A unified TUI dashboard that combines all litellm-cli subcommands (logs, stats, team_rank, models, teams, keyinfo, login) into a single interactive application. Users run `litellm-cli` without subcommand and see a browser-style interface with tabs at the top, arrow keys to switch between views, logs as the default tab. Separate subcommand entries remain for non-interactive use with `--json` flag.

## Problem Statement

Currently each subcommand (`litellm-cli logs`, `litellm-cli stats`, `litellm-cli team`) is a separate TUI experience launched independently. This means:
- Users must exit one view and launch another to switch between logs and stats
- No shared state or navigation between views
- Each subcommand has its own entry point and model
- The experience feels like "a collection of CLI tools" rather than "an integrated application"

## User Experience

### Layout
- **Browser-style layout**: Tabs at the top (like a browser's tab bar), main content area below
- **Tab bar**: Horizontal row of tab names/ icons in the header area
- **Active tab**: Highlighted tab indicates current view
- **Content area**: Renders the selected subcommand's view (logs list, stats dashboard, etc.)

### Navigation
- **Arrow keys (←/→)**: Switch between tabs
- **Number keys (optional)**: Quick jump to specific tab (1-7)
- **Mouse click**: Click on tab to switch (if terminal supports it)

### Default Behavior
- Running `litellm-cli` without subcommand opens the unified dashboard
- Default tab is **logs** (most frequently used)
- Tab state persists during the session

### Header Elements
- App title/version on the left
- Tab row in the center/right
- Optional: global status (API connection, user info)

## Requirements

### R-1: Unified Entry Point
- `litellm-cli` (no subcommand) launches the unified dashboard TUI
- Default tab is "logs"

### R-2: Tab Navigation
- Arrow keys (←/→) switch between tabs
- Active tab is visually highlighted
- Tab order: logs, stats, team_rank, models, teams, keyinfo, login

### R-3: All Subcommands as Tabs
- Each existing subcommand becomes a tab in the unified view:
  - **logs**: Real-time log viewing with polling
  - **stats**: Usage statistics dashboard
  - **team_rank**: Team usage leaderboard
  - **models**: Available models list
  - **teams**: User's accessible teams
  - **keyinfo**: API key information
  - **login**: Authentication (or show current auth status)

### R-4: Independent Tab State
- Each tab maintains its own scroll position, filters, and view state
- Switching tabs preserves the previous tab's state
- Returning to a tab shows it exactly as left

### R-5: Backward Compatibility
- Existing subcommand entries (`litellm-cli logs`, `litellm-cli stats`, etc.) remain functional
- Individual subcommands launch their own TUI (as before)
- Both unified dashboard and individual TUIs share the same underlying code

### R-6: Non-Interactive Mode
- Individual subcommands support `--json` flag for script/CI usage
- `litellm-cli logs --json` outputs raw JSON to stdout
- `litellm-cli stats --json` outputs usage data as JSON

### R-7: Architecture
- Single Bubble Tea Program
- Model contains: `activeTab`, `logsModel`, `statsModel`, `teamRankModel`, `modelsModel`, `teamsModel`, `keyinfoModel`, `loginModel`
- `View()` function switches content based on `activeTab`
- Shared header/footer rendering for consistent UI

### R-8: Visual Design
- Consistent color scheme across all tabs
- Tab bar uses lipgloss styling (active vs inactive tabs)
- Header shows app name and current user (if logged in)
- Footer shows keyboard shortcuts hint

## Out of Scope (Deferred)

- Drag-and-drop tab reordering
- Tab persistence across sessions (future enhancement)
- Multiple dashboard windows
- Custom themes/user color schemes

## Technical Considerations

### Implementation Pattern
```
type Model struct {
    // Global state
    activeTab string

    // Tab-specific models (each is the existing model for that view)
    logs    logs.Model
    stats   stats.Model
    // ... etc

    // Shared UI state
    width   int
    height  int
}
```

### View Switching
```go
func (m Model) View() string {
    header := renderHeader(m.activeTab, m.width)
    content := m.renderActiveTab()
    footer := renderFooter(m.width)
    return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}
```

### Shared Components
- Header renderer (app title, tabs)
- Footer renderer (shortcuts hint)
- Status bar (API status, user info)

## Success Criteria

1. User can run `litellm-cli` and see the unified dashboard with tabs
2. Arrow keys switch between all 7 tabs smoothly
3. Each tab renders the correct content (logs show logs, stats show stats, etc.)
4. Switching tabs preserves the previous tab's state
5. Running `litellm-cli logs` still works as before (backward compat)
6. Running `litellm-cli logs --json` outputs valid JSON

## Open Questions

- **Q1**: Should tab order be configurable? (Deferred to future)
- **Q2**: Should there be a "favorites" or "pinned" tabs feature? (Deferred to future)
- **Q3**: How to handle tabs that require authentication (keyinfo, login)? (Show auth status or redirect)