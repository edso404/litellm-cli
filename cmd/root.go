package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
	"litellm-cli/internal/tui/dashboard"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "litellm-cli",
	Short: "LiteLLM CLI - 查看 API 用量统计和日志",
	Long: `LiteLLM CLI 工具
- 直接运行启动 Dashboard
- stats: 查看用量统计
- logs: 轮询查看日志
- models: 查看可用模型`,
	Run: func(cmd *cobra.Command, args []string) {
		// 没有子命令时启动 Dashboard
		runDashboard()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径 (默认 ~/.litellm-cli.yaml)")
	rootCmd.PersistentFlags().String("api-key", "", "API Key (或设置 LITELLM_API_KEY 环境变量)")
	rootCmd.PersistentFlags().String("base-url", "http://localhost:4000", "API 基础地址")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "获取用户目录失败:", err)
			os.Exit(1)
		}
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".litellm-cli")
	}

	viper.AutomaticEnv()

	// Bind flags
	viper.BindPFlag("api_key", rootCmd.PersistentFlags().Lookup("api-key"))
	viper.BindPFlag("base_url", rootCmd.PersistentFlags().Lookup("base-url"))

	// 设置默认值
	viper.SetDefault("base_url", "http://localhost:4000")

	if err := viper.ReadInConfig(); err == nil {
		// 配置文件存在
	} else if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
		fmt.Fprintln(os.Stderr, "读取配置文件失败:", err)
	}
}

// runDashboard 启动 Dashboard TUI
func runDashboard() {
	cfg, err := config.LoadWithAutoLogin()
	if err != nil {
		log.Fatal(err)
	}

	c := client.New(cfg)

	p := tea.NewProgram(
		dashboard.NewModel(c.API(), c.GetAPIKey()),
		tea.WithAltScreen(),
	)

	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}
