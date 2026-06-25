package cmd

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
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
	interval int
	model    string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "轮询查看日志 (TUI)",
	Run:   runLogs,
}

func init() {
	logsCmd.Flags().IntVarP(&interval, "interval", "i", 5, "刷新间隔 (秒)")
	logsCmd.Flags().StringVarP(&model, "model", "m", "", "过滤模型")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	p := tea.NewProgram(
		NewLogsModel(c, interval, model),
		tea.WithAltScreen(),
	)

	// 处理退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		p.Send(tea.Quit())
	}()

	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}

// 文本模式 - 用于非交互环境
func runLogsText(c *client.Client, interval int, model string) {
	// 创建退出信号通道
	quitChan := make(chan struct{})

	// 监听系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		close(quitChan)
	}()

	// 监听键盘输入 (q 退出)
	go func() {
		var buf [1]byte
		for {
			n, err := os.Stdin.Read(buf[:])
			if err != nil {
				return
			}
			if n > 0 && buf[0] == 'q' {
				close(quitChan)
				return
			}
		}
	}()

	tick := 0
	for {
		clearScreen()
		printLogs(c, model, tick)
		tick++

		select {
		case <-quitChan:
			fmt.Println("\n👋 已退出")
			return
		case <-time.After(time.Duration(interval) * time.Second):
			continue
		}
	}
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// formatLocalTime 将 UTC 时间转换为本地时区显示
func formatLocalTime(utcTime string) string {
	if len(utcTime) >= 19 {
		// 解析 UTC 时间
		t, err := time.Parse("2006-01-02T15:04:05", utcTime[:19])
		if err == nil {
			// 转换为本地时区并格式化为字符串
			return t.Local().Format("2006-01-02 15:04")
		}
		// 解析失败则回退到简单替换
		fallback := utcTime[:19]
		fallback = strings.Replace(fallback, "T", " ", 1)
		return fallback
	}
	return utcTime
}

