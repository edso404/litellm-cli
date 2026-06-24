package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "litellm-cli",
	Short: "LiteLLM CLI - 查看 API 用量统计和日志",
	Long: `LiteLLM CLI 工具
- stats: 查看用量统计
- logs: 轮询查看日志
- models: 查看可用模型`,
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