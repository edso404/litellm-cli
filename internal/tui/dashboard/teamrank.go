package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// TeamRankClient 定义 Team Rank 数据获取的接口
type TeamRankClient interface {
	GetUserInfo() (*UserInfo, error)
	GetTeamRank(teamID string) (*TeamRankResponse, error)
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
	UserID string
	Spend  float64
}

// TeamRankResponse 团队排行榜响应
type TeamRankResponse struct {
	Team       *UserTeam
	CurrentUID string
	Ranks      []UserRank
}

// UserRank 用户排名
type UserRank struct {
	UserID  string
	Email   string
	Spend   float64
	Percent float64
	Rank    int
	IsMe    bool
}

// teamRankModel 是 Team Rank 的 TUI Model
type teamRankModel struct {
	client        TeamRankClient
	teamID        string
	data          *TeamRankResponse
	selectedIndex int
	width         int
	height        int
	loading       bool
	err           string
	quitting      bool
}

// newTeamRankModel 创建新的 Team Rank Model
func newTeamRankModel(client TeamRankClient) *teamRankModel {
	return &teamRankModel{
		client:        client,
		teamID:        "",
		selectedIndex: 0,
		width:         120,
		height:        40,
		loading:       true,
	}
}

// Init 实现 tea.Model 接口
func (m *teamRankModel) Init() tea.Cmd {
	return m.refreshCmd()
}

// Update 实现 tea.Model 接口
func (m *teamRankModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "down", "j":
			if m.data != nil && len(m.data.Ranks) > 0 && m.selectedIndex < len(m.data.Ranks)-1 {
				m.selectedIndex++
			}
		case "up", "k":
			if m.selectedIndex > 0 {
				m.selectedIndex--
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
	}
	return m, nil
}

// View 实现 tea.Model 接口
func (m *teamRankModel) View() string {
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

	var sb strings.Builder

	// 渲染标题
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	teamName := m.data.Team.TeamAlias
	if teamName == "" {
		teamName = m.data.Team.TeamID
	}

	sb.WriteString(title.Render(fmt.Sprintf(" 🏆 %s 用量排行榜 ", teamName)))
	sb.WriteString("\n\n")

	// 渲染表格
	// 计算列宽
	rankWidth := 4
	emailWidth := 30
	spendWidth := 11
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
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(fmt.Sprintf("  %s %s %s %s\n",
		padRight("排名", rankWidth),
		padRight("用户", emailWidth),
		padLeft("用量", spendWidth),
		padLeft("占比", percentWidth)))
	sb.WriteString(mutedStyle.Render(" " + strings.Repeat("─", 65)))
	sb.WriteString("\n")

	// 渲染排名
	for i, r := range m.data.Ranks {
		email := r.Email
		if runewidth.StringWidth(email) > emailWidth {
			email = runewidth.Truncate(email, emailWidth-3, "...")
		}

		if r.IsMe {
			cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
			sb.WriteString(cyanStyle.Render(fmt.Sprintf("  →%d", r.Rank)))
		} else if i == m.selectedIndex {
			sb.WriteString("  ▶ ")
		} else {
			sb.WriteString("    ")
		}

		emailPadded := padRight(email, emailWidth)
		spendPadded := padLeft(fmt.Sprintf("$%.2f", r.Spend), spendWidth)
		percentPadded := padLeft(fmt.Sprintf("%.1f%%", r.Percent), percentWidth)

		sb.WriteString(emailPadded)
		sb.WriteString(greenStyle.Render(" "+spendPadded))
		sb.WriteString(" ")
		sb.WriteString(greenStyle.Render(percentPadded))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓: 移动 | q: 退出"))

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

		// 选择第一个团队
		var targetTeam *UserTeam
		if len(userInfo.Teams) > 0 {
			targetTeam = &userInfo.Teams[0]
		}

		if targetTeam == nil {
			return teamRankLoadedMsg{Error: fmt.Errorf("没有找到团队")}
		}

		// 构建 user_id -> email 映射
		userEmailMap := make(map[string]string)
		for _, member := range targetTeam.Members {
			userEmailMap[member.UserID] = member.Email
		}

		// 按 user_id 聚合用量
		userSpend := make(map[string]float64)
		for _, k := range targetTeam.Keys {
			if k.UserID != "" {
				userSpend[k.UserID] += k.Spend
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
			ranks = append(ranks, UserRank{
				UserID:  uid,
				Email:   email,
				Spend:   spend,
				Percent: percent,
				IsMe:    uid == userInfo.UserID,
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

		return teamRankLoadedMsg{
			Data: &TeamRankResponse{
				Team:       targetTeam,
				CurrentUID: userInfo.UserID,
				Ranks:      ranks,
			},
		}
	}
}

type teamRankLoadedMsg struct {
	Data  *TeamRankResponse
	Error error
}