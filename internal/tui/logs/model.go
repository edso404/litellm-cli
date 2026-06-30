package logs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
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

// clearCopiedNotificationMsg 消息类型用于通知清理临时复制提示
type clearCopiedNotificationMsg struct{}

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
	client           LogsClient
	data             string
	interval         int
	model            string // 过滤模型
	tick             int
	quitting         bool
	logData          *api.SpendLogsUIResponse
	logDataOld       *api.SpendLogsResponse
	seenLogIDs       map[string]bool    // 已看到的日志ID
	newLogIDs        map[string]bool    // 本次新增的日志ID（用于高亮）
	initialized      bool               // 是否已完成首次加载
	width            int                // 窗口宽度
	height           int                // 窗口高度
	selectedIndex    int                // 当前选中的日志索引
	selectedEntry    *api.SpendLogEntry // 当前选中的日志条目（用于详情页）
	listScrollOffset int                // 列表视图滚动偏移量
	viewMode         string             // "list" 或 "detail"
	detailData       map[string]interface{}
	detailError      string
	detailScroll     int              // 详情视图滚动偏移量
	detailState      *detailViewState // 详情视图状态（展开/折叠）
	showHeader       bool             // 是否显示顶部 header（在 dashboard 中隐藏）
	showFooter       bool             // 是否显示底部 help footer（在 dashboard 中隐藏，由父容器统一渲染）
	debug            bool             // 是否启用调试日志
	showHelp         bool             // 是否显示帮助面板
	pollingPaused    bool             // 轮询是否已暂停
	loadError        string           // 加载错误信息
	refreshing       bool             // 是否正在刷新
	lastRefreshTime  time.Time        // 上次刷新时间
	sortField        string           // 排序字段: "time", "spend", "tokens"
	sortAscending    bool             // 排序顺序
}

