package cmd

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
	"litellm-cli/internal/api"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
)

var (
	period   string
	by       string
	fromDate string
	toDate   string
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "查看用量统计",
	Run:   runStats,
}

func init() {
	statsCmd.Flags().StringVar(&period, "period", "day", "统计周期: day, week, month")
	statsCmd.Flags().StringVar(&by, "by", "user", "聚合维度: user, team, model")
	statsCmd.Flags().StringVar(&fromDate, "from", "", "开始日期 (YYYY-MM-DD)")
	statsCmd.Flags().StringVar(&toDate, "to", "", "结束日期 (YYYY-MM-DD)")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	startDate, endDate := getDateRange(period)

	// 启动 TUI
	p := tea.NewProgram(
		NewStatsModel(c, startDate, endDate),
		tea.WithAltScreen(),
	)

	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}

func getDateRange(p string) (string, string) {
	// 如果提供了自定义日期，使用自定义日期
	if fromDate != "" && toDate != "" {
		// 验证日期格式
		from, err := time.Parse("2006-01-02", fromDate)
		if err != nil {
			log.Fatalf("无效的开始日期格式: %s, 应使用 YYYY-MM-DD", fromDate)
		}
		to, err := time.Parse("2006-01-02", toDate)
		if err != nil {
			log.Fatalf("无效的结束日期格式: %s, 应使用 YYYY-MM-DD", toDate)
		}
		if from.After(to) {
			log.Fatal("开始日期不能晚于结束日期")
		}
		return fromDate, toDate
	}

	now := time.Now()
	endDate := now.Format("2006-01-02")

	var startDate time.Time
	switch p {
	case "week":
		startDate = now.AddDate(0, 0, -7)
	case "month":
		startDate = now.AddDate(0, -1, 0)
	case "day":
		startDate = now.AddDate(0, 0, -1) // 昨天到今天
	default:
		startDate = now.AddDate(0, 0, -1)
	}

	return startDate.Format("2006-01-02"), endDate
}

