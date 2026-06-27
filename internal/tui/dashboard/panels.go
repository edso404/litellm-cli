package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"litellm-cli/internal/api"
)

// teamRankClientAdapter 适配器，将 *api.Client 适配为 TeamRankClient
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
				UserID: k.UserID,
				Spend:  k.Spend,
			}
		}
		teams[i] = UserTeam{
			TeamID:    t.TeamID,
			TeamAlias: t.TeamAlias,
			Members:   members,
			Keys:       keys,
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

// TabPanelModel 是简单面板的接口（用于 models, teams, keyinfo, login）
type TabPanelModel interface {
	tea.Model
	Title() string
}

// modelsTabModel 是 models 面板的 Model
type modelsTabModel struct {
	client     *api.Client
	data       *api.ModelsResponse
	loading    bool
	err        string
	width      int
	height     int
	quitting   bool
	selected   int
}

func newModelsTabModel(client *api.Client) *modelsTabModel {
	return &modelsTabModel{
		client:   client,
		loading:  true,
		width:    120,
		height:   40,
		selected: 0,
	}
}

func (m *modelsTabModel) Title() string {
	return "📦 模型列表"
}

func (m *modelsTabModel) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m *modelsTabModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "down", "j":
			if m.data != nil && m.selected < len(m.data.Models)-1 {
				m.selected++
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case modelsLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
	}
	return m, nil
}

func (m *modelsTabModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}
	if m.loading {
		return "加载中...\n"
	}
	if m.err != "" {
		return "错误: " + m.err + "\n"
	}
	if m.data == nil || len(m.data.Models) == 0 {
		return "暂无数据\n"
	}

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	sb.WriteString(titleStyle.Render(" 📦 模型列表 "))
	sb.WriteString("\n\n")

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// 渲染模型列表
	for i, model := range m.data.Models {
		if i == m.selected {
			sb.WriteString(greenStyle.Render("▶ " + model.ID))
		} else {
			sb.WriteString("  " + model.ID)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓: 移动 | q: 退出"))

	return sb.String()
}

func (m *modelsTabModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := m.client.GetModels()
		return modelsLoadedMsg{Data: data, Error: err}
	}
}

type modelsLoadedMsg struct {
	Data  *api.ModelsResponse
	Error error
}

// teamsTabModel 是 teams 面板的 Model
type teamsTabModel struct {
	client   *api.Client
	data     *api.UserInfoResponse
	loading  bool
	err      string
	width    int
	height   int
	quitting bool
	selected int
}

func newTeamsTabModel(client *api.Client) *teamsTabModel {
	return &teamsTabModel{
		client:   client,
		loading:  true,
		width:    120,
		height:   40,
		selected: 0,
	}
}

func (m *teamsTabModel) Title() string {
	return "👥 团队列表"
}

func (m *teamsTabModel) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m *teamsTabModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "down", "j":
			if m.data != nil && m.selected < len(m.data.Teams)-1 {
				m.selected++
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case teamsLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
	}
	return m, nil
}

func (m *teamsTabModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}
	if m.loading {
		return "加载中...\n"
	}
	if m.err != "" {
		return "错误: " + m.err + "\n"
	}
	if m.data == nil || len(m.data.Teams) == 0 {
		return "暂无数据\n"
	}

	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	sb.WriteString(titleStyle.Render(" 👥 团队列表 "))
	sb.WriteString("\n\n")

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)

	// 渲染团队列表
	for i, team := range m.data.Teams {
		alias := team.TeamAlias
		if alias == "" {
			alias = team.TeamID
		}
		memberCount := len(team.MembersWithRoles)

		info := fmt.Sprintf("%s (成员: %d)", alias, memberCount)
		if i == m.selected {
			sb.WriteString(cyanStyle.Render("▶ " + info))
		} else {
			sb.WriteString(greenStyle.Render("  " + info))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓: 移动 | q: 退出"))

	return sb.String()
}

func (m *teamsTabModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := m.client.GetUserInfo()
		return teamsLoadedMsg{Data: data, Error: err}
	}
}

type teamsLoadedMsg struct {
	Data  *api.UserInfoResponse
	Error error
}

// keyinfoTabModel 是 keyinfo 面板的 Model
type keyinfoTabModel struct {
	client  *api.Client
	apiKey  string
	data    *api.KeyInfoResponse
	loading bool
	err     string
	width   int
	height  int
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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	sb.WriteString(titleStyle.Render(" 🔑 Key 详情 "))
	sb.WriteString("\n\n")

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

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

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  q: 退出"))

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

// loginTabModel 是 login 面板的 Model（显示登录信息）
type loginTabModel struct {
	client   *api.Client
	data     *api.UserInfoResponse
	loading  bool
	err      string
	width    int
	height   int
	quitting bool
}

func newLoginTabModel(client *api.Client) *loginTabModel {
	return &loginTabModel{
		client:  client,
		loading: true,
		width:   120,
		height:  40,
	}
}

func (m *loginTabModel) Title() string {
	return "🔐 登录信息"
}

func (m *loginTabModel) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m *loginTabModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case loginLoadedMsg:
		m.loading = false
		if msg.Error != nil {
			m.err = fmt.Sprintf("获取数据失败: %v", msg.Error)
			return m, nil
		}
		m.data = msg.Data
	}
	return m, nil
}

func (m *loginTabModel) View() string {
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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	sb.WriteString(titleStyle.Render(" 🔐 登录信息 "))
	sb.WriteString("\n\n")

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("159")).Bold(true)

	sb.WriteString(keyStyle.Render("  👤 用户 ID: "))
	sb.WriteString(valueStyle.Render(m.data.UserID))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  📧 用户邮箱: "))
	sb.WriteString(valueStyle.Render(m.data.UserEmail))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  🏢 组织 ID: "))
	sb.WriteString(valueStyle.Render(m.data.OrganizationID))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  👔 用户角色: "))
	sb.WriteString(greenStyle.Render(m.data.UserRole))
	sb.WriteString("\n")

	sb.WriteString(keyStyle.Render("  👥 团队数量: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%d", len(m.data.Teams))))
	sb.WriteString("\n")

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  q: 退出"))

	return sb.String()
}

func (m *loginTabModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := m.client.GetUserInfo()
		return loginLoadedMsg{Data: data, Error: err}
	}
}

type loginLoadedMsg struct {
	Data  *api.UserInfoResponse
	Error error
}