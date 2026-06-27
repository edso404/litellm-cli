package cmd

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
)

var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "查看可用团队列表",
	Run:   runTeams,
}

var jsonOutTeams bool

func init() {
	teamsCmd.Flags().BoolVar(&jsonOutTeams, "json", false, "JSON 格式输出")
	rootCmd.AddCommand(teamsCmd)
}

func runTeams(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	resp, err := c.GetUserInfo()
	if err != nil {
		log.Fatal(err)
	}

	// JSON 输出模式
	if jsonOutTeams {
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(jsonBytes))
		return
	}

	fmt.Println("\n👥 可用团队列表:")
	fmt.Println("=================")

	if resp == nil || len(resp.Teams) == 0 {
		fmt.Println("暂无数据")
		return
	}

	for _, team := range resp.Teams {
		alias := team.TeamAlias
		if alias == "" {
			alias = team.TeamID
		}
		memberCount := len(team.MembersWithRoles)
		fmt.Printf("  • %s (ID: %s, 成员: %d)\n", alias, team.TeamID, memberCount)
	}

	fmt.Printf("\n共 %d 个团队\n", len(resp.Teams))
}