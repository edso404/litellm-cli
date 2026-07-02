package dashboard

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"litellm-cli/internal/api"
	"litellm-cli/internal/tui/logs"
	"litellm-cli/internal/tui/stats"
)

// TabOrder 定义了所有 Tab 的顺序
var TabOrder = []string{"logs", "stats", "team_rank", "keyinfo"}

// TabNames Tab 显示名称映射
var TabNames = map[string]string{
	"logs":      "📜 日志",
	"stats":     "📊 统计",
	"team_rank": "🏆 排行",
	"keyinfo":   "🔑 Key",
}

// TabHelpTips Tab 操作提示映射
// 格式：<特定操作> | <通用操作>
// 通用操作固定为：esc 返回 | ←/→ 切换 tab | q 退出
var TabHelpTips = map[string]string{
	"logs":      "↑↓: 切换 | enter: 详情 | c: 复制 | g/G: 跳转 | r: 刷新/重试 | esc: 返回 | ←/→: 切换 tab | q: 退出",
	"stats":     "1-5: 时间范围 | d/w/m: 粒度 | ↑↓: 选择日期 | esc: 返回 | ←/→: 切换 tab | q: 退出",
	"team_rank": "Tab: 切换 Key/用量排行 | ↑↓: 移动 | enter: 详情 | esc: 返回 | ←/→: 切换 tab | q: 退出",
	"keyinfo":   "esc: 返回 | ←/→: 切换 tab | q: 退出",
}

// Model 是 Dashboard 的主 Model，包含所有 Tab 的子 Model
type Model struct {
	// Active tab
	activeTab string

	// Child models
	Logs       *logs.Model
	Stats      *stats.Model
	TeamRank   *teamRankModel
	KeyinfoTab *keyinfoTabModel

	// API client
	apiClient *api.Client
	apiKey    string

	// Window dimensions
	width  int
	height int

	// 是否正在退出
	quitting bool


}

// DashboardQuitMsg 是 Dashboard 专用退出消息，用于在子模型之前捕获退出键
type DashboardQuitMsg struct{}


// NewModel 创建一个新的 Dashboard Model
func NewModel(client *api.Client, apiKey string) *Model {
	m := &Model{
		activeTab: "logs", // 默认 tab
		apiClient: client,
		apiKey:    apiKey,
		width:     120,
		height:    40,
	}
	// 初始化子模型
	m.initChildModels()
	return m
}

func (m *Model) initChildModels() {
	// Logs - 使用实际的 client，隐藏 header 和 footer（由 dashboard 统一渲染）
	m.Logs = logs.NewModel(clientAdapter{client: m.apiClient}, 5, "")
	m.Logs.ShowHeader(false)
	m.Logs.ShowFooter(false)
	// Stats - 使用用户维度的数据（team 数据从 2026-05-20 开始，user 数据从 2026-01-01 开始），隐藏 header
	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -7).Format("2006-01-02") // 默认最近一周
	m.Stats = stats.NewModel(statsClientAdapter{client: m.apiClient}, startDate, endDate)
	m.Stats.By = "user"
	m.Stats.ShowHeader(false)
	// Team Rank - 使用适配器
	m.TeamRank = newTeamRankModel(NewTeamRankClientAdapter(m.apiClient))
	// Keyinfo
	m.KeyinfoTab = newKeyinfoTabModel(m.apiClient, m.apiKey)
}

// Init 实现 tea.Model 接口
func (m *Model) Init() tea.Cmd {
	// 初始化所有子模型的命令（这样切换到任何 tab 时数据都已加载）
	var cmds []tea.Cmd
	cmds = append(cmds, m.Logs.Init())
	cmds = append(cmds, m.Stats.Init())
	cmds = append(cmds, m.TeamRank.Init())
	cmds = append(cmds, m.KeyinfoTab.Init())
	return tea.Batch(cmds...)
}

