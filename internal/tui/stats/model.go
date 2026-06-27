package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"litellm-cli/internal/api"
	"litellm-cli/internal/tui/components"
)

// StatsClient defines the client interface required by the stats TUI
type StatsClient interface {
	GetUserDailyActivity(startDate, endDate string) (*api.UserDailyActivityResponse, error)
	GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error)
}

// Model represents the stats TUI model
type Model struct {
	client           StatsClient
	startDate        string
	endDate          string
	viewMode         string // "counter" or "bar"
	data             []api.UserDailyActivity
	aggregated       aggregatedMetrics
	selectedBarIndex int
	width            int
	height           int
	quitting         bool
	err              string
	loading          bool
	By               string // Aggregation dimension: "user", "team", etc.
	showHeader       bool   // 是否显示顶部 header（在 dashboard 中隐藏）
}

type aggregatedMetrics struct {
	TotalSpend       float64
	TotalRequests    int64
	Successful       int64
	Failed           int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	AvgCostPerReq    float64
}

// StatsLoadedMsg is triggered when stats data has finished loading asynchronously
type StatsLoadedMsg struct {
	Data  []api.UserDailyActivity
	Error error
}

// NewModel creates a new stats TUI Model
func NewModel(client StatsClient, startDate, endDate string) *Model {
	return &Model{
		client:           client,
		startDate:        startDate,
		endDate:          endDate,
		viewMode:         "counter",
		selectedBarIndex: -1,
		width:            120,
		height:           40,
		loading:          true,
		By:               "user",
		showHeader:      true, // 默认显示 header
	}
}

// Init initializes the Model by returning the refresh command
func (m *Model) Init() tea.Cmd {
	return m.RefreshCmd()
}

// Update handles messages and updates the Model's state
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case StatsLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
		m.calculateAggregated()
		if len(m.data) > 0 && m.selectedBarIndex < 0 {
			m.selectedBarIndex = 0
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			if m.viewMode == "counter" {
				m.viewMode = "bar"
				if len(m.data) > 0 && m.selectedBarIndex < 0 {
					m.selectedBarIndex = 0
				}
			} else {
				m.viewMode = "counter"
			}
			return m, nil
		case "shift+tab":
			if m.viewMode == "bar" {
				m.viewMode = "counter"
			} else {
				m.viewMode = "bar"
				if len(m.data) > 0 && m.selectedBarIndex < 0 {
					m.selectedBarIndex = 0
				}
			}
			return m, nil
		case "down", "j":
			if len(m.data) > 0 {
				if m.selectedBarIndex < len(m.data)-1 {
					m.selectedBarIndex++
				}
			}
			return m, nil
		case "up", "k":
			if len(m.data) > 0 {
				if m.selectedBarIndex > 0 {
					m.selectedBarIndex--
				}
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View renders the terminal user interface
func (m *Model) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}
	if m.err != "" {
		return components.NewErrorBanner(m.err).View(m.width) + "\n"
	}
	if m.loading {
		return components.NewLoader("正在加载统计数据...").View() + "\n"
	}
	if len(m.data) == 0 {
		return components.NewPlaceholder("暂无数据").View() + "\n"
	}

	isLargeScreen := m.width >= 115

	var sb strings.Builder

	// 显示 header（如果启用）
	if m.showHeader {
		if isLargeScreen {
			header := components.NewHeader("用量统计看板", fmt.Sprintf("%s - %s | 按 q 退出", m.startDate, m.endDate))
			sb.WriteString(header.View(m.width))
			sb.WriteString("\n\n")
		} else if m.viewMode == "bar" {
			header := components.NewHeader("每日花费", fmt.Sprintf("%s - %s | 按 Tab 切换视图 | 按 q 退出", m.startDate, m.endDate))
			sb.WriteString(header.View(m.width))
			sb.WriteString("\n\n")
		} else {
			header := components.NewHeader("用量统计", fmt.Sprintf("%s - %s | 按 Tab 切换视图 | 按 q 退出", m.startDate, m.endDate))
			sb.WriteString(header.View(m.width))
			sb.WriteString("\n\n")
		}
	}

	// 根据屏幕大小和视图模式显示内容
	if isLargeScreen {
		// 大屏：同时显示 counter 和 bar（横向排列），帮助信息显示 j/k 导航
		leftWidth := 55
		rightWidth := m.width - leftWidth - 4

		leftContent := m.renderCounterContent(leftWidth)
		rightContent := m.renderBarContent(rightWidth)

		joined := lipgloss.JoinHorizontal(lipgloss.Top,
			leftContent,
			"    ",
			rightContent,
		)
		sb.WriteString(joined)
		sb.WriteString("\n\n")

		help := components.NewHelp([]components.HelpKey{
			{Key: "j/k 或 ↓/↑", Desc: "在右侧图表中移动"},
			{Key: "q", Desc: "退出"},
		})
		sb.WriteString(help.View(m.width))
	} else {
		// 小屏幕模式 (复用原有的 else 分支代码)
		if m.viewMode == "bar" {
			sb.WriteString(m.renderBarContent(m.width))
			sb.WriteString("\n\n")

			help := components.NewHelp([]components.HelpKey{
				{Key: "Tab", Desc: "切换视图"},
				{Key: "j/k 或 ↓/↑", Desc: "移动"},
				{Key: "q", Desc: "退出"},
			})
			sb.WriteString(help.View(m.width))
		} else {
			sb.WriteString(m.renderCounterContent(m.width))
			sb.WriteString("\n\n")

			help := components.NewHelp([]components.HelpKey{
				{Key: "Tab", Desc: "切换视图"},
				{Key: "q", Desc: "退出"},
			})
			sb.WriteString(help.View(m.width))
		}
	}

	return sb.String()
}

