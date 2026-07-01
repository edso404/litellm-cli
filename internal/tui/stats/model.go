package stats

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"litellm-cli/internal/api"
	"litellm-cli/internal/tui/components"
)

// TimeRangePreset 定义时间范围预设
type TimeRangePreset string

const (
	TimeRangeWeek     TimeRangePreset = "week"     // 最近一周
	TimeRangeMonth    TimeRangePreset = "month"    // 最近一个月
	TimeRange3Months  TimeRangePreset = "3months"  // 最近3个月
	TimeRangeHalfYear TimeRangePreset = "halfyear" // 最近半年
	TimeRangeYear     TimeRangePreset = "year"     // 今年
	TimeRangeCustom   TimeRangePreset = "custom"   // 自定义

	// 布局常量
	MinWindowHeight    = 10 // 最小窗口高度
	MinBarHeight       = 3  // 最小柱状图高度
	CounterHeaderLines = 2  // header 占用行数
	CounterFooterLines = 1  // footer 占用行数
)

// StatsClient defines the client interface required by the stats TUI
type StatsClient interface {
	GetUserDailyActivity(startDate, endDate string, pageSize int, page int) (*api.UserDailyActivityResponse, error)
	GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error)
}

// Model represents the stats TUI model
type Model struct {
	client           StatsClient
	startDate        string
	endDate          string
	timeRangePreset  TimeRangePreset // 当前时间范围预设
	data             []api.UserDailyActivity
	metadata         api.Metadata     // 从 API 返回的聚合数据
	aggregated       aggregatedMetrics
	selectedBarIndex int
	width            int
	height           int
	quitting         bool
	err              string
	loading          bool
	By               string // Aggregation dimension: "user", "team", etc.
	showHeader       bool   // 是否显示顶部 header（在 dashboard 中隐藏）
	granularity      string // "daily", "weekly", "monthly"
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
	m := &Model{
		client:           client,
		startDate:        startDate,
		endDate:          endDate,
		selectedBarIndex: -1,
		width:            120,
		height:           40,
		loading:          true,
		By:               "user",
		showHeader:       true, // 默认显示 header
		granularity:      "daily",
	}

	// 根据日期范围自动推断预设
	m.timeRangePreset = inferTimeRangePreset(startDate, endDate)

	// 根据范围推断粒度
	m.granularity = inferGranularity(startDate, endDate)

	// 默认选中第一项
	m.selectedBarIndex = 0

	return m
}

// inferGranularity 根据日期范围推断粒度
func inferGranularity(startDate, endDate string) string {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return "daily"
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return "daily"
	}

	days := int(end.Sub(start).Hours() / 24)

	switch {
	case days <= 15:
		return "daily"
	case days <= 60:
		return "weekly"
	default:
		return "monthly"
	}
}