// Update 实现 tea.Model 接口
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DashboardQuitMsg:
		m.quitting = true
		return m, tea.Quit


	case tea.KeyMsg:
		// 首先处理 Tab 切换键 (←/→)
		if m.handleTabKey(msg) {
			return m, nil
		}

		// 处理退出键 - 在转发给子模型之前捕获
		if msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}

		// 转发给当前活动子模型
		child, cmd := m.activeModel().Update(msg)
		return m.updateChildModel(child, cmd)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// 计算子模型可用的实际高度（减去 dashboard 的 header 和 footer）
		// header: Tab bar (1 行) + 空行 (1 行) = 2 行
		// footer: 帮助信息 (1 行) + 空行 (1 行) = 2 行（或者 logs tab 可能更少）
		availableHeight := msg.Height - 4
		if availableHeight < 10 {
			availableHeight = 10
		}

		// 转发窗口大小给当前活动子模型，确保子模型能获取正确的高度
		child, cmd := m.activeModel().Update(tea.WindowSizeMsg{Width: msg.Width, Height: availableHeight})
		return m.updateChildModel(child, cmd)

	// 处理各子模型加载完成的消息，路由到对应的子模型
	case logs.LogsLoadedMsg:
		child, cmd := m.Logs.Update(msg)
		if logsModel, ok := child.(*logs.Model); ok {
			m.Logs = logsModel
		}
		return m, cmd

	case stats.StatsLoadedMsg:
		child, cmd := m.Stats.Update(msg)
		if statsModel, ok := child.(*stats.Model); ok {
			m.Stats = statsModel
		}
		return m, cmd


	case keyinfoLoadedMsg:
		child, cmd := m.KeyinfoTab.Update(msg)
		if keyinfoTab, ok := child.(*keyinfoTabModel); ok {
			m.KeyinfoTab = keyinfoTab
		}
		return m, cmd
	case teamRankLoadedMsg:
		child, cmd := m.TeamRank.Update(msg)
		if teamRankModel, ok := child.(*teamRankModel); ok {
			m.TeamRank = teamRankModel
		}
		return m, cmd
	case usageRankLoadedMsg:
		// 转发给 TeamRank 处理（内部会路由给 usageRank）
		child, cmd := m.TeamRank.Update(msg)
		if teamRankModel, ok := child.(*teamRankModel); ok {
			m.TeamRank = teamRankModel
		}
		return m, cmd

	default:
		// 转发其他消息给当前活动子模型
		child, cmd := m.activeModel().Update(msg)
		return m.updateChildModel(child, cmd)
	}
}

// View 实现 tea.Model 接口
func (m *Model) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}

	header := m.renderHeader()

	// 渲染退出确认
	if false /* was showQuitConfirm */ {
		confirmStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("220")). // 黄色
			Bold(true)
		confirmMsg := "\n\n" + confirmStyle.Render("  确认退出？(y/n)")
		footer := m.renderFooter()
		// 使用 lipgloss 居中内容
		return header + confirmMsg + "\n" + footer
	}

	content := m.activeModel().View()
	footer := m.renderFooter()

	// Footer 固定底部：计算填充行
	var result strings.Builder
	result.WriteString(header)
	result.WriteString("\n")

	// 计算内容区域需要填充的行数，使 footer 固定在底部
	lines := strings.Split(content, "\n")
	contentLines := len(lines)
	footerLines := 1 // Footer 通常 1 行
	
	availableHeight := m.height - 4 // 减去 header 和空行
	if contentLines < availableHeight {
		// 填充空白行使 footer 固定底部
		padding := availableHeight - contentLines - footerLines
		if padding > 0 {
			result.WriteString(content)
			result.WriteString(strings.Repeat("\n", padding))
		} else {
			result.WriteString(content)
		}
	} else {
		result.WriteString(content)
	}

	result.WriteString("\n")
	result.WriteString(footer)

	return result.String()
}

// handleTabKey 处理 Tab 切换键
// 返回 true 表示已处理，false 表示继续传递
func (m *Model) handleTabKey(msg tea.KeyMsg) bool {
	// 计算子模型可用的实际高度
	availableHeight := m.height - 4
	if availableHeight < 10 {
		availableHeight = 10
	}

	switch msg.String() {
	case "right", "l":
		oldTab := m.activeTab
		m.activeTab = nextTab(m.activeTab, 1)
		// 离开日志 Tab 时暂停轮询
		if oldTab == "logs" {
			m.Logs.Update(logs.PausePollingMsg{})
		}
		// 进入日志 Tab 时恢复轮询
		if m.activeTab == "logs" {
			m.Logs.Update(logs.ResumePollingMsg{})
		}
		// 发送窗口大小给新的活动子模型，确保它能正确渲染
		child, _ := m.activeModel().Update(tea.WindowSizeMsg{Width: m.width, Height: availableHeight})
		m.updateChildModelFromSwitch(child)
		return true
	case "left", "h":
		oldTab := m.activeTab
		m.activeTab = nextTab(m.activeTab, -1)
		// 离开日志 Tab 时暂停轮询
		if oldTab == "logs" {
			m.Logs.Update(logs.PausePollingMsg{})
		}
		// 进入日志 Tab 时恢复轮询
		if m.activeTab == "logs" {
			m.Logs.Update(logs.ResumePollingMsg{})
		}
		// 发送窗口大小给新的活动子模型
		child, _ := m.activeModel().Update(tea.WindowSizeMsg{Width: m.width, Height: availableHeight})
		m.updateChildModelFromSwitch(child)
		return true
	}
	return false
}