func printUserStats(c *client.Client, startDate, endDate string) {
	resp, err := c.GetUserDailyActivity(startDate, endDate)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n📊 用户用量统计 (%s - %s)\n", startDate, endDate)
	fmt.Println("========================================")

	if len(resp.Results) == 0 {
		fmt.Println("暂无数据")
		return
	}

	// 计算整个时间段的汇总
	var totalSpend float64
	var totalPrompt, totalCompletion, totalTokens int64
	var totalSuccess, totalFailed, totalRequests int64

	for _, r := range resp.Results {
		totalSpend += r.Metrics.Spend
		totalPrompt += r.Metrics.PromptTokens
		totalCompletion += r.Metrics.CompletionTokens
		totalTokens += r.Metrics.TotalTokens
		totalSuccess += r.Metrics.SuccessfulRequests
		totalFailed += r.Metrics.FailedRequests
		totalRequests += r.Metrics.APIRequests
	}

	// 辅助函数：按显示宽度填充
	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}

	// 显示汇总
	fmt.Println("\n📈 汇总")
	fmt.Printf("   💰 花费: $%.4f\n", totalSpend)
	fmt.Printf("   📝 Prompt Tokens: %s\n", formatTokens(totalPrompt))
	fmt.Printf("   ✍️ Completion Tokens: %s\n", formatTokens(totalCompletion))
	fmt.Printf("   📊 总 Tokens: %s\n", formatTokens(totalTokens))
	fmt.Printf("   ✅ 成功请求: %d\n", totalSuccess)
	fmt.Printf("   ❌ 失败请求: %d\n", totalFailed)
	fmt.Printf("   📤 总请求: %d\n", totalRequests)

	// 显示最近几天的明细
	if len(resp.Results) > 1 {
		// 自动计算每列的最大宽度
		type colWidths struct {
			date   int
			cost   int
			reqs   int
			input  int
			output int
			total  int
			rate   int
		}

		headers := colWidths{
			date:   runewidth.StringWidth("日期"),
			cost:   runewidth.StringWidth("Cost"),
			reqs:   runewidth.StringWidth("Requests"),
			input:  runewidth.StringWidth("Input"),
			output: runewidth.StringWidth("Output"),
			total:  runewidth.StringWidth("Total"),
			rate:   runewidth.StringWidth("成功率"),
		}

		widths := headers
		days := min(7, len(resp.Results))
		for i := 0; i < days; i++ {
			r := resp.Results[i]
			widths.date = max(widths.date, runewidth.StringWidth(r.Date))
			widths.cost = max(widths.cost, runewidth.StringWidth(fmt.Sprintf("$%.2f", r.Metrics.Spend)))
			widths.reqs = max(widths.reqs, runewidth.StringWidth(fmt.Sprintf("%d", r.Metrics.APIRequests)))
			widths.input = max(widths.input, runewidth.StringWidth(formatTokens(r.Metrics.PromptTokens)))
			widths.output = max(widths.output, runewidth.StringWidth(formatTokens(r.Metrics.CompletionTokens)))
			widths.total = max(widths.total, runewidth.StringWidth(formatTokens(r.Metrics.TotalTokens)))
			widths.rate = max(widths.rate, 6) // "99.9%" = 5 chars, use min 6
		}

		// 打印表头
		fmt.Println("\n📅 最近几天:")
		fmt.Printf("   %s %s %s %s %s %s %s\n",
			padRight("日期", widths.date),
			padRight("Cost", widths.cost),
			padRight("Requests", widths.reqs),
			padRight("Input", widths.input),
			padRight("Output", widths.output),
			padRight("Total", widths.total),
			padRight("成功率", widths.rate))

		// 打印分隔线
		totalWidth := widths.date + widths.cost + widths.reqs + widths.input + widths.output + widths.total + widths.rate + 6 // +6 for spaces between cols
		fmt.Println("   " + strings.Repeat("-", totalWidth))

		// 打印数据
		for i := 0; i < days; i++ {
			r := resp.Results[i]
			successRate := 0.0
			if r.Metrics.APIRequests > 0 {
				successRate = float64(r.Metrics.SuccessfulRequests) / float64(r.Metrics.APIRequests) * 100
			}
			fmt.Printf("   %s %s %s %s %s %s %s\n",
				padRight(r.Date, widths.date),
				padRight(fmt.Sprintf("$%.2f", r.Metrics.Spend), widths.cost),
				padRight(fmt.Sprintf("%d", r.Metrics.APIRequests), widths.reqs),
				padRight(formatTokens(r.Metrics.PromptTokens), widths.input),
				padRight(formatTokens(r.Metrics.CompletionTokens), widths.output),
				padRight(formatTokens(r.Metrics.TotalTokens), widths.total),
				padRight(fmt.Sprintf("%.1f%%", successRate), widths.rate),
			)
		}
	}
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	} else if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func printTeamStats(c *client.Client, startDate, endDate string) {
	resp, err := c.GetTeamDailyActivity(startDate, endDate)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n📊 团队用量统计 (%s - %s)\n", startDate, endDate)
	fmt.Println("========================================")

	if len(resp.Results) == 0 {
		fmt.Println("暂无数据")
		return
	}

	// 显示最新一天的数据
	latest := resp.Results[0]
	fmt.Printf("\n📅 %s\n", latest.Date)
	fmt.Printf("   💰 花费: $%.4f\n", latest.Metrics.Spend)
	fmt.Printf("   📝 Prompt Tokens: %s\n", formatTokens(latest.Metrics.PromptTokens))
	fmt.Printf("   ✍️ Completion Tokens: %s\n", formatTokens(latest.Metrics.CompletionTokens))
	fmt.Printf("   📊 总 Tokens: %s\n", formatTokens(latest.Metrics.TotalTokens))
	fmt.Printf("   ✅ 成功请求: %d\n", latest.Metrics.SuccessfulRequests)
	fmt.Printf("   ❌ 失败请求: %d\n", latest.Metrics.FailedRequests)
	fmt.Printf("   📤 总请求: %d\n", latest.Metrics.APIRequests)

	// 按模型显示
	if len(latest.Breakdown.Models) > 0 {
		fmt.Println("\n📦 按模型:")
		for model, data := range latest.Breakdown.Models {
			fmt.Printf("   %s: $%.4f (%s tokens)\n", model, data.Metrics.Spend, formatTokens(data.Metrics.TotalTokens))
		}
	}
}

// ==================== TUI 模式 ====================

// statsModel TUI 模型
type statsModel struct {
	client           *client.Client
	startDate        string
	endDate          string
	viewMode         string // "counter" 或 "bar"
	data             []api.UserDailyActivity
	aggregated       aggregatedMetrics
	selectedBarIndex int
	width            int
	height           int
	quitting         bool
	err              string
}

