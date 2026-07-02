package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"litellm-cli/internal/api"
)

// TeamRankClient 定义 Team Rank 数据获取的接口
type TeamRankClient interface {
	GetUserInfo() (*UserInfo, error)
	GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error)
}

// UserInfo 用户信息
type UserInfo struct {
	UserID string
	Teams  []UserTeam
}

// UserTeam 用户团队
type UserTeam struct {
	TeamID    string
	TeamAlias string
	Members   []TeamMember
	Keys      []TeamKey
}

// TeamMember 团队成员
type TeamMember struct {
	UserID string
	Email  string
}

// TeamKey 团队 Key
type TeamKey struct {
	UserID   string
	Spend    float64
	KeyName  string
	KeyAlias string
}

// TeamRankResponse 团队排行榜响应
type TeamRankResponse struct {
	TeamID      string
	TeamAlias   string
	TotalSpend  float64
	CurrentRank *UserRank
	Ranks       []UserRank
}

// UserRank 用户排名
type UserRank struct {
	UserID   string
	Email    string
	Spend    float64
	Percent  float64
	Rank     int
	IsMe     bool
	KeyCount int
	Keys     []TeamKey
}

// teamRankModel 是 Team Rank 的 TUI Model
type teamRankModel struct {
	client          TeamRankClient
	apiClient       *api.Client // 直接使用 API client 调用 /team 接口
	teamID          string
	data            *TeamRankResponse
	selectedIndex   int
	detailView      bool       // 是否在详情视图
	detailSelected  int        // 详情视图选中索引
	width           int
	height          int
	loading         bool
	err             string
	quitting        bool

	// Sub-tab 支持
	activeSubTab   string // "key" = Key 排行, "usage" = 用量排行
	usageRank      *usageRankModel
}

// newTeamRankModel 创建新的 Team Rank Model
func newTeamRankModel(client TeamRankClient) *teamRankModel {
	// TeamRankClient 已经被扩展为同时支持 UsageRankClient（通过 GetTeamDailyActivity 方法）
	m := &teamRankModel{
		client:         client,
		selectedIndex:  0,
		width:          120,
		height:         40,
		loading:        true,
		activeSubTab:   "key", // 默认 Key 排行
	}
	// 初始化用量排行子模型（client 同时实现了 TeamRankClient 和 UsageRankClient）
	m.usageRank = newUsageRankModel(client)
	return m
}

// SetAPIClient 设置 API client
func (m *teamRankModel) SetAPIClient(apiClient *api.Client) {
	m.apiClient = apiClient
	// 同时设置给用量排行子模型
	if m.usageRank != nil {
		m.usageRank.SetAPIClient(apiClient)
	}
}

// SetCurrentUserID 设置当前用户 ID
func (m *teamRankModel) SetCurrentUserID(userID string) {
	if m.usageRank != nil {
		m.usageRank.SetCurrentUserID(userID)
	}
}

// Init 实现 tea.Model 接口
func (m *teamRankModel) Init() tea.Cmd {
	return m.refreshCmd()
}

