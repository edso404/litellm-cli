package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"litellm-cli/internal/api"
)

// UsageRankSortType 定义排序类型
type UsageRankSortType string

const (
	SortByTokens   UsageRankSortType = "tokens"
	SortByRequests UsageRankSortType = "requests"
)

// UsageRankTimeRange 定义时间范围
type UsageRankTimeRange string

const (
	TimeRangeWeek      UsageRankTimeRange = "week"      // 最近一周
	TimeRangeMonth     UsageRankTimeRange = "month"     // 最近一个月
	TimeRange3Months   UsageRankTimeRange = "3months"   // 最近3个月
	TimeRangeHalfYear  UsageRankTimeRange = "halfyear"  // 最近半年
	TimeRangeYear      UsageRankTimeRange = "year"      // 今年
	TimeRangeCustom    UsageRankTimeRange = "custom"    // 自定义
)

// UsageRankClient 定义用量排行数据获取的接口
type UsageRankClient interface {
	GetUserInfo() (*UserInfo, error)
	GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error)
}

// UserUsageRank 用户用量排名
type UserUsageRank struct {
	UserID           string
	Email            string
	TotalTokens      int64
	PromptTokens     int64
	CompletionTokens int64
	Requests         int64
	FailedRequests   int64
	Percent          float64
	Rank             int
	IsMe             bool
	Models           map[string]int64 // model -> tokens
}

// UsageRankResponse 用量排行响应
type UsageRankResponse struct {
	StartDate    string
	EndDate      string
	TotalTokens  int64
	TotalReqs    int64
	CurrentRank  *UserUsageRank
	Ranks        []UserUsageRank
	TimeRange    UsageRankTimeRange
	SortType     UsageRankSortType
}

// usageRankModel 是用量排行的 TUI Model
type usageRankModel struct {
	client         UsageRankClient
	apiClient      *api.Client
	teamID         string
	data           *UsageRankResponse
	selectedIndex  int
	detailView     bool
	detailSelected int
	width          int
	height         int
	loading        bool
	err            string
	quitting       bool
	timeRange      UsageRankTimeRange
	sortType       UsageRankSortType
	startDate      string
	endDate        string
	keyAliasToUser map[string]string // key_alias -> user_id
	userIDToEmail  map[string]string // user_id -> email
	currentUserID  string
}

// newUsageRankModel 创建新的用量排行 Model
func newUsageRankModel(client UsageRankClient) *usageRankModel {
	now := time.Now()
	today := now.Format("2006-01-02")
	m := &usageRankModel{
		client:         client,
		selectedIndex:  0,
		width:          120,
		height:         40,
		loading:        true,
		timeRange:      TimeRangeWeek,
		sortType:       SortByTokens,
		keyAliasToUser: make(map[string]string),
		userIDToEmail:  make(map[string]string),
	}
	// 设置默认日期范围
	m.startDate = now.AddDate(0, 0, -7).Format("2006-01-02")
	m.endDate = today
	return m
}

// SetAPIClient 设置 API client
func (m *usageRankModel) SetAPIClient(apiClient *api.Client) {
	m.apiClient = apiClient
}

// SetCurrentUserID 设置当前用户 ID
func (m *usageRankModel) SetCurrentUserID(userID string) {
	m.currentUserID = userID
}

// Init 实现 tea.Model 接口
func (m *usageRankModel) Init() tea.Cmd {
	return m.refreshCmd()
}

