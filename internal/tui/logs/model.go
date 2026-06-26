package logs

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"litellm-cli/internal/api"
	"litellm-cli/internal/tui/components"
)

// detailTabs 定义详情视图的 tab 页面
var detailTabs = []string{"main", "system", "tools", "messages", "choices"}

// detailMainSections 定义主视图的区块
var detailMainSections = []string{"request", "response"}

// LogsClient 接口，解耦对具体 *client.Client 的强依赖
type LogsClient interface {
	GetSpendLogsUI(startDateTime, endDateTime string) (*api.SpendLogsUIResponse, error)
	GetSpendLogs(startDate, endDate string) (*api.SpendLogsResponse, error)
	GetSpendLogDetail(requestID string) (map[string]interface{}, error)
}

// Model 结构体存放日志TUI的状态
type Model struct {
	client        LogsClient
	data          string
	interval      int
	model         string // 过滤模型
	tick          int
	quitting      bool
	logData       *api.SpendLogsUIResponse
	logDataOld    *api.SpendLogsResponse
	seenLogIDs    map[string]bool // 已看到的日志ID
	newLogIDs     map[string]bool // 本次新增的日志ID（用于高亮）
	initialized   bool            // 是否已完成首次加载
	width         int             // 窗口宽度
	height        int             // 窗口高度
	selectedIndex int             // 当前选中的日志索引
	selectedEntry *api.SpendLogEntry // 当前选中的日志条目（用于详情页）
	viewMode      string          // "list" 或 "detail"
	detailData    map[string]interface{}
	detailError   string
	detailScroll  int // 详情视图滚动偏移量
	detailState   *detailViewState // 详情视图状态（展开/折叠）
}

// NewModel 构造工厂函数
func NewModel(client LogsClient, interval int, modelFilter string) *Model {
	m := &Model{
		client:        client,
		interval:      interval,
		model:         modelFilter,
		data:          "加载中...",
		seenLogIDs:    make(map[string]bool),
		newLogIDs:     make(map[string]bool),
		width:         120,  // 默认宽度
		height:        40,   // 默认高度
		viewMode:      "list", // 默认视图模式
		selectedIndex: 0,
	}
	return m
}

// LogsLoadedMsg 异步数据加载成功的消息
type LogsLoadedMsg struct {
	Response    *api.SpendLogsUIResponse
	ResponseOld *api.SpendLogsResponse
	Error       error
}

// TickMsg 轮询定时消息
type TickMsg time.Time

// DetailLoadedMsg 异步详情加载成功的消息
type DetailLoadedMsg struct {
	Data  map[string]interface{}
	Error string
}

// RefreshCmd 异步命令，并发执行 HTTP 请求并向事件循环投递 LogsLoadedMsg
func (m *Model) RefreshCmd() tea.Cmd {
	return func() tea.Msg {
		// 使用 datetime 格式，并 URL 编码空格
		endDate := url.QueryEscape(time.Now().Format("2006-01-02 15:04:05"))
		startDate := url.QueryEscape(time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05"))

		// 优先使用 /spend/logs/ui
		resp, err := m.client.GetSpendLogsUI(startDate, endDate)
		if err != nil {
			// 回退到旧的 /spend/logs
			respOld, err2 := m.client.GetSpendLogs(
				time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
				time.Now().Format("2006-01-02"),
			)
			if err2 != nil {
				return LogsLoadedMsg{Error: err}
			}
			return LogsLoadedMsg{ResponseOld: respOld}
		}
		return LogsLoadedMsg{Response: resp}
	}
}

// Init 实现 tea.Model 接口，返回初始异步刷新命令
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.RefreshCmd(),
		tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
			return TickMsg(t)
		}),
	)
}

// Update 实现 tea.Model 接口
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "system" && m.detailState.itemDetailMode {
					m.detailState.itemDetailMode = false
					m.detailState.markdownScrollOffset = 0
				} else if m.detailState.activeTab != "main" {
					m.detailState.activeTab = "main"
					m.detailState.selectedItem = 0
					m.detailState.scrollOffset = 0
					m.detailState.itemDetailMode = false
				} else {
					m.viewMode = "list"
					m.detailData = nil
					m.detailError = ""
					m.detailScroll = 0
					m.detailState = nil
				}
			}
			return m, nil
		case "enter":
			if m.viewMode == "list" {
				m.detailScroll = 0
				m.detailState = nil
				cmd := m.loadDetail()
				return m, cmd
			} else if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					tabMap := map[int]string{
						0: "system",
						1: "tools",
						2: "messages",
						3: "choices",
					}
					m.detailState.activeTab = tabMap[m.detailState.focusedSection]
					m.detailState.selectedItem = 0
					m.detailState.scrollOffset = 0
					m.detailState.itemDetailMode = false
					m.detailState.currentItemIndex = 0
				} else if m.detailState.activeTab == "system" {
					if !m.detailState.itemDetailMode {
						m.detailState.itemDetailMode = true
						m.detailState.currentItemIndex = m.detailState.selectedItem
						m.detailState.markdownScrollOffset = 0
					}
				} else {
					tab := m.detailState.activeTab
					key := fmt.Sprintf("%s_%d", tab, m.detailState.selectedItem)
					m.detailState.expandedSections[key] = !m.detailState.expandedSections[key]
				}
			}
			return m, nil
		case "tab":
			if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					m.detailState.focusedSection = (m.detailState.focusedSection + 1) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = (m.detailState.selectedItem + 1) % maxItems
					}
				}
			}
			return m, nil
		case "up", "k", "ctrl+p":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab == "system" {
				if m.detailState.itemDetailMode && m.detailState.markdownViewMode == "rendered" {
					m.detailState.markdownScrollOffset = max(0, m.detailState.markdownScrollOffset-3)
				} else if !m.detailState.itemDetailMode {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = (m.detailState.selectedItem - 1 + maxItems) % maxItems
					}
					m.detailState.scrollOffset = 0
				}
			} else if m.viewMode == "list" {
				if m.selectedIndex > 0 {
					m.selectedIndex--
				}
			} else if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					m.detailState.focusedSection = (m.detailState.focusedSection - 1 + 4) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = (m.detailState.selectedItem - 1 + maxItems) % maxItems
					}
				}
				m.detailState.scrollOffset = 0
			}
			return m, nil
		case "down", "j", "ctrl+n":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab == "system" {
				if m.detailState.itemDetailMode && m.detailState.markdownViewMode == "rendered" {
					m.detailState.markdownScrollOffset++
				} else if !m.detailState.itemDetailMode {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = (m.detailState.selectedItem + 1) % maxItems
					}
					m.detailState.scrollOffset = 0
				}
			} else if m.viewMode == "list" {
				maxIdx := -1
				if m.logData != nil && len(m.logData.Data) > 0 {
					maxIdx = len(m.logData.Data) - 1
				} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
					maxIdx = len(*m.logDataOld) - 1
				}
				if maxIdx >= 0 && m.selectedIndex < maxIdx {
					m.selectedIndex++
				}
			} else if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					m.detailState.focusedSection = (m.detailState.focusedSection + 1) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = (m.detailState.selectedItem + 1) % maxItems
					}
				}
				m.detailState.scrollOffset = 0
			}
			return m, nil
		case "pgup", "\x1b[5~":
			if m.viewMode == "detail" {
				m.detailScroll = max(0, m.detailScroll-20)
			}
			return m, nil
		case "pgdown", "\x1b[6~":
			if m.viewMode == "detail" {
				m.detailScroll += 20
			}
			return m, nil
		case " ":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab == "system" {
				if m.detailState.markdownViewMode == "raw" {
					m.detailState.markdownViewMode = "rendered"
				} else {
					m.detailState.markdownViewMode = "raw"
				}
				m.detailState.markdownScrollOffset = 0
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case TickMsg:
		m.tick++
		return m, tea.Batch(
			m.RefreshCmd(),
			tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
				return TickMsg(t)
			}),
		)
	case LogsLoadedMsg:
		if msg.Error != nil {
			m.data = fmt.Sprintf("❌ 获取失败: %v", msg.Error)
			m.logData = nil
			m.logDataOld = nil
			return m, nil
		}

		if msg.ResponseOld != nil {
			respOld := msg.ResponseOld
			if len(*respOld) == 0 {
				m.data = "暂无数据"
				m.logData = nil
				m.logDataOld = nil
				return m, nil
			}
			m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录", len(*respOld))

			// 首次加载只记录日志ID，不高亮
			if !m.initialized {
				m.initialized = true
				for _, entry := range *respOld {
					if id, ok := entry["request_id"]; ok {
						if logID, ok := id.(string); ok {
							m.seenLogIDs[logID] = true
						}
					}
				}
				m.logData = nil
				m.logDataOld = respOld
				return m, nil
			}

			// 识别新增日志
			m.newLogIDs = make(map[string]bool)
			for _, entry := range *respOld {
				var logID string
				if id, ok := entry["request_id"]; ok {
					logID, _ = id.(string)
				}
				if logID != "" && !m.seenLogIDs[logID] {
					m.newLogIDs[logID] = true
				}
				if logID != "" {
					m.seenLogIDs[logID] = true
				}
			}

			m.logData = nil
			m.logDataOld = respOld
			return m, nil
		}

		if msg.Response != nil {
			resp := msg.Response
			if len(resp.Data) == 0 {
				m.data = "暂无数据"
				m.logData = nil
				m.logDataOld = nil
				return m, nil
			}

			m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录 (总 %d)", len(resp.Data), resp.Total)

			// 首次加载只记录日志ID，不高亮
			if !m.initialized {
				m.initialized = true
				for _, entry := range resp.Data {
					m.seenLogIDs[entry.ID] = true
				}
				m.logData = resp
				m.logDataOld = nil
				return m, nil
			}

			// 识别新增日志
			m.newLogIDs = make(map[string]bool)
			for _, entry := range resp.Data {
				if !m.seenLogIDs[entry.ID] {
					m.newLogIDs[entry.ID] = true
				}
				m.seenLogIDs[entry.ID] = true
			}

			m.logData = resp
			m.logDataOld = nil
			return m, nil
		}
	case DetailLoadedMsg:
		if msg.Error != "" {
			m.detailError = msg.Error
		} else {
			m.detailData = msg.Data
			m.detailError = ""
		}
		return m, nil
	}
	return m, nil
}