func printLogs(c *client.Client, model string, tick int) {
	// 使用 datetime 格式，并 URL 编码空格
	endDate := url.QueryEscape(time.Now().Format("2006-01-02 15:04:05"))
	startDate := url.QueryEscape(time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05"))

	// 优先使用 /spend/logs/ui (需要 JWT token)
	resp, err := c.GetSpendLogsUI(startDate, endDate)
	if err != nil {
		// 如果失败，回退到旧的 /spend/logs
		respOld, err2 := c.GetSpendLogs(
			time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
			time.Now().Format("2006-01-02"),
		)
		if err2 != nil {
			fmt.Printf("❌ 获取失败: %v\n", err)
			return
		}
		printSpendLogs(respOld, tick, model)
		return
	}

	printSpendLogsUI(resp, tick, model)
}

func printSpendLogsUI(resp *api.SpendLogsUIResponse, tick int, modelFilter string) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226"))

	// 辅助函数：按显示宽度填充
	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}

	fmt.Println(headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 实时日志 (刷新: %ds) | 按 q 退出 ", interval)))
	fmt.Println()

	if resp == nil || len(resp.Data) == 0 {
		fmt.Println(contentStyle.Render("暂无数据"))
	} else {
		// 先过滤数据
		var filteredData []api.SpendLogEntry
		for _, entry := range resp.Data {
			if modelFilter != "" && !strings.Contains(entry.Model, modelFilter) {
				continue
			}
			filteredData = append(filteredData, entry)
		}

		// 自动计算每列的最大宽度
		colWidths := struct {
			time   int
			status int
			spend  int
			latency int
			tokens  int
			model   int
			tags    int
		}{
			time:   runewidth.StringWidth("时间"),
			status: runewidth.StringWidth("状态"),
			spend:  runewidth.StringWidth("费用"),
			latency: runewidth.StringWidth("耗时"),
			tokens: runewidth.StringWidth("Tokens"),
			model:  runewidth.StringWidth("模型"),
			tags:   runewidth.StringWidth("Tags"),
		}

		for _, entry := range filteredData {
			// 时间
			startTime := formatLocalTime(entry.StartTime)
			colWidths.time = max(colWidths.time, runewidth.StringWidth(startTime))

			// 状态
			status := "✓"
			if entry.Status != "success" && entry.ErrorMessage != "" {
				status = "✗"
			}
			colWidths.status = max(colWidths.status, runewidth.StringWidth(status))

			// 费用
			spendStr := "-"
			if entry.TotalSpend > 0 {
				spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
			}
			colWidths.spend = max(colWidths.spend, runewidth.StringWidth(spendStr))

			// 耗时
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

			// Tokens
			tokensStr := "-"
			if entry.TotalTokens > 0 {
				tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
			}
			colWidths.tokens = max(colWidths.tokens, runewidth.StringWidth(tokensStr))

			// 模型
			model := entry.ModelGroup
			if model == "" {
				model = entry.Model
			}
			colWidths.model = max(colWidths.model, runewidth.StringWidth(model))

			// Tags
			tag := ""
			if len(entry.RequestTags) > 0 {
				tags := entry.RequestTags
				if len(tags) > 1 {
					sort.Slice(tags, func(i, j int) bool {
						return len(tags[i]) < len(tags[j])
					})
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

		// 打印表头
		fmt.Println(headerStyle.Render(fmt.Sprintf("%s %s %s %s %s %s %s",
			padRight("时间", colWidths.time),
			padRight("状态", colWidths.status),
			padRight("费用", colWidths.spend),
			padRight("耗时", colWidths.latency),
			padRight("Tokens", colWidths.tokens),
			padRight("模型", colWidths.model),
			padRight("Tags", colWidths.tags))))

		// 打印分隔线
		totalWidth := colWidths.time + colWidths.status + colWidths.spend + colWidths.latency + colWidths.tokens + colWidths.model + colWidths.tags + 6
		fmt.Println(mutedStyle.Render(strings.Repeat("─", totalWidth)))

		// 打印数据
		for _, entry := range filteredData {
			// 时间
			startTime := formatLocalTime(entry.StartTime)
			startTime = padRight(startTime, colWidths.time)

			// 状态
			status := "✓"
			if entry.Status != "success" && entry.ErrorMessage != "" {
				status = "✗"
			}
			status = padRight(status, colWidths.status)

			// 费用
			spendStr := "-"
			if entry.TotalSpend > 0 {
				spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
			}
			spendStr = padRight(spendStr, colWidths.spend)

			// 耗时
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
			latencyStr = padRight(latencyStr, colWidths.latency)

			// Tokens
			var tokensStr string
			if entry.TotalTokens > 0 {
				tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
			} else {
				tokensStr = "-"
			}
			tokensStr = padRight(tokensStr, colWidths.tokens)

			// 模型
			model := entry.ModelGroup
			if model == "" {
				model = entry.Model
			}
			model = padRight(model, colWidths.model)

			// Tags
			tag := ""
			if len(entry.RequestTags) > 0 {
				tags := entry.RequestTags
				if len(tags) > 1 {
					sort.Slice(tags, func(i, j int) bool {
						return len(tags[i]) < len(tags[j])
					})
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

			// 打印行
			if entry.Status != "success" && entry.ErrorMessage != "" {
				fmt.Printf("%s %s %s %s %s %s %s\n",
					contentStyle.Render(startTime),
					errorStyle.Render(status),
					greenStyle.Render(spendStr),
					yellowStyle.Render(latencyStr),
					contentStyle.Render(tokensStr),
					contentStyle.Render(model),
					mutedStyle.Render(tag))
			} else {
				fmt.Printf("%s %s %s %s %s %s %s\n",
					contentStyle.Render(startTime),
					greenStyle.Render(status),
					greenStyle.Render(spendStr),
					yellowStyle.Render(latencyStr),
					contentStyle.Render(tokensStr),
					contentStyle.Render(model),
					mutedStyle.Render(tag))
			}
		}

		fmt.Println()
		fmt.Println(mutedStyle.Render(fmt.Sprintf("共 %d 条记录 (总 %d)", len(filteredData), resp.Total)))
	}

	fmt.Println()
	fmt.Printf("⏱ 更新次数: %d | 时间: %s\n", tick, time.Now().Format("15:04:05"))
}

// printSpendLogs 回退使用的旧格式显示
func printSpendLogs(resp *api.SpendLogsResponse, tick int, modelFilter string) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))

	fmt.Println(headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | Ctrl+C 退出 ", interval)))
	fmt.Println()

	if resp == nil || len(*resp) == 0 {
		fmt.Println(contentStyle.Render("暂无数据"))
	} else {
		for _, entry := range *resp {
			spendVal, hasSpend := entry["spend"]
			if hasSpend {
				spend, _ := spendVal.(float64)

				keyLabel := "当前 Key"
				if len(entry) > 0 {
					for k := range entry {
						if k != "spend" && k != "models" && k != "users" && k != "startTime" {
							keyLabel = k
							break
						}
					}
				}
				if len(keyLabel) > 12 {
					keyLabel = keyLabel[:8] + "..."
				}

				fmt.Printf(contentStyle.Render("📦 %s "), keyLabel)
				if spend > 0 {
					fmt.Printf("%s", greenStyle.Render(fmt.Sprintf("$%.4f ", spend)))
				}
				fmt.Println()
			}
		}

		fmt.Println()
		fmt.Println(mutedStyle.Render(fmt.Sprintf("共 %d 条记录", len(*resp))))
	}

	fmt.Println()
	fmt.Printf("⏱ 更新次数: %d | 时间: %s\n", tick, time.Now().Format("15:04:05"))
	fmt.Println(mutedStyle.Render("\n提示: 使用 --text 或 -t 参数可在非交互环境运行 (回退模式)"))
}

// renderLogsTable 渲染日志表格 (用于 TUI 模式)
func renderLogsTable(data []api.SpendLogEntry, total int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226"))

	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}

	var sb strings.Builder

	// 自动计算列宽
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

	// 打印表头
	sb.WriteString(headerStyle.Render(fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight("时间", colWidths.time),
		padRight("状态", colWidths.status),
		padRight("费用", colWidths.spend),
		padRight("耗时", colWidths.latency),
		padRight("Tokens", colWidths.tokens),
		padRight("模型", colWidths.model),
		padRight("Tags", colWidths.tags))) + "\n")

	// 分隔线
	totalWidth := colWidths.time + colWidths.status + colWidths.spend + colWidths.latency + colWidths.tokens + colWidths.model + colWidths.tags + 6
	sb.WriteString(mutedStyle.Render(strings.Repeat("─", totalWidth)) + "\n")

	// 打印数据
	for _, entry := range data {
		startTime := formatLocalTime(entry.StartTime)

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

		if entry.Status != "success" && entry.ErrorMessage != "" {
			sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s %s\n",
				contentStyle.Render(padRight(startTime, colWidths.time)),
				errorStyle.Render(padRight(status, colWidths.status)),
				greenStyle.Render(padRight(spendStr, colWidths.spend)),
				yellowStyle.Render(padRight(latencyStr, colWidths.latency)),
				contentStyle.Render(padRight(tokensStr, colWidths.tokens)),
				contentStyle.Render(padRight(model, colWidths.model)),
				mutedStyle.Render(padRight(tag, colWidths.tags))))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s %s\n",
				contentStyle.Render(padRight(startTime, colWidths.time)),
				greenStyle.Render(padRight(status, colWidths.status)),
				greenStyle.Render(padRight(spendStr, colWidths.spend)),
				yellowStyle.Render(padRight(latencyStr, colWidths.latency)),
				contentStyle.Render(padRight(tokensStr, colWidths.tokens)),
				contentStyle.Render(padRight(model, colWidths.model)),
				mutedStyle.Render(padRight(tag, colWidths.tags))))
		}
	}

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("共 %d 条记录 (总 %d)", len(data), total))))

	return sb.String()
}