// Update 实现 tea.Model 接口
func (m *usageRankModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "1":
			m.timeRange = TimeRangeWeek
			m.loading = true
			return m, m.refreshCmd()
		case "2":
			m.timeRange = TimeRangeMonth
			m.loading = true
			return m, m.refreshCmd()
		case "3":
			m.timeRange = TimeRange3Months
			m.loading = true
			return m, m.refreshCmd()
		case "4":
			m.timeRange = TimeRangeHalfYear
			m.loading = true
			return m, m.refreshCmd()
		case "5":
			m.timeRange = TimeRangeYear
			m.loading = true
			return m, m.refreshCmd()
		case "t":
			// 循环切换时间范围（兼容旧快捷键）
			m.cycleTimeRange()
			m.loading = true
			return m, m.refreshCmd()
		case "o":
			// 切换排序方式
			m.toggleSortType()
			if m.data != nil {
				m.sortData()
			}
			return m, nil
		case "/":
			// TODO: 自定义日期范围（后续实现）
			return m, nil
		case "enter":
			// 进入详情视图
			if !m.detailView && m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex < len(m.data.Ranks) {
				r := m.data.Ranks[m.selectedIndex]
				if len(r.Models) >= 1 {
					m.detailView = true
					m.detailSelected = 0
				}
			}
		case "esc":
			// 退出详情视图
			if m.detailView {
				m.detailView = false
				m.detailSelected = 0
			}
		case "down", "j":
			if m.detailView {
				r := m.data.Ranks[m.selectedIndex]
				modelCount := len(r.Models)
				if modelCount > 0 && m.detailSelected < modelCount-1 {
					m.detailSelected++
				}
			} else {
				if m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex < len(m.data.Ranks)-1 {
					m.selectedIndex++
				}
			}
		case "up", "k":
			if m.detailView {
				if m.detailSelected > 0 {
					m.detailSelected--
				}
			} else {
				if m.selectedIndex > 0 {
					m.selectedIndex--
				}
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case usageRankLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
		// 数据刷新后重置 selectedIndex
		if m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex >= len(m.data.Ranks) {
			m.selectedIndex = len(m.data.Ranks) - 1
		}
	}
	return m, nil
}

// cycleTimeRange 循环切换时间范围（兼容旧快捷键）
func (m *usageRankModel) cycleTimeRange() {
	now := time.Now()
	today := now.Format("2006-01-02")

	switch m.timeRange {
	case TimeRangeWeek:
		m.timeRange = TimeRangeMonth
	case TimeRangeMonth:
		m.timeRange = TimeRange3Months
	case TimeRange3Months:
		m.timeRange = TimeRangeHalfYear
	case TimeRangeHalfYear:
		m.timeRange = TimeRangeYear
	case TimeRangeYear:
		m.timeRange = TimeRangeWeek
	default:
		m.timeRange = TimeRangeWeek
	}

	// 更新日期范围
	m.updateDateRange(today)
}

// updateDateRange 根据预设更新日期范围
func (m *usageRankModel) updateDateRange(today string) {
	now, _ := time.Parse("2006-01-02", today)
	switch m.timeRange {
	case TimeRangeWeek:
		m.startDate = now.AddDate(0, 0, -7).Format("2006-01-02")
		m.endDate = today
	case TimeRangeMonth:
		m.startDate = now.AddDate(0, -1, 0).Format("2006-01-02")
		m.endDate = today
	case TimeRange3Months:
		m.startDate = now.AddDate(0, -3, 0).Format("2006-01-02")
		m.endDate = today
	case TimeRangeHalfYear:
		m.startDate = now.AddDate(0, -6, 0).Format("2006-01-02")
		m.endDate = today
	case TimeRangeYear:
		m.startDate = fmt.Sprintf("%d-01-01", now.Year())
		m.endDate = today
	}
}

// toggleSortType 切换排序方式
func (m *usageRankModel) toggleSortType() {
	if m.sortType == SortByTokens {
		m.sortType = SortByRequests
	} else {
		m.sortType = SortByTokens
	}
}

// sortData 对数据进行排序
func (m *usageRankModel) sortData() {
	if m.data == nil {
		return
	}

	sort.Slice(m.data.Ranks, func(i, j int) bool {
		if m.sortType == SortByTokens {
			return m.data.Ranks[i].TotalTokens > m.data.Ranks[j].TotalTokens
		}
		return m.data.Ranks[i].Requests > m.data.Ranks[j].Requests
	})

	// 重新设置排名
	for i := range m.data.Ranks {
		m.data.Ranks[i].Rank = i + 1
	}

	// 找到当前用户排名
	var currentRank *UserUsageRank
	for i := range m.data.Ranks {
		if m.data.Ranks[i].IsMe {
			currentRank = &m.data.Ranks[i]
			break
		}
	}
	m.data.CurrentRank = currentRank
}

