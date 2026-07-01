package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
	"litellm-cli/internal/tui/stats"
)

var (
	period       string
	by           string
	fromDate     string
	toDate       string
	jsonOutStats bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "查看用量统计",
	Run:   runStats,
}

func init() {
	statsCmd.Flags().StringVar(&period, "period", "day", "统计周期: day, week, month")
	statsCmd.Flags().StringVar(&by, "by", "user", "聚合维度: user, team, model")
	statsCmd.Flags().StringVar(&fromDate, "from", "", "开始日期 (YYYY-MM-DD)")
	statsCmd.Flags().StringVar(&toDate, "to", "", "结束日期 (YYYY-MM-DD)")
	statsCmd.Flags().BoolVar(&jsonOutStats, "json", false, "JSON 格式输出")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	startDate, endDate := getDateRange(period)

	// JSON 输出模式
	if jsonOutStats {
		outputStatsJSON(c, startDate, endDate)
		return
	}

	m := stats.NewModel(c, startDate, endDate)
	m.By = by

	// 启动 TUI
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)

	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}

// outputStatsJSON 以 JSON 格式输出统计
func outputStatsJSON(c *client.Client, startDate, endDate string) {
	if by == "team" {
		resp, err := c.GetTeamDailyActivity(startDate, endDate)
		if err != nil {
			log.Fatal(err)
		}
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		resp, err := c.GetUserDailyActivity(startDate, endDate, 0, 1)
		if err != nil {
			log.Fatal(err)
		}
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(jsonBytes))
	}
}

func getDateRange(p string) (string, string) {
	// 如果提供了自定义日期，使用自定义日期
	if fromDate != "" && toDate != "" {
		// 验证日期格式
		from, err := time.Parse("2006-01-02", fromDate)
		if err != nil {
			log.Fatalf("无效的开始日期格式: %s, 应使用 YYYY-MM-DD", fromDate)
		}
		to, err := time.Parse("2006-01-02", toDate)
		if err != nil {
			log.Fatalf("无效的结束日期格式: %s, 应使用 YYYY-MM-DD", toDate)
		}
		if from.After(to) {
			log.Fatal("开始日期不能晚于结束日期")
		}
		return fromDate, toDate
	}

	now := time.Now()
	endDate := now.Format("2006-01-02")

	var startDate time.Time
	switch p {
	case "week":
		startDate = now.AddDate(0, 0, -7)
	case "month":
		startDate = now.AddDate(0, -1, 0)
	case "day":
		startDate = now.AddDate(0, 0, -1) // 昨天到今天
	default:
		startDate = now.AddDate(0, 0, -1)
	}

	return startDate.Format("2006-01-02"), endDate
}
