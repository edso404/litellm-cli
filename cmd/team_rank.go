package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
	"litellm-cli/internal/api"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
)

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "查看团队用量排行榜",
	Run:   runTeam,
}

var teamID string
var jsonOutTeam bool

func init() {
	teamCmd.Flags().StringVar(&teamID, "team-id", "", "团队 ID (不指定则显示所有团队)")
	teamCmd.Flags().BoolVar(&jsonOutTeam, "json", false, "JSON 格式输出")
	rootCmd.AddCommand(teamCmd)
}

func runTeam(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	// 获取用户信息（包含团队列表）
	userInfo, err := c.GetUserInfo()
	if err != nil {
		log.Fatalf("获取用户信息失败: %v", err)
	}

	if len(userInfo.Teams) == 0 {
		log.Fatal("没有找到所属团队")
	}

	// 找到目标团队
	var targetTeam *api.UserTeam
	if teamID != "" {
		for i := range userInfo.Teams {
			if userInfo.Teams[i].TeamID == teamID {
				targetTeam = &userInfo.Teams[i]
				break
			}
		}
		if targetTeam == nil {
			log.Fatalf("未找到团队: %s", teamID)
		}
	} else {
		// 默认使用第一个团队
		targetTeam = &userInfo.Teams[0]
	}

	// JSON 输出模式
	if jsonOutTeam {
		outputTeamRankJSON(targetTeam, userInfo.UserID)
		return
	}

	printTeamLeaderboard(targetTeam, userInfo.UserID)
}