// View 实现 tea.Model 接口
func (m *Model) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}

	// 详情视图
	if m.viewMode == "detail" {
		return m.renderDetailView()
	}

	// 列表视图
	return m.renderListView()
}

// detailViewState 保存详情视图的状态
type detailViewState struct {
	activeTab            string              // 当前 tab: "main", "system", "tools", "messages", "choices"
	expandedSections     map[string]bool     // 展开的区块
	focusedSection       int                 // 当前聚焦的区块索引
	selectedItem         int                 // 选中的数组项索引（用于列表tab）
	scrollOffset         int                 // 滚动偏移量
	markdownViewMode     string              // "raw" 或 "rendered" - markdown 查看模式
	markdownScrollOffset int                 // markdown 渲染滚动偏移量
	itemDetailMode       bool                // 是否处于查看某项详情的模式
	currentItemIndex     int                 // 当前查看详情的项索引
}

func (m *Model) renderDetailView() string {
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).MarginRight(1)
	groupStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("210"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75"))

	// 卡片样式
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("236")).
		Padding(0, 1)

	// 聚焦卡片样式（高亮边框）
	focusedCardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(0, 1)

	// 初始化状态（如果需要）
	if m.detailState == nil {
		m.detailState = &detailViewState{
			activeTab:            "main",
			expandedSections:     make(map[string]bool),
			focusedSection:       0,
			selectedItem:         0,
			scrollOffset:         0,
			markdownViewMode:     "rendered",
			markdownScrollOffset: 0,
			itemDetailMode:       false,
			currentItemIndex:     0,
		}
	}

	// 获取数据
	proxyReq, _ := m.detailData["proxy_server_request"].(map[string]interface{})
	respData, _ := m.detailData["response"].(map[string]interface{})

	var lines []string

	// 渲染头部
	var header *components.Header
	if m.detailState.activeTab == "main" {
		header = components.NewHeader("日志详情", "ESC 返回 | ↑↓ 切换 | Tab 切换 | Enter 进入")
	} else if m.detailState.activeTab == "system" {
		if m.detailState.itemDetailMode {
			modeHint := "Raw"
			if m.detailState.markdownViewMode == "rendered" {
				modeHint = "Rendered"
			}
			header = components.NewHeader(fmt.Sprintf("日志详情 > System[%d] (%s)", m.detailState.currentItemIndex, modeHint), "ESC 返回列表 | ↑↓ 滚动 | 空格 切换")
		} else {
			header = components.NewHeader("日志详情 > System", "ESC 返回 | ↑↓ 切换 | Enter 查看详情")
		}
	} else {
		tabTitle := map[string]string{
			"system":   "System Messages",
			"tools":    "Tools",
			"messages": "Messages",
			"choices":  "Choices",
		}[m.detailState.activeTab]
		header = components.NewHeader(fmt.Sprintf("日志详情 > %s", tabTitle), "ESC 返回 | ↑↓ 选择 | Enter 展开")
	}
	lines = append(lines, header.View(m.width))
	lines = append(lines, "")

	// 加载与错误状态
	if m.detailError != "" {
		if m.detailError == "加载中..." {
			lines = append(lines, components.NewLoader("正在加载详情...").View())
		} else {
			lines = append(lines, components.NewErrorBanner(m.detailError).View(m.width))
		}
		return strings.Join(lines, "\n")
	}

	if m.detailData == nil {
		lines = append(lines, components.NewPlaceholder("无详情数据，请按 Enter 刷新").View())
		return strings.Join(lines, "\n")
	}

	// 根据当前 tab 渲染不同内容
	if m.detailState.activeTab == "main" {
		lines = append(lines, m.renderMainView(proxyReq, respData, cardStyle, focusedCardStyle, contentStyle, mutedStyle, groupStyle, valueStyle, keyStyle, infoStyle, successStyle)...)
	} else {
		lines = append(lines, m.renderArrayDetailView(proxyReq, respData, cardStyle, focusedCardStyle, contentStyle, mutedStyle, groupStyle, valueStyle, keyStyle)...)
	}

	// 底部提示
	lines = append(lines, "")
	var help *components.Help
	if m.detailState.activeTab == "system" {
		if m.detailState.itemDetailMode {
			help = components.NewHelp([]components.HelpKey{
				{Key: "↑↓", Desc: "滚动"},
				{Key: "空格", Desc: "切换 Raw/Rendered"},
				{Key: "ESC", Desc: "返回列表"},
			})
		} else {
			help = components.NewHelp([]components.HelpKey{
				{Key: "↑↓", Desc: "切换"},
				{Key: "Enter", Desc: "查看详情"},
				{Key: "ESC", Desc: "返回"},
			})
		}
	} else {
		help = components.NewHelp([]components.HelpKey{
			{Key: "↑↓", Desc: "切换"},
			{Key: "Tab", Desc: "切换"},
			{Key: "Enter", Desc: "进入"},
			{Key: "ESC", Desc: "返回"},
		})
	}
	lines = append(lines, help.View(m.width))

	// System 详情模式：统一滚动处理
	if m.detailState.activeTab == "system" && m.detailState.itemDetailMode {
		// 计算可见区域：总高度 - 头部(1行) - 空行 - 底部提示(2行)
		availableLines := m.height - 4
		return m.applyMarkdownScrollUnified(lines, availableLines)
	} else {
		// 普通模式：使用通用滚动
		scrollOffset := m.detailState.scrollOffset
		maxDisplayLines := m.height - 3
		if maxDisplayLines < 10 {
			maxDisplayLines = 20
		}

		totalLines := len(lines)
		if scrollOffset > totalLines-maxDisplayLines {
			scrollOffset = max(0, totalLines-maxDisplayLines)
			m.detailState.scrollOffset = scrollOffset
		}

		endLine := scrollOffset + maxDisplayLines
		if endLine > totalLines {
			endLine = totalLines
		}
		visibleLines := lines[scrollOffset:endLine]

		// 构建最终输出
		var sb strings.Builder
		for i, line := range visibleLines {
			sb.WriteString(line)
			if i < len(visibleLines)-1 {
				sb.WriteString("\n")
			}
		}

		// 添加滚动指示器
		if scrollOffset > 0 || endLine < totalLines {
			sb.WriteString("\n")
			sb.WriteString(mutedStyle.Render(fmt.Sprintf(" ──◀ %d/%d ▶─ ", scrollOffset+1, totalLines)))
		}

		return sb.String()
	}
}