// RefreshCmd performs asynchronous data loading
func (m *Model) RefreshCmd() tea.Cmd {
	return func() tea.Msg {
		var data []api.UserDailyActivity
		var err error
		if m.By == "team" {
			var resp *api.TeamDailyActivityResponse
			resp, err = m.client.GetTeamDailyActivity(m.startDate, m.endDate)
			if err == nil && resp != nil {
				data = make([]api.UserDailyActivity, len(resp.Results))
				for i, r := range resp.Results {
					data[i] = api.UserDailyActivity{
						Date:      r.Date,
						Metrics:   r.Metrics,
						Breakdown: r.Breakdown,
					}
				}
			}
		} else {
			var resp *api.UserDailyActivityResponse
			resp, err = m.client.GetUserDailyActivity(m.startDate, m.endDate)
			if err == nil && resp != nil {
				data = resp.Results
			}
		}
		return StatsLoadedMsg{Data: data, Error: err}
	}
}

func (m *Model) calculateAggregated() {
	var totalSpend float64
	var totalPrompt, totalCompletion, totalTokens int64
	var totalSuccess, totalFailed, totalRequests int64

	for _, r := range m.data {
		totalSpend += r.Metrics.Spend
		totalPrompt += r.Metrics.PromptTokens
		totalCompletion += r.Metrics.CompletionTokens
		totalTokens += r.Metrics.TotalTokens
		totalSuccess += r.Metrics.SuccessfulRequests
		totalFailed += r.Metrics.FailedRequests
		totalRequests += r.Metrics.APIRequests
	}

	avgCost := 0.0
	if totalRequests > 0 {
		avgCost = totalSpend / float64(totalRequests)
	}

	m.aggregated = aggregatedMetrics{
		TotalSpend:       totalSpend,
		TotalRequests:    totalRequests,
		Successful:       totalSuccess,
		Failed:           totalFailed,
		PromptTokens:     totalPrompt,
		CompletionTokens: totalCompletion,
		TotalTokens:      totalTokens,
		AvgCostPerReq:    avgCost,
	}
}