// NewModel 构造工厂函数
func NewModel(client LogsClient, interval int, modelFilter string) *Model {
	m := &Model{
		client:        client,
		interval:      interval,
		model:         modelFilter,
		showHelp:      false,
		pollingPaused: false,
		loadError:     "",
		refreshing:    true,
		sortField:     "time",
		sortAscending: false,
		data:          "加载中...",
		seenLogIDs:    make(map[string]bool),
		newLogIDs:     make(map[string]bool),
		width:         DefaultWidth,
		height:        DefaultHeight,
		viewMode:      "list", // 默认视图模式
		selectedIndex: 0,
		showHeader:    true, // 默认显示 header
		showFooter:    true, // 默认显示 footer
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

// PausePollingMsg 暂停轮询消息
type PausePollingMsg struct{}

// ResumePollingMsg 恢复轮询消息
type ResumePollingMsg struct{}

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
	case clearCopiedNotificationMsg:
		if m.detailState != nil {
			m.detailState.copiedNotification = ""
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		switch key {
		// 帮助面板切换
		case "?":
			if m.viewMode == "list" {
				m.showHelp = !m.showHelp
			}
			return m, nil
		// 手动刷新
		case "r":
			if m.viewMode == "list" {
				m.refreshing = true
				return m, m.RefreshCmd()
			}
			return m, nil
		// 排序切换
		case "s":
			if m.viewMode == "list" {
				fields := []string{"time", "spend", "tokens"}
				currentIdx := 0
				for i, f := range fields {
					if m.sortField == f {
						currentIdx = i
						break
					}
				}
				// 切换到下一个字段
				m.sortField = fields[(currentIdx+1)%len(fields)]
				m.sortAscending = false
			}
			return m, nil
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			// 帮助面板模式下按 Esc 关闭帮助
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			// 详情视图的返回逻辑
			if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.itemDetailMode {
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
					m.listScrollOffset = 0
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
				} else if m.detailState.activeTab != "main" {
					if !m.detailState.itemDetailMode {
						m.detailState.itemDetailMode = true
						m.detailState.currentItemIndex = m.detailState.selectedItem
						m.detailState.markdownScrollOffset = 0
					} else {
						if len(m.detailState.blocks) > 0 && m.detailState.focusedBlock < len(m.detailState.blocks) {
							currentBlock := m.detailState.blocks[m.detailState.focusedBlock]
							if collapsed, exists := m.detailState.blockCollapsed[currentBlock]; exists {
								m.detailState.blockCollapsed[currentBlock] = !collapsed
							} else {
								// 第一次按 Enter：默认展开显示
								m.detailState.blockCollapsed[currentBlock] = false
							}
						}
					}
				}
			}
			return m, nil
		case "tab":
			if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.itemDetailMode && len(m.detailState.blocks) > 0 {
					m.detailState.focusedBlock = (m.detailState.focusedBlock + 1) % len(m.detailState.blocks)
					return m, nil
				}
				if m.detailState.activeTab == "main" {
					m.detailState.focusedSection = (m.detailState.focusedSection + 1) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = min(maxItems-1, m.detailState.selectedItem+1)
					}
				}
			}
			return m, nil
		// g: 跳转顶部，G: 跳转底部
		case "g":
			if m.viewMode == "list" && m.logData != nil && len(m.logData.Data) > 0 {
				m.selectedIndex = 0
				m.listScrollOffset = 0
			}
			return m, nil
		case "G":
			if m.viewMode == "list" && m.logData != nil && len(m.logData.Data) > 0 {
				m.selectedIndex = len(m.logData.Data) - 1
				// 计算滚动偏移使最后一项可见
				maxVisible := m.height - 10
				if maxVisible > 0 {
					m.listScrollOffset = max(0, len(m.logData.Data)-maxVisible)
				}
			}
			return m, nil

		case "c", "C":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.itemDetailMode {
				if len(m.detailState.blocks) > 0 && m.detailState.focusedBlock < len(m.detailState.blocks) {
					currentBlock := m.detailState.blocks[m.detailState.focusedBlock]
					var textToCopy string

					var itemData interface{}
					if m.detailState.activeTab != "main" {
						itemData = m.getArrayItem(m.detailState.activeTab, m.detailState.currentItemIndex)
					}

					if itemData != nil {
						var rawText string
						if m.detailState.activeTab == "choices" {
							if choiceMap, ok := itemData.(map[string]interface{}); ok {
								msg, ok := choiceMap["message"].(map[string]interface{})
								if !ok {
									msg, _ = choiceMap["delta"].(map[string]interface{})
								}
								if msg != nil {
									rawText = extractMessageContentFull(msg)
								} else if text, ok := choiceMap["text"].(string); ok {
									rawText = text
								}
							}
						} else if m.detailState.activeTab == "messages" {
							if msgMap, ok := itemData.(map[string]interface{}); ok {
								rawText = extractMessageContentFull(msgMap)
							}
						}

						if rawText != "" {
							thinking, cleanText := extractThinking(rawText)
							if currentBlock == "thinking" {
								textToCopy = thinking
							} else {
								textToCopy = cleanText
							}
						}
					}

					if textToCopy != "" {
						// 跨平台终端剪贴板 OSC 52 复制命令
						cmd := copyToClipboardOSC52(textToCopy)
						m.detailState.copiedNotification = "已将该块内容成功复制到剪贴板！"

						clearCmd := tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
							return clearCopiedNotificationMsg{}
						})
						return m, tea.Batch(cmd, clearCmd)
					}
				}
			}
			return m, nil
		case "up", "ctrl+p":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab != "main" {
				if m.detailState.itemDetailMode {
					m.detailState.markdownScrollOffset = max(0, m.detailState.markdownScrollOffset-3)
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = max(0, m.detailState.selectedItem-1)
					}
				}
			} else if m.viewMode == "list" {
				// 计算可见行数
				availableRows := DetailDefaultRows
				if m.height > 10 {
					availableRows = m.height - DetailMinRows
				}
				visibleRows := availableRows - 2
				if visibleRows < 1 {
					visibleRows = 1
				}

				if m.selectedIndex > 0 {
					m.selectedIndex--
					// 如果选中项在可见范围上方，调整滚动偏移
					if m.selectedIndex < m.listScrollOffset && m.listScrollOffset > 0 {
						m.listScrollOffset--
					}
				}
			} else if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					// 宽屏模式下：循环 0-3 (system → tools → messages → choices)
					// 但只在 focusedSection == 3 时才切换到右侧高亮
					m.detailState.focusedSection = (m.detailState.focusedSection - 1 + 4) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = max(0, m.detailState.selectedItem-1)
					}
				}
			}
			return m, nil
		case "k":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab == "main" {
				// detail 主视图：k 切换到上一条日志
				if m.selectedIndex > 0 {
					m.selectedIndex--
					m.detailScroll = 0
					m.detailState = nil
					return m, m.loadDetail()
				}
			} else if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab != "main" {
				if m.detailState.itemDetailMode {
					m.detailState.markdownScrollOffset = max(0, m.detailState.markdownScrollOffset-3)
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = max(0, m.detailState.selectedItem-1)
					}
				}
			} else if m.viewMode == "list" {
				availableRows := DetailDefaultRows
				if m.height > 10 {
					availableRows = m.height - DetailMinRows
				}
				visibleRows := availableRows - 2
				if visibleRows < 1 {
					visibleRows = 1
				}
				if m.selectedIndex > 0 {
					m.selectedIndex--
					if m.selectedIndex < m.listScrollOffset && m.listScrollOffset > 0 {
						m.listScrollOffset--
					}
				}
			}
			return m, nil
		case "down", "ctrl+n":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab != "main" {
				if m.detailState.itemDetailMode {
					m.detailState.markdownScrollOffset++
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = min(maxItems-1, m.detailState.selectedItem+1)
					}
				}
			} else if m.viewMode == "list" {
				visibleData := m.getVisibleData()
				maxIdx := -1
				if len(visibleData) > 0 {
					maxIdx = len(visibleData) - 1
				} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
					maxIdx = len(*m.logDataOld) - 1
				}

				// 计算可见行数
				availableRows := DetailDefaultRows
				if m.height > 10 {
					availableRows = m.height - DetailMinRows
				}
				visibleRows := availableRows - 2 // 减去提示行
				if visibleRows < 1 {
					visibleRows = 1
				}

				if maxIdx >= 0 && m.selectedIndex < maxIdx {
					m.selectedIndex++
					// 如果选中项超出可见范围，调整滚动偏移
					if m.selectedIndex >= m.listScrollOffset+visibleRows && m.listScrollOffset < maxIdx-visibleRows+1 {
						m.listScrollOffset++
					}
				}
			} else if m.viewMode == "detail" && m.detailState != nil {
				if m.detailState.activeTab == "main" {
					// 宽屏模式下：循环 0-3 (system → tools → messages → choices)
					// 但只在 focusedSection == 3 时才切换到右侧高亮，避免之前"同时"切换的怪异感
					m.detailState.focusedSection = (m.detailState.focusedSection + 1) % 4
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = min(maxItems-1, m.detailState.selectedItem+1)
					}
				}
			}
			return m, nil
		case "j":
			if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab == "main" {
				// detail 主视图：j 切换到下一条日志
				visibleData := m.getVisibleData()
				maxIdx := -1
				if len(visibleData) > 0 {
					maxIdx = len(visibleData) - 1
				} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
					maxIdx = len(*m.logDataOld) - 1
				}
				if maxIdx >= 0 && m.selectedIndex < maxIdx {
					m.selectedIndex++
					m.detailScroll = 0
					m.detailState = nil
					return m, m.loadDetail()
				}
			} else if m.viewMode == "detail" && m.detailState != nil && m.detailState.activeTab != "main" {
				if m.detailState.itemDetailMode {
					m.detailState.markdownScrollOffset++
				} else {
					maxItems := m.getTabItemCount(m.detailState.activeTab)
					if maxItems > 0 {
						m.detailState.selectedItem = min(maxItems-1, m.detailState.selectedItem+1)
					}
				}
			} else if m.viewMode == "list" {
				visibleData := m.getVisibleData()
				maxIdx := -1
				if len(visibleData) > 0 {
					maxIdx = len(visibleData) - 1
				} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
					maxIdx = len(*m.logDataOld) - 1
				}
				availableRows := DetailDefaultRows
				if m.height > 10 {
					availableRows = m.height - DetailMinRows
				}
				visibleRows := availableRows - 2
				if visibleRows < 1 {
					visibleRows = 1
				}
				if maxIdx >= 0 && m.selectedIndex < maxIdx {
					m.selectedIndex++
					if m.selectedIndex >= m.listScrollOffset+visibleRows && m.listScrollOffset < maxIdx-visibleRows+1 {
						m.listScrollOffset++
					}
				}
			}
			return m, nil
		case "pgup", "\x1b[5~":
			if m.viewMode == "detail" {
				m.detailScroll = max(0, m.detailScroll-ScrollStep)
			}
			return m, nil
		case "pgdown", "\x1b[6~":
			if m.viewMode == "detail" {
				m.detailScroll += ScrollStep
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
		// 如果轮询已暂停，跳过刷新但继续定时
		if m.pollingPaused {
			return m, tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
				return TickMsg(t)
			})
		}
		return m, tea.Batch(
			m.RefreshCmd(),
			tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
				return TickMsg(t)
			}),
		)

	// 暂停轮询
	case PausePollingMsg:
		m.pollingPaused = true
		return m, nil

	// 恢复轮询
	case ResumePollingMsg:
		m.pollingPaused = false
		// 立即触发一次刷新
		return m, m.RefreshCmd()

	case LogsLoadedMsg:
		m.refreshing = false
		m.lastRefreshTime = time.Now()
		if msg.Error != nil {
			m.data = "❌ 获取失败"
			m.loadError = fmt.Sprintf("%v", msg.Error)
			m.logData = nil
			m.logDataOld = nil
			return m, nil
		}
		// 成功后清除错误信息
		m.loadError = ""

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
				m.listScrollOffset = 0
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
			m.listScrollOffset = 0
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
				m.listScrollOffset = 0
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
			m.listScrollOffset = 0
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

	// 刷新中显示提示
	if m.refreshing {
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		var timeStr string
		if !m.lastRefreshTime.IsZero() {
			timeStr = " | 上次: " + m.lastRefreshTime.Format("15:04:05")
		}
		return mutedStyle.Render("🔄 刷新中..." + timeStr)
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
	activeTab            string          // 当前 tab: "main", "system", "tools", "messages", "choices"
	expandedSections     map[string]bool // 展开的区块
	focusedSection       int             // 当前聚焦的区块索引
	selectedItem         int             // 选中的数组项索引（用于列表tab）
	scrollOffset         int             // 滚动偏移量
	markdownViewMode     string          // "raw" 或 "rendered" - markdown 查看模式
	markdownScrollOffset int             // markdown 渲染滚动偏移量
	itemDetailMode       bool            // 是否处于查看某项详情的模式
	currentItemIndex     int             // 当前查看详情的项索引
	selectedStartLine    int             // 选中项在 lines 中的起始行
	selectedEndLine      int             // 选中项在 lines 中的结束行
	focusedBlock         int             // 当前聚焦的 Block 索引 (如 0 表示思考过程，1 表示响应内容)
	blocks               []string        // 详情中当前可聚焦的所有 Block 列表 (如 ["thinking", "content"])
	blockCollapsed       map[string]bool // 记录各个 Block 的展开/折叠状态
	copiedNotification   string          // 复制成功时的临时通知提示文本
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
			selectedStartLine:    -1,
			selectedEndLine:      -1,
			focusedBlock:         0,
			blocks:               []string{},
			blockCollapsed:       make(map[string]bool),
			copiedNotification:   "",
		}
	} else {
		// 每次渲染前，先重置选中项的物理行范围
		m.detailState.selectedStartLine = -1
		m.detailState.selectedEndLine = -1
		if m.detailState.blockCollapsed == nil {
			m.detailState.blockCollapsed = make(map[string]bool)
		}
		if m.detailState.blocks == nil {
			m.detailState.blocks = []string{}
		}
	}

	// 获取数据
	proxyReq, _ := m.detailData["proxy_server_request"].(map[string]interface{})
	respData, _ := m.detailData["response"].(map[string]interface{})

	// 在渲染 Header 之前，预先收集单项详情中的可聚焦 Block 列表，解决头部与主体渲染的时序依赖 Bug
	if m.detailState != nil && m.detailState.itemDetailMode && m.detailState.activeTab != "main" {
		var availableBlocks []string
		itemData := m.getArrayItem(m.detailState.activeTab, m.detailState.currentItemIndex)
		if itemData != nil {
			var rawText string
			if m.detailState.activeTab == "choices" {
				if choiceMap, ok := itemData.(map[string]interface{}); ok {
					msg, ok := choiceMap["message"].(map[string]interface{})
					if !ok {
						msg, _ = choiceMap["delta"].(map[string]interface{})
					}
					if msg != nil {
						rawText = extractMessageContentFull(msg)
					} else if text, ok := choiceMap["text"].(string); ok {
						rawText = text
					}
				}
			} else if m.detailState.activeTab == "messages" {
				if msgMap, ok := itemData.(map[string]interface{}); ok {
					rawText = extractMessageContentFull(msgMap)
				}
			}
			if rawText != "" {
				thinking, cleanText := extractThinking(rawText)
				if thinking != "" {
					availableBlocks = append(availableBlocks, "thinking")
				}
				if cleanText != "" {
					availableBlocks = append(availableBlocks, "content")
				}
			}
		}
		m.detailState.blocks = availableBlocks
	}

	var lines []string

	// 渲染头部 - 面包屑导航从 Tab 开始
	var header *components.Header
	if m.detailState.activeTab == "main" {
		header = components.NewHeader("日志 > 日志详情", "") // help 在 footer 显示
	} else if m.detailState.itemDetailMode {
		tabName := map[string]string{
			"system":   "System",
			"tools":    "Tool",
			"messages": "Message",
			"choices":  "Choice",
		}[m.detailState.activeTab]
		header = components.NewHeader(fmt.Sprintf("日志 > 日志详情 > %s[%d]", tabName, m.detailState.currentItemIndex), "")
	} else {
		tabTitle := map[string]string{
			"system":   "System Messages",
			"tools":    "Tools",
			"messages": "Messages",
			"choices":  "Choices",
		}[m.detailState.activeTab]
		header = components.NewHeader(fmt.Sprintf("日志 > 日志详情 > %s", tabTitle), "")
	}
	lines = append(lines, header.View(m.width))

	// 顶部元信息行
	if m.selectedEntry != nil {
		metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

		// 第一行: 模型 + 时间
		var row1 []string
		row1 = append(row1, iconStyle.Render("📦")+metaStyle.Render(" "+m.selectedEntry.Model))
		row1 = append(row1, iconStyle.Render("⏱️")+metaStyle.Render(" "+m.selectedEntry.StartTime))
		lines = append(lines, lipgloss.JoinHorizontal(0, row1...))

		// 第二行: tokens 详情 + 花费 + Request ID
		var row2 []string
		if m.selectedEntry.TotalTokens > 0 {
			row2 = append(row2, iconStyle.Render("📊")+metaStyle.Render(fmt.Sprintf(" %d (📝%d + ✍️%d)", m.selectedEntry.TotalTokens, m.selectedEntry.PromptTokens, m.selectedEntry.CompletionTokens)))
		}
		if m.selectedEntry.TotalSpend > 0 {
			row2 = append(row2, iconStyle.Render("💰")+metaStyle.Render(fmt.Sprintf(" $%.4f", m.selectedEntry.TotalSpend)))
		}
		row2 = append(row2, iconStyle.Render("🔖")+metaStyle.Render(" "+m.selectedEntry.ID))
		if len(row2) > 0 {
			lines = append(lines, lipgloss.JoinHorizontal(0, row2...))
		}
	}
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

	// 渲染临时复制成功通知
	if m.detailState != nil && m.detailState.copiedNotification != "" {
		notifStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76")).Bold(true)
		lines = append(lines, notifStyle.Render("  ✨ "+m.detailState.copiedNotification))
		lines = append(lines, "")
	}

	var help *components.Help
	if m.detailState.itemDetailMode {
		var keys []components.HelpKey
		keys = append(keys, components.HelpKey{Key: "↑↓", Desc: "滚动"})
		if len(m.detailState.blocks) > 0 {
			keys = append(keys, components.HelpKey{Key: "Tab", Desc: "切换聚焦块"})
			keys = append(keys, components.HelpKey{Key: "Enter", Desc: "展开/折叠"})
			keys = append(keys, components.HelpKey{Key: "C", Desc: "复制聚焦块"})
		}
		keys = append(keys, components.HelpKey{Key: "ESC", Desc: "返回"}, components.HelpKey{Key: "←/→", Desc: "切换 tab"}, components.HelpKey{Key: "Q", Desc: "退出"})
		help = components.NewHelp(keys)
	} else {
		help = components.NewHelp([]components.HelpKey{
			{Key: "↑↓", Desc: "切换"},
			{Key: "Enter", Desc: "查看详情"},
			{Key: "ESC", Desc: "返回"},
			{Key: "←/→", Desc: "切换 tab"},
			{Key: "Q", Desc: "退出"},
		})
	}
	if m.showFooter {
		lines = append(lines, help.View(m.width))
	}

	// 统一滚动处理：任何 tab 在单项展开详情模式下都按物理行处理滚动
	if m.detailState.itemDetailMode {
		// 计算可见区域：总高度 - 头部(1行) - 空行 - 底部提示(2行)
		availableLines := m.height - 4
		return m.applyMarkdownScrollUnified(lines, availableLines)
	} else {
		// 普通模式：固定 header（前 2 行），只对内容部分做智能焦点追踪滚动
		// lines[0] = header, lines[1] = 空行, lines[2:] = 内容
		const headerLines = 2
		contentLines := lines[headerLines:]
		totalContent := len(contentLines)

		// 可显示内容行数 = 总高度 - header 固定行 - 底部 help/指示器占用
		maxDisplayLines := m.height - headerLines - 1
		if maxDisplayLines < 8 {
			maxDisplayLines = 8
		}

		scrollOffset := m.detailState.scrollOffset

		// 根据选中项在内容区中的物理行坐标，自适应平滑滚动
		// selectedStartLine / selectedEndLine 是相对于 lines（含 header）的索引，
		// 需换算为相对于 contentLines 的索引
		startLine := m.detailState.selectedStartLine - headerLines
		endLine := m.detailState.selectedEndLine - headerLines

		if startLine >= 0 && endLine >= 0 {
			// 如果当前项的结束行超出了可视范围，向下滚动以完全包含它
			if endLine > scrollOffset+maxDisplayLines {
				scrollOffset = endLine - maxDisplayLines
			}
			// 如果当前项的起始行在可视范围上方，向上滚动以完全包含它
			if startLine < scrollOffset {
				scrollOffset = startLine
			}
		}

		// 边界纠正：确保 scrollOffset 不会导致内容向上滚动超出范围
		// 如果内容总行数小于可显示行数，scrollOffset 应该为 0
		if totalContent <= maxDisplayLines {
			scrollOffset = 0
		} else if scrollOffset > totalContent-maxDisplayLines {
			scrollOffset = totalContent - maxDisplayLines
		}
		if scrollOffset < 0 {
			scrollOffset = 0
		}
		m.detailState.scrollOffset = scrollOffset

		endLineIdx := scrollOffset + maxDisplayLines
		if endLineIdx > totalContent {
			endLineIdx = totalContent
		}
		visibleContent := contentLines[scrollOffset:endLineIdx]

		// 构建最终输出：header 固定在顶部，不参与滚动
		var sb strings.Builder
		sb.WriteString(lines[0]) // header
		sb.WriteString("\n")
		sb.WriteString(lines[1]) // 空行
		for _, line := range visibleContent {
			sb.WriteString("\n")
			sb.WriteString(line)
		}

		// 添加滚动指示器
		if scrollOffset > 0 || endLineIdx < totalContent {
			sb.WriteString("\n")
			sb.WriteString(mutedStyle.Render(fmt.Sprintf(" ──◀ %d/%d ▶─ ", scrollOffset+1, totalContent)))
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

	// 动态滚动限位算法：当尾部已完全可见时，锁定 scrollOffset 不再增加，彻底杜绝尾部留白
	maxScroll := contentLineCount - contentAvailable
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}
	if scrollOffset < 0 {
		scrollOffset = 0
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

func (m *Model) getSingleChoicePreview(singleChoice map[string]interface{}, mutedStyle, valueStyle lipgloss.Style, maxAvailLines int, maxWidth int) []string {
	var lines []string
	if singleChoice == nil {
		return lines
	}

	// 思考内容使用稍暗的颜色，与正文产生视觉层次
	thinkingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	// 预算：从可用行数中扣除固定行（finish_reason、标题等），剩余按比例分配思考/内容
	budget := maxAvailLines
	if budget < 4 {
		budget = 4
	}
	// 使用传入的 maxWidth 计算实际渲染行数，而不是全屏宽度
	availWidth := maxWidth - 10
	if availWidth < 30 {
		availWidth = 30
	}

	// 1. 尝试获取 finish_reason
	if fr, ok := singleChoice["finish_reason"].(string); ok && fr != "" {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("  🏁 结束原因: %s", fr)))
		budget--
	}

	// 2. 获取 message (同时兼容 delta 字段以完美支持流式输出预览)
	msg, _ := singleChoice["message"].(map[string]interface{})
	if msg == nil {
		msg, _ = singleChoice["delta"].(map[string]interface{})
	}

	if msg == nil {
		if text, ok := singleChoice["text"].(string); ok && text != "" {
			text = strings.TrimSpace(text)
			rendered := m.renderMarkdownWithWidth(text, m.width-10)
			lines = append(lines, "  💬 内容:")
			budget-- // 标题行
			maxContent := budget
			if maxContent < 1 {
				maxContent = 1
			}
			if len(rendered) <= maxContent {
				for _, rl := range rendered {
					lines = append(lines, "    "+rl)
				}
			} else {
				for i := 0; i < maxContent; i++ {
					lines = append(lines, "    "+rendered[i])
				}
				lines = append(lines, mutedStyle.Render("    ... [长文本已折叠，按 Enter/Tab 切换 choices 查看全文]"))
			}
		}
		return lines
	}

	// 3. 提取 content 并智能分流渲染思考链与正文预览
	content := extractMessagePreview(msg)
	if content != "" {
		content = strings.TrimSpace(content)
		thinking, cleanText := extractThinking(content)

		if thinking != "" {
			thinking = strings.TrimSpace(thinking)
			renderedThinking := m.renderMarkdownWithWidth(thinking, availWidth)
			thinkingCount := len(renderedThinking)

			lines = append(lines, mutedStyle.Render("  🧠 思考过程:"))
			// 预留一行给标题，剩余空间自适应分配
			budget--

			// 自适应分配：如果思考内容很少，全部显示；否则限制行数
			maxDisplay := budget / 3 // 思考最多占1/3预算，保留空间给内容
			if maxDisplay < 1 {
				maxDisplay = 1
			}
			if thinkingCount <= maxDisplay {
				// 内容足够少，全部显示
				for _, rl := range renderedThinking {
					lines = append(lines, thinkingStyle.Render("    "+rl))
				}
			} else {
				// 内容太多，限制显示并提示
				for i := 0; i < maxDisplay; i++ {
					lines = append(lines, thinkingStyle.Render("    "+renderedThinking[i]))
				}
				lines = append(lines, mutedStyle.Render("    ... [思考过程较长已折叠，按 Enter/Tab 切换 choices 查看完整思维链] ..."))
			}
			// 更新 budget 为剩余可用行数（用于后续内容）
			budget = budget - maxDisplay
			if budget < 0 {
				budget = 0
			}
		}

		if cleanText != "" {
			rendered := m.renderMarkdownWithWidth(cleanText, availWidth)
			lines = append(lines, "  💬 内容:")
			budget-- // 标题行
			maxContent := budget
			if maxContent < 1 {
				maxContent = 1
			}
			if len(rendered) <= maxContent {
				for _, rl := range rendered {
					lines = append(lines, "    "+rl)
				}
			} else {
				for i := 0; i < maxContent; i++ {
					lines = append(lines, "    "+rendered[i])
				}
				lines = append(lines, mutedStyle.Render("    ... [长文本已折叠，按 Enter/Tab 切换 choices 查看全文]"))
			}
		}
	}

	// 4. 提取 tool_calls 并格式化为对人类极度友好的条目预览
	toolCalls, _ := msg["tool_calls"].([]interface{})
	if len(toolCalls) > 0 {
		lines = append(lines, "  🔧 工具调用:")
		budget-- // 标题行
		maxToolCalls := budget
		if maxToolCalls < 1 {
			maxToolCalls = 1
		}
		toolCallIdx := 0
		for _, tc := range toolCalls {
			toolCallIdx++
			if toolCallIdx > maxToolCalls {
				lines = append(lines, mutedStyle.Render(fmt.Sprintf("    ... [+%d 个工具调用已折叠]", len(toolCalls)-maxToolCalls)))
				break
			}
			if tcMap, ok := tc.(map[string]interface{}); ok {
				var name string
				var argumentsStr string
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					name, _ = fn["name"].(string)
					argumentsStr, _ = fn["arguments"].(string)
				}
				if name == "" {
					name, _ = tcMap["name"].(string)
				}

				// 精美解析 arguments key-value
				previewArgs := ""
				if argumentsStr != "" {
					var argsMap map[string]interface{}
					if err := json.Unmarshal([]byte(argumentsStr), &argsMap); err == nil {
						var keys []string
						for k := range argsMap {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						var parts []string
						for _, k := range keys {
							valStr := fmt.Sprintf("%v", argsMap[k])
							if len(valStr) > 15 {
								valStr = valStr[:12] + "..."
							}
							parts = append(parts, fmt.Sprintf("%s=%s", k, valStr))
						}
						previewArgs = strings.Join(parts, ", ")
					} else {
						previewArgs = truncate(argumentsStr, 30)
					}
				}

				lines = append(lines, valueStyle.Render(fmt.Sprintf("    - %s(%s)", name, previewArgs)))
			}
		}
	}

	return lines
}

