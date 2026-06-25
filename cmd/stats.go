package cmd

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
)

var (
	period string
	by     string
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "查看用量统计",
	Run:   runStats,
}

func init() {
	statsCmd.Flags().StringVar(&period, "period", "day", "统计周期: day, week, month")
	statsCmd.Flags().StringVar(&by, "by", "user", "聚合维度: user, team, model")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	startDate, endDate := getDateRange(period)

	switch by {
	case "user":
		printUserStats(c, startDate, endDate)
	case "team":
		printTeamStats(c, startDate, endDate)
	default:
		printUserStats(c, startDate, endDate)
	}
}

func getDateRange(period string) (string, string) {
	now := time.Now()
	endDate := now.Format("2006-01-02")

	var startDate time.Time
	switch period {
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

func printUserStats(c *client.Client, startDate, endDate string) {
	resp, err := c.GetUserDailyActivity(startDate, endDate)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n📊 用户用量统计 (%s - %s)\n", startDate, endDate)
	fmt.Println("========================================")

	if len(resp.Results) == 0 {
		fmt.Println("暂无数据")
		return
	}

	// 计算整个时间段的汇总
	var totalSpend float64
	var totalPrompt, totalCompletion, totalTokens int64
	var totalSuccess, totalFailed, totalRequests int64

	for _, r := range resp.Results {
		totalSpend += r.Metrics.Spend
		totalPrompt += r.Metrics.PromptTokens
		totalCompletion += r.Metrics.CompletionTokens
		totalTokens += r.Metrics.TotalTokens
		totalSuccess += r.Metrics.SuccessfulRequests
		totalFailed += r.Metrics.FailedRequests
		totalRequests += r.Metrics.APIRequests
	}

	// 辅助函数：按显示宽度填充
	padRight := func(s string, width int) string {
		w := runewidth.StringWidth(s)
		if w >= width {
			return s
		}
		return s + strings.Repeat(" ", width-w)
	}

	// 显示汇总
	fmt.Println("\n📈 汇总")
	fmt.Printf("   💰 花费: $%.4f\n", totalSpend)
	fmt.Printf("   📝 Prompt Tokens: %s\n", formatTokens(totalPrompt))
	fmt.Printf("   ✍️ Completion Tokens: %s\n", formatTokens(totalCompletion))
	fmt.Printf("   📊 总 Tokens: %s\n", formatTokens(totalTokens))
	fmt.Printf("   ✅ 成功请求: %d\n", totalSuccess)
	fmt.Printf("   ❌ 失败请求: %d\n", totalFailed)
	fmt.Printf("   📤 总请求: %d\n", totalRequests)

	// 显示最近几天的明细
	if len(resp.Results) > 1 {
		fmt.Println("\n📅 最近几天:")
		fmt.Printf("   %s %s %s %s %s %s %s\n",
			padRight("日期", 10),
			padRight("Cost", 8),
			padRight("Requests", 8),
			padRight("Input", 8),
			padRight("Output", 8),
			padRight("Total", 8),
			padRight("成功率", 8))
		fmt.Println("   " + strings.Repeat("-", 65))

		days := min(7, len(resp.Results))
		for i := 0; i < days; i++ {
			r := resp.Results[i]
			successRate := 0.0
			if r.Metrics.APIRequests > 0 {
				successRate = float64(r.Metrics.SuccessfulRequests) / float64(r.Metrics.APIRequests) * 100
			}
			fmt.Printf("   %s %s %d %s %s %s %.1f%%\n",
				padRight(r.Date, 10),
				padRight(fmt.Sprintf("$%.2f", r.Metrics.Spend), 8),
				r.Metrics.APIRequests,
				padRight(formatTokens(r.Metrics.PromptTokens), 8),
				padRight(formatTokens(r.Metrics.CompletionTokens), 8),
				padRight(formatTokens(r.Metrics.TotalTokens), 8),
				successRate,
			)
		}
	}
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	} else if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func printTeamStats(c *client.Client, startDate, endDate string) {
	resp, err := c.GetTeamDailyActivity(startDate, endDate)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n📊 团队用量统计 (%s - %s)\n", startDate, endDate)
	fmt.Println("========================================")

	if len(resp.Results) == 0 {
		fmt.Println("暂无数据")
		return
	}

	// 显示最新一天的数据
	latest := resp.Results[0]
	fmt.Printf("\n📅 %s\n", latest.Date)
	fmt.Printf("   💰 花费: $%.4f\n", latest.Metrics.Spend)
	fmt.Printf("   📝 Prompt Tokens: %s\n", formatTokens(latest.Metrics.PromptTokens))
	fmt.Printf("   ✍️ Completion Tokens: %s\n", formatTokens(latest.Metrics.CompletionTokens))
	fmt.Printf("   📊 总 Tokens: %s\n", formatTokens(latest.Metrics.TotalTokens))
	fmt.Printf("   ✅ 成功请求: %d\n", latest.Metrics.SuccessfulRequests)
	fmt.Printf("   ❌ 失败请求: %d\n", latest.Metrics.FailedRequests)
	fmt.Printf("   📤 总请求: %d\n", latest.Metrics.APIRequests)

	// 按模型显示
	if len(latest.Breakdown.Models) > 0 {
		fmt.Println("\n📦 按模型:")
		for model, data := range latest.Breakdown.Models {
			fmt.Printf("   %s: $%.4f (%s tokens)\n", model, data.Metrics.Spend, formatTokens(data.Metrics.TotalTokens))
		}
	}
}