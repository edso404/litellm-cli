package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"litellm-cli/internal/api"
)

// teamRankClientAdapter 适配器，将 *api.Client 适配为 TeamRankClient 和 UsageRankClient
type teamRankClientAdapter struct {
	client *api.Client
}

// GetUserInfo 从 API 获取用户信息
func (a *teamRankClientAdapter) GetUserInfo() (*UserInfo, error) {
	resp, err := a.client.GetUserInfo()
	if err != nil {
		return nil, err
	}

	teams := make([]UserTeam, len(resp.Teams))
	for i, t := range resp.Teams {
		members := make([]TeamMember, len(t.MembersWithRoles))
		for j, m := range t.MembersWithRoles {
			members[j] = TeamMember{
				UserID: m.UserID,
				Email:  m.Email,
			}
		}
		keys := make([]TeamKey, len(t.Keys))
		for j, k := range t.Keys {
			keys[j] = TeamKey{
				UserID:   k.UserID,
				Spend:    k.Spend,
				KeyName:  k.KeyName,
				KeyAlias: k.KeyAlias,
			}
		}
		teams[i] = UserTeam{
			TeamID:    t.TeamID,
			TeamAlias: t.TeamAlias,
			Members:   members,
			Keys:      keys,
		}
	}

	return &UserInfo{
		UserID: resp.UserID,
		Teams:  teams,
	}, nil
}

// GetTeamRank 不需要单独实现，因为数据已经在 UserInfo 中
func (a *teamRankClientAdapter) GetTeamRank(teamID string) (*TeamRankResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// NewTeamRankClientAdapter 创建适配器
func NewTeamRankClientAdapter(client *api.Client) TeamRankClient {
	return &teamRankClientAdapter{client: client}
}

// GetTeamDailyActivity 获取团队每日活动
func (a *teamRankClientAdapter) GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error) {
	return a.client.GetTeamDailyActivity(startDate, endDate)
}

// NewUsageRankClientAdapter 创建用量排行适配器（复用 teamRankClientAdapter）
func NewUsageRankClientAdapter(client *api.Client) UsageRankClient {
	return &teamRankClientAdapter{client: client}
}

// TabPanelModel 是简单面板的接口（用于 models, teams, keyinfo, login）
type TabPanelModel interface {
	tea.Model
	Title() string
}





// keyinfoTabModel 是 keyinfo 面板的 Model
type keyinfoTabModel struct {
	client   *api.Client
	apiKey   string
	data     *api.KeyInfoResponse
	loading  bool
	err      string
	width    int
	height   int
	quitting bool
}

func newKeyinfoTabModel(client *api.Client, apiKey string) *keyinfoTabModel {
	return &keyinfoTabModel{
		client:  client,
		apiKey:  apiKey,
		loading: true,
		width:   120,
		height:  40,
	}
}

func (m *keyinfoTabModel) Title() string {
	return "🔑 Key 详情"
}

func (m *keyinfoTabModel) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m *keyinfoTabModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case keyinfoLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
	}
	return m, nil
}

func (m *keyinfoTabModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}
	if m.loading {
		return "加载中...\n"
	}
	if m.err != "" {
		return "错误: " + m.err + "\n"
	}
	if m.data == nil {
		return "暂无数据\n"
	}

	var sb strings.Builder

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("159")).Bold(true)

	sb.WriteString(keyStyle.Render("  Key Alias: "))
	sb.WriteString(valueStyle.Render(m.data.Info.KeyAlias))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  Key Name: "))
	sb.WriteString(valueStyle.Render(m.data.Info.KeyName))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  💰 已花费: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("$%.4f", m.data.Info.Spend)))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  👤 User ID: "))
	sb.WriteString(valueStyle.Render(m.data.Info.UserID))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  👥 Team ID: "))
	sb.WriteString(valueStyle.Render(m.data.Info.TeamID))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  📅 创建时间: "))
	sb.WriteString(valueStyle.Render(m.data.Info.CreatedAt))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  🕐 最后活跃: "))
	sb.WriteString(valueStyle.Render(m.data.Info.LastActive))
	sb.WriteString("\n")

	if len(m.data.Info.Models) > 0 {
		sb.WriteString("\n")
		sb.WriteString(keyStyle.Render("  📦 可用模型: "))
		sb.WriteString(greenStyle.Render(strings.Join(m.data.Info.Models, ", ")))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m *keyinfoTabModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := m.client.GetKeyInfo(m.apiKey)
		return keyinfoLoadedMsg{Data: data, Error: err}
	}
}

type keyinfoLoadedMsg struct {
	Data  *api.KeyInfoResponse
	Error error
}