// renderLastMessagePreview 渲染 request 最后一条消息的预览，限制高度和宽度防止撑破卡片。
// availWidth 为可用列宽（单位：字符），用于控制内部 word-wrap；传 0 则使用全屏宽度。
// availRows 为可用行数，用于动态计算折叠阈值充分利用屏幕空间；传 0 则使用默认值。
func (m *Model) renderLastMessagePreview(proxyReq map[string]interface{}, mutedStyle lipgloss.Style, availWidth int, availRows int) []string {

	var lines []string
	if proxyReq == nil {
		return lines
	}
	messages, ok := proxyReq["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return lines
	}
	lastMsg, _ := messages[len(messages)-1].(map[string]interface{})
	if lastMsg == nil {
		return lines
	}
	role, _ := lastMsg["role"].(string)

	// 智能检测 content 类型
	contentType := detectMessageContentType(lastMsg)

	// 根据 content 类型选择合适的渲染方式
	// 动态计算最大显示行数：可用行数减去固定开销（标题行、空行、提示行）
	maxMsgLines := 3
	if availRows > 6 {
		maxMsgLines = availRows - 2 // 只预留提示行和边距
		if maxMsgLines < 3 {
			maxMsgLines = 3
		}
	}
	var flatLines []string
	wrapWidth := availWidth - 6
	if wrapWidth < 30 {
		wrapWidth = 30
	}
	if wrapWidth > 120 {
		wrapWidth = 120
	}

	switch contentType {
	case "tool_result", "json":
		// tool_result 或 JSON 内容不应该用 markdown 渲染，直接展示原始文本
		content := extractMessageContentFull(lastMsg)
		content = strings.TrimSpace(content)
		rawLines := strings.Split(content, "\n")
		for _, l := range rawLines {
			l = strings.TrimRight(l, " \t")
			if len([]rune(l)) > wrapWidth {
				l = string([]rune(l)[:wrapWidth-1]) + "…"
			}
			if l != "" {
				flatLines = append(flatLines, l)
			}
		}
	case "tool_use_with_text":
		// 同时存在 text 和 tool_use 的混合内容
		content := extractMessageContentFull(lastMsg)
		content = strings.TrimSpace(content)
		rendered := m.renderMarkdownWithWidth(content, wrapWidth)
		for _, r := range rendered {
			for _, l := range strings.Split(r, "\n") {
				flatLines = append(flatLines, l)
			}
		}
	default:
		// 普通文本内容，使用 markdown 渲染
		content := extractMessageContentFull(lastMsg)
		content = strings.TrimSpace(content)
		rendered := m.renderMarkdownWithWidth(content, wrapWidth)
		for _, r := range rendered {
			for _, l := range strings.Split(r, "\n") {
				flatLines = append(flatLines, l)
			}
		}
	}

	if len(flatLines) == 0 {
		return lines
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("  💬 最后消息 (%s):", role)))

	if len(flatLines) <= maxMsgLines {
		for _, rl := range flatLines {
			lines = append(lines, "    "+rl)
		}
	} else {
		for i := 0; i < maxMsgLines; i++ {
			lines = append(lines, "    "+flatLines[i])
		}
		lines = append(lines, mutedStyle.Render("    ... [长消息已折叠，按 Enter/Tab 切换 messages 查看完整对话] ..."))
	}
	return lines
}