func (m *Model) applyMarkdownScrollUnified(allLines []string, availableLines int) string {
	// 先对 allLines 进行全量按 \n 展开，确保行切片的物理精确性
	var flatLines []string
	for _, line := range allLines {
		if strings.Contains(line, "\n") {
			flatLines = append(flatLines, strings.Split(line, "\n")...)
		} else {
			flatLines = append(flatLines, line)
		}
	}
	allLines = flatLines

	if len(allLines) <= 4 {
		return strings.Join(allLines, "\n")
	}

	contentStart := 2
	contentEnd := len(allLines) - 2 // exclude tip footer and scroll indicator

	contentLines := allLines[contentStart:contentEnd]
	contentLineCount := len(contentLines)

	if contentLineCount == 0 {
		return strings.Join(allLines, "\n")
	}

	contentAvailable := availableLines - 4
	if contentAvailable < 5 {
		contentAvailable = 5
	}

	scrollOffset := m.detailState.markdownScrollOffset

	if scrollOffset >= contentLineCount {
		scrollOffset = contentLineCount - 1
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	}

	contentEndIdx := scrollOffset + contentAvailable
	if contentEndIdx > contentLineCount {
		contentEndIdx = contentLineCount
	}

	m.detailState.markdownScrollOffset = scrollOffset

	visibleContent := contentLines[scrollOffset:contentEndIdx]

	var sb strings.Builder

	// Header
	sb.WriteString(allLines[0])
	sb.WriteString("\n")

	// Empty
	sb.WriteString(allLines[1])
	sb.WriteString("\n")

	// Visible content
	for i, line := range visibleContent {
		sb.WriteString(line)
		if i < len(visibleContent)-1 {
			sb.WriteString("\n")
		}
	}

	// Tip footer
	sb.WriteString("\n")
	sb.WriteString(allLines[len(allLines)-2])

	// Scroll indicator
	if scrollOffset > 0 || contentEndIdx < contentLineCount {
		sb.WriteString(fmt.Sprintf("\n──◀ %d/%d ▶-- ", scrollOffset+1, contentLineCount))
	}

	return sb.String()
}

func (m *Model) renderMainView(proxyReq, respData map[string]interface{}, cardStyle, focusedCardStyle, contentStyle, mutedStyle, groupStyle, valueStyle, keyStyle, infoStyle, successStyle lipgloss.Style) []string {
	var lines []string

	isWideScreen := m.width >= 100

	systemCount := 0
	toolsCount := 0
	messagesCount := 0
	if proxyReq != nil {
		if system, ok := proxyReq["system"].([]interface{}); ok {
			systemCount = len(system)
		}
		if messages, ok := proxyReq["messages"].([]interface{}); ok {
			messagesCount = len(messages)
		}
		if tools, ok := proxyReq["tools"].([]interface{}); ok {
			toolsCount = len(tools)
		}
	}

	choicesCount := 0
	if respData != nil {
		if choices, ok := respData["choices"].([]interface{}); ok {
			choicesCount = len(choices)
		}
	}

	modelName := "-"
	if proxyReq != nil {
		if model, ok := proxyReq["model"].(string); ok {
			modelName = model
		}
	}

	if isWideScreen {
		var leftCol, rightCol []string

		leftCol = append(leftCol, groupStyle.Render("📤 REQUEST"))
		leftCol = append(leftCol, fmt.Sprintf("  🤖 %s", valueStyle.Render(modelName)))
		leftCol = append(leftCol, "")

		requestOptions := []struct {
			key      string
			icon     string
			label    string
			count    int
		}{
			{"system", "📦", "system", systemCount},
			{"tools", "🔧", "tools", toolsCount},
			{"messages", "💬", "messages", messagesCount},
		}

		requestFocusedIdx := m.detailState.focusedSection

		for i, opt := range requestOptions {
			prefix := "  "
			if opt.count > 0 {
				if i == requestFocusedIdx {
					prefix = "▶ "
					leftCol = append(leftCol, successStyle.Render(fmt.Sprintf("%s%s %s [%d]", prefix, opt.icon, opt.label, opt.count)))
				} else {
					leftCol = append(leftCol, fmt.Sprintf("%s%s %s [%d]", prefix, opt.icon, opt.label, opt.count))
				}
			} else {
				leftCol = append(leftCol, mutedStyle.Render(fmt.Sprintf("  %s %s [无]", opt.icon, opt.label)))
			}
		}

		var metaInfo []string
		if proxyReq != nil {
			if maxTokens, ok := proxyReq["max_tokens"].(float64); ok {
				metaInfo = append(metaInfo, fmt.Sprintf("max_tokens: %.0f", maxTokens))
			}
			if outputConfig, ok := proxyReq["output_config"].(map[string]interface{}); ok {
				if reasoningEffort, ok := outputConfig["reasoning_effort"].(string); ok {
					metaInfo = append(metaInfo, fmt.Sprintf("thinking: %s", reasoningEffort))
				}
			}
		}
		if len(metaInfo) > 0 {
			leftCol = append(leftCol, "")
			leftCol = append(leftCol, mutedStyle.Render("  ⚙️ 元信息"))
			for _, m := range metaInfo {
				leftCol = append(leftCol, fmt.Sprintf("    %s", contentStyle.Render(m)))
			}
		}

		rightCol = append(rightCol, groupStyle.Render("📥 RESPONSE"))
		rightCol = append(rightCol, "")

		if requestFocusedIdx == 3 {
			rightCol = append(rightCol, successStyle.Render("  ▶ 💬 choices ["+fmt.Sprintf("%d", choicesCount)+"]"))
		} else {
			if choicesCount > 0 {
				rightCol = append(rightCol, fmt.Sprintf("  💬 choices [%d]", choicesCount))
			} else {
				rightCol = append(rightCol, mutedStyle.Render("  💬 choices [无]"))
			}
		}

		if respData != nil {
			if usage, ok := respData["usage"].(map[string]interface{}); ok {
				var pt, ct, tt float64
				if p, ok := usage["prompt_tokens"].(float64); ok {
					pt = p
				}
				if c, ok := usage["completion_tokens"].(float64); ok {
					ct = c
				}
				if t, ok := usage["total_tokens"].(float64); ok {
					tt = t
				}
				if tt > 0 {
					rightCol = append(rightCol, fmt.Sprintf("  📊 tokens: %.0f (📝%.0f + ✍️%.0f)", tt, pt, ct))
				}
			}
		}
		rightCol = append(rightCol, fmt.Sprintf("  💬 choices: %d", choicesCount))

		if m.selectedEntry != nil && m.selectedEntry.TotalSpend > 0 {
			rightCol = append(rightCol, fmt.Sprintf("  💰 $%.4f", m.selectedEntry.TotalSpend))
		}

		leftWidth := m.width / 2
		rightWidth := m.width - leftWidth - 1

		leftContent := strings.Join(leftCol, "\n")
		rightContent := strings.Join(rightCol, "\n")

		var leftCardStyle, rightCardStyle lipgloss.Style
		if m.detailState.focusedSection == 0 {
			leftCardStyle = focusedCardStyle
			rightCardStyle = cardStyle
		} else {
			leftCardStyle = cardStyle
			rightCardStyle = focusedCardStyle
		}

		leftCard := leftCardStyle.Width(leftWidth - 2).Render(leftContent)
		rightCard := rightCardStyle.Width(rightWidth - 2).Render(rightContent)

		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, leftCard, rightCard))
	} else {
		requestLines := []string{groupStyle.Render("📤 REQUEST")}
		requestLines = append(requestLines, fmt.Sprintf("  🤖 %s", valueStyle.Render(modelName)))
		requestLines = append(requestLines, "")

		requestOptions := []struct {
			key      string
			icon     string
			label    string
			count    int
		}{
			{"system", "📦", "system", systemCount},
			{"tools", "🔧", "tools", toolsCount},
			{"messages", "💬", "messages", messagesCount},
		}

		requestFocusedIdx := m.detailState.focusedSection

		for i, opt := range requestOptions {
			if opt.count > 0 {
				if i == requestFocusedIdx {
					requestLines = append(requestLines, successStyle.Render(fmt.Sprintf("  ▶ %s %s [%d]", opt.icon, opt.label, opt.count)))
				} else {
					requestLines = append(requestLines, fmt.Sprintf("  %s %s [%d]", opt.icon, opt.label, opt.count))
				}
			} else {
				requestLines = append(requestLines, mutedStyle.Render(fmt.Sprintf("  %s %s [无]", opt.icon, opt.label)))
			}
		}

		var metaInfo []string
		if proxyReq != nil {
			if maxTokens, ok := proxyReq["max_tokens"].(float64); ok {
				metaInfo = append(metaInfo, fmt.Sprintf("max_tokens: %.0f", maxTokens))
			}
			if outputConfig, ok := proxyReq["output_config"].(map[string]interface{}); ok {
				if reasoningEffort, ok := outputConfig["reasoning_effort"].(string); ok {
					metaInfo = append(metaInfo, fmt.Sprintf("thinking: %s", reasoningEffort))
				}
			}
		}
		if len(metaInfo) > 0 {
			requestLines = append(requestLines, "")
			requestLines = append(requestLines, mutedStyle.Render("  ⚙️ 元信息"))
			for _, m := range metaInfo {
				requestLines = append(requestLines, fmt.Sprintf("    %s", contentStyle.Render(m)))
			}
		}

		responseLines := []string{groupStyle.Render("📥 RESPONSE")}
		responseLines = append(responseLines, "")

		if requestFocusedIdx == 3 {
			responseLines = append(responseLines, successStyle.Render(fmt.Sprintf("  ▶ 💬 choices [%d]", choicesCount)))
		} else {
			if choicesCount > 0 {
				responseLines = append(responseLines, fmt.Sprintf("  💬 choices [%d]", choicesCount))
			} else {
				responseLines = append(responseLines, mutedStyle.Render("  💬 choices [无]"))
			}
		}
		if respData != nil {
			if usage, ok := respData["usage"].(map[string]interface{}); ok {
				var pt, ct, tt float64
				if p, ok := usage["prompt_tokens"].(float64); ok {
					pt = p
				}
				if c, ok := usage["completion_tokens"].(float64); ok {
					ct = c
				}
				if t, ok := usage["total_tokens"].(float64); ok {
					tt = t
				}
				if tt > 0 {
					responseLines = append(responseLines, fmt.Sprintf("  📊 tokens: %.0f (📝%.0f + ✍️%.0f)", tt, pt, ct))
				}
			}
		}
		if m.selectedEntry != nil && m.selectedEntry.TotalSpend > 0 {
			responseLines = append(responseLines, fmt.Sprintf("  💰 $%.4f", m.selectedEntry.TotalSpend))
		}

		cardWidth := m.width - 4

		lines = append(lines, cardStyle.Width(cardWidth).Render(strings.Join(requestLines, "\n")))
		lines = append(lines, cardStyle.Width(cardWidth).Render(strings.Join(responseLines, "\n")))
	}

	return lines
}