// inferTimeRangePreset 根据日期范围推断预设
func inferTimeRangePreset(startDate, endDate string) TimeRangePreset {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return TimeRangeCustom
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return TimeRangeCustom
	}

	days := int(end.Sub(start).Hours() / 24)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yearStart := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)

	switch {
	case days <= 1:
		return TimeRangeWeek
	case days <= 7:
		return TimeRangeWeek
	case days <= 31:
		return TimeRangeMonth
	case days <= 93:
		return TimeRange3Months
	case days <= 183:
		return TimeRangeHalfYear
	default:
		if start.After(yearStart) || start.Equal(yearStart) {
			return TimeRangeYear
		}
		return TimeRangeCustom
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
		case "1":
			return m, m.switchTimeRange(TimeRangeWeek)
		case "2":
			return m, m.switchTimeRange(TimeRangeMonth)
		case "3":
			return m, m.switchTimeRange(TimeRange3Months)
		case "4":
			return m, m.switchTimeRange(TimeRangeHalfYear)
		case "5":
			return m, m.switchTimeRange(TimeRangeYear)
		case "d":
			m.granularity = "daily"
			return m, nil
		case "w":
			m.granularity = "weekly"
			return m, nil
		case "m":
			m.granularity = "monthly"
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

	// 响应式断点
	isLargeScreen := m.width >= 100

	var sb strings.Builder

	// 显示 header（如果启用）
	if m.showHeader {
		if isLargeScreen {
			header := components.NewHeader("用量统计看板", fmt.Sprintf("%s - %s | 按 q 退出", m.startDate, m.endDate))
			sb.WriteString(header.View(m.width))
			sb.WriteString("\n")
		} else {
			header := components.NewHeader("用量统计", fmt.Sprintf("%s - %s | 按 q 退出", m.startDate, m.endDate))
			sb.WriteString(header.View(m.width))
			sb.WriteString("\n")
		}
	}

	// 时间范围选择器
	sb.WriteString(m.renderTimeRangeSelector())
	sb.WriteString("\n")

	// 新布局：顶部紧凑卡片 + 底部水平柱状图
	// 顶部卡片区域（紧凑排列）
	counterWidth := m.width - 4
	if counterWidth > 100 {
		counterWidth = 100
	}

	// 动态计算可用高度，确保内容不会超出显示区域
	availableHeight := m.height
	if m.showHeader {
		availableHeight -= CounterHeaderLines + CounterFooterLines // header(2) + footer(1)
	}
	if availableHeight < MinWindowHeight {
		availableHeight = MinWindowHeight
	}

	// 时间选择器固定 1 行，分隔线 1 行（如果是大屏）
	fixedContentLines := 1
	if isLargeScreen {
		fixedContentLines++ // 大屏有分隔线
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

	// 传递 maxRows 给 counter 渲染，获取实际渲染行数和内容
	counterOutput, counterActualRows := m.renderCounterContent(counterWidth, counterMaxRows)
	sb.WriteString(counterOutput)
	sb.WriteString("\n")

	// 分隔线（在 counter 之后）
	if isLargeScreen {
		sb.WriteString(strings.Repeat("─", m.width))
		sb.WriteString("\n")
	}

	// 计算 bar 可用高度
	barAvailableHeight := availableHeight - counterActualRows
	if isLargeScreen {
		barAvailableHeight-- // 减去分隔线
	}
	if barAvailableHeight < MinBarHeight {
		barAvailableHeight = MinBarHeight
	}

	// 底部水平柱状图
	barWidth := m.width - 4
	sb.WriteString(m.renderBarContent(barWidth, barAvailableHeight))
	sb.WriteString("\n")

	// 底部帮助信息（独立运行时显示，嵌入 dashboard 时由 dashboard 提供 footer）
	if m.showHeader {
		sb.WriteString(m.renderFooter())
	}

	return sb.String()
}

// RefreshCmd performs asynchronous data loading with pagination support
func (m *Model) RefreshCmd() tea.Cmd {
	return func() tea.Msg {
		var data []api.UserDailyActivity
		var meta api.Metadata
		var err error

		// 根据时间范围动态调整 pageSize
		// 大范围用更大的 pageSize 减少请求次数
		pageSize := m.calculatePageSize()

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
			// 并发获取所有页面数据
			data, meta, err = m.fetchAllPages(pageSize)
		}
		m.metadata = meta
		return StatsLoadedMsg{Data: data, Error: err}
	}
}

// calculatePageSize 根据时间范围计算合适的 pageSize
func (m *Model) calculatePageSize() int {
	start, err := time.Parse("2006-01-02", m.startDate)
	if err != nil {
		return 0 // 使用默认
	}
	end, err := time.Parse("2006-01-02", m.endDate)
	if err != nil {
		return 0
	}

	days := int(end.Sub(start).Hours() / 24)
	if days <= 7 {
		return 0 // 小范围用默认
	}
	// 大范围用更大的 pageSize
	if days <= 31 {
		return 100
	}
	if days <= 90 {
		return 200
	}
	return 500 // 半年及以上的都用大 pageSize
}