// detectMessageContentType 检测消息 content 的类型
// 返回值：text（普通文本）、tool_result（工具结果）、tool_use_with_text（混合内容）、json（纯 JSON）
func detectMessageContentType(msg map[string]interface{}) string {
	contentRaw := msg["content"]
	if contentRaw == nil {
		return "text"
	}

	// content 是字符串
	if s, ok := contentRaw.(string); ok {
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			// 尝试解析为 JSON
			var jsonDoc interface{}
			if err := json.Unmarshal([]byte(s), &jsonDoc); err == nil {
				return "json"
			}
		}
		return "text"
	}

	// content 是数组
	if list, ok := contentRaw.([]interface{}); ok {
		hasToolResult := false
		hasToolUse := false
		hasText := false

		for _, item := range list {
			if itemMap, ok := item.(map[string]interface{}); ok {
				itemType, _ := itemMap["type"].(string)
				switch itemType {
				case "tool_result":
					hasToolResult = true
				case "tool_use", "tool_call":
					hasToolUse = true
				case "text":
					hasText = true
				default:
					// 检查是否有 text 字段
					if _, ok := itemMap["text"].(string); ok {
						hasText = true
					}
				}
			}
		}

		if hasToolResult {
			return "tool_result"
		}
		if hasToolUse && hasText {
			return "tool_use_with_text"
		}
		return "text"
	}

	// content 是单个对象
	if cMap, ok := contentRaw.(map[string]interface{}); ok {
		itemType, _ := cMap["type"].(string)
		if itemType == "tool_result" {
			return "tool_result"
		}
		// 检查是否是纯 JSON
		if _, hasText := cMap["text"].(string); !hasText {
			// 没有 text 字段，可能是纯 JSON
			if _, err := json.MarshalIndent(cMap, "", "  "); err == nil {
				return "json"
			}
		}
	}

	return "text"
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
			key   string
			icon  string
			label string
			count int
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

		// 宽屏时左列约占一半宽度，传入实际可用列宽以正确控制 word-wrap
		leftColWidth := m.width/2 - 4
		leftCol = append(leftCol, m.renderLastMessagePreview(proxyReq, mutedStyle, leftColWidth, (m.height-8)/2-4)...)

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

		// Choices 只有一个时的上提与工具参数智能预览
		var singleChoice map[string]interface{}
		if respData != nil {
			if choices, ok := respData["choices"].([]interface{}); ok && len(choices) == 1 {
				singleChoice, _ = choices[0].(map[string]interface{})
			}
		}
		if singleChoice != nil {
			// 宽屏时 response 占右半列，减去 header/border/usage/spend 等固定行后的可用行数
			availForPreview := (m.height - 8) - 4
			if availForPreview < 4 {
				availForPreview = 4
			}
			// 宽屏时右列约占一半宽度
			rightColWidth := m.width/2 - 2
			previewLines := m.getSingleChoicePreview(singleChoice, mutedStyle, valueStyle, availForPreview, rightColWidth)
			rightCol = append(rightCol, previewLines...)
		}

		// tokens 和花费已移至顶部元信息显示

		leftWidth := m.width / 2
		rightWidth := m.width - leftWidth - 1

		leftContent := strings.Join(leftCol, "\n")
		rightContent := strings.Join(rightCol, "\n")

		// 宽屏模式下：focusedSection == 3 时右侧 choices 高亮，其他时候左侧高亮
		// 这样 system → tools → messages 切换时不会"同时"切换 panel 焦点
		var leftCardStyle, rightCardStyle lipgloss.Style
		if m.detailState.focusedSection == 3 {
			leftCardStyle = cardStyle
			rightCardStyle = focusedCardStyle
		} else {
			leftCardStyle = focusedCardStyle
			rightCardStyle = cardStyle
		}

		leftCard := leftCardStyle.Width(leftWidth - 2).Render(leftContent)
		rightCard := rightCardStyle.Width(rightWidth - 2).Render(rightContent)

		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, leftCard, rightCard))
	} else {
		requestLines := []string{groupStyle.Render("📤 REQUEST")}
		requestLines = append(requestLines, fmt.Sprintf("  🤖 %s", valueStyle.Render(modelName)))
		requestLines = append(requestLines, "")

		requestOptions := []struct {
			key   string
			icon  string
			label string
			count int
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

		// 窄屏时单列，传入全屏可用宽度
		requestLines = append(requestLines, m.renderLastMessagePreview(proxyReq, mutedStyle, m.width-4, (m.height/2-8)-4)...)

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

		// Choices 只有一个时的上提与工具参数智能预览
		var singleChoice map[string]interface{}
		if respData != nil {
			if choices, ok := respData["choices"].([]interface{}); ok && len(choices) == 1 {
				singleChoice, _ = choices[0].(map[string]interface{})
			}
		}
		if singleChoice != nil {
			// 窄屏时两个卡片纵向排列，response 卡片约占下半屏，减去固定行后的可用行数
			availForPreview := (m.height/2 - 8) - 4 // 更保守的预算
			if availForPreview < 4 {
				availForPreview = 4
			}
			previewLines := m.getSingleChoicePreview(singleChoice, mutedStyle, valueStyle, availForPreview, m.width)
			responseLines = append(responseLines, previewLines...)
		}

		// tokens 和花费已移至顶部元信息显示

		cardWidth := m.width - 4

		lines = append(lines, cardStyle.Width(cardWidth).Render(strings.Join(requestLines, "\n")))
		lines = append(lines, cardStyle.Width(cardWidth).Render(strings.Join(responseLines, "\n")))
	}

	return lines
}

func (m *Model) renderArrayDetailView(proxyReq, respData map[string]interface{}, cardStyle, focusedCardStyle, contentStyle, mutedStyle, groupStyle, valueStyle, keyStyle lipgloss.Style) []string {
	var lines []string

	tab := m.detailState.activeTab
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
							m.detailState.selectedStartLine = len(lines) + 2
							itemLines := m.renderSystemSummary(system[i], i, contentStyle, mutedStyle, true)
							lines = append(lines, itemLines...)
							m.detailState.selectedEndLine = len(lines) + 2
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

				if m.detailState.itemDetailMode {
					idx := m.detailState.currentItemIndex
					if idx >= 0 && idx < len(messages) {
						lines = append(lines, m.renderMessageItem(messages[idx], idx, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					}
				} else {
					for i := 0; i < len(messages); i++ {
						if i == selectedIdx {
							m.detailState.selectedStartLine = len(lines) + 2
							itemLines := m.renderMessageSummary(messages[i], i, contentStyle, mutedStyle, true)
							lines = append(lines, itemLines...)
							m.detailState.selectedEndLine = len(lines) + 2
						} else {
							lines = append(lines, m.renderMessageSummary(messages[i], i, contentStyle, mutedStyle, false)...)
						}
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

				if m.detailState.itemDetailMode {
					idx := m.detailState.currentItemIndex
					if idx >= 0 && idx < len(tools) {
						lines = append(lines, m.renderToolItem(tools[idx], idx, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					}
				} else {
					for i := 0; i < len(tools); i++ {
						if i == selectedIdx {
							m.detailState.selectedStartLine = len(lines) + 2
							itemLines := m.renderToolSummary(tools[i], i, contentStyle, mutedStyle, true)
							lines = append(lines, itemLines...)
							m.detailState.selectedEndLine = len(lines) + 2
						} else {
							lines = append(lines, m.renderToolSummary(tools[i], i, contentStyle, mutedStyle, false)...)
						}
					}
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

				if m.detailState.itemDetailMode {
					idx := m.detailState.currentItemIndex
					if idx >= 0 && idx < len(choices) {
						lines = append(lines, m.renderChoiceItem(choices[idx], idx, contentStyle, mutedStyle, groupStyle, valueStyle)...)
					}
				} else {
					for i := 0; i < len(choices); i++ {
						if i == selectedIdx {
							m.detailState.selectedStartLine = len(lines) + 2
							itemLines := m.renderChoiceSummary(choices[i], i, contentStyle, mutedStyle, true)
							lines = append(lines, itemLines...)
							m.detailState.selectedEndLine = len(lines) + 2
						} else {
							lines = append(lines, m.renderChoiceSummary(choices[i], i, contentStyle, mutedStyle, false)...)
						}
					}
				}
			}
		}
	}

	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("无数据"))
	}

	return lines
}

func extractMessagePreview(msg interface{}) string {
	if msg == nil {
		return ""
	}
	if s, ok := msg.(string); ok {
		return s
	}
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(msg); err == nil {
			return string(jsonBytes)
		}
		return ""
	}

	contentRaw := msgMap["content"]
	if contentRaw != nil {
		if s, ok := contentRaw.(string); ok && s != "" {
			return s
		}
		if list, ok := contentRaw.([]interface{}); ok {
			var parts []string
			for _, item := range list {
				if itemMap, ok := item.(map[string]interface{}); ok {
					itemType, _ := itemMap["type"].(string)
					if text, ok := itemMap["text"].(string); ok && text != "" {
						parts = append(parts, text)
					} else if itemType == "tool_use" {
						name, _ := itemMap["name"].(string)
						parts = append(parts, fmt.Sprintf("[Tool Call: %s]", name))
					} else if itemType == "image" {
						parts = append(parts, "[Image]")
					}
				} else if s, ok := item.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, " ")
			}
		}
		if cMap, ok := contentRaw.(map[string]interface{}); ok {
			if text, ok := cMap["text"].(string); ok {
				return text
			}
		}
	}

	if toolCallsRaw, ok := msgMap["tool_calls"].([]interface{}); ok && len(toolCallsRaw) > 0 {
		var toolNames []string
		for _, tc := range toolCallsRaw {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok {
						toolNames = append(toolNames, name)
					}
				} else if name, ok := tcMap["name"].(string); ok {
					toolNames = append(toolNames, name)
				}
			}
		}
		if len(toolNames) > 0 {
			return fmt.Sprintf("[Tool Calls: %s]", strings.Join(toolNames, ", "))
		}
	}

	if text, ok := msgMap["text"].(string); ok && text != "" {
		return text
	}

	if fc, ok := msgMap["function_call"].(map[string]interface{}); ok {
		if name, ok := fc["name"].(string); ok {
			return fmt.Sprintf("[Function Call: %s]", name)
		}
	}

	return ""
}