func (m *Model) renderArrayDetailView(proxyReq, respData map[string]interface{}, cardStyle, focusedCardStyle, contentStyle, mutedStyle, groupStyle, valueStyle, keyStyle lipgloss.Style) []string {
	var lines []string

	tab := m.detailState.activeTab
	_ = m.getTabItemCount(tab)
	selectedIdx := m.detailState.selectedItem

	if selectedIdx < 0 {
		selectedIdx = 0
	}

	switch tab {
	case "system":
		if proxyReq != nil {
			if system, ok := proxyReq["system"].([]interface{}); ok {
				itemCount := len(system)
				if selectedIdx >= itemCount {
					selectedIdx = 0
					m.detailState.selectedItem = 0
				}

				if m.detailState.itemDetailMode {
					idx := m.detailState.currentItemIndex
					if idx >= 0 && idx < len(system) {
						lines = append(lines, m.renderSystemItem(system[idx], idx, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					}
				} else {
					for i := 0; i < len(system); i++ {
						if i == selectedIdx {
							lines = append(lines, m.renderSystemSummary(system[i], i, contentStyle, mutedStyle, true)...)
						} else {
							lines = append(lines, m.renderSystemSummary(system[i], i, contentStyle, mutedStyle, false)...)
						}
					}
				}
			}
		}
	case "messages":
		if proxyReq != nil {
			if messages, ok := proxyReq["messages"].([]interface{}); ok {
				itemCount := len(messages)
				if selectedIdx >= itemCount {
					selectedIdx = 0
					m.detailState.selectedItem = 0
				}

				for i := 0; i < len(messages); i++ {
					if i == selectedIdx {
						lines = append(lines, m.renderMessageItem(messages[i], i, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					} else {
						lines = append(lines, m.renderMessageSummary(messages[i], i, contentStyle, mutedStyle)...)
					}
				}
			}
		}
	case "tools":
		if proxyReq != nil {
			if tools, ok := proxyReq["tools"].([]interface{}); ok {
				itemCount := len(tools)
				if selectedIdx >= itemCount {
					selectedIdx = 0
					m.detailState.selectedItem = 0
				}

				for i := 0; i < len(tools) && i < 20; i++ {
					if i == selectedIdx {
						lines = append(lines, m.renderToolItem(tools[i], i, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					} else {
						lines = append(lines, m.renderToolSummary(tools[i], i, contentStyle, mutedStyle)...)
					}
				}
				if len(tools) > 20 {
					lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ... 还有 %d 个", len(tools)-20)))
				}
			}
		}
	case "choices":
		if respData != nil {
			if choices, ok := respData["choices"].([]interface{}); ok {
				itemCount := len(choices)
				if selectedIdx >= itemCount {
					selectedIdx = 0
					m.detailState.selectedItem = 0
				}

				for i := 0; i < len(choices) && i < 10; i++ {
					if i == selectedIdx {
						lines = append(lines, m.renderChoiceItem(choices[i], i, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					} else {
						lines = append(lines, m.renderChoiceSummary(choices[i], i, contentStyle, mutedStyle)...)
					}
				}
				if len(choices) > 10 {
					lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ... 还有 %d 个", len(choices)-10)))
				}
			}
		}
	}

	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("无数据"))
	}

	return lines
}

func (m *Model) renderMessageSummary(msg interface{}, idx int, contentStyle, mutedStyle lipgloss.Style) []string {
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(msg); err == nil {
			return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 50)))}
		}
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, msg))}
	}

	role, _ := msgMap["role"].(string)
	content, _ := msgMap["content"].(string)
	toolCalls, _ := msgMap["tool_calls"].([]interface{})

	roleIcon := map[string]string{
		"system":   "📦",
		"user":     "👤",
		"assistant": "🤖",
		"tool":     "🔧",
	}[role]
	if roleIcon == "" {
		roleIcon = "💬"
	}

	summary := roleIcon + " " + role
	if content != "" {
		summary += ": " + truncate(content, 50)
	}
	if len(toolCalls) > 0 {
		summary += fmt.Sprintf(" [+%d tool_calls]", len(toolCalls))
	}

	return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, summary))}
}

func (m *Model) renderMessageItem(msg interface{}, idx int, contentStyle, mutedStyle, groupStyle, valueStyle lipgloss.Style) []string {
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(msg); err == nil {
			return []string{contentStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 200)))}
		}
		return []string{contentStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, msg))}
	}

	role, _ := msgMap["role"].(string)
	content, contentIsString := msgMap["content"].(string)
	if !contentIsString {
		rawContent := msgMap["content"]
		if rawContent != nil {
			if jsonBytes, err := json.Marshal(rawContent); err == nil {
				content = fmt.Sprintf("(type: %T) %s", rawContent, truncate(string(jsonBytes), 100))
			}
		}
	}
	toolCalls, _ := msgMap["tool_calls"].([]interface{})

	var lines []string
	lines = append(lines, groupStyle.Render(fmt.Sprintf("  [%d] %s", idx, role)))

	if content != "" {
		lines = append(lines, contentStyle.Render(truncate(content, 500)))
	}

	if len(toolCalls) > 0 {
		lines = append(lines, mutedStyle.Render("  tool_calls:"))
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				var fnName string
				var args string
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					if n, ok := fn["name"].(string); ok {
						fnName = n
					}
					if a, ok := fn["arguments"].(string); ok {
						args = truncate(a, 200)
					} else if rawArgs := fn["arguments"]; rawArgs != nil {
						if jsonBytes, err := json.Marshal(rawArgs); err == nil {
							args = truncate(string(jsonBytes), 200)
						}
					}
				}
				if args != "" {
					lines = append(lines, contentStyle.Render(fmt.Sprintf("    - %s(%s)", fnName, args)))
				} else {
					lines = append(lines, contentStyle.Render(fmt.Sprintf("    - %s()", fnName)))
				}
			}
		}
	}

	return lines
}

func (m *Model) renderToolSummary(tool interface{}, idx int, contentStyle, mutedStyle lipgloss.Style) []string {
	toolMap, ok := tool.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(tool); err == nil {
			return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 50)))}
		}
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, tool))}
	}

	var name, desc string
	if fn, ok := toolMap["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
		if d, ok := fn["description"].(string); ok {
			desc = truncate(d, 40)
		}
	}

	summary := fmt.Sprintf("🔧 %s", name)
	if desc != "" {
		summary += ": " + desc
	}

	return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, summary))}
}