// View 实现 tea.Model 接口
func (m *usageRankModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}
	if m.loading {
		return "加载中...\n"
	}
	if m.err != "" {
		return "错误: " + m.err + "\n"
	}
	if m.data == nil || len(m.data.Ranks) == 0 {
		return "暂无数据\n"
	}

	// 确保 selectedIndex 不会越界
	if m.selectedIndex >= len(m.data.Ranks) {
		m.selectedIndex = len(m.data.Ranks) - 1
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}

	var sb strings.Builder

	// 渲染时间范围选择器
	sb.WriteString(m.renderTimeRangeSelector())
	sb.WriteString("\n")

	// 排序信息
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sortStr := "Token"
	if m.sortType == SortByRequests {
		sortStr = "请求数"
	}
	sb.WriteString(greenStyle.Render(fmt.Sprintf("  排序: %s (按 o 切换)", sortStr)))
	sb.WriteString("\n\n")

	// 计算可用高度
	fixedLines := 10
	maxRankLines := m.height - fixedLines
	if maxRankLines < 3 {
		maxRankLines = 3
	}

	// 计算可视窗口
	totalRanks := len(m.data.Ranks)
	viewStart := 0
	viewEnd := maxRankLines
	if totalRanks > maxRankLines {
		if m.selectedIndex >= viewEnd {
			viewStart = m.selectedIndex - maxRankLines + 1
			viewEnd = m.selectedIndex + 1
		}
		if m.selectedIndex < viewStart {
			viewStart = m.selectedIndex
			viewEnd = m.selectedIndex + maxRankLines
		}
		if viewEnd > totalRanks {
			viewEnd = totalRanks
			viewStart = viewEnd - maxRankLines
			if viewStart < 0 {
				viewStart = 0
			}
		}
	}

	// 列宽
	rankWidth := 5
	emailWidth := 22
	tokenWidth := 12
	reqWidth := 10
	percentWidth := 8

	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}
	padLeft := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return strings.Repeat(" ", width-w) + s
	}

	// 表头
	sb.WriteString(fmt.Sprintf("  %s %s %s %s %s\n",
		padRight("排名", rankWidth),
		padRight("用户", emailWidth),
		padLeft("Token", tokenWidth),
		padLeft("请求数", reqWidth),
		padLeft("占比", percentWidth)))
	sb.WriteString(mutedStyle.Render(" " + strings.Repeat("─", 70)))
	sb.WriteString("\n")

	// 渲染排名
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))

	for i := viewStart; i < viewEnd && i < totalRanks; i++ {
		r := m.data.Ranks[i]
		email := r.Email
		if runewidth.StringWidth(email) > emailWidth {
			email = runewidth.Truncate(email, emailWidth-3, "...")
		}

		rankStr := fmt.Sprintf("#%d", r.Rank)
		if r.IsMe {
			rankStr = "→" + fmt.Sprintf("%d", r.Rank)
		}

		rankPadded := padRight(rankStr, rankWidth)
		emailPadded := padRight(email, emailWidth)

		// Token 显示格式化
		tokenStr := formatTokens(r.TotalTokens)
		tokenPadded := padLeft(tokenStr, tokenWidth)

		reqStr := fmt.Sprintf("%d", r.Requests)
		reqPadded := padLeft(reqStr, reqWidth)

		percentPadded := padLeft(fmt.Sprintf("%.1f%%", r.Percent), percentWidth)

		lineStyle := greenStyle
		if r.IsMe {
			lineStyle = cyanStyle
		} else if i == m.selectedIndex {
			lineStyle = selectedStyle
		}

		sb.WriteString(lineStyle.Render("  " + rankPadded))
		sb.WriteString(lineStyle.Render(emailPadded))
		sb.WriteString(" " + lineStyle.Render(tokenPadded))
		sb.WriteString(" " + lineStyle.Render(reqPadded))
		sb.WriteString(" " + lineStyle.Render(percentPadded))
		sb.WriteString("\n")
	}

	// 滚动提示
	if totalRanks > maxRankLines {
		visibleStart := viewStart + 1
		visibleEnd := viewEnd
		if visibleEnd > totalRanks {
			visibleEnd = totalRanks
		}
		sb.WriteString(mutedStyle.Render(fmt.Sprintf("  ▼ 第 %d-%d / %d 名 ▼", visibleStart, visibleEnd, totalRanks)))
		sb.WriteString("\n")
	}

	// 显示当前用户排名
	if m.data.CurrentRank != nil {
		r := m.data.CurrentRank
		sb.WriteString("\n")
		sb.WriteString(cyanStyle.Render(fmt.Sprintf("  📊 你的排名: #%d / %d", r.Rank, len(m.data.Ranks))))
		sb.WriteString("\n")
		sb.WriteString(cyanStyle.Render(fmt.Sprintf("    Token: %s (占比 %.1f%%)  |  请求数: %d",
			formatTokens(r.TotalTokens), r.Percent, r.Requests)))
		sb.WriteString("\n")
	}

	// 详情视图
	if m.detailView {
		return m.renderDetailView()
	}

	return sb.String()
}