// fetchAllPages 并发获取所有页面数据
func (m *Model) fetchAllPages(pageSize int) ([]api.UserDailyActivity, api.Metadata, error) {
	// 先获取第一页，确定总页数
	firstResp, err := m.client.GetUserDailyActivity(m.startDate, m.endDate, pageSize, 1)
	if err != nil {
		return nil, api.Metadata{}, err
	}
	if firstResp == nil {
		return nil, api.Metadata{}, err
	}

	totalPages := firstResp.Metadata.TotalPages
	hasMore := firstResp.Metadata.HasMore

	// 如果只有一页或没有更多数据，直接返回
	if totalPages <= 1 && !hasMore {
		return firstResp.Results, firstResp.Metadata, nil
	}

	// 并发获取剩余页面
	var allResults []api.UserDailyActivity
	allResults = append(allResults, firstResp.Results...)

	if totalPages > 1 {
		// 并发获取第 2 页到第 totalPages 页
		var wg sync.WaitGroup
		var mu sync.Mutex
		results := make([][]api.UserDailyActivity, totalPages)
		errors := make([]error, totalPages)

		for page := 2; page <= totalPages; page++ {
			wg.Add(1)
			go func(p int) {
				defer wg.Done()
				resp, err := m.client.GetUserDailyActivity(m.startDate, m.endDate, pageSize, p)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errors[p-1] = err
				} else if resp != nil {
					results[p-1] = resp.Results
				}
			}(page)
		}
		wg.Wait()

		// 检查是否有错误
		for _, e := range errors {
			if e != nil {
				return nil, api.Metadata{}, e
			}
		}

		// 合并结果
		for _, r := range results {
			allResults = append(allResults, r...)
		}
	}

	// 使用第一页的 metadata（包含 total_spend 等聚合数据）
	return allResults, firstResp.Metadata, nil
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

// renderFooter 渲染底部帮助信息
func (m *Model) renderFooter() string {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tip := "1-5: 时间范围 | d/w/m: 粒度 | ↑↓: 选择日期 | esc: 返回 | ←/→: 切换 tab | q: 退出"
	return mutedStyle.Render(tip)
}

func (m *Model) renderCounterContent(width int, maxRows int) (string, int) {
	var sb strings.Builder

	// 紧凑卡片样式（无边框紧凑显示）
	cardStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("159"))

	// 6 个核心指标（紧凑显示：图标 + 标签 + 数值在同一行）
	metrics := []struct {
		label string
		value string
	}{
		{"💰 总花费", fmt.Sprintf("$%.2f", m.aggregated.TotalSpend)},
		{"📤 请求", fmt.Sprintf("%d", m.aggregated.TotalRequests)},
		{"✅ 成功", fmt.Sprintf("%d", m.aggregated.Successful)},
		{"❌ 失败", fmt.Sprintf("%d", m.aggregated.Failed)},
		{"📊 Tokens", formatTokens(m.aggregated.TotalTokens)},
		{"📈 均费", fmt.Sprintf("$%.4f", m.aggregated.AvgCostPerReq)},
	}

	// 动态列数：大屏3列，中屏2列，小屏1列
	cols := 3
	if width < 80 {
		cols = 2
	}
	if width < 40 {
		cols = 1
	}

	// 根据 maxRows 限制实际渲染的指标数量
	visibleMetrics := metrics
	maxVisibleRows := maxRows
	if maxVisibleRows < 1 {
		maxVisibleRows = 1
	}
	maxVisibleCount := maxVisibleRows * cols
	if len(visibleMetrics) > maxVisibleCount {
		visibleMetrics = metrics[:maxVisibleCount]
	}

	// 计算实际渲染行数
	actualRows := (len(visibleMetrics) + cols - 1) / cols
	if actualRows < 1 {
		actualRows = 1
	}

	// 更短的卡片宽度
	cardWidth := (width / cols) - 1
	if cardWidth < 14 {
		cardWidth = 14
	}
	if cardWidth > 22 {
		cardWidth = 22
	}

	// 渲染紧凑卡片（图标+标签+数值单行显示，无边框高度）
	for row := 0; row < len(visibleMetrics); row += cols {
		var rowCards []string
		for col := 0; col < cols && row+col < len(visibleMetrics); col++ {
			metric := visibleMetrics[row+col]
			// 紧凑格式：标签 + 数值（单行）
			card := labelStyle.Render(metric.label) + " " + valueStyle.Render(metric.value)
			rowCards = append(rowCards, cardStyle.Width(cardWidth).Render(card))
		}
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rowCards...))
		sb.WriteString("\n")
	}

	return sb.String(), actualRows
}