func (m *Model) renderSystemSummary(sys interface{}, idx int, contentStyle, mutedStyle lipgloss.Style, focused bool) []string {
	sysMap, ok := sys.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(sys); err == nil {
			return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 50)))}
		}
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, sys))}
	}

	sysType, _ := sysMap["type"].(string)
	text, _ := sysMap["text"].(string)

	prefix := "  "
	style := mutedStyle
	if focused {
		prefix = "▶ "
		style = contentStyle.Bold(true)
	}

	summary := fmt.Sprintf("system[%d] (%s)", idx, sysType)
	if text != "" {
		summary += ": " + truncate(text, 40)
	}

	return []string{style.Render(prefix + summary)}
}

func (m *Model) renderSystemItem(sys interface{}, idx int, contentStyle, mutedStyle, groupStyle, valueStyle lipgloss.Style) []string {
	sysMap, ok := sys.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(sys); err == nil {
			return []string{contentStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 200)))}
		}
		return []string{contentStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, sys))}
	}

	var lines []string
	sysType, _ := sysMap["type"].(string)
	lines = append(lines, groupStyle.Render(fmt.Sprintf("  [%d] system (%s)", idx, sysType)))

	if text, ok := sysMap["text"].(string); ok && text != "" {
		if m.detailState != nil && m.detailState.activeTab == "system" && m.detailState.itemDetailMode {
			if m.detailState.markdownViewMode == "rendered" {
				rendered := m.renderMarkdownFull(text)
				lines = append(lines, rendered...)
			} else {
				// raw 模式：为了保持按行物理滚动的确定性，将 text 按 \n 拆分并逐行加入 lines
				rawLines := strings.Split(text, "\n")
				for _, rl := range rawLines {
					lines = append(lines, contentStyle.Render(rl))
				}
			}
		} else {
			lines = append(lines, contentStyle.Render(truncate(text, 500)))
		}
	}

	if cacheControl, ok := sysMap["cache_control"].(map[string]interface{}); ok {
		if ctype, ok := cacheControl["type"].(string); ok {
			lines = append(lines, mutedStyle.Render("  cache_control: "+ctype))
		}
	}

	return lines
}

func (m *Model) renderMarkdownFull(text string) []string {
	maxWidth := m.width - 10
	if maxWidth < 40 {
		maxWidth = 60
	}
	if maxWidth > 120 {
		maxWidth = 120
	}

	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("154")).Background(lipgloss.Color("236"))
	boldStyle := lipgloss.NewStyle().Bold(true)
	italicStyle := lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("219"))
	listStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	quoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

	lines := processMarkdownWithStyles(text, maxWidth, headingStyle, codeStyle, boldStyle, italicStyle, listStyle, quoteStyle)

	return lines
}

func (m *Model) applyMarkdownScroll(lines []string, maxDisplayLines int) []string {
	if len(lines) == 0 {
		return lines
	}

	if maxDisplayLines < 5 {
		maxDisplayLines = 10
	}

	scrollOffset := m.detailState.markdownScrollOffset

	if scrollOffset >= len(lines) {
		scrollOffset = len(lines) - 1
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	}

	endLine := scrollOffset + maxDisplayLines
	if endLine > len(lines) {
		endLine = len(lines)
	}

	m.detailState.markdownScrollOffset = scrollOffset

	visibleLines := lines[scrollOffset:endLine]

	if scrollOffset > 0 || endLine < len(lines) {
		visibleLines = append(visibleLines, fmt.Sprintf(" ──◀ %d/%d ▶─ ", scrollOffset+1, len(lines)))
	}

	return visibleLines
}

func (m *Model) renderMarkdown(text string) []string {
	maxWidth := m.width - 10
	if maxWidth < 40 {
		maxWidth = 60
	}
	if maxWidth > 120 {
		maxWidth = 120
	}

	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("154")).Background(lipgloss.Color("236"))
	boldStyle := lipgloss.NewStyle().Bold(true)
	italicStyle := lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("219"))
	listStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	quoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

	lines := processMarkdownWithStyles(text, maxWidth, headingStyle, codeStyle, boldStyle, italicStyle, listStyle, quoteStyle)

	scrollOffset := 0
	if m.detailState != nil {
		scrollOffset = m.detailState.markdownScrollOffset
	}

	if scrollOffset >= len(lines) {
		scrollOffset = len(lines) - 1
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	}

	maxDisplayLines := m.height - 15
	if maxDisplayLines < 5 {
		maxDisplayLines = 10
	}

	endLine := scrollOffset + maxDisplayLines
	if endLine > len(lines) {
		endLine = len(lines)
	}

	if m.detailState != nil {
		m.detailState.markdownScrollOffset = scrollOffset
	}

	visibleLines := lines[scrollOffset:endLine]

	if scrollOffset > 0 || endLine < len(lines) {
		visibleLines = append(visibleLines, fmt.Sprintf(" ──◀ %d/%d ▶─ ", scrollOffset+1, len(lines)))
	}

	return visibleLines
}

func processMarkdownWithStyles(text string, width int, headingStyle, codeStyle, boldStyle, italicStyle, listStyle, quoteStyle lipgloss.Style) []string {
	var result []string

	paragraphs := strings.Split(text, "\n\n")

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if strings.HasPrefix(para, "### ") {
			result = append(result, headingStyle.Render(strings.TrimPrefix(para, "### ")))
			continue
		} else if strings.HasPrefix(para, "## ") {
			result = append(result, headingStyle.Render(strings.TrimPrefix(para, "## ")))
			continue
		} else if strings.HasPrefix(para, "# ") {
			result = append(result, headingStyle.Render(strings.TrimPrefix(para, "# ")))
			continue
		}

		if strings.HasPrefix(para, "```") {
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				result = append(result, codeStyle.Render(wrapText(line, width)))
			}
			continue
		}

		if strings.HasPrefix(para, "- ") || strings.HasPrefix(para, "* ") {
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
					line = "• " + strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
				}
				result = append(result, wrapText(listStyle.Render(line), width))
			}
			continue
		}

		if strings.HasPrefix(para, "> ") {
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				line = strings.TrimPrefix(line, "> ")
				result = append(result, quoteStyle.Render("│ "+line))
			}
			continue
		}

		reOrderedList := regexp.MustCompile(`^\d+\.\s`)
		if reOrderedList.MatchString(para) {
			lines := strings.Split(para, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				matched := reOrderedList.FindString(line)
				if matched != "" {
					line = reOrderedList.ReplaceAllString(line, " $1. ")
					line = listStyle.Render(line)
				}
				result = append(result, wrapText(line, width))
			}
			continue
		}

		processed := processInlineMarkdownWithStyles(para, codeStyle, boldStyle, italicStyle)
		wrappedLines := wrapText(processed, width)
		result = append(result, wrappedLines)
	}

	if len(result) == 0 {
		result = append(result, text)
	}

	return result
}

func processInlineMarkdownWithStyles(text string, codeStyle, boldStyle, italicStyle lipgloss.Style) string {
	reCode := regexp.MustCompile("`([^`]+)`")
	text = reCode.ReplaceAllStringFunc(text, func(match string) string {
		content := match[1 : len(match)-1]
		return codeStyle.Render(content)
	})

	reBold := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		return boldStyle.Render(content)
	})

	reBoldAlt := regexp.MustCompile(`__([^_]+)__`)
	text = reBoldAlt.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		return boldStyle.Render(content)
	})

	reItalic := regexp.MustCompile(`\*([^*]+)\*`)
	text = reItalic.ReplaceAllStringFunc(text, func(match string) string {
		content := match[1 : len(match)-1]
		return italicStyle.Render(content)
	})

	reItalicAlt := regexp.MustCompile(`_([^_]+)_`)
	text = reItalicAlt.ReplaceAllStringFunc(text, func(match string) string {
		content := match[1 : len(match)-1]
		return italicStyle.Render(content)
	})

	return text
}

func wrapText(text string, width int) string {
	if width <= 0 {
		width = 80
	}

	cleanText := stripANSI(text)
	if runewidth.StringWidth(cleanText) <= width {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteString("\n")
		}

		cleanLine := stripANSI(line)
		if runewidth.StringWidth(cleanLine) <= width {
			result.WriteString(line)
			continue
		}

		var currentLine strings.Builder
		for _, r := range line {
			currentLine.WriteRune(r)
			checkStr := currentLine.String()
			cleanCheck := stripANSI(checkStr)
			if runewidth.StringWidth(cleanCheck) >= width {
				lastSpace := strings.LastIndex(checkStr, " ")
				if lastSpace > 0 {
					result.WriteString(checkStr[:lastSpace])
					result.WriteString("\n")
					remaining := checkStr[lastSpace+1:]
					if remaining != "" {
						currentLine = strings.Builder{}
						currentLine.WriteString(remaining)
					} else {
						currentLine = strings.Builder{}
					}
				} else {
					result.WriteString(checkStr)
					currentLine = strings.Builder{}
				}
			}
		}
		result.WriteString(currentLine.String())
	}

	return result.String()
}

