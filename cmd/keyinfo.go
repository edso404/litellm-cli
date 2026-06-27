package cmd

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
)

var keyInfoCmd = &cobra.Command{
	Use:   "keyinfo",
	Short: "查看当前 Key 详情",
	Run:   runKeyInfo,
}

var jsonOutKeyinfo bool

func init() {
	keyInfoCmd.Flags().BoolVar(&jsonOutKeyinfo, "json", false, "JSON 格式输出")
	rootCmd.AddCommand(keyInfoCmd)
}

func runKeyInfo(cmd *cobra.Command, args []string) {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	resp, err := c.GetKeyInfo(c.GetAPIKey())
	if err != nil {
		log.Fatal(err)
	}

	// JSON 输出模式
	if jsonOutKeyinfo {
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(jsonBytes))
		return
	}

	fmt.Println("\n🔑 Key 详情:")
	fmt.Println("=============")
	fmt.Printf("  Key Alias: %s\n", resp.Info.KeyAlias)
	fmt.Printf("  Key Name:  %s\n", resp.Info.KeyName)
	fmt.Printf("  💰 已花费: $%.4f\n", resp.Info.Spend)
	fmt.Printf("  👤 User ID: %s\n", resp.Info.UserID)
	fmt.Printf("  👥 Team ID: %s\n", resp.Info.TeamID)
	fmt.Printf("  📅 创建时间: %s\n", resp.Info.CreatedAt)
	fmt.Printf("  🕐 最后活跃: %s\n", resp.Info.LastActive)

	if len(resp.Info.Models) > 0 {
		fmt.Println("  📦 可用模型:")
		for _, m := range resp.Info.Models {
			fmt.Printf("    • %s\n", m)
		}
	}
}