// extractMessageContentFull 统一且健壮地从 message 或 delta 结构体中提取出完整的 markdown 文本
func extractMessageContentFull(msg interface{}) string {
	if msg == nil {
		return ""
	}
	if s, ok := msg.(string); ok {
		return s
	}
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(msg); err == nil {
			return string(jsonBytes)
		}
		return ""
	}

	contentRaw := msgMap["content"]
	if contentRaw == nil {
		if text, ok := msgMap["text"].(string); ok && text != "" {
			return text
		}
		return ""
	}

	// 1. 如果 content 是普通的 string
	if s, ok := contentRaw.(string); ok {
		return s
	}

	// 2. 如果 content 是对象数组 ([]interface{})
	if list, ok := contentRaw.([]interface{}); ok {
		var parts []string
		// 分别收集不同类型的 block
		var textBlocks []string
		var toolUseBlocks []string
		var toolResultBlocks []string
		var imageBlocks []string
		var otherBlocks []string

		for _, item := range list {
			if itemMap, ok := item.(map[string]interface{}); ok {
				itemType, _ := itemMap["type"].(string)
				if text, ok := itemMap["text"].(string); ok && text != "" {
					// text 类型且内容非空
					textBlocks = append(textBlocks, text)
				} else if itemType == "text" && text == "" {
					// text 类型但内容为空，跳过（不渲染）
					continue
				} else if itemType == "tool_use" || itemType == "tool_call" {
					toolName, _ := itemMap["name"].(string)
					toolID, _ := itemMap["id"].(string)
					var inputStr string
					if input, ok := itemMap["input"].(map[string]interface{}); ok {
						if bytes, err := json.Marshal(input); err == nil {
							inputStr = string(bytes)
						}
					} else if args, ok := itemMap["arguments"].(string); ok {
						inputStr = args
					}
					toolUseBlocks = append(toolUseBlocks, fmt.Sprintf("🔧 **[Tool Use: %s (ID: %s)]**\n`input: %s`", toolName, toolID, inputStr))
				} else if itemType == "tool_result" {
					var resultContent string
					if content, ok := itemMap["content"].(string); ok {
						resultContent = content
					} else if content, ok := itemMap["content"].(map[string]interface{}); ok {
						if bytes, err := json.MarshalIndent(content, "", "  "); err == nil {
							resultContent = string(bytes)
						} else {
							resultContent = fmt.Sprintf("%v", content)
						}
					}
					toolUseID, _ := itemMap["tool_use_id"].(string)
					if toolUseID != "" {
						toolResultBlocks = append(toolResultBlocks, fmt.Sprintf("📥 **[Tool Result: %s]**\n```\n%s\n```", toolUseID, resultContent))
					} else {
						toolResultBlocks = append(toolResultBlocks, fmt.Sprintf("📥 **[Tool Result]**\n```\n%s\n```", resultContent))
					}
				} else if itemType == "image" {
					imageBlocks = append(imageBlocks, "🖼️ **[Image Block]**")
				} else {
					if bytes, err := json.Marshal(itemMap); err == nil {
						otherBlocks = append(otherBlocks, fmt.Sprintf("```json\n%s\n```", string(bytes)))
					}
				}
			} else if s, ok := item.(string); ok {
				textBlocks = append(textBlocks, s)
			}
		}

		// 按顺序组装：text → tool_use → tool_result → image → other
		for _, t := range textBlocks {
			parts = append(parts, t)
		}
		for _, t := range toolUseBlocks {
			parts = append(parts, "\n\n"+t)
		}
		for _, t := range toolResultBlocks {
			parts = append(parts, "\n\n"+t)
		}
		for _, t := range imageBlocks {
			parts = append(parts, "\n\n"+t)
		}
		for _, t := range otherBlocks {
			parts = append(parts, "\n\n"+t)
		}

		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n\n")
	}

	// 3. 如果 content 是单对象 map
	if cMap, ok := contentRaw.(map[string]interface{}); ok {
		if text, ok := cMap["text"].(string); ok && text != "" {
			return text
		}
		if bytes, err := json.MarshalIndent(cMap, "", "  "); err == nil {
			return fmt.Sprintf("```json\n%s\n```", string(bytes))
		}
	}

	// 4. 兜底处理
	if bytes, err := json.MarshalIndent(contentRaw, "", "  "); err == nil {
		return fmt.Sprintf("```json\n%s\n```", string(bytes))
	}
	return ""
}

func extractThinking(text string) (thinking string, cleanText string) {
	// 提取所有嵌套的思考内容
	var thinkingParts []string
	clean := text

	// 循环提取所有嵌套的<think>...</think>
	for {
		startIdx := strings.Index(clean, "<think>")
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(clean, "</think>")
		if endIdx == -1 || endIdx < startIdx+7 {
			// 没有闭合标签或顺序异常，剩余都是思考
			thinkingParts = append(thinkingParts, clean[startIdx+7:])
			clean = clean[:startIdx]
			break
		}
		thinkingParts = append(thinkingParts, clean[startIdx+7:endIdx])
		clean = clean[:startIdx] + clean[endIdx+8:]
	}

	if len(thinkingParts) > 0 {
		thinking = strings.TrimSpace(strings.Join(thinkingParts, "\n\n"))
		cleanText = strings.TrimSpace(clean)
		return thinking, cleanText
	}

	return "", text
}

func (m *Model) renderMessageSummary(msg interface{}, idx int, contentStyle, mutedStyle lipgloss.Style, focused bool) []string {
	msgMap, ok := msg.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(msg); err == nil {
			return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 50)))}
		}
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, msg))}
	}

	role, _ := msgMap["role"].(string)
	previewContent := extractMessagePreview(msg)

	// 检测 content 类型
	contentType := detectMessageContentType(msgMap)

	roleIcon := map[string]string{
		"system":    "📦",
		"user":      "👤",
		"assistant": "🤖",
		"tool":      "🔧",
	}[role]
	if roleIcon == "" {
		roleIcon = "💬"
	}

	// 检测是否是"伪装的" tool_result（role=user 但 content 是 tool_result）
	isHiddenToolResult := role == "user" && contentType == "tool_result"

	prefix := "  "
	style := mutedStyle
	if focused {
		prefix = "▶ "
		style = contentStyle.Bold(true)
	}

	previewLen := m.width - 25
	if previewLen < 20 {
		previewLen = 20
	}

	previewContent = strings.ReplaceAll(previewContent, "\n", " ")
	previewContent = strings.ReplaceAll(previewContent, "\r", "")

	// 角色高亮特异化展示
	roleColors := map[string]string{
		"system":    "208", // 橙色
		"user":      "76",  // 绿色
		"assistant": "135", // 紫色
		"tool":      "75",  // 蓝色
	}
	colorCode := roleColors[role]
	if colorCode == "" {
		colorCode = "86"
	}
	roleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorCode))
	if focused {
		roleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	}
	rolePart := roleStyle.Render(role)

	summary := roleIcon + " " + rolePart
	if previewContent != "" {
		summary += ": " + style.Render(truncate(previewContent, previewLen))
	}

	// 如果是伪装的 tool_result，在右侧添加标识，与 [Tool Call: xxx] 样式保持一致（灰色）
	if isHiddenToolResult {
		toolResultTag := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")). // 灰色，与 [Tool Call: xxx] 一致
			Render("[tool_result]")
		summary = truncate(summary, m.width-30) + " " + toolResultTag
	}

	return []string{fmt.Sprintf("%s[%d] %s", style.Render(prefix), idx, summary)}
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
	var lines []string
	lines = append(lines, groupStyle.Render(fmt.Sprintf("  [%d] %s", idx, role)))

	// 使用统一且健壮的提取逻辑，支持多模态 blocks、单对象 map 等
	markdownText := extractMessageContentFull(msgMap)

	// 动态收集当前 Message 中的可聚焦块
	var availableBlocks []string
	var thinking, cleanText string
	if markdownText != "" {
		thinking, cleanText = extractThinking(markdownText)
		if thinking != "" {
			availableBlocks = append(availableBlocks, "thinking")
		}
		if cleanText != "" {
			availableBlocks = append(availableBlocks, "content")
		}
	}
	m.detailState.blocks = availableBlocks
	if m.detailState.focusedBlock >= len(availableBlocks) {
		m.detailState.focusedBlock = 0
	}

	if markdownText != "" {
		// 动态计算半屏折叠阈值
		threshold := m.height / 2
		if threshold < 8 {
			threshold = 8
		}

		// 预先渲染以确定真实的行数
		var renderedThinking, renderedLines []string
		var thinkingLinesCount, contentLinesCount int
		var canThinkingCollapse, canContentCollapse bool

		if thinking != "" {
			renderedThinking = m.renderMarkdownFull(thinking)
			thinkingLinesCount = len(renderedThinking)
			canThinkingCollapse = thinkingLinesCount > threshold
		}

		if cleanText != "" {
			renderedLines = m.renderMarkdownFull(cleanText)
			contentLinesCount = len(renderedLines)
			canContentCollapse = contentLinesCount > threshold
		}

		if thinking != "" {
			isFocused := len(availableBlocks) > 0 && availableBlocks[m.detailState.focusedBlock] == "thinking"
			isCollapsed := false
			if canThinkingCollapse {
				if collapsed, exists := m.detailState.blockCollapsed["thinking"]; exists {
					isCollapsed = collapsed
				} else {
					isCollapsed = true // 默认折叠
				}
			}

			var title string
			if isFocused {
				prefix := "▶ "
				tip := " (按 C 复制)"
				if canThinkingCollapse {
					if isCollapsed {
						tip = " (按 Enter 展开全部，按 C 复制)"
					} else {
						tip = " (按 Enter 折叠内容，按 C 复制)"
					}
				}
				title = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(prefix + "🧠 思考过程" + tip)
			} else {
				prefix := "  "
				status := ""
				if isCollapsed {
					status = " [已折叠]"
				}
				title = mutedStyle.Render(prefix + "🧠 思考过程" + status + ":")
			}
			lines = append(lines, title)

			if isCollapsed {
				// 折叠状态下只展示前 threshold 行
				for _, rl := range renderedThinking[:threshold] {
					lines = append(lines, "    "+rl)
				}
				remaining := thinkingLinesCount - threshold
				lines = append(lines, mutedStyle.Render(fmt.Sprintf("    ... 已折叠多余 %d 行思考过程，按 Enter 展开全部 ...", remaining)))
				lines = append(lines, "")
			} else {
				// 展开状态下展示全文
				for _, rl := range renderedThinking {
					lines = append(lines, "    "+rl)
				}
				lines = append(lines, "")
			}
		}

		if cleanText != "" {
			isFocused := len(availableBlocks) > 0 && availableBlocks[m.detailState.focusedBlock] == "content"
			isCollapsed := false
			if canContentCollapse {
				if collapsed, exists := m.detailState.blockCollapsed["content"]; exists {
					isCollapsed = collapsed
				} else {
					isCollapsed = true // 默认折叠
				}
			}

			var title string
			if isFocused {
				prefix := "▶ "
				tip := " (按 C 复制)"
				if canContentCollapse {
					if isCollapsed {
						tip = " (按 Enter 展开全部，按 C 复制)"
					} else {
						tip = " (按 Enter 折叠内容，按 C 复制)"
					}
				}
				title = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(prefix + "💬 正文内容" + tip)
			} else {
				prefix := "  "
				status := ""
				if isCollapsed {
					status = " [已折叠]"
				}
				title = mutedStyle.Render(prefix + "💬 正文内容" + status + ":")
			}
			lines = append(lines, title)

			if isCollapsed {
				// 折叠状态下只展示前 threshold 行
				for _, rl := range renderedLines[:threshold] {
					lines = append(lines, "    "+rl)
				}
				remaining := contentLinesCount - threshold
				lines = append(lines, mutedStyle.Render(fmt.Sprintf("    ... 已折叠多余 %d 行正文内容，按 Enter 展开全部 ...", remaining)))
			} else {
				// 展开状态下展示全文
				for _, rl := range renderedLines {
					lines = append(lines, "    "+rl)
				}
			}
		}
	}

	toolCalls, _ := msgMap["tool_calls"].([]interface{})
	if len(toolCalls) > 0 {
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render("  🔧 Tool Calls:"))
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				var fnName string
				var args string
				tcID, _ := tcMap["id"].(string)
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					if n, ok := fn["name"].(string); ok {
						fnName = n
					}
					if a, ok := fn["arguments"].(string); ok {
						var parsed interface{}
						if err := json.Unmarshal([]byte(a), &parsed); err == nil {
							if pretty, err := json.MarshalIndent(parsed, "      ", "  "); err == nil {
								args = string(pretty)
							} else {
								args = a
							}
						} else {
							args = a
						}
					}
				}

				if tcID != "" {
					lines = append(lines, valueStyle.Render(fmt.Sprintf("    - Call: %s (ID: %s)", fnName, tcID)))
				} else {
					lines = append(lines, valueStyle.Render(fmt.Sprintf("    - Call: %s", fnName)))
				}

				if args != "" {
					argsMarkdown := "```json\n" + args + "\n```"
					renderedArgs := m.renderMarkdownFull(argsMarkdown)
					for _, rl := range renderedArgs {
						lines = append(lines, "      "+rl)
					}
				}
			}
		}
	}

	return lines
}