func stripANSI(s string) string {
	result := strings.Builder{}
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i++
			continue
		}
		if inEscape {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

func (m *Model) renderToolItem(tool interface{}, idx int, contentStyle, mutedStyle, groupStyle, valueStyle lipgloss.Style) []string {
	toolMap, ok := tool.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(tool); err == nil {
			return []string{contentStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 200)))}
		}
		return []string{contentStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, tool))}
	}

	var lines []string
	lines = append(lines, groupStyle.Render(fmt.Sprintf("  [%d] tool", idx)))

	if fn, ok := toolMap["function"].(map[string]interface{}); ok {
		if name, ok := fn["name"].(string); ok {
			lines = append(lines, valueStyle.Render("  name: "+name))
		}
		if desc, ok := fn["description"].(string); ok {
			lines = append(lines, contentStyle.Render("  description: "+truncate(desc, 300)))
		}
		if params, ok := fn["parameters"].(map[string]interface{}); ok {
			if jsonBytes, err := json.MarshalIndent(params, "    ", "  "); err == nil {
				lines = append(lines, mutedStyle.Render("  parameters:"))
				lines = append(lines, contentStyle.Render("    "+truncate(string(jsonBytes), 300)))
			}
		}
	}

	return lines
}

func (m *Model) renderChoiceSummary(choice interface{}, idx int, contentStyle, mutedStyle lipgloss.Style) []string {
	c, ok := choice.(map[string]interface{})
	if !ok {
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据", idx))}
	}

	var finishReason string
	if fr, ok := c["finish_reason"].(string); ok {
		finishReason = fr
	}

	summary := fmt.Sprintf("💬 choice[%d]", idx)
	if finishReason != "" {
		summary += fmt.Sprintf(" (%s)", finishReason)
	}

	return []string{mutedStyle.Render("  " + summary)}
}

func (m *Model) renderChoiceItem(choice interface{}, idx int, contentStyle, mutedStyle, groupStyle, valueStyle lipgloss.Style) []string {
	c, ok := choice.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(choice); err == nil {
			return []string{contentStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 200)))}
		}
		return []string{contentStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, choice))}
	}

	var lines []string
	lines = append(lines, groupStyle.Render(fmt.Sprintf("  [%d] choice", idx)))

	if fr, ok := c["finish_reason"].(string); ok {
		lines = append(lines, valueStyle.Render("  finish_reason: "+fr))
	}

	if msg, ok := c["message"].(map[string]interface{}); ok {
		if role, ok := msg["role"].(string); ok {
			lines = append(lines, valueStyle.Render("  role: "+role))
		}
		rawContent := msg["content"]
		if content, ok := rawContent.(string); ok && content != "" {
			lines = append(lines, contentStyle.Render("  content: "+truncate(content, 300)))
		} else if rawContent != nil {
			if jsonBytes, err := json.Marshal(rawContent); err == nil {
				lines = append(lines, contentStyle.Render("  content: "+truncate(string(jsonBytes), 200)))
			}
		}
		if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			lines = append(lines, mutedStyle.Render("  tool_calls:"))
			for _, tc := range toolCalls {
				if tcMap, ok := tc.(map[string]interface{}); ok {
					var fnName string
					var args string
					if fn, ok := tcMap["function"].(map[string]interface{}); ok {
						if n, ok := fn["name"].(string); ok {
							fnName = n
						}
						if a, ok := fn["arguments"].(string); ok {
							args = truncate(a, 200)
						} else if rawArgs := fn["arguments"]; rawArgs != nil {
							if jsonBytes, err := json.Marshal(rawArgs); err == nil {
								args = truncate(string(jsonBytes), 200)
							}
						}
					}
					if args != "" {
						lines = append(lines, contentStyle.Render(fmt.Sprintf("    - %s(%s)", fnName, args)))
					} else {
						lines = append(lines, contentStyle.Render(fmt.Sprintf("    - %s()", fnName)))
					}
				}
			}
		}
	}

	return lines
}

type toolInfo struct {
	name   string
	called bool
	schema string
}

func (m *Model) parseToolsInfo() (result struct {
	total  int
	called int
	tools  []toolInfo
}) {
	result.tools = []toolInfo{}

	proxyReq, _ := m.detailData["proxy_server_request"].(map[string]interface{})
	if proxyReq != nil {
		if tools, ok := proxyReq["tools"].([]interface{}); ok {
			for _, tool := range tools {
				if toolMap, ok := tool.(map[string]interface{}); ok {
					var name string
					var schema string
					if fn, ok := toolMap["function"].(map[string]interface{}); ok {
						if n, ok := fn["name"].(string); ok {
							name = n
						}
						if desc, ok := fn["description"].(string); ok {
							schema = truncate(desc, 100)
						}
					}
					if name != "" {
						result.total++
						result.tools = append(result.tools, toolInfo{name: name, called: false, schema: schema})
					}
				}
			}
		}
	}

	if messages, ok := m.detailData["messages"].([]interface{}); ok {
		calledNames := make(map[string]bool)
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				if toolCalls, ok := msgMap["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						if tcMap, ok := tc.(map[string]interface{}); ok {
							var name string
							if n, ok := tcMap["function"].(map[string]interface{}); ok {
								if fn, ok := n["name"].(string); ok {
									name = fn
								}
							}
							if name != "" {
								calledNames[name] = true
								found := false
								for i, t := range result.tools {
									if t.name == name {
										result.tools[i].called = true
										found = true
										break
									}
								}
								if !found {
									result.total++
									result.called++
									result.tools = append(result.tools, toolInfo{name: name, called: true})
								}
							}
						}
					}
				}
			}
		}
		for i := range result.tools {
			if calledNames[result.tools[i].name] {
				result.called++
			}
		}
	}

	return result
}