func (m *Model) renderBarContent(width int, maxLines int) string {
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

	spendLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("159"))

	// 根据时间范围和数据量调整显示粒度
	displayData := m.adjustDataForGranularity()

	var maxSpend float64
	for _, r := range displayData {
		if r.Metrics.Spend > maxSpend {
			maxSpend = r.Metrics.Spend
		}
	}

	// 水平柱状图：计算进度条可用宽度
	// 布局：[日期] [████████░░░░] $XX.XX
	// 日期占 12 字符，金额占 10 字符，预留 2 字符间距
	labelWidth := 12
	spendWidth := 10
	barAvailableWidth := width - labelWidth - spendWidth - 4
	if barAvailableWidth < 8 {
		barAvailableWidth = 8
	}
	if barAvailableWidth > 50 {
		barAvailableWidth = 50
	}

	// 限制实际渲染的行数（确保不超出可用高度）
	renderCount := len(displayData)
	if renderCount > maxLines {
		renderCount = maxLines
	}

	// 渲染每日的水平进度条
	for i := 0; i < renderCount; i++ {
		r := displayData[i]
		isSelected := i == m.selectedBarIndex

		// 计算进度条宽度
		var barWidth int
		if maxSpend > 0 {
			barWidth = int(float64(barAvailableWidth) * r.Metrics.Spend / maxSpend)
		}
		barStr := strings.Repeat("█", barWidth)
		barStr += strings.Repeat("░", barAvailableWidth-barWidth)

		// 格式化日期（简化显示）
		dateStr := r.Date
		if len(dateStr) > 10 {
			dateStr = dateStr[5:] // 只显示 MM-DD
		}

		// 渲染行：[日期] [████████░░] $XX.XX
		if isSelected {
			sb.WriteString(selectedStyle.Render("▶ "))
			sb.WriteString(dateLabelStyle.Render(fmt.Sprintf("%-12s", dateStr)))
			sb.WriteString(" ")
			sb.WriteString(barFocusedStyle.Render(barStr))
			sb.WriteString(" ")
			sb.WriteString(spendLabelStyle.Render(fmt.Sprintf("$%.2f", r.Metrics.Spend)))
		} else {
			sb.WriteString("  ")
			sb.WriteString(dateLabelStyle.Render(fmt.Sprintf("%-12s", dateStr)))
			sb.WriteString(" ")
			sb.WriteString(barStyle.Render(barStr))
			sb.WriteString(" ")
			sb.WriteString(spendLabelStyle.Render(fmt.Sprintf("$%.2f", r.Metrics.Spend)))
		}

		sb.WriteString("\n")
	}

	// 如果有更多数据未显示，添加提示
	if len(displayData) > maxLines {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("  ... 还有 %d 条数据未显示", len(displayData)-maxLines)))
		sb.WriteString("\n")
	}

	// 选中项详情面板（显示在底部）
	if m.selectedBarIndex >= 0 && m.selectedBarIndex < len(displayData) {
		sb.WriteString("\n")
		sb.WriteString(m.renderDetailPanelCompact(displayData[m.selectedBarIndex], width))
	}

	return sb.String()
}

// adjustDataForGranularity 根据手动选择的粒度调整数据
func (m *Model) adjustDataForGranularity() []api.UserDailyActivity {
	days := len(m.data)
	if days == 0 {
		return m.data
	}

	// 使用手动选择的粒度
	switch m.granularity {
	case "weekly":
		return m.aggregateByWeek()
	case "monthly":
		return m.aggregateByMonth()
	default: // "daily"
		return m.data
	}
}