// Update 实现 tea.Model 接口
func (m *teamRankModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 如果在用量排行 sub-tab，委托给 usageRank 处理
	if m.activeSubTab == "usage" && m.usageRank != nil {
		// 同步窗口大小
		if wsm, ok := msg.(tea.WindowSizeMsg); ok {
			m.width = wsm.Width
			m.height = wsm.Height
		}

		// Tab 键切换 sub-tab
		if km, ok := msg.(tea.KeyMsg); ok {
			if km.String() == "tab" {
				m.cycleSubTab()
				return m, m.usageRank.Init()
			}
		}

		// 委托给 usageRank 处理
		child, cmd := m.usageRank.Update(msg)
		if um, ok := child.(*usageRankModel); ok {
			m.usageRank = um
			// 同步状态
			m.loading = m.usageRank.loading
			m.err = m.usageRank.err
		}
		return m, cmd
	}

	// Key 排行的处理逻辑（原有）
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab", "enter":
			// Tab 键切换 sub-tab
			if msg.String() == "tab" {
				m.cycleSubTab()
				return m, m.usageRank.Init()
			}
			// enter 进入详情视图
			if !m.detailView && m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex < len(m.data.Ranks) {
				r := m.data.Ranks[m.selectedIndex]
				if len(r.Keys) >= 1 {
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
				// 详情视图导航
				r := m.data.Ranks[m.selectedIndex]
				if len(r.Keys) > 0 && m.detailSelected < len(r.Keys)-1 {
					m.detailSelected++
				}
			} else {
				// 排行榜导航
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
	case teamRankLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
		// 数据刷新后重置 selectedIndex，防止越界
		if m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex >= len(m.data.Ranks) {
			m.selectedIndex = len(m.data.Ranks) - 1
		}
	}
	return m, nil
}

// View 实现 tea.Model 接口
func (m *teamRankModel) View() string {
	// 如果在用量排行 sub-tab，委托给 usageRank 渲染
	if m.activeSubTab == "usage" && m.usageRank != nil {
		var sb strings.Builder
		// 渲染 sub-tab header
		sb.WriteString(m.renderSubTabHeader())
		sb.WriteString("\n")
		sb.WriteString(m.usageRank.View())
		return sb.String()
	}

	// Key 排行的渲染（原有逻辑）
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

	// 总用量
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(greenStyle.Render(fmt.Sprintf("  团队总用量: $%.2f", m.data.TotalSpend)))
	sb.WriteString("\n\n")

	// 计算可用高度，确保内容不会超出显示区域
	// 固定行数：总用量+空行(2) + 表头+分隔线(3) + 滚动提示(1) + 我的排名(3) = 9
	// 注意：这里不渲染 footer，由 dashboard 统一渲染
	fixedLines := 9
	maxRankLines := m.height - fixedLines
	if maxRankLines < 3 {
		maxRankLines = 3
	}

	// 计算可视窗口：根据 selectedIndex 确定显示的数据范围
	// 确保选中的项始终在可视区域内
	totalRanks := len(m.data.Ranks)
	viewStart := 0
	viewEnd := maxRankLines
	if totalRanks > maxRankLines {
		// 如果选中项在当前可视区域下方，调整视图使选中项进入可视区域
		if m.selectedIndex >= viewEnd {
			viewStart = m.selectedIndex - maxRankLines + 1
			viewEnd = m.selectedIndex + 1
		}
		// 如果选中项在可视区域上方
		if m.selectedIndex < viewStart {
			viewStart = m.selectedIndex
			viewEnd = m.selectedIndex + maxRankLines
		}
		// 确保不越界
		if viewEnd > totalRanks {
			viewEnd = totalRanks
			viewStart = viewEnd - maxRankLines
			if viewStart < 0 {
				viewStart = 0
			}
		}
	}

	// 渲染表格
	// 计算列宽
	rankWidth := 5
	emailWidth := 26
	spendWidth := 12
	percentWidth := 8
	keysWidth := 6

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
		padLeft("用量", spendWidth),
		padLeft("占比", percentWidth),
		padRight("Keys", keysWidth)))
	sb.WriteString(mutedStyle.Render(" " + strings.Repeat("─", 70)))
	sb.WriteString("\n")

	// 渲染排名（使用 viewStart 和 viewEnd 控制渲染范围）
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	// 选中行样式（与 logs tab 对齐）
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	mutedCyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("73"))
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

		// Key 数量显示
		keyCountStr := fmt.Sprintf("%d", r.KeyCount)
		if r.KeyCount > 1 {
			keyCountStr = mutedCyanStyle.Render(keyCountStr)
		}

		rankPadded := padRight(rankStr, rankWidth)
		emailPadded := padRight(email, emailWidth)
		spendPadded := padLeft(fmt.Sprintf("$%.2f", r.Spend), spendWidth)
		percentPadded := padLeft(fmt.Sprintf("%.1f%%", r.Percent), percentWidth)
		keysPadded := padRight(keyCountStr, keysWidth)

		// 选中行样式（使用绝对索引 i 比较）
		lineStyle := greenStyle
		if r.IsMe {
			lineStyle = cyanStyle
		} else if i == m.selectedIndex {
			lineStyle = selectedStyle
		}

		sb.WriteString(lineStyle.Render("  " + rankPadded))
		sb.WriteString(lineStyle.Render(emailPadded))
		sb.WriteString(" " + lineStyle.Render(spendPadded))
		sb.WriteString(" " + lineStyle.Render(percentPadded))
		sb.WriteString(" " + lineStyle.Render(keysPadded))
		sb.WriteString("\n")
	}

	// 如果有更多数据未显示，显示滚动提示
	if totalRanks > maxRankLines {
		// 计算当前视图显示的数据范围
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
		sb.WriteString("\n")
		sb.WriteString(cyanStyle.Render(fmt.Sprintf("  📊 你的排名: #%d / %d", m.data.CurrentRank.Rank, len(m.data.Ranks))))
		sb.WriteString("\n")
		sb.WriteString(cyanStyle.Render(fmt.Sprintf("    你的用量: $%.2f (占比 %.1f%%)", m.data.CurrentRank.Spend, m.data.CurrentRank.Percent)))
		sb.WriteString("\n")
	}

	// 详情视图
	if m.detailView {
		return m.renderDetailView()
	}

	return sb.String()
}