// renderLogsTableOld 渲染旧版日志表格 (用于 TUI 模式回退)
func renderLogsTableOld(resp *api.SpendLogsResponse, intervalVal int) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))

	var sb strings.Builder
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | Ctrl+C 退出 ", intervalVal)) + "\n\n")

	for _, entry := range *resp {
		spendVal, hasSpend := entry["spend"]
		if hasSpend {
			spend, _ := spendVal.(float64)

			keyLabel := "当前 Key"
			if len(entry) > 0 {
				for k := range entry {
					if k != "spend" && k != "models" && k != "users" && k != "startTime" {
						keyLabel = k
						break
					}
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
		}
	}

	sb.WriteString(fmt.Sprintf("\n%s\n", mutedStyle.Render(fmt.Sprintf("共 %d 条记录", len(*resp)))))
	return sb.String()
}

// TUI 模式
type logsModel struct {
	client     *client.Client
	data       string
	interval   int
	model      string
	tick       int
	quitting   bool
	logData    *api.SpendLogsUIResponse
	logDataOld *api.SpendLogsResponse
}

func NewLogsModel(c *client.Client, interval int, model string) *logsModel {
	m := &logsModel{
		client:   c,
		interval: interval,
		model:    model,
		data:     "加载中...",
	}
	m.refresh()
	return m
}

func (m *logsModel) Init() tea.Cmd {
	return tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *logsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		}
	case tickMsg:
		m.refresh()
		m.tick++
		return m, tea.Tick(time.Duration(m.interval)*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

func (m *logsModel) View() string {
	if m.quitting {
		return "👋 已退出\n"
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))

	// 直接渲染完整的日志表格
	var content strings.Builder

	if m.logData != nil && len(m.logData.Data) > 0 {
		// 过滤数据
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
		content.WriteString(renderLogsTable(filteredData, int(m.logData.Total)))
	} else if m.logDataOld != nil && len(*m.logDataOld) > 0 {
		content.WriteString(renderLogsTableOld(m.logDataOld, m.interval))
	} else {
		content.WriteString("暂无数据")
	}

	return headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | 按 q 退出 ", m.interval)) +
		"\n\n" +
		content.String() +
		fmt.Sprintf("\n\n⏱ 更新次数: %d | 时间: %s", m.tick, time.Now().Format("15:04:05"))
}

func (m *logsModel) refresh() {
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
			m.data = fmt.Sprintf("❌ 获取失败: %v", err)
			m.logData = nil
			m.logDataOld = nil
			return
		}
		if respOld == nil || len(*respOld) == 0 {
			m.data = "暂无数据"
			m.logData = nil
			m.logDataOld = nil
			return
		}
		m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录", len(*respOld))
		m.logData = nil
		m.logDataOld = respOld
		return
	}

	if resp == nil || len(resp.Data) == 0 {
		m.data = "暂无数据"
		m.logData = nil
		m.logDataOld = nil
		return
	}

	m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录 (总 %d)", len(resp.Data), resp.Total)
	m.logData = resp
	m.logDataOld = nil
}

type tickMsg time.Time