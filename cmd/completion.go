package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [shell]",
	Short: "生成 Shell 补全脚本",
	Long: `为指定 shell 生成补全脚本。

支持的 shell:
  - bash
  - zsh
  - fish
  - powershell

示例:
  # Bash
  source <(litellm-cli completion bash)

  # Zsh
  source <(litellm-cli completion zsh)

  # Fish
  litellm-cli completion fish > ~/.config/fish/completions/litellm-cli.fish

  # PowerShell
  litellm-cli completion powershell > litellm-cli.ps1
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