// renderDetailView 渲染 Key 明细视图
func (m *teamRankModel) renderDetailView() string {
	r := m.data.Ranks[m.selectedIndex]
	keys := r.Keys

	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))
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
	sb.WriteString(headerStyle.Render(fmt.Sprintf("  📋 %s 的 API Keys (%d个)", r.Email, len(keys))))
	sb.WriteString("\n\n")

	// 表头
	sb.WriteString("  # ")
	sb.WriteString(mutedStyle.Render(padRight("Key Alias", 35)))
	sb.WriteString(mutedStyle.Render(padLeft("用量", 12)))
	sb.WriteString(mutedStyle.Render(padLeft("占比", 8)))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  " + strings.Repeat("─", 60)))
	sb.WriteString("\n")

	// 计算每个 key 的占比
	totalSpend := r.Spend

	// 确保 detailSelected 不越界
	if m.detailSelected >= len(keys) {
		m.detailSelected = len(keys) - 1
	}
	if m.detailSelected < 0 {
		m.detailSelected = 0
	}

	for i, k := range keys {
		percent := 0.0
		if totalSpend > 0 {
			percent = (k.Spend / totalSpend) * 100
		}

		keyAlias := k.KeyAlias
		if keyAlias == "" {
			keyAlias = k.KeyName[:12] + "..."
		}
		if runewidth.StringWidth(keyAlias) > 32 {
			keyAlias = runewidth.Truncate(keyAlias, 29, "...")
		}

		lineStyle := greenStyle
		if i == m.detailSelected {
			lineStyle = selectedStyle
		}

		indexStr := fmt.Sprintf("%d", i+1)
		sb.WriteString(lineStyle.Render("  " + padRight(indexStr, 2)))
		sb.WriteString(lineStyle.Render(padRight(keyAlias, 35)))
		sb.WriteString(" " + lineStyle.Render(padLeft(fmt.Sprintf("$%.2f", k.Spend), 12)))
		sb.WriteString(" " + lineStyle.Render(padLeft(fmt.Sprintf("%.1f%%", percent), 8)))
		sb.WriteString("\n")
	}

	// 统计信息
	sb.WriteString("\n")
	sb.WriteString(greenStyle.Render(fmt.Sprintf("  总用量: $%.2f", totalSpend)))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓: 移动 | esc: 返回 | enter: 选中"))

	return sb.String()
}

// refreshCmd 刷新数据的命令
func (m *teamRankModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		// 获取用户信息和团队列表
		userInfo, err := m.client.GetUserInfo()
		if err != nil {
			return teamRankLoadedMsg{Error: err}
		}

		if len(userInfo.Teams) == 0 {
			return teamRankLoadedMsg{Error: fmt.Errorf("没有找到所属团队")}
		}

		// 使用第一个团队
		team := userInfo.Teams[0]

		// 构建 user_id -> email 映射
		userEmailMap := make(map[string]string)
		for _, member := range team.Members {
			userEmailMap[member.UserID] = member.Email
		}

		// 按 user_id 聚合用量，同时收集每个用户的 keys
		userSpend := make(map[string]float64)
		userKeys := make(map[string][]TeamKey)
		for _, k := range team.Keys {
			if k.UserID != "" {
				userSpend[k.UserID] += k.Spend
				userKeys[k.UserID] = append(userKeys[k.UserID], k)
			}
		}

		// 计算团队总用量
		var totalSpend float64
		for _, s := range userSpend {
			totalSpend += s
		}

		// 构建排名
		var ranks []UserRank
		for uid, spend := range userSpend {
			email := userEmailMap[uid]
			if email == "" {
				email = uid[:8] + "..."
			}
			percent := 0.0
			if totalSpend > 0 {
				percent = (spend / totalSpend) * 100
			}
			keys := userKeys[uid]
			ranks = append(ranks, UserRank{
				UserID:   uid,
				Email:    email,
				Spend:    spend,
				Percent:  percent,
				IsMe:     uid == userInfo.UserID,
				KeyCount: len(keys),
				Keys:     keys,
			})
		}

		// 按用量降序排序
		sort.Slice(ranks, func(i, j int) bool {
			return ranks[i].Spend > ranks[j].Spend
		})

		// 设置排名
		for i := range ranks {
			ranks[i].Rank = i + 1
		}

		// 找到当前用户排名
		var currentRank *UserRank
		for i := range ranks {
			if ranks[i].IsMe {
				currentRank = &ranks[i]
				break
			}
		}

		return teamRankLoadedMsg{
			Data: &TeamRankResponse{
				TeamID:      team.TeamID,
				TeamAlias:   team.TeamAlias,
				TotalSpend:  totalSpend,
				CurrentRank: currentRank,
				Ranks:       ranks,
			},
		}
	}
}

type teamRankLoadedMsg struct {
	Data  *TeamRankResponse
	Error error
}

// cycleSubTab 切换 sub-tab
func (m *teamRankModel) cycleSubTab() {
	if m.activeSubTab == "key" {
		m.activeSubTab = "usage"
	} else {
		m.activeSubTab = "key"
	}
	// 切换后重置状态
	m.detailView = false
	m.detailSelected = 0
	m.selectedIndex = 0
}

// renderSubTabHeader 渲染 sub-tab header
func (m *teamRankModel) renderSubTabHeader() string {
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("76")).Bold(true)

	var sb strings.Builder
	sb.WriteString("  ")

	if m.activeSubTab == "key" {
		sb.WriteString(selectedStyle.Render(" [Key 排行] "))
	} else {
		sb.WriteString(greenStyle.Render(" [Key 排行] "))
	}

	sb.WriteString(" | ")

	if m.activeSubTab == "usage" {
		sb.WriteString(selectedStyle.Render(" [用量排行] "))
	} else {
		sb.WriteString(greenStyle.Render(" [用量排行] "))
	}

	sb.WriteString("  ")
	sb.WriteString(mutedStyle.Render("按 Tab 切换"))

	return sb.String()
}