// aggregatedMetrics 聚合后的指标
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

// NewStatsModel 创建 stats model
func NewStatsModel(c *client.Client, startDate, endDate string) *statsModel {
	m := &statsModel{
		client:           c,
		startDate:        startDate,
		endDate:          endDate,
		viewMode:         "counter",
		selectedBarIndex: -1,
		width:            120,
		height:           40,
	}
	m.refresh()
	return m
}

func (m *statsModel) Init() tea.Cmd {
	return nil
}

func (m *statsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			// 切换视图
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
			// 反向切换视图
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
			// 在 bar 视图中向下移动
			if m.viewMode == "bar" && len(m.data) > 0 {
				if m.selectedBarIndex < len(m.data)-1 {
					m.selectedBarIndex++
				}
			}
			return m, nil
		case "up", "k":
			// 在 bar 视图中向上移动
			if m.viewMode == "bar" && len(m.data) > 0 {
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

func (m *statsModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}

	if m.err != "" {
		return m.err + "\n"
	}

	if len(m.data) == 0 {
		return "暂无数据\n"
	}

	// 渲染当前视图
	switch m.viewMode {
	case "bar":
		return m.renderBarView()
	default:
		return m.renderCounterView()
	}
}

func (m *statsModel) refresh() {
	resp, err := m.client.GetUserDailyActivity(m.startDate, m.endDate)
	if err != nil {
		m.err = fmt.Sprintf("获取数据失败: %v", err)
		return
	}

	m.data = resp.Results

	// 计算聚合指标
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
	m.err = ""
}

// renderCounterView 渲染计数器视图
func (m *statsModel) renderCounterView() string {
	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

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

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// 标题
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" 📊 用量统计 (%s - %s) | 按 Tab 切换视图 | 按 q 退出 ", m.startDate, m.endDate)))
	sb.WriteString("\n\n")

	// 使用网格布局显示指标
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

	// 计算每行的列数
	cols := 4
	if m.width < 100 {
		cols = 2
	}
	if m.width < 60 {
		cols = 1
	}

	for i, metric := range metrics {
		card := labelStyle.Render(metric.label) + "\n" + valueStyle.Render(metric.value)
		sb.WriteString(cardStyle.Width(20).Render(card))
		if (i+1)%cols == 0 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(helpStyle.Render("  Tab: 切换视图  |  j/k 或 ↓/↑: 在图表视图中导航  |  q: 退出"))

	return sb.String()
}

// renderBarView 渲染柱状图视图
func (m *statsModel) renderBarView() string {
	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

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

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// 标题
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" 📊 每日花费 (%s - %s) | 按 Tab 切换视图 | 按 q 退出 ", m.startDate, m.endDate)))
	sb.WriteString("\n\n")

	// 计算最大花费用于归一化
	var maxSpend float64
	for _, r := range m.data {
		if r.Metrics.Spend > maxSpend {
			maxSpend = r.Metrics.Spend
		}
	}

	// 渲染柱状图
	barMaxWidth := 30
	if m.width < 80 {
		barMaxWidth = 15
	}

	for i, r := range m.data {
		// 日期标签
		dateStr := r.Date
		if len(m.data) > 7 {
			// 如果数据太多，只显示部分日期
			if i%2 == 1 && i != len(m.data)-1 {
				dateStr = ""
			}
		}

		isSelected := i == m.selectedBarIndex

		// 柱状条
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

		// 显示花费
		sb.WriteString(fmt.Sprintf(" $%.2f", r.Metrics.Spend))

		// 显示日期
		if dateStr != "" {
			sb.WriteString("  ")
			sb.WriteString(dateLabelStyle.Render(dateStr))
		}

		sb.WriteString("\n")
	}

	// 详情面板
	if m.selectedBarIndex >= 0 && m.selectedBarIndex < len(m.data) {
		sb.WriteString("\n")
		sb.WriteString(m.renderDetailPanel(m.data[m.selectedBarIndex]))
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("  Tab: 切换视图  |  j/k 或 ↓/↑: 移动  |  q: 退出"))

	return sb.String()
}

// renderDetailPanel 渲染详情面板
func (m *statsModel) renderDetailPanel(data api.UserDailyActivity) string {
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

	sb.WriteString(panelStyle.Width(30).Render(content))

	return sb.String()
}
