package cmd

import (
	"fmt"
	"log"
	"time"

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
	cfg, err := config.Load()
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

	// 显示汇总
	fmt.Println("\n📈 汇总")
	fmt.Printf("   💰 花费: $%.4f\n", totalSpend)
	fmt.Printf("   📝 Prompt Tokens: %d\n", totalPrompt)
	fmt.Printf("   ✍️ Completion Tokens: %d\n", totalCompletion)
	fmt.Printf("   📊 总 Tokens: %d\n", totalTokens)
	fmt.Printf("   ✅ 成功请求: %d\n", totalSuccess)
	fmt.Printf("   ❌ 失败请求: %d\n", totalFailed)
	fmt.Printf("   📤 总请求: %d\n", totalRequests)

	// 显示最近几天的明细
	if len(resp.Results) > 1 {
		fmt.Println("\n📅 最近几天:")
		for i := 0; i < min(5, len(resp.Results)); i++ {
			r := resp.Results[i]
			fmt.Printf("   %s: $%.4f (%d 请求)\n", r.Date, r.Metrics.Spend, r.Metrics.APIRequests)
		}
	}
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
	fmt.Printf("   📝 Prompt Tokens: %d\n", latest.Metrics.PromptTokens)
	fmt.Printf("   ✍️ Completion Tokens: %d\n", latest.Metrics.CompletionTokens)
	fmt.Printf("   📊 总 Tokens: %d\n", latest.Metrics.TotalTokens)
	fmt.Printf("   ✅ 成功请求: %d\n", latest.Metrics.SuccessfulRequests)
	fmt.Printf("   ❌ 失败请求: %d\n", latest.Metrics.FailedRequests)
	fmt.Printf("   📤 总请求: %d\n", latest.Metrics.APIRequests)

	// 按模型显示
	if len(latest.Breakdown.Models) > 0 {
		fmt.Println("\n📦 按模型:")
		for model, data := range latest.Breakdown.Models {
			fmt.Printf("   %s: $%.4f (%d tokens)\n", model, data.Metrics.Spend, data.Metrics.TotalTokens)
		}
	}
}