func (m *Model) renderInputContent() string {
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	roleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("210")).Bold(true)

	var lines []string

	if messages, ok := m.detailData["messages"].([]interface{}); ok {
		for i, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				role, _ := msgMap["role"].(string)
				content, _ := msgMap["content"].(string)

				roleStr := roleStyle.Render(role + ":")
				contentStr := truncate(content, 200)

				if i < len(messages)-1 || role == "system" || role == "assistant" {
					contentStr = mutedStyle.Render("[点击展开] " + truncate(content, 50))
				}

				lines = append(lines, roleStr+" "+contentStyle.Render(contentStr))
			}
		}
	} else if proxyReq, ok := m.detailData["proxy_server_request"].(map[string]interface{}); ok {
		if messages, ok := proxyReq["messages"].([]interface{}); ok {
			for i, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					role, _ := msgMap["role"].(string)
					content, _ := msgMap["content"].(string)

					roleStr := roleStyle.Render(role + ":")
					contentStr := truncate(content, 200)

					if i < len(messages)-1 || role == "system" || role == "assistant" {
						contentStr = mutedStyle.Render("[点击展开] " + truncate(content, 50))
					}

					lines = append(lines, roleStr+" "+contentStyle.Render(contentStr))
				}
			}
		}
	}

	if len(lines) == 0 {
		return mutedStyle.Render("无 Input 数据")
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderOutputContent() string {
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	roleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("210")).Bold(true)
	toolCallStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)

	var lines []string

	if response, ok := m.detailData["response"].(map[string]interface{}); ok {
		if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
			for _, choice := range choices {
				if c, ok := choice.(map[string]interface{}); ok {
					if msg, ok := c["message"].(map[string]interface{}); ok {
						if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
							lines = append(lines, toolCallStyle.Render("🔧 Tool Calls:"))
							for _, tc := range toolCalls {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									var fnName string
									var args string
									if fn, ok := tcMap["function"].(map[string]interface{}); ok {
										if n, ok := fn["name"].(string); ok {
											fnName = n
										}
										if a, ok := fn["arguments"].(string); ok {
											args = truncate(a, 100)
										} else if rawArgs := fn["arguments"]; rawArgs != nil {
											if jsonBytes, err := json.Marshal(rawArgs); err == nil {
												args = truncate(string(jsonBytes), 100)
											}
										}
									}
									if args != "" {
										lines = append(lines, "  "+toolCallStyle.Render("• ")+contentStyle.Render(fnName+"("+args+")"))
									} else {
										lines = append(lines, "  "+toolCallStyle.Render("• ")+contentStyle.Render(fnName+"()"))
									}
								}
							}
							lines = append(lines, "")
						}

						if content, ok := msg["content"].(string); ok && content != "" {
							lines = append(lines, roleStyle.Render("assistant:")+" "+contentStyle.Render(truncate(content, 300)))
						}
					}
				}
			}
		}
	}

	if len(lines) == 0 {
		if messages, ok := m.detailData["messages"].([]interface{}); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					if role, ok := msgMap["role"].(string); ok && role == "assistant" {
						if toolCalls, ok := msgMap["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
							lines = append(lines, toolCallStyle.Render("🔧 Tool Calls:"))
							for _, tc := range toolCalls {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									var fnName string
									var args string
									if fn, ok := tcMap["function"].(map[string]interface{}); ok {
										if n, ok := fn["name"].(string); ok {
											fnName = n
										}
										if a, ok := fn["arguments"].(string); ok {
											args = truncate(a, 100)
										} else if rawArgs := fn["arguments"]; rawArgs != nil {
											if jsonBytes, err := json.Marshal(rawArgs); err == nil {
												args = truncate(string(jsonBytes), 100)
											}
										}
									}
									if args != "" {
										lines = append(lines, "  "+toolCallStyle.Render("• ")+contentStyle.Render(fnName+"("+args+")"))
									} else {
										lines = append(lines, "  "+toolCallStyle.Render("• ")+contentStyle.Render(fnName+"()"))
									}
								}
							}
							lines = append(lines, "")
						}
						if content, ok := msgMap["content"].(string); ok && content != "" {
							lines = append(lines, roleStyle.Render("assistant:")+" "+contentStyle.Render(truncate(content, 300)))
						}
					}
				}
			}
		}
	}

	if len(lines) == 0 {
		return mutedStyle.Render("无 Output 数据")
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderMetadataContent() string {
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	if metadata, ok := m.detailData["metadata"].(map[string]interface{}); ok && len(metadata) > 0 {
		jsonBytes, err := json.MarshalIndent(metadata, "  ", "  ")
		if err != nil {
			return mutedStyle.Render("metadata 解析失败")
		}
		return contentStyle.Render(string(jsonBytes))
	}

	var otherFields []string
	skipKeys := map[string]bool{
		"request_tags": true, "model": true, "model_id": true, "call_type": true,
		"status": true, "total_tokens": true, "prompt_tokens": true, "completion_tokens": true,
		"spend": true, "latency": true, "cache_hit": true, "litellm_overhead_time": true,
		"retries": true, "startTime": true, "endTime": true, "prompt_cost": true,
		"completion_cost": true, "messages": true, "response": true, "proxy_server_request": true,
		"available_tools": true, "tools": true, "metadata": true,
	}

	for k, v := range m.detailData {
		if skipKeys[k] {
			continue
		}
		valStr := fmt.Sprintf("%v", v)
		if len(valStr) > 80 {
			valStr = valStr[:80] + "..."
		}
		otherFields = append(otherFields, k+": "+valStr)
	}

	if len(otherFields) > 0 {
		return contentStyle.Render(strings.Join(otherFields, "\n"))
	}

	return mutedStyle.Render("无 Metadata 数据")
}

func (m *Model) renderListView() string {
	var content strings.Builder

	availableRows := 50
	if m.height > 10 {
		availableRows = m.height - 10
	}

	if m.logData != nil && len(m.logData.Data) > 0 {
		filteredData := m.logData.Data
		if m.model != "" {
			var filtered []api.SpendLogEntry
			for _, entry := range m.logData.Data {
				if strings.Contains(entry.Model, m.model) {
					filtered = append(filtered, entry)
				}
			}
			filteredData = filtered
		}
		content.WriteString(renderLogsTable(filteredData, int(m.logData.Total), m.newLogIDs, availableRows, m.selectedIndex))
	} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
		content.WriteString(renderLogsTableOld(m.logDataOld, m.interval, m.newLogIDs, availableRows, m.selectedIndex))
	} else {
		content.WriteString(components.NewPlaceholder("暂无数据").View())
	}

	header := components.NewHeader("LiteLLM 日志", fmt.Sprintf("刷新: %ds | ↑↓ 选择 | Enter 详情 | q 退出", m.interval))

	return header.View(m.width) +
		"\n\n" +
		content.String() +
		fmt.Sprintf("\n\n⏱ 更新次数: %d | 时间: %s", m.tick, time.Now().Format("15:04:05"))
}

func (m *Model) loadDetail() tea.Cmd {
	var requestID string

	if m.logData != nil && m.selectedIndex < len(m.logData.Data) {
		requestID = m.logData.Data[m.selectedIndex].ID
		m.selectedEntry = &m.logData.Data[m.selectedIndex]
	} else if m.logDataOld != nil && m.selectedIndex < len(*m.logDataOld) {
		if id, ok := (*m.logDataOld)[m.selectedIndex]["request_id"]; ok {
			requestID, _ = id.(string)
		}
		m.selectedEntry = nil
	} else {
		m.detailError = "暂无数据"
		m.viewMode = "detail"
		m.selectedEntry = nil
		return nil
	}

	if requestID == "" {
		m.detailError = "无法获取日志ID"
		m.viewMode = "detail"
		m.selectedEntry = nil
		return nil
	}

	m.viewMode = "detail"
	m.detailData = nil
	m.detailError = "加载中..."

	return func() tea.Msg {
		log.Printf("[loadDetail] 开始加载详情, requestID=%s", requestID)
		detail, err := m.client.GetSpendLogDetail(requestID)
		if err != nil {
			log.Printf("[loadDetail] 请求失败: %v", err)
			return DetailLoadedMsg{Error: fmt.Sprintf("请求失败: %v", err)}
		}
		log.Printf("[loadDetail] 请求完成, requestID=%s, keys=%v", requestID, getMapKeys(detail))
		if detail == nil {
			return DetailLoadedMsg{Error: "API 返回空数据，请确认日志详情接口是否可用"}
		}
		return DetailLoadedMsg{Data: detail}
	}
}

func (m *Model) getTabItemCount(tab string) int {
	proxyReq, _ := m.detailData["proxy_server_request"].(map[string]interface{})
	respData, _ := m.detailData["response"].(map[string]interface{})

	switch tab {
	case "system":
		if proxyReq != nil {
			if system, ok := proxyReq["system"].([]interface{}); ok {
				return len(system)
			}
		}
	case "tools":
		if proxyReq != nil {
			if tools, ok := proxyReq["tools"].([]interface{}); ok {
				return len(tools)
			}
		}
	case "messages":
		if proxyReq != nil {
			if messages, ok := proxyReq["messages"].([]interface{}); ok {
				return len(messages)
			}
		}
	case "choices":
		if respData != nil {
			if choices, ok := respData["choices"].([]interface{}); ok {
				return len(choices)
			}
		}
	}
	return 0
}

