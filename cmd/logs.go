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
	textMode bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "轮询查看日志 (TUI)",
	Run:   runLogs,
}

func init() {
	logsCmd.Flags().IntVarP(&interval, "interval", "i", 5, "刷新间隔 (秒)")
	logsCmd.Flags().StringVarP(&model, "model", "m", "", "过滤模型")
	logsCmd.Flags().BoolVarP(&textMode, "text", "t", false, "文本模式 (非交互环境)")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	if textMode {
		runLogsText(c, interval, model)
		return
	}

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
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	tick := 0
	for {
		clearScreen()
		printLogs(c, model, tick)
		tick++

		select {
		case <-ticker.C:
			continue
		}
	}
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
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
		// 表头 (使用 runewidth 计算显示宽度，列宽根据实际数据调整)
		fmt.Println(headerStyle.Render(fmt.Sprintf("%s %s %s %s %s %s %s",
			padRight("时间", 16),
			padRight("状态", 2),
			padRight("费用", 7),
			padRight("耗时", 8),
			padRight("Tokens", 18),
			padRight("模型", 26),
			"Tags")))
		fmt.Println(mutedStyle.Render(strings.Repeat("─", 95)))

		// 显示日志条目
		count := 0
		for _, entry := range resp.Data {
			// 过滤模型
			if modelFilter != "" && !strings.Contains(entry.Model, modelFilter) {
				continue
			}
			count++

			// 时间 (只显示日期和时间)
			startTime := entry.StartTime
			if len(startTime) >= 19 {
				startTime = startTime[:16] // 去掉秒和时区
				startTime = strings.Replace(startTime, "T", " ", 1)
			}
			// 补齐时间到显示宽度 16
			startTime = padRight(startTime, 16)

			// 状态
			status := "✓"
			if entry.Status != "success" && entry.ErrorMessage != "" {
				status = "✗"
			}

			// 费用
			spendStr := "-"
			if entry.TotalSpend > 0 {
				spendStr = fmt.Sprintf("$%.2f", entry.TotalSpend)
			}
			spendStr = padRight(spendStr, 7)

			// 耗时 (通过 startTime 和 endTime 计算)
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
			latencyStr = padRight(latencyStr, 8)

			// Tokens 显示为 total(prompt+completion)
			var tokensStr string
			if entry.TotalTokens > 0 {
				tokensStr = fmt.Sprintf("%d(%d+%d)", entry.TotalTokens, entry.PromptTokens, entry.CompletionTokens)
			} else {
				tokensStr = "-"
			}
			tokensStr = padRight(tokensStr, 18)

			// 模型 (从 model_group 取值，截断显示)
			model := entry.ModelGroup
			if model == "" {
				model = entry.Model
			}
			if len(model) > 26 {
				model = model[:26]
			}
			model = padRight(model, 26)

			// Tags (从 request_tags 读取，解析显示)
			tag := ""
			if len(entry.RequestTags) > 0 {
				tags := entry.RequestTags
				if len(tags) > 1 {
					// 按长度排序
					sort.Slice(tags, func(i, j int) bool {
						return len(tags[i]) < len(tags[j])
					})
					// 从最长的 tag 中提取: name/version 的格式
					longest := tags[len(tags)-1]
					// 去掉 "User-Agent: " 前缀
					longest = strings.TrimPrefix(longest, "User-Agent: ")
					// 去掉括号内容 like (external, cli)
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
					errorStyle.Render(padRight(status, 2)),
					greenStyle.Render(spendStr),
					yellowStyle.Render(latencyStr),
					contentStyle.Render(tokensStr),
					contentStyle.Render(model),
					mutedStyle.Render(tag))
			} else {
				fmt.Printf("%s %s %s %s %s %s %s\n",
					contentStyle.Render(startTime),
					greenStyle.Render(padRight(status, 2)),
					greenStyle.Render(spendStr),
					yellowStyle.Render(latencyStr),
					contentStyle.Render(tokensStr),
					contentStyle.Render(model),
					mutedStyle.Render(tag))
			}
		}

		fmt.Println()
		fmt.Println(mutedStyle.Render(fmt.Sprintf("共 %d 条记录 (总 %d)", count, resp.Total)))
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

// TUI 模式
type logsModel struct {
	client   *client.Client
	data     string
	interval int
	model    string
	tick     int
	quitting bool
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

	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	return headerStyle.Render(fmt.Sprintf(" 📊 LiteLLM 日志 (刷新: %ds) | 按 q 退出 ", m.interval)) +
		"\n\n" +
		contentStyle.Render(m.data) +
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
			return
		}
		if respOld == nil || len(*respOld) == 0 {
			m.data = "暂无数据"
			return
		}
		m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录", len(*respOld))
		return
	}

	if resp == nil || len(resp.Data) == 0 {
		m.data = "暂无数据"
		return
	}

	m.data = fmt.Sprintf("✅ 获取到 %d 条日志记录 (总 %d)", len(resp.Data), resp.Total)
}

type tickMsg time.Time