// outputTeamRankJSON 以 JSON 格式输出团队排行
func outputTeamRankJSON(team *api.UserTeam, currentUserID string) {
	// 构建 user_id -> email 映射
	userEmailMap := make(map[string]string)
	for _, m := range team.MembersWithRoles {
		userEmailMap[m.UserID] = m.Email
	}

	// 按 user_id 聚合用量
	userSpend := make(map[string]float64)
	for _, k := range team.Keys {
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
	type userRank struct {
		UserID  string  `json:"user_id"`
		Email   string  `json:"email"`
		Spend   float64 `json:"spend"`
		Percent float64 `json:"percent"`
		Rank    int     `json:"rank"`
		IsMe    bool    `json:"is_me"`
	}
	var ranks []userRank
	for uid, spend := range userSpend {
		email := userEmailMap[uid]
		if email == "" {
			email = uid[:8] + "..."
		}
		percent := 0.0
		if totalSpend > 0 {
			percent = (spend / totalSpend) * 100
		}
		ranks = append(ranks, userRank{
			UserID:  uid,
			Email:   email,
			Spend:   spend,
			Percent: percent,
			IsMe:    uid == currentUserID,
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

	// 输出 JSON
	type output struct {
		TeamID      string     `json:"team_id"`
		TeamAlias   string     `json:"team_alias"`
		TotalSpend  float64    `json:"total_spend"`
		CurrentRank *userRank   `json:"current_user_rank,omitempty"`
		Ranks       []userRank  `json:"ranks"`
	}
	var currentRank *userRank
	for i := range ranks {
		if ranks[i].IsMe {
			currentRank = &ranks[i]
			break
		}
	}
	o := output{
		TeamID:      team.TeamID,
		TeamAlias:   team.TeamAlias,
		TotalSpend:  totalSpend,
		CurrentRank: currentRank,
		Ranks:       ranks,
	}
	jsonBytes, _ := json.MarshalIndent(o, "", "  ")
	fmt.Println(string(jsonBytes))
}

func printTeamLeaderboard(team *api.UserTeam, currentUserID string) {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("236"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("51"))

	// 使用实际聚合的用户用量作为团队总用量
	teamAlias := team.TeamAlias

	// 构建 user_id -> email 映射
	userEmailMap := make(map[string]string)
	for _, m := range team.MembersWithRoles {
		userEmailMap[m.UserID] = m.Email
	}

	// 按 user_id 聚合用量
	userSpend := make(map[string]float64)
	for _, k := range team.Keys {
		if k.UserID != "" {
			userSpend[k.UserID] += k.Spend
		}
	}

	// 计算团队总用量（使用实际聚合的用量）
	var calculatedTeamSpend float64
	for _, s := range userSpend {
		calculatedTeamSpend += s
	}

	// 样式
	greenStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("76"))
	yellowStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("226"))

	// 转换为排序切片
	type userRank struct {
		userID  string
		email   string
		spend   float64
		percent float64
	}
	var ranks []userRank
	for uid, spend := range userSpend {
		email := userEmailMap[uid]
		if email == "" {
			email = uid[:8] + "..."
		}
		percent := 0.0
		if calculatedTeamSpend > 0 {
			percent = (spend / calculatedTeamSpend) * 100
		}
		ranks = append(ranks, userRank{
			userID:  uid,
			email:   email,
			spend:   spend,
			percent: percent,
		})
	}

	// 按用量降序排序
	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].spend > ranks[j].spend
	})

	// 打印
	fmt.Println()
	fmt.Println(headerStyle.Render(fmt.Sprintf(" 🏆 %s 用量排行榜 ", teamAlias)))
	fmt.Println()
	fmt.Println(contentStyle.Render(fmt.Sprintf(" 团队总用量: %s", greenStyle.Render(fmt.Sprintf("$%.2f", calculatedTeamSpend)))))
	fmt.Println()

	// 计算列宽（使用 runewidth 计算显示宽度）
	rankColWidth := 4
	emailColWidth := 30
	spendColWidth := 11
	percentColWidth := 8

	// 辅助函数：用空格填充到指定显示宽度
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
	fmt.Printf("  %s %s %s %s\n",
		padRight("排名", rankColWidth),
		padRight("用户", emailColWidth),
		padLeft("用量", spendColWidth),
		padLeft("占比", percentColWidth))
	fmt.Println(mutedStyle.Render(" " + strings.Repeat("─", 65)))

	// 找出当前用户排名
	var myRank int
	var mySpend float64
	var myPercent float64

	for i, r := range ranks {
		rank := i + 1
		email := r.email
		// 使用 runewidth 处理中文字符宽度
		emailWidth := runewidth.StringWidth(email)
		if emailWidth > emailColWidth {
			// 截断并添加省略号
			email = runewidth.Truncate(email, emailColWidth-3, "...")
		}

		// 高亮当前用户
		isMe := r.userID == currentUserID
		rankStr := fmt.Sprintf("#%d", rank)

		if isMe {
			rankStr = "→" + fmt.Sprintf("%d", rank)
			myRank = rank
			mySpend = r.spend
			myPercent = r.percent
		}

		percentStr := fmt.Sprintf("%.1f%%", r.percent)
		spendStr := fmt.Sprintf("$%.2f", r.spend)

		// 使用 runewidth 精确对齐
		rankPadded := padRight(rankStr, rankColWidth)
		emailPadded := padRight(email, emailColWidth)
		spendPadded := padLeft(spendStr, spendColWidth)
		percentPadded := padLeft(percentStr, percentColWidth)

		// 渲染带颜色的部分
		if isMe {
			fmt.Printf("  %s %s %s %s\n",
				cyanStyle.Bold(true).Render(rankPadded),
				emailPadded,
				greenStyle.Render(spendPadded),
				yellowStyle.Render(percentPadded),
			)
		} else {
			fmt.Printf("  %s %s %s %s\n",
				contentStyle.Render(rankPadded),
				emailPadded,
				greenStyle.Render(spendPadded),
				yellowStyle.Render(percentPadded),
			)
		}
	}

	// 显示我的排名统计
	if myRank > 0 {
		fmt.Println()
		fmt.Println(cyanStyle.Render(fmt.Sprintf(" 📊 你的排名: #%d / %d", myRank, len(ranks))))
		fmt.Println(cyanStyle.Render(fmt.Sprintf("    你的用量: %s (占总用量 %.1f%%)", greenStyle.Render(fmt.Sprintf("$%.2f", mySpend)), myPercent)))
	}

	// 图示化
	if len(ranks) > 0 && calculatedTeamSpend > 0 {
		fmt.Println()
		fmt.Println(mutedStyle.Render(" 用量分布:"))
		barWidth := 30
		for i, r := range ranks {
			if i >= 10 { // 只显示 top 10
				break
			}
			barLen := int((r.spend / calculatedTeamSpend) * float64(barWidth))
			if barLen == 0 && r.spend > 0 {
				barLen = 1
			}
			bar := strings.Repeat("█", barLen)
			if r.userID == currentUserID {
				fmt.Printf("  %s %s\n", cyanStyle.Render(bar), mutedStyle.Render(fmt.Sprintf(" ← 你 (%.1f%%)", r.percent)))
			} else {
				fmt.Printf("  %s\n", contentStyle.Render(bar))
			}
		}
	}

	fmt.Println()
}