// nextTab 返回当前 tab 索引后 offset 个位置的 tab 名称
func nextTab(current string, offset int) string {
	for i, tab := range TabOrder {
		if tab == current {
			newIdx := (i + offset + len(TabOrder)) % len(TabOrder)
			return TabOrder[newIdx]
		}
	}
	return TabOrder[0]
}

// activeModel 返回当前活动的子模型
func (m *Model) activeModel() tea.Model {
	switch m.activeTab {
	case "logs":
		return m.Logs
	case "stats":
		return m.Stats
	case "team_rank":
		return m.TeamRank
	case "keyinfo":
		return m.KeyinfoTab
	default:
		return m.Logs
	}
}

// updateChildModel 更新子模型并返回
func (m *Model) updateChildModel(child tea.Model, cmd tea.Cmd) (*Model, tea.Cmd) {
	m.updateChildModelFromSwitch(child)
	return m, cmd
}

// updateChildModelFromSwitch 从 tab 切换时更新子模型（不返回 cmd）
func (m *Model) updateChildModelFromSwitch(child tea.Model) {
	switch m.activeTab {
	case "logs":
		if logsModel, ok := child.(*logs.Model); ok {
			m.Logs = logsModel
		}
	case "stats":
		if statsModel, ok := child.(*stats.Model); ok {
			m.Stats = statsModel
		}
	case "team_rank":
		if teamRankModel, ok := child.(*teamRankModel); ok {
			m.TeamRank = teamRankModel
		}
	case "keyinfo":
		if keyinfoTab, ok := child.(*keyinfoTabModel); ok {
			m.KeyinfoTab = keyinfoTab
		}
	}
}

// renderHeader 渲染 Tab bar
func (m *Model) renderHeader() string {
	tabCount := len(TabOrder)
	if tabCount == 0 {
		return ""
	}

	// 计算每个 Tab 的宽度：总宽度 / Tab 数量，保持最小宽度
	minTabWidth := 8
	tabWidth := m.width / tabCount
	if tabWidth < minTabWidth {
		tabWidth = minTabWidth
	}

	// Tab 样式
	activeTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("86")).
		Bold(true).
		Width(tabWidth).
		Align(lipgloss.Center)

	inactiveTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Background(lipgloss.Color("236")).
		Width(tabWidth).
		Align(lipgloss.Center)

	// 构建 Tab bar
	var tabParts []string
	for _, tab := range TabOrder {
		tabName := TabNames[tab]
		if tab == m.activeTab {
			tabParts = append(tabParts, activeTabStyle.Render(tabName))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render(tabName))
		}
	}

	tabs := strings.Join(tabParts, "")

	// 整体布局
	var sb strings.Builder
	sb.WriteString(tabs)

	return sb.String()
}

// renderFooter 渲染底部帮助信息
func (m *Model) renderFooter() string {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	// logs tab 动态获取当前状态的帮助文本（列表/详情/嵌套详情三个状态的按键不同）
	if m.activeTab == "logs" {
		return mutedStyle.Render(m.Logs.HelpText())
	}
	tip, ok := TabHelpTips[m.activeTab]
	if !ok {
		tip = "←/→: 切换 Tab | q: 退出"
	}
	return mutedStyle.Render(tip)
}

// clientAdapter 将 *api.Client 适配为 logs.LogsClient
type clientAdapter struct {
	client *api.Client
}

func (a clientAdapter) GetSpendLogsUI(startDateTime, endDateTime string) (*api.SpendLogsUIResponse, error) {
	return a.client.GetSpendLogsUI(startDateTime, endDateTime)
}

func (a clientAdapter) GetSpendLogs(startDate, endDate string) (*api.SpendLogsResponse, error) {
	return a.client.GetSpendLogs(startDate, endDate)
}

func (a clientAdapter) GetSpendLogDetail(requestID string) (map[string]interface{}, error) {
	return a.client.GetSpendLogDetail(requestID)
}

// statsClientAdapter 将 *api.Client 适配为 stats.StatsClient
type statsClientAdapter struct {
	client *api.Client
}

func (a statsClientAdapter) GetUserDailyActivity(startDate, endDate string, pageSize int, page int) (*api.UserDailyActivityResponse, error) {
	return a.client.GetUserDailyActivity(startDate, endDate, pageSize, page)
}

func (a statsClientAdapter) GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error) {
	return a.client.GetTeamDailyActivity(startDate, endDate)
}