func (m *Model) renderToolSummary(tool interface{}, idx int, contentStyle, mutedStyle lipgloss.Style, focused bool) []string {
	toolMap, ok := tool.(map[string]interface{})
	if !ok {
		if jsonBytes, err := json.Marshal(tool); err == nil {
			return []string{mutedStyle.Render(fmt.Sprintf("  [%d] %s", idx, truncate(string(jsonBytes), 50)))}
		}
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据类型: %T", idx, tool))}
	}

	previewLen := m.width - 25
	if previewLen < 20 {
		previewLen = 20
	}

	var name, desc string
	// 1. 尝试从最外层读取
	if n, ok := toolMap["name"].(string); ok {
		name = n
	}
	if d, ok := toolMap["description"].(string); ok {
		desc = d
	}

	// 2. 尝试从嵌套 of function 字段读取并覆盖
	if fn, ok := toolMap["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
		if d, ok := fn["description"].(string); ok {
			desc = d
		}
	}

	// 3. 如果依然没有 name，尝试读取 "type" 字段
	if name == "" {
		if t, ok := toolMap["type"].(string); ok {
			name = "type: " + t
		}
	}

	// 4. 如果最后什么都没读到，直接使用简短 of JSON 作为摘要以防空白
	if name == "" && desc == "" {
		if jsonBytes, err := json.Marshal(toolMap); err == nil {
			name = string(jsonBytes)
		} else {
			name = "未知工具"
		}
	}

	prefix := "  "
	style := mutedStyle
	if focused {
		prefix = "▶ "
		style = contentStyle.Bold(true)
	}

	// 清洗换行符
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", "")
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = strings.ReplaceAll(desc, "\r", "")

	// 特异化展示：聚焦时反显，非聚焦时展现醒目的青色粗体
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	if focused {
		nameStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	}
	namePart := nameStyle.Render(name)

	var summary string
	if desc != "" {
		summary = fmt.Sprintf("🔧 %s: %s", namePart, style.Render(truncate(desc, previewLen)))
	} else {
		summary = fmt.Sprintf("🔧 %s", namePart)
	}

	return []string{fmt.Sprintf("%s[%d] %s", style.Render(prefix), idx, summary)}
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

	previewLen := m.width - 30
	if previewLen < 20 {
		previewLen = 20
	}

	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", "")

	typeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("172")) // 浅棕色
	if focused {
		typeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("86"))
	}
	typePart := typeStyle.Render(sysType)

	summary := fmt.Sprintf("system[%d] (%s)", idx, typePart)
	if text != "" {
		summary += ": " + style.Render(truncate(text, previewLen))
	}

	return []string{fmt.Sprintf("%s%s", style.Render(prefix), summary)}
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
				rendered := m.renderMarkdownWithWidth(text, m.width-10)
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