// aggregateByWeek 按周汇总数据
func (m *Model) aggregateByWeek() []api.UserDailyActivity {
	if len(m.data) == 0 {
		return m.data
	}

	// 按周分组
	weeklyData := make(map[string]api.UserDailyActivity)
	for _, d := range m.data {
		date, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		// 获取周一的日期
		weekday := int(date.Weekday())
		if weekday == 0 {
			weekday = 7 // 周日转为7
		}
		monday := date.AddDate(0, 0, -(weekday - 1))
		weekKey := monday.Format("2006-01-02")

		if existing, ok := weeklyData[weekKey]; ok {
			existing.Metrics.Spend += d.Metrics.Spend
			existing.Metrics.APIRequests += d.Metrics.APIRequests
			existing.Metrics.SuccessfulRequests += d.Metrics.SuccessfulRequests
			existing.Metrics.FailedRequests += d.Metrics.FailedRequests
			existing.Metrics.PromptTokens += d.Metrics.PromptTokens
			existing.Metrics.CompletionTokens += d.Metrics.CompletionTokens
			existing.Metrics.TotalTokens += d.Metrics.TotalTokens
			weeklyData[weekKey] = existing
		} else {
			weeklyData[weekKey] = api.UserDailyActivity{
				Date: weekKey,
				Metrics: api.ActivityMetrics{
					Spend:              d.Metrics.Spend,
					APIRequests:        d.Metrics.APIRequests,
					SuccessfulRequests: d.Metrics.SuccessfulRequests,
					FailedRequests:     d.Metrics.FailedRequests,
					PromptTokens:       d.Metrics.PromptTokens,
					CompletionTokens:   d.Metrics.CompletionTokens,
					TotalTokens:        d.Metrics.TotalTokens,
				},
			}
		}
	}

	// 转换为切片并排序
	result := make([]api.UserDailyActivity, 0, len(weeklyData))
	for _, v := range weeklyData {
		result = append(result, v)
	}

	// 按日期排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	return result
}

// aggregateByMonth 按月汇总数据
func (m *Model) aggregateByMonth() []api.UserDailyActivity {
	if len(m.data) == 0 {
		return m.data
	}

	// 按月分组
	monthlyData := make(map[string]api.UserDailyActivity)
	for _, d := range m.data {
		date, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		monthKey := date.Format("2006-01")

		if existing, ok := monthlyData[monthKey]; ok {
			existing.Metrics.Spend += d.Metrics.Spend
			existing.Metrics.APIRequests += d.Metrics.APIRequests
			existing.Metrics.SuccessfulRequests += d.Metrics.SuccessfulRequests
			existing.Metrics.FailedRequests += d.Metrics.FailedRequests
			existing.Metrics.PromptTokens += d.Metrics.PromptTokens
			existing.Metrics.CompletionTokens += d.Metrics.CompletionTokens
			existing.Metrics.TotalTokens += d.Metrics.TotalTokens
			monthlyData[monthKey] = existing
		} else {
			monthlyData[monthKey] = api.UserDailyActivity{
				Date: monthKey,
				Metrics: api.ActivityMetrics{
					Spend:              d.Metrics.Spend,
					APIRequests:        d.Metrics.APIRequests,
					SuccessfulRequests: d.Metrics.SuccessfulRequests,
					FailedRequests:     d.Metrics.FailedRequests,
					PromptTokens:       d.Metrics.PromptTokens,
					CompletionTokens:   d.Metrics.CompletionTokens,
					TotalTokens:        d.Metrics.TotalTokens,
				},
			}
		}
	}

	// 转换为切片并排序
	result := make([]api.UserDailyActivity, 0, len(monthlyData))
	for _, v := range monthlyData {
		result = append(result, v)
	}

	// 按日期排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	return result
}