func (m *Model) renderCounterContent(width int) string {
	var sb strings.Builder

	cardStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("159"))

	metrics := []struct {
		label string
		value string
	}{
		{"💰 总花费", fmt.Sprintf("$%.4f", m.aggregated.TotalSpend)},
		{"📤 总请求", fmt.Sprintf("%d", m.aggregated.TotalRequests)},
		{"✅ 成功请求", fmt.Sprintf("%d", m.aggregated.Successful)},
		{"❌ 失败请求", fmt.Sprintf("%d", m.aggregated.Failed)},
		{"📝 Prompt Tokens", formatTokens(m.aggregated.PromptTokens)},
		{"✍️ Completion Tokens", formatTokens(m.aggregated.CompletionTokens)},
		{"📊 总 Tokens", formatTokens(m.aggregated.TotalTokens)},
		{"📈 平均请求费用", fmt.Sprintf("$%.6f", m.aggregated.AvgCostPerReq)},
	}

	cols := 4
	if width < 100 {
		cols = 2
	}
	if width < 60 {
		cols = 1
	}

	// 动态计算卡片宽度：让卡片横向铺满 width。
	// 每个卡片的 border 占用 2 个字符，所以 cardStyle.Width 应该设为 (width / cols) - 2。
	cardWidth := (width / cols) - 2
	if cardWidth < 20 {
		cardWidth = 20
	}
	if cardWidth > 35 {
		cardWidth = 35
	}
	cardHeight := 4

	for row := 0; row < len(metrics); row += cols {
		var rowCards []string
		for col := 0; col < cols && row+col < len(metrics); col++ {
			metric := metrics[row+col]
			card := labelStyle.Render(metric.label) + "\n" + valueStyle.Render(metric.value)
			rowCards = append(rowCards, cardStyle.Width(cardWidth).Height(cardHeight).AlignVertical(lipgloss.Top).Render(card))
		}
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rowCards...))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m *Model) renderBarContent(width int) string {
	var sb strings.Builder

	barStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("76"))

	barFocusedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("82")).
		Bold(true)

	dateLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("159")).
		Bold(true)

	var maxSpend float64
	for _, r := range m.data {
		if r.Metrics.Spend > maxSpend {
			maxSpend = r.Metrics.Spend
		}
	}

	// 动态计算柱状图的最大宽度，排除两旁的文字所占宽度，让柱状图尽量占满屏幕
	barMaxWidth := width - 26
	if barMaxWidth < 10 {
		barMaxWidth = 10
	}
	if barMaxWidth > 80 {
		barMaxWidth = 80
	}

	for i, r := range m.data {
		dateStr := r.Date
		if len(m.data) > 7 {
			if i%2 == 1 && i != len(m.data)-1 {
				dateStr = ""
			}
		}

		isSelected := i == m.selectedBarIndex

		var barWidth int
		if maxSpend > 0 {
			barWidth = int(float64(barMaxWidth) * r.Metrics.Spend / maxSpend)
		}

		barStr := strings.Repeat("█", barWidth)
		if isSelected {
			sb.WriteString(selectedStyle.Render("▶ "))
			sb.WriteString(barFocusedStyle.Render(barStr))
		} else {
			sb.WriteString("  ")
			sb.WriteString(barStyle.Render(barStr))
		}

		sb.WriteString(fmt.Sprintf(" $%.2f", r.Metrics.Spend))

		if dateStr != "" {
			sb.WriteString("  ")
			sb.WriteString(dateLabelStyle.Render(dateStr))
		}

		sb.WriteString("\n")
	}

	if m.selectedBarIndex >= 0 && m.selectedBarIndex < len(m.data) {
		sb.WriteString("\n")
		sb.WriteString(m.renderDetailPanel(m.data[m.selectedBarIndex], width))
	}

	return sb.String()
}

func (m *Model) renderDetailPanel(data api.UserDailyActivity, width int) string {
	var sb strings.Builder

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(0, 1)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("159"))

	content := titleStyle.Render(fmt.Sprintf("📅 %s", data.Date)) + "\n"
	content += keyStyle.Render("💰 花费: ") + valueStyle.Render(fmt.Sprintf("$%.4f", data.Metrics.Spend)) + "\n"
	content += keyStyle.Render("📤 请求: ") + valueStyle.Render(fmt.Sprintf("%d", data.Metrics.APIRequests)) + "\n"
	content += keyStyle.Render("✅ 成功: ") + valueStyle.Render(fmt.Sprintf("%d", data.Metrics.SuccessfulRequests)) + "\n"
	content += keyStyle.Render("❌ 失败: ") + valueStyle.Render(fmt.Sprintf("%d", data.Metrics.FailedRequests)) + "\n"
	content += keyStyle.Render("📝 Prompt: ") + valueStyle.Render(formatTokens(data.Metrics.PromptTokens)) + "\n"
	content += keyStyle.Render("✍️ Completion: ") + valueStyle.Render(formatTokens(data.Metrics.CompletionTokens)) + "\n"
	content += keyStyle.Render("📊 总 Tokens: ") + valueStyle.Render(formatTokens(data.Metrics.TotalTokens))

	// 动态计算详情面板的宽度
	panelWidth := int(float64(width) * 0.7)
	if panelWidth < 30 {
		panelWidth = 30
	}
	if panelWidth > 50 {
		panelWidth = 50
	}

	sb.WriteString(panelStyle.Width(panelWidth).Render(content))

	return sb.String()
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	} else if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// ShowHeader 控制是否显示顶部 header
func (m *Model) ShowHeader(show bool) {
	m.showHeader = show
}