// renderMarkdownWithWidth 与 renderMarkdownFull 相同，但使用调用方指定的宽度做 word-wrap，
// 适用于需要按列宽（而非全屏宽）渲染的场景（如双列布局的左列预览）。
func (m *Model) renderMarkdownWithWidth(text string, maxWidth int) []string {
	if maxWidth < 30 {
		maxWidth = 30
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

	return processMarkdownWithStyles(text, maxWidth, headingStyle, codeStyle, boldStyle, italicStyle, listStyle, quoteStyle)
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

	var name, desc string
	var schema interface{}

	// 1. 尝试从 function 中提取
	if fn, ok := toolMap["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
		if d, ok := fn["description"].(string); ok {
			desc = d
		}
		if p, ok := fn["parameters"].(interface{}); ok {
			schema = p
		} else if i, ok := fn["input_schema"].(interface{}); ok {
			schema = i
		}
	}

	// 2. 尝试从最外层提取
	if name == "" {
		if n, ok := toolMap["name"].(string); ok {
			name = n
		}
	}
	if desc == "" {
		if d, ok := toolMap["description"].(string); ok {
			desc = d
		}
	}
	if schema == nil {
		if p, ok := toolMap["parameters"].(interface{}); ok {
			schema = p
		} else if i, ok := toolMap["input_schema"].(interface{}); ok {
			schema = i
		}
	}

	if name != "" {
		lines = append(lines, valueStyle.Render("  name: "+name))
	}
	if desc != "" {
		lines = append(lines, "  description:")
		renderedDesc := m.renderMarkdownFull(desc)
		for _, rl := range renderedDesc {
			lines = append(lines, "    "+rl)
		}
	}

	if schema != nil {
		if jsonBytes, err := json.MarshalIndent(schema, "", "  "); err == nil {
			lines = append(lines, mutedStyle.Render("  schema / parameters (input_schema):"))
			schemaMarkdown := "```json\n" + string(jsonBytes) + "\n```"
			renderedSchema := m.renderMarkdownFull(schemaMarkdown)
			for _, rl := range renderedSchema {
				lines = append(lines, "    "+rl)
			}
		}
	}

	// 兜底：如果还是空白
	if len(lines) == 1 {
		if jsonBytes, err := json.MarshalIndent(toolMap, "  ", "  "); err == nil {
			lines = append(lines, contentStyle.Render(string(jsonBytes)))
		}
	}

	return lines
}

func (m *Model) renderChoiceSummary(choice interface{}, idx int, contentStyle, mutedStyle lipgloss.Style, focused bool) []string {
	c, ok := choice.(map[string]interface{})
	if !ok {
		return []string{mutedStyle.Render(fmt.Sprintf("  [%d] 无效数据", idx))}
	}

	var finishReason string
	if fr, ok := c["finish_reason"].(string); ok {
		finishReason = fr
	}

	prefix := "  "
	style := mutedStyle
	if focused {
		prefix = "▶ "
		style = contentStyle.Bold(true)
	}

	summary := fmt.Sprintf("💬 choice[%d]", idx)
	if finishReason != "" {
		summary += fmt.Sprintf(" (%s)", finishReason)
	}

	return []string{style.Render(prefix + summary)}
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

	var markdownText string
	var toolCalls []interface{}

	// 统一获取 message (兼容流式输出中的 delta 字段以完美渲染流式日志)
	msg, ok := c["message"].(map[string]interface{})
	if !ok {
		msg, _ = c["delta"].(map[string]interface{})
	}

	if msg != nil {
		if role, ok := msg["role"].(string); ok {
			lines = append(lines, valueStyle.Render("  role: "+role))
		}
		// 使用统一且健壮的提取逻辑，完美兼容单对象 map、多模态 block 等复杂情况
		markdownText = extractMessageContentFull(msg)

		if tc, ok := msg["tool_calls"].([]interface{}); ok && len(tc) > 0 {
			toolCalls = tc
		}
	} else {
		// 兼容非 chat completion 的 text 字段
		if text, ok := c["text"].(string); ok && text != "" {
			markdownText = text
		}
	}

	// 动态收集当前 Choice 中的可聚焦块
	var availableBlocks []string
	var thinking, cleanText string
	if markdownText != "" {
		thinking, cleanText = extractThinking(markdownText)
		if thinking != "" {
			availableBlocks = append(availableBlocks, "thinking")
		}
		if cleanText != "" {
			availableBlocks = append(availableBlocks, "content")
		}
	}
	m.detailState.blocks = availableBlocks
	if m.detailState.focusedBlock >= len(availableBlocks) {
		m.detailState.focusedBlock = 0
	}

	if markdownText != "" {
		// 动态计算半屏折叠阈值
		threshold := m.height / 2
		if threshold < 8 {
			threshold = 8
		}

		// 预先渲染以确定真实的行数
		var renderedThinking, renderedLines []string
		var thinkingLinesCount, contentLinesCount int
		var canThinkingCollapse, canContentCollapse bool

		if thinking != "" {
			renderedThinking = m.renderMarkdownFull(thinking)
			thinkingLinesCount = len(renderedThinking)
			canThinkingCollapse = thinkingLinesCount > threshold
		}

		if cleanText != "" {
			renderedLines = m.renderMarkdownFull(cleanText)
			contentLinesCount = len(renderedLines)
			canContentCollapse = contentLinesCount > threshold
		}

		if thinking != "" {
			isFocused := len(availableBlocks) > 0 && availableBlocks[m.detailState.focusedBlock] == "thinking"
			isCollapsed := false
			if canThinkingCollapse {
				if collapsed, exists := m.detailState.blockCollapsed["thinking"]; exists {
					isCollapsed = collapsed
				} else {
					isCollapsed = true // 默认折叠
				}
			}

			var title string
			if isFocused {
				prefix := "▶ "
				tip := " (按 C 复制)"
				if canThinkingCollapse {
					if isCollapsed {
						tip = " (按 Enter 展开全部，按 C 复制)"
					} else {
						tip = " (按 Enter 折叠内容，按 C 复制)"
					}
				}
				title = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(prefix + "🧠 思考过程" + tip)
			} else {
				prefix := "  "
				status := ""
				if canThinkingCollapse && isCollapsed {
					status = " [已折叠]"
				}
				title = mutedStyle.Render(prefix + "🧠 思考过程" + status + ":")
			}
			lines = append(lines, title)

			if canThinkingCollapse && isCollapsed {
				// 折叠状态下只展示前 threshold 行
				for _, rl := range renderedThinking[:threshold] {
					lines = append(lines, "    "+rl)
				}
				remaining := thinkingLinesCount - threshold
				lines = append(lines, mutedStyle.Render(fmt.Sprintf("    ... 已折叠多余 %d 行思考过程，按 Enter 展开全部 ...", remaining)))
				lines = append(lines, "")
			} else {
				// 展开状态下展示全文
				for _, rl := range renderedThinking {
					lines = append(lines, "    "+rl)
				}
				lines = append(lines, "")
			}
		}

		if cleanText != "" {
			isFocused := len(availableBlocks) > 0 && availableBlocks[m.detailState.focusedBlock] == "content"
			isCollapsed := false
			if canContentCollapse {
				if collapsed, exists := m.detailState.blockCollapsed["content"]; exists {
					isCollapsed = collapsed
				} else {
					isCollapsed = true // 默认折叠
				}
			}

			var title string
			if isFocused {
				prefix := "▶ "
				tip := " (按 C 复制)"
				if canContentCollapse {
					if isCollapsed {
						tip = " (按 Enter 展开全部，按 C 复制)"
					} else {
						tip = " (按 Enter 折叠内容，按 C 复制)"
					}
				}
				title = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).Render(prefix + "💬 响应内容" + tip)
			} else {
				prefix := "  "
				status := ""
				if canContentCollapse && isCollapsed {
					status = " [已折叠]"
				}
				title = mutedStyle.Render(prefix + "💬 响应内容" + status + ":")
			}
			lines = append(lines, title)

			if canContentCollapse && isCollapsed {
				// 折叠状态下只展示前 threshold 行
				for _, rl := range renderedLines[:threshold] {
					lines = append(lines, "    "+rl)
				}
				remaining := contentLinesCount - threshold
				lines = append(lines, mutedStyle.Render(fmt.Sprintf("    ... 已折叠多余 %d 行响应内容，按 Enter 展开全部 ...", remaining)))
			} else {
				// 展开状态下展示全文
				for _, rl := range renderedLines {
					lines = append(lines, "    "+rl)
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render("  🔧 Tool Calls:"))
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				var fnName string
				var args string
				tcID, _ := tcMap["id"].(string)
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					if n, ok := fn["name"].(string); ok {
						fnName = n
					}
					if a, ok := fn["arguments"].(string); ok {
						var parsed interface{}
						if err := json.Unmarshal([]byte(a), &parsed); err == nil {
							if pretty, err := json.MarshalIndent(parsed, "      ", "  "); err == nil {
								args = string(pretty)
							} else {
								args = a
							}
						} else {
							args = a
						}
					}
				}

				if tcID != "" {
					lines = append(lines, valueStyle.Render(fmt.Sprintf("    - Call: %s (ID: %s)", fnName, tcID)))
				} else {
					lines = append(lines, valueStyle.Render(fmt.Sprintf("    - Call: %s", fnName)))
				}

				if args != "" {
					argsMarkdown := "```json\n" + args + "\n```"
					renderedArgs := m.renderMarkdownFull(argsMarkdown)
					for _, rl := range renderedArgs {
						lines = append(lines, "      "+rl)
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

// getVisibleData 返回经过滤和排序后的可见数据
func (m *Model) getVisibleData() []api.SpendLogEntry {
	var data []api.SpendLogEntry

	if m.logData != nil {
		data = m.logData.Data
	} else if m.logDataOld != nil {
		// 转换旧格式
		for _, entry := range *m.logDataOld {
			var spendLogEntry api.SpendLogEntry
			b, _ := json.Marshal(entry)
			json.Unmarshal(b, &spendLogEntry)
			data = append(data, spendLogEntry)
		}
	}

	// 1. 按 model 过滤
	if m.model != "" {
		var filtered []api.SpendLogEntry
		for _, entry := range data {
			if strings.Contains(entry.Model, m.model) {
				filtered = append(filtered, entry)
			}
		}
		data = filtered
	}

	// 2. 排序
	if len(data) > 0 && m.sortField != "time" {
		sorted := make([]api.SpendLogEntry, len(data))
		copy(sorted, data)
		switch m.sortField {
		case "spend":
			sort.Slice(sorted, func(i, j int) bool {
				if m.sortAscending {
					return sorted[i].TotalSpend < sorted[j].TotalSpend
				}
				return sorted[i].TotalSpend > sorted[j].TotalSpend
			})
		case "tokens":
			sort.Slice(sorted, func(i, j int) bool {
				if m.sortAscending {
					return sorted[i].TotalTokens < sorted[j].TotalTokens
				}
				return sorted[i].TotalTokens > sorted[j].TotalTokens
			})
		}
		data = sorted
	}

	return data
}

func (m *Model) renderListView() string {
	var content strings.Builder

	// 显示帮助面板
	if m.showHelp {
		return m.renderHelpPanel()
	}

	availableRows := DetailDefaultRows
	if m.height > 10 {
		availableRows = m.height - DetailMinRows
	}
	visibleRows := availableRows - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	// 确保滚动偏移在有效范围内
	if m.listScrollOffset < 0 {
		m.listScrollOffset = 0
	}

	// 使用统一的 getVisibleData 获取过滤后的数据
	filteredData := m.getVisibleData()

	// 计算总记录数
	var total int
	if m.logData != nil {
		total = int(m.logData.Total)
	} else if m.logDataOld != nil {
		total = len(*m.logDataOld)
	}

	if len(filteredData) > 0 {
		// 限制滚动偏移不超过数据末尾
		if m.listScrollOffset > len(filteredData)-visibleRows && len(filteredData) > visibleRows {
			m.listScrollOffset = len(filteredData) - visibleRows
		}
		if m.listScrollOffset < 0 {
			m.listScrollOffset = 0
		}

		// 选中项超出范围时调整
		if m.selectedIndex >= len(filteredData) {
			m.selectedIndex = len(filteredData) - 1
		}
		if m.selectedIndex < 0 {
			m.selectedIndex = 0
		}

		// 计算渲染用的索引（相对于可见范围）
		renderIndex := m.selectedIndex - m.listScrollOffset

		// 限制渲染索引
		if renderIndex < 0 {
			renderIndex = 0
		}
		if renderIndex >= visibleRows {
			renderIndex = visibleRows - 1
		}

		content.WriteString(renderLogsTable(filteredData, total, m.newLogIDs, availableRows, m.listScrollOffset, m.width, renderIndex, visibleRows))
	} else {
		// 改进的空状态提示
		if m.model != "" {
			content.WriteString(components.NewPlaceholder("无匹配结果").View())
			content.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("尝试调整搜索条件或清除筛选"))
		} else {
			content.WriteString(components.NewPlaceholder("暂无日志记录").View())
			content.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("检查 API Key 权限 或 调整时间范围"))
		}
	}

	// 构建状态栏信息
	var statusInfo []string
	statusInfo = append(statusInfo, fmt.Sprintf("刷新: %ds", m.interval))

	// 显示排序状态
	if m.model != "" {
		statusInfo = append(statusInfo, fmt.Sprintf("模型: %s", m.model))
	}
	statusInfo = append(statusInfo, fmt.Sprintf("排序: %s", m.sortField))
	statusInfo = append(statusInfo, fmt.Sprintf("更新: %d", m.tick))

	statusBar := strings.Join(statusInfo, " | ")

	header := components.NewHeader("LiteLLM 日志", statusBar+" | ? 帮助")

	// 计算内容行数
	contentStr := content.String()
	contentLineCount := strings.Count(contentStr, "\n") + 1

	// 底部状态栏 (时间 + 帮助文本 = 2行)
	footerLineCount := 2

	// header 1行 + 分隔线 1行 = 2行
	headerLineCount := 2

	// 计算需要填充的空白行数，使底部状态固定在屏幕底部
	usedLines := headerLineCount + contentLineCount + footerLineCount
	paddingLines := m.height - usedLines
	if paddingLines > 0 {
		contentStr += strings.Repeat("\n", paddingLines)
	}

	// 底部状态
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
		fmt.Sprintf("时间: %s", time.Now().Format("15:04:05")))

	if m.showHeader {
		return header.View(m.width) + "\n" + contentStr + footer
	}
	return contentStr + footer
}

func (m *Model) loadDetail() tea.Cmd {
	var requestID string

	// Use filtered data for selection
	visibleData := m.getVisibleData()
	if len(visibleData) > 0 && m.selectedIndex < len(visibleData) {
		requestID = visibleData[m.selectedIndex].ID
		m.selectedEntry = &visibleData[m.selectedIndex]
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
		if m.debug {
			log.Printf("[loadDetail] 开始加载详情, requestID=%s", requestID)
		}
		detail, err := m.client.GetSpendLogDetail(requestID)
		if err != nil {
			if m.debug {
				log.Printf("[loadDetail] 请求失败: %v", err)
			}
			return DetailLoadedMsg{Error: fmt.Sprintf("请求失败: %v", err)}
		}
		if m.debug {
			log.Printf("[loadDetail] 请求完成, requestID=%s, keys=%v", requestID, getMapKeys(detail))
		}
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

func renderLogsTable(data []api.SpendLogEntry, total int, newLogIDs map[string]bool, maxRows int, scrollOffset int, width int, renderIndex int, visibleRows int) string {
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

	if width <= 0 {
		width = 120
	}

	// 限制允许列分配的宽度，留出 6 个空格作为列与列之间的间隔
	allowedWidth := width - 6
	if allowedWidth < 50 {
		allowedWidth = 50
	}

	// 最小列宽定义
	minWidths := struct {
		time    int
		status  int
		spend   int
		latency int
		tokens  int
		model   int
		tags    int
	}{
		time:    16,
		status:  4,
		spend:   6,
		latency: 6,
		tokens:  8,
		model:   10,
		tags:    8,
	}

	// 统计每一列内容的最大宽度
	contentWidths := struct {
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
		contentWidths.time = max(contentWidths.time, runewidth.StringWidth(startTime))

		status := "✓"
		if entry.Status != "success" && entry.ErrorMessage != "" {
			status = "✗"
		}
		contentWidths.status = max(contentWidths.status, runewidth.StringWidth(status))

		spendStr := "-"
		if entry.TotalSpend > 0 {
			spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
		}
		contentWidths.spend = max(contentWidths.spend, runewidth.StringWidth(spendStr))

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
		contentWidths.latency = max(contentWidths.latency, runewidth.StringWidth(latencyStr))

		tokensStr := "-"
		if entry.TotalTokens > 0 {
			tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
		}
		contentWidths.tokens = max(contentWidths.tokens, runewidth.StringWidth(tokensStr))

		model := entry.ModelGroup
		if model == "" {
			model = entry.Model
		}
		contentWidths.model = max(contentWidths.model, runewidth.StringWidth(model))

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
		contentWidths.tags = max(contentWidths.tags, runewidth.StringWidth(tag))
	}

	// 计算每列的最终分配宽度
	var w_time, w_status, w_spend, w_latency, w_tokens, w_model, w_tags int

	// 前 5 列为内容相对固定的列，我们按 max(minWidth, contentWidth) 分配
	w_time = max(minWidths.time, contentWidths.time)
	w_status = max(minWidths.status, contentWidths.status)
	w_spend = max(minWidths.spend, contentWidths.spend)
	w_latency = max(minWidths.latency, contentWidths.latency)
	w_tokens = max(minWidths.tokens, contentWidths.tokens)

	fixedWidthSum := w_time + w_status + w_spend + w_latency + w_tokens

	if allowedWidth-fixedWidthSum >= minWidths.model+minWidths.tags {
		// 剩余宽度分配给 model 和 tags
		remainingWidth := allowedWidth - fixedWidthSum
		w_model = max(minWidths.model, int(float64(remainingWidth)*0.60))
		w_tags = max(minWidths.tags, remainingWidth-w_model)
	} else {
		// 屏幕太窄，压缩前 5 列到它们的最小宽度
		w_time = minWidths.time
		w_status = minWidths.status
		w_spend = minWidths.spend
		w_latency = minWidths.latency
		w_tokens = minWidths.tokens

		fixedWidthSum = w_time + w_status + w_spend + w_latency + w_tokens
		remainingWidth := allowedWidth - fixedWidthSum
		w_model = max(minWidths.model, int(float64(remainingWidth)*0.60))
		w_tags = max(minWidths.tags, remainingWidth-w_model)
	}

	// 单元格格式化辅助函数：既保证总宽，又在溢出时优雅截断
	formatCell := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w == width {
			return s
		}
		if w > width {
			if width > 3 {
				return runewidth.Truncate(s, width-2, "..")
			}
			return runewidth.Truncate(s, width, "")
		}
		return s + strings.Repeat(" ", width-w)
	}

	var sb strings.Builder

	// 渲染表头
	sb.WriteString(headerStyle.Render(fmt.Sprintf("%s %s %s %s %s %s %s",
		formatCell("时间", w_time),
		formatCell("状态", w_status),
		formatCell("费用", w_spend),
		formatCell("耗时", w_latency),
		formatCell("Tokens", w_tokens),
		formatCell("模型", w_model),
		formatCell("Tags", w_tags))) + "\n")

	// 华丽的分隔线
	sb.WriteString(mutedStyle.Render(strings.Repeat("─", width)) + "\n")

	rowCount := 0
	dataLen := len(data)
	for i := 0; i < dataLen; i++ {
		// 根据滚动偏移计算实际显示的数据索引
		actualIndex := scrollOffset + i
		if actualIndex >= dataLen {
			break
		}
		entry := data[actualIndex]

		if maxRows > 0 && i >= visibleRows {
			sb.WriteString(mutedStyle.Render(fmt.Sprintf("\n显示 %d-%d 条，共 %d 条", scrollOffset+1, scrollOffset+visibleRows, total)))
			break
		}
		rowCount++

		isSelected := i == renderIndex
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

		if isNew {
			timeStyle = newHighlightStyle
			statusStyle = newHighlightStyle
			spendStyle = newHighlightStyle
			latencyStyle = newHighlightStyle
			tokensStyle = newHighlightStyle
			modelStyle = newHighlightStyle
			tagStyle = newHighlightMutedStyle
		} else if isSelected {
			timeStyle = selectedStyle
			statusStyle = selectedStyle
			spendStyle = selectedStyle
			latencyStyle = selectedStyle
			tokensStyle = selectedStyle
			modelStyle = selectedStyle
			tagStyle = selectedMutedStyle
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
			// 奇偶行背景
			if actualIndex%2 == 1 {
				timeStyle = timeStyle.Background(lipgloss.Color("236"))
				statusStyle = statusStyle.Background(lipgloss.Color("236"))
				spendStyle = spendStyle.Background(lipgloss.Color("236"))
				latencyStyle = latencyStyle.Background(lipgloss.Color("236"))
				tokensStyle = tokensStyle.Background(lipgloss.Color("236"))
				modelStyle = modelStyle.Background(lipgloss.Color("236"))
				tagStyle = tagStyle.Background(lipgloss.Color("236"))
			}
		}

		sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s %s\n",
			timeStyle.Render(formatCell(startTime, w_time)),
			statusStyle.Render(formatCell(status, w_status)),
			spendStyle.Render(formatCell(spendStr, w_spend)),
			latencyStyle.Render(formatCell(latencyStr, w_latency)),
			tokensStyle.Render(formatCell(tokensStr, w_tokens)),
			modelStyle.Render(formatCell(model, w_model)),
			tagStyle.Render(formatCell(tag, w_tags))))
	}

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("已加载 %d 条", len(data)))))

	return sb.String()
}