// getTimeRangeDisplay 获取时间范围显示文本
func (m *usageRankModel) getTimeRangeDisplay() string {
	switch m.timeRange {
	case TimeRangeWeek:
		return "最近7天"
	case TimeRangeMonth:
		return "最近30天"
	case TimeRange3Months:
		return "最近3个月"
	case TimeRangeHalfYear:
		return "最近半年"
	case TimeRangeYear:
		return "今年"
	case TimeRangeCustom:
		return m.startDate + " ~ " + m.endDate
	default:
		return "最近7天"
	}
}

// renderTimeRangeSelector 渲染时间范围选择器
func (m *usageRankModel) renderTimeRangeSelector() string {
	presets := []struct {
		preset   UsageRankTimeRange
		label    string
		shortcut string
	}{
		{TimeRangeWeek, "最近7天", "1"},
		{TimeRangeMonth, "最近30天", "2"},
		{TimeRange3Months, "最近3个月", "3"},
		{TimeRangeHalfYear, "最近半年", "4"},
		{TimeRangeYear, "今年", "5"},
	}

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// 构建时间范围按钮
	var buttons []string
	for _, p := range presets {
		isActive := m.timeRange == p.preset
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

	return greenStyle.Render("  时间范围: ") + mutedStyle.Render(strings.Join(buttons, " "))
}

// formatTokens 格式化 Token 数量
func formatTokens(tokens int64) string {
	if tokens >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(tokens)/1_000_000_000)
	}
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

// renderDetailView 渲染模型明细视图
func (m *usageRankModel) renderDetailView() string {
	r := m.data.Ranks[m.selectedIndex]
	models := r.Models

	// 转换为切片并排序
	type modelUsage struct {
		name   string
		tokens int64
	}
	var modelList []modelUsage
	for name, tokens := range models {
		modelList = append(modelList, modelUsage{name: name, tokens: tokens})
	}
	sort.Slice(modelList, func(i, j int) bool {
		return modelList[i].tokens > modelList[j].tokens
	})

	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))

	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}
	padLeft := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return strings.Repeat(" ", width-w) + s
	}

	// 标题
	sb.WriteString(headerStyle.Render(fmt.Sprintf("  📋 %s 使用的模型 (%d个)", r.Email, len(models))))
	sb.WriteString("\n\n")

	// 表头
	sb.WriteString("  # ")
	sb.WriteString(mutedStyle.Render(padRight("模型", 45)))
	sb.WriteString(mutedStyle.Render(padLeft("Token", 12)))
	sb.WriteString(mutedStyle.Render(padLeft("占比", 8)))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  " + strings.Repeat("─", 70)))
	sb.WriteString("\n")

	// 确保 detailSelected 不越界
	if m.detailSelected >= len(modelList) {
		m.detailSelected = len(modelList) - 1
	}
	if m.detailSelected < 0 {
		m.detailSelected = 0
	}

	totalTokens := r.TotalTokens
	for i, mu := range modelList {
		percent := 0.0
		if totalTokens > 0 {
			percent = float64(mu.tokens) / float64(totalTokens) * 100
		}

		modelName := mu.name
		if runewidth.StringWidth(modelName) > 40 {
			modelName = runewidth.Truncate(modelName, 37, "...")
		}

		lineStyle := greenStyle
		if i == m.detailSelected {
			lineStyle = selectedStyle
		}

		indexStr := fmt.Sprintf("%d", i+1)
		sb.WriteString(lineStyle.Render("  " + padRight(indexStr, 2)))
		sb.WriteString(lineStyle.Render(padRight(modelName, 45)))
		sb.WriteString(" " + lineStyle.Render(padLeft(formatTokens(mu.tokens), 12)))
		sb.WriteString(" " + lineStyle.Render(padLeft(fmt.Sprintf("%.1f%%", percent), 8)))
		sb.WriteString("\n")
	}

	// 统计信息
	sb.WriteString("\n")
	sb.WriteString(greenStyle.Render(fmt.Sprintf("  总 Token: %s", formatTokens(totalTokens))))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓: 移动 | esc: 返回 | enter: 选中"))

	return sb.String()
}

