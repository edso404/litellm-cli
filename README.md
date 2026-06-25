# LiteLLM CLI

LiteLLM CLI 是一个用于查看 LiteLLM 用量、日志、模型、Key 信息和团队信息的命令行工具。项目使用 Go、Cobra 和 Bubble Tea 构建，支持普通命令输出和日志 TUI 视图。

## 功能

- 查看用户或团队维度的用量统计
- 轮询查看实时请求日志，并支持 TUI 详情视图
- 查询当前 Key 信息和可用模型列表
- 查看用户可访问团队与团队用量排行
- 支持用户名密码登录、API Key、环境变量和配置文件
- 生成 Bash、Zsh、Fish、PowerShell 补全脚本

## 安装

需要 Go 1.24 或更高版本。

```bash
make build
./litellm-cli --help
```

安装到 `~/.local/bin`：

```bash
make install
```

也可以直接本地运行：

```bash
go run . models
```

## 配置

最简单的方式是设置 API Key：

```bash
export LITELLM_API_KEY="your-api-key"
```

可选配置项：

```bash
export LITELLM_BASE_URL="https://litellm.com"
export LITELLM_USERNAME="your-username"
export LITELLM_PASSWORD="your-password"
```

命令行参数会覆盖默认配置：

```bash
litellm-cli --api-key "$LITELLM_API_KEY" --base-url "$LITELLM_BASE_URL" models
```

默认配置文件路径是 `~/.litellm-cli.yaml`。使用用户名密码登录时，工具会将 token 缓存在 `~/.litellm-cli-cache`，缓存有效期为 24 小时。

## 常用命令

```bash
litellm-cli login -u <username> -p <password>
litellm-cli models
litellm-cli keyinfo
litellm-cli teams
litellm-cli team --team-id <team-id>
```

查看用量统计：

```bash
litellm-cli stats --period day --by user
litellm-cli stats --period week --by team
```

查看实时日志：

```bash
litellm-cli logs
litellm-cli logs --interval 3 --model gpt
litellm-cli logs --verbose
```

生成 shell 补全：

```bash
source <(litellm-cli completion zsh)
```

## 开发

常用开发命令：

```bash
make test      # go test -v -race ./...
make build     # 构建 litellm-cli
make release   # 构建 darwin/linux 发布产物
make clean     # 清理生成文件
```

项目结构：

```text
cmd/              Cobra 命令和 TUI 实现
internal/api/     LiteLLM API 类型和底层请求封装
internal/client/  面向命令层的客户端方法
internal/config/  配置读取、登录和 token 缓存
docs/             设计、计划和 ideation 文档
```

测试文件与被测包放在同一目录，命名为 `*_test.go`。提交前建议运行 `make test`。

## 安全提示

不要提交 API Key、用户名密码、token 缓存或个人配置文件。开启 `--verbose` 时会生成 `litellm-cli.log`，提交前请检查其中是否包含敏感信息。