// renderDetailPanelCompact 显示选中日期的超紧凑详情面板（单行）
func (m *Model) renderDetailPanelCompact(data api.UserDailyActivity, width int) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("159"))

	// 单行显示所有指标
	detail := keyStyle.Render("📅 ") + valueStyle.Render(fmt.Sprintf("%s ", data.Date))
	detail += keyStyle.Render("💰 ") + valueStyle.Render(fmt.Sprintf("$%.2f ", data.Metrics.Spend))
	detail += keyStyle.Render("📤 ") + valueStyle.Render(fmt.Sprintf("%d ", data.Metrics.APIRequests))
	detail += keyStyle.Render("✅ ") + valueStyle.Render(fmt.Sprintf("%d ", data.Metrics.SuccessfulRequests))
	detail += keyStyle.Render("❌ ") + valueStyle.Render(fmt.Sprintf("%d ", data.Metrics.FailedRequests))
	detail += keyStyle.Render("📊 ") + valueStyle.Render(formatTokens(data.Metrics.TotalTokens))

	return detail
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

// switchTimeRange 切换时间范围并重新加载数据
func (m *Model) switchTimeRange(preset TimeRangePreset) tea.Cmd {
	m.timeRangePreset = preset
	m.startDate, m.endDate = getTimeRangeDates(preset)
	m.loading = true
	m.data = nil // 清空数据，确保刷新
	m.selectedBarIndex = -1

	// 同时更新粒度
	m.granularity = inferGranularity(m.startDate, m.endDate)

	return m.RefreshCmd()
}

// getTimeRangeDates 根据预设获取日期范围
func getTimeRangeDates(preset TimeRangePreset) (string, string) {
	now := time.Now()
	endDate := now.Format("2006-01-02")

	var startDate time.Time
	switch preset {
	case TimeRangeWeek:
		startDate = now.AddDate(0, 0, -7)
	case TimeRangeMonth:
		startDate = now.AddDate(0, -1, 0)
	case TimeRange3Months:
		startDate = now.AddDate(0, -3, 0)
	case TimeRangeHalfYear:
		startDate = now.AddDate(0, -6, 0)
	case TimeRangeYear:
		startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		startDate = now.AddDate(0, 0, -7)
	}

	return startDate.Format("2006-01-02"), endDate
}

// renderTimeRangeSelector 渲染时间范围选择器
func (m *Model) renderTimeRangeSelector() string {
	presets := []struct {
		preset   TimeRangePreset
		label    string
		shortcut string
	}{
		{TimeRangeWeek, "最近一周", "1"},
		{TimeRangeMonth, "最近一个月", "2"},
		{TimeRange3Months, "最近3个月", "3"},
		{TimeRangeHalfYear, "最近半年", "4"},
		{TimeRangeYear, "今年", "5"},
	}

	// 当前范围显示
	currentRange := fmt.Sprintf("%s - %s", m.startDate, m.endDate)

	// 构建时间范围按钮
	var buttons []string
	for _, p := range presets {
		isActive := m.timeRangePreset == p.preset
		btnStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1)

		if isActive {
			btnStyle = btnStyle.
				Foreground(lipgloss.Color("86")).
				Bold(true)
		}

		btn := btnStyle.Render(fmt.Sprintf("[%s] %s", p.shortcut, p.label))
		buttons = append(buttons, btn)
	}

	// 粒度切换按钮
	granularityOptions := []struct {
		granularity string
		label       string
		shortcut    string
	}{
		{"daily", "按天", "d"},
		{"weekly", "按周", "w"},
		{"monthly", "按月", "m"},
	}

	var granButtons []string
	for _, g := range granularityOptions {
		isActive := m.granularity == g.granularity
		btnStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1)

		if isActive {
			btnStyle = btnStyle.
				Foreground(lipgloss.Color("82")). // 亮绿色
				Bold(true)
		}

		btn := btnStyle.Render(fmt.Sprintf("[%s] %s", g.shortcut, g.label))
		granButtons = append(granButtons, btn)
	}

	// 当前范围标签
	rangeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("159")).
		Bold(true)

	// 第一行：时间范围
	result := rangeStyle.Render("📅 " + currentRange + "  ")
	result += lipgloss.JoinHorizontal(0, buttons...)
	result += "\n"

	// 第二行：粒度选择
	result += rangeStyle.Render("📊 粒度: ")
	result += lipgloss.JoinHorizontal(0, granButtons...)

	return result
}