// refreshCmd 刷新数据的命令
func (m *usageRankModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		// 1. 获取用户信息和团队列表
		userInfo, err := m.client.GetUserInfo()
		if err != nil {
			return usageRankLoadedMsg{Error: err}
		}

		if len(userInfo.Teams) == 0 {
			return usageRankLoadedMsg{Error: fmt.Errorf("没有找到所属团队")}
		}

		// 保存当前用户ID
		m.currentUserID = userInfo.UserID

		// 使用第一个团队
		team := userInfo.Teams[0]
		m.teamID = team.TeamID

		// 构建 user_id -> email 映射
		m.userIDToEmail = make(map[string]string)
		for _, member := range team.Members {
			m.userIDToEmail[member.UserID] = member.Email
		}

		// 构建 key_alias -> user_id 映射
		m.keyAliasToUser = make(map[string]string)
		for _, k := range team.Keys {
			if k.UserID != "" && k.KeyAlias != "" {
				m.keyAliasToUser[k.KeyAlias] = k.UserID
			}
		}

		// 2. 获取团队每日活动数据
		activityResp, err := m.client.GetTeamDailyActivity(m.startDate, m.endDate)
		if err != nil {
			return usageRankLoadedMsg{Error: err}
		}

		// 3. 按 user_id 聚合数据
		userTokens := make(map[string]int64)
		userPromptTokens := make(map[string]int64)
		userCompletionTokens := make(map[string]int64)
		userRequests := make(map[string]int64)
		userFailedRequests := make(map[string]int64)
		userModels := make(map[string]map[string]int64) // user_id -> model -> tokens

		for _, day := range activityResp.Results {
			// 遍历 api_keys 获取每个 key 的数据
			// API 返回的 key 是实际的 api_key，key_alias 在 metadata 中
			for key, keyData := range day.Breakdown.APIKeys {
				// 获取 key_alias
				keyAlias := key
				if keyData.Metadata != nil {
					if alias, ok := keyData.Metadata["key_alias"]; ok {
						keyAlias = alias
					}
				}

				userID, ok := m.keyAliasToUser[keyAlias]
				if !ok {
					continue
				}

				metrics := keyData.Metrics
				userTokens[userID] += metrics.TotalTokens
				userPromptTokens[userID] += metrics.PromptTokens
				userCompletionTokens[userID] += metrics.CompletionTokens
				userRequests[userID] += metrics.SuccessfulRequests
				userFailedRequests[userID] += metrics.FailedRequests

				// 收集模型信息（简化处理：暂不按用户细分模型）
				if _, ok := userModels[userID]; !ok {
					userModels[userID] = make(map[string]int64)
				}
			}
		}

		// 计算总量
		var totalTokens int64
		var totalReqs int64
		for _, t := range userTokens {
			totalTokens += t
		}
		for _, r := range userRequests {
			totalReqs += r
		}

		// 4. 构建排名列表
		var ranks []UserUsageRank
		for uid, tokens := range userTokens {
			email := m.userIDToEmail[uid]
			if email == "" {
				email = uid[:8] + "..."
			}
			percent := 0.0
			if m.sortType == SortByTokens && totalTokens > 0 {
				percent = float64(tokens) / float64(totalTokens) * 100
			} else if totalReqs > 0 {
				percent = float64(userRequests[uid]) / float64(totalReqs) * 100
			}

			ranks = append(ranks, UserUsageRank{
				UserID:           uid,
				Email:            email,
				TotalTokens:      tokens,
				PromptTokens:     userPromptTokens[uid],
				CompletionTokens: userCompletionTokens[uid],
				Requests:         userRequests[uid],
				FailedRequests:   userFailedRequests[uid],
				Percent:          percent,
				IsMe:             uid == userInfo.UserID,
				Models:           userModels[uid],
			})
		}

		// 5. 排序
		sort.Slice(ranks, func(i, j int) bool {
			if m.sortType == SortByTokens {
				return ranks[i].TotalTokens > ranks[j].TotalTokens
			}
			return ranks[i].Requests > ranks[j].Requests
		})

		// 设置排名
		for i := range ranks {
			ranks[i].Rank = i + 1
		}

		// 找到当前用户排名
		var currentRank *UserUsageRank
		for i := range ranks {
			if ranks[i].IsMe {
				currentRank = &ranks[i]
				break
			}
		}

		return usageRankLoadedMsg{
			Data: &UsageRankResponse{
				StartDate:   m.startDate,
				EndDate:     m.endDate,
				TotalTokens: totalTokens,
				TotalReqs:   totalReqs,
				CurrentRank: currentRank,
				Ranks:       ranks,
				TimeRange:   m.timeRange,
				SortType:    m.sortType,
			},
		}
	}
}

type usageRankLoadedMsg struct {
	Data  *UsageRankResponse
	Error error
}