func (m *Model) getArrayItem(tab string, index int) interface{} {
	proxyReq, _ := m.detailData["proxy_server_request"].(map[string]interface{})
	respData, _ := m.detailData["response"].(map[string]interface{})

	switch tab {
	case "system":
		if proxyReq != nil {
			if messages, ok := proxyReq["messages"].([]interface{}); ok {
				count := 0
				for _, msg := range messages {
					if msgMap, ok := msg.(map[string]interface{}); ok {
						if role, _ := msgMap["role"].(string); role == "system" {
							if count == index {
								return msgMap
							}
							count++
						}
					}
				}
			}
		}
	case "tools":
		if proxyReq != nil {
			if tools, ok := proxyReq["tools"].([]interface{}); ok {
				if index >= 0 && index < len(tools) {
					return tools[index]
				}
			}
		}
	case "messages":
		if proxyReq != nil {
			if messages, ok := proxyReq["messages"].([]interface{}); ok {
				if index >= 0 && index < len(messages) {
					return messages[index]
				}
			}
		}
	case "choices":
		if respData != nil {
			if choices, ok := respData["choices"].([]interface{}); ok {
				if index >= 0 && index < len(choices) {
					return choices[index]
				}
			}
		}
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func formatLocalTime(utcTime string) string {
	if len(utcTime) >= 19 {
		t, err := time.Parse("2006-01-02T15:04:05", utcTime[:19])
		if err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
		fallback := utcTime[:19]
		fallback = strings.Replace(fallback, "T", " ", 1)
		return fallback
	}
	return utcTime
}

func renderLogsTable(data []api.SpendLogEntry, total int, newLogIDs map[string]bool, maxRows int, selectedIndex int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	newHighlightStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	newHighlightMutedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	selectedMutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86")).Bold(true)

	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}

	var sb strings.Builder

	colWidths := struct {
		time    int
		status  int
		spend   int
		latency int
		tokens  int
		model   int
		tags    int
	}{
		time:    runewidth.StringWidth("时间"),
		status:  runewidth.StringWidth("状态"),
		spend:   runewidth.StringWidth("费用"),
		latency: runewidth.StringWidth("耗时"),
		tokens:  runewidth.StringWidth("Tokens"),
		model:   runewidth.StringWidth("模型"),
		tags:    runewidth.StringWidth("Tags"),
	}

	for _, entry := range data {
		startTime := formatLocalTime(entry.StartTime)
		colWidths.time = max(colWidths.time, runewidth.StringWidth(startTime))

		status := "✓"
		if entry.Status != "success" && entry.ErrorMessage != "" {
			status = "✗"
		}
		colWidths.status = max(colWidths.status, runewidth.StringWidth(status))

		spendStr := "-"
		if entry.TotalSpend > 0 {
			spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
		}
		colWidths.spend = max(colWidths.spend, runewidth.StringWidth(spendStr))

		latencyStr := "-"
		if entry.StartTime != "" && entry.EndTime != "" {
			start, err := time.Parse(time.RFC3339, entry.StartTime)
			if err == nil {
				end, err := time.Parse(time.RFC3339, entry.EndTime)
				if err == nil {
					duration := end.Sub(start)
					if duration > 0 {
						latencyStr = fmt.Sprintf("%.2fs", duration.Seconds())
					}
				}
			}
		}
		colWidths.latency = max(colWidths.latency, runewidth.StringWidth(latencyStr))

		tokensStr := "-"
		if entry.TotalTokens > 0 {
			tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
		}
		colWidths.tokens = max(colWidths.tokens, runewidth.StringWidth(tokensStr))

		model := entry.ModelGroup
		if model == "" {
			model = entry.Model
		}
		colWidths.model = max(colWidths.model, runewidth.StringWidth(model))

		tag := ""
		if len(entry.RequestTags) > 0 {
			tags := entry.RequestTags
			if len(tags) > 1 {
				sort.Slice(tags, func(i, j int) bool { return len(tags[i]) < len(tags[j]) })
				longest := tags[len(tags)-1]
				longest = strings.TrimPrefix(longest, "User-Agent: ")
				if idx := strings.Index(longest, "("); idx != -1 {
					longest = longest[:idx]
				}
				tag = strings.TrimSpace(longest)
			} else {
				tag = tags[0]
			}
		}
		colWidths.tags = max(colWidths.tags, runewidth.StringWidth(tag))
	}

	sb.WriteString(headerStyle.Render(fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight("时间", colWidths.time),
		padRight("状态", colWidths.status),
		padRight("费用", colWidths.spend),
		padRight("耗时", colWidths.latency),
		padRight("Tokens", colWidths.tokens),
		padRight("模型", colWidths.model),
		padRight("Tags", colWidths.tags))) + "\n")

	totalWidth := colWidths.time + colWidths.status + colWidths.spend + colWidths.latency + colWidths.tokens + colWidths.model + colWidths.tags + 6
	sb.WriteString(mutedStyle.Render(strings.Repeat("─", totalWidth)) + "\n")

	rowCount := 0
	for i, entry := range data {
		if maxRows > 0 && rowCount >= maxRows-2 {
			sb.WriteString(mutedStyle.Render(fmt.Sprintf("\n... 还有 %d 条记录 (总 %d)", len(data)-rowCount, total)))
			break
		}
		rowCount++

		isSelected := i == selectedIndex
		startTime := formatLocalTime(entry.StartTime)
		isNew := newLogIDs != nil && newLogIDs[entry.ID]

		status := "✓"
		if entry.Status != "success" && entry.ErrorMessage != "" {
			status = "✗"
		}

		spendStr := "-"
		if entry.TotalSpend > 0 {
			spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
		}

		latencyStr := "-"
		if entry.StartTime != "" && entry.EndTime != "" {
			start, _ := time.Parse(time.RFC3339, entry.StartTime)
			end, _ := time.Parse(time.RFC3339, entry.EndTime)
			duration := end.Sub(start)
			if duration > 0 {
				latencyStr = fmt.Sprintf("%.2fs", duration.Seconds())
			}
		}

		tokensStr := "-"
		if entry.TotalTokens > 0 {
			tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
		}

		model := entry.ModelGroup
		if model == "" {
			model = entry.Model
		}

		tag := ""
		if len(entry.RequestTags) > 0 {
			tags := entry.RequestTags
			if len(tags) > 1 {
				sort.Slice(tags, func(i, j int) bool { return len(tags[i]) < len(tags[j]) })
				longest := tags[len(tags)-1]
				longest = strings.TrimPrefix(longest, "User-Agent: ")
				if idx := strings.Index(longest, "("); idx != -1 {
					longest = longest[:idx]
				}
				tag = strings.TrimSpace(longest)
			} else {
				tag = tags[0]
			}
		}

		var timeStyle, statusStyle, spendStyle, latencyStyle, tokensStyle, modelStyle, tagStyle lipgloss.Style

		if isSelected {
			timeStyle = selectedStyle
			statusStyle = selectedStyle
			spendStyle = selectedStyle
			latencyStyle = selectedStyle
			tokensStyle = selectedStyle
			modelStyle = selectedStyle
			tagStyle = selectedMutedStyle
		} else if isNew {
			timeStyle = newHighlightStyle
			statusStyle = newHighlightStyle
			spendStyle = newHighlightStyle
			latencyStyle = newHighlightStyle
			tokensStyle = newHighlightStyle
			modelStyle = newHighlightStyle
			tagStyle = newHighlightMutedStyle
		} else if entry.Status != "success" && entry.ErrorMessage != "" {
			timeStyle = contentStyle
			statusStyle = errorStyle
			spendStyle = greenStyle
			latencyStyle = yellowStyle
			tokensStyle = contentStyle
			modelStyle = contentStyle
			tagStyle = mutedStyle
		} else {
			timeStyle = contentStyle
			statusStyle = greenStyle
			spendStyle = greenStyle
			latencyStyle = yellowStyle
			tokensStyle = contentStyle
			modelStyle = contentStyle
			tagStyle = mutedStyle
		}

		sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s %s\n",
			timeStyle.Render(padRight(startTime, colWidths.time)),
			statusStyle.Render(padRight(status, colWidths.status)),
			spendStyle.Render(padRight(spendStr, colWidths.spend)),
			latencyStyle.Render(padRight(latencyStr, colWidths.latency)),
			tokensStyle.Render(padRight(tokensStr, colWidths.tokens)),
			modelStyle.Render(padRight(model, colWidths.model)),
			tagStyle.Render(padRight(tag, colWidths.tags))))
	}

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("共 %d 条记录 (总 %d)", len(data), total))))

	return sb.String()
}

func renderLogsTableOld(resp *api.SpendLogsResponse, intervalVal int, newLogIDs map[string]bool, maxRows int, selectedIndex int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))

	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | Ctrl+C 退出 ", intervalVal)) + "\n\n")

	rowCount := 0
	for _, entry := range *resp {
		if maxRows > 0 && rowCount >= maxRows {
			sb.WriteString(mutedStyle.Render(fmt.Sprintf("\n... 还有 %d 条记录", len(*resp)-rowCount)))
			break
		}

		spendVal, hasSpend := entry["spend"]
		if hasSpend {
			spend, _ := spendVal.(float64)

			keyLabel := "当前 Key"
			if len(entry) > 0 {
				var keys []string
				for k := range entry {
					if k != "spend" && k != "models" && k != "users" && k != "startTime" {
						keys = append(keys, k)
					}
				}
				if len(keys) > 0 {
					sort.Strings(keys)
					keyLabel = keys[0]
				}
			}
			if len(keyLabel) > 12 {
				keyLabel = keyLabel[:8] + "..."
			}

			sb.WriteString(contentStyle.Render(fmt.Sprintf("📦 %s ", keyLabel)))
			if spend > 0 {
				sb.WriteString(greenStyle.Render(fmt.Sprintf("$%.4f ", spend)))
			}
			sb.WriteString("\n")
			rowCount++
		}
	}

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("共 %d 条记录", len(*resp)))))
	return sb.String()
}