func renderLogsTableOld(resp *api.SpendLogsResponse, intervalVal int, newLogIDs map[string]bool, maxRows int, scrollOffset int, renderIndex int, visibleRows int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))

	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | Ctrl+C 退出 ", intervalVal)) + "\n\n")

	respLen := len(*resp)
	rowCount := 0
	displayed := 0

	// 跳过 scrollOffset 条记录
	skipped := 0
	for _, entry := range *resp {
		if skipped < scrollOffset {
			skipped++
			continue
		}

		if maxRows > 0 && displayed >= visibleRows {
			sb.WriteString(mutedStyle.Render(fmt.Sprintf("\n显示 %d-%d 条，共 %d 条", scrollOffset+1, scrollOffset+displayed, respLen)))
			break
		}

		spendVal, hasSpend := entry["spend"]
		if hasSpend {
			displayed++
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

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("已加载 %d 条", len(*resp)))))
	return sb.String()
}

// copyToClipboardOSC52 跨平台、无依赖的终端剪贴板复制命令 (利用现代终端的 OSC 52 复制指令协议)
func copyToClipboardOSC52(text string) tea.Cmd {
	return func() tea.Msg {
		b64 := base64.StdEncoding.EncodeToString([]byte(text))
		_, _ = os.Stdout.WriteString(fmt.Sprintf("\x1b]52;c;%s\x07", b64))
		return nil
	}
}

// ShowHeader 控制是否显示顶部 header
func (m *Model) ShowHeader(show bool) {
	m.showHeader = show
}

// ShowFooter 控制是否显示底部 help footer
func (m *Model) ShowFooter(show bool) {
	m.showFooter = show
}

// SetDebug 设置调试日志模式
func (m *Model) SetDebug(enabled bool) {
	m.debug = enabled
}

// HelpText 返回当前视图状态对应的帮助文本（供父容器统一渲染 footer 时使用）
func (m *Model) HelpText() string {
	if m.showHelp {
		return "?: 关闭帮助"
	}
	if m.viewMode == "detail" && m.detailState != nil {
		if m.detailState.itemDetailMode {
			var keys []string
			keys = append(keys, "↑↓: 滚动")
			if len(m.detailState.blocks) > 0 {
				keys = append(keys, "Tab: 切换块", "Enter: 展开/折叠", "C: 复制")
			}
			keys = append(keys, "ESC: 返回", "←/→: 切换 tab", "Q: 退出")
			return strings.Join(keys, " | ")
		}
		return "↑↓: 切换 | Enter: 查看详情 | ESC: 返回 | ←/→: 切换 tab | Q: 退出"
	}
	// 加载错误状态
	if m.loadError != "" {
		return "❌ " + m.loadError + " | r: 重试 | esc: 返回 | ←/→: 切换 tab | q: 退出"
	}
	// 列表视图
	return "↑↓: 切换 | Enter: 详情 | c: 复制 | g/G: 跳转 | r: 刷新 | esc: 返回 | ←/→: 切换 tab | q: 退出"
}

// renderHelpPanel 渲染帮助面板
func (m *Model) renderHelpPanel() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(" 快捷键帮助 (按 ? 关闭) "))
	sb.WriteString("\n\n")

	// 基本导航
	sb.WriteString(keyStyle.Render("↑↓") + " " + descStyle.Render("切换日志条目"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("Enter") + " " + descStyle.Render("查看详情"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("Esc") + " " + descStyle.Render("返回/关闭"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("q") + " " + descStyle.Render("退出程序"))
	sb.WriteString("\n\n")

	// 搜索和排序
	sb.WriteString(keyStyle.Render("/") + " " + descStyle.Render("搜索过滤 (输入关键词后回车)"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("s") + " " + descStyle.Render("切换排序 (时间→花费→tokens)"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("c") + " " + descStyle.Render("复制选中项"))
	sb.WriteString("\n\n")

	// 详情视图
	sb.WriteString(keyStyle.Render("←→") + " " + descStyle.Render("切换详情 tab"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("Tab") + " " + descStyle.Render("切换区块"))
	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("Enter") + " " + descStyle.Render("展开/折叠区块"))
	sb.WriteString("\n\n")

	// 其他
	sb.WriteString(mutedStyle.Render("按 ? 返回日志列表"))

	// 包装为面板样式
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2)

	if m.showHeader {
		header := components.NewHeader("LiteLLM 日志", fmt.Sprintf("刷新: %ds | ? 关闭帮助", m.interval))
		return header.View(m.width) + "\n\n" + panelStyle.Render(sb.String())
	}
	return panelStyle.Render(sb.String())
}

// UI 常量
const (
	DefaultWidth      = 120 // 默认终端宽度
	DefaultHeight     = 40  // 默认终端高度
	ScrollStep        = 20  // 滚动步长
	DetailMinRows     = 10  // 详情视图最小行数
	DetailDefaultRows = 50  // 详情视图默认行数
)
