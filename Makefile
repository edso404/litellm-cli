.PHONY: build install clean test release

# 默认目标
all: build

# 构建二进制
build:
	go build -ldflags "-s -w" -o litellm-cli .

# 安装到 ~/.local/bin/
install: build
	@mkdir -p ~/.local/bin/
	@mv litellm-cli ~/.local/bin/
	@echo "✅ 已安装到 ~/.local/bin/litellm-cli"

# 发布构建 (用于分发)
release:
	@mkdir -p release
	GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o litellm-cli-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o litellm-cli-darwin-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o litellm-cli-linux-amd64 .
	@echo "✅ Release 构建完成:"
	@ls -lh litellm-cli-*

# 清理
clean:
	rm -f litellm-cli
	rm -rf release
	rm -f litellm-cli-darwin-amd64 litellm-cli-darwin-arm64 litellm-cli-linux-amd64

# 测试
test:
	go test -v ./...

# 查看帮助
help:
	@echo "LiteLLM CLI - Make targets:"
	@echo ""
	@echo "  make build     - 构建二进制"
	@echo "  make install   - 构建并安装到 ~/.local/bin/"
	@echo "  make release   - 构建发布版本 (darwin/linux)"
	@echo "  make clean     - 清理构建文件"
	@echo "  make test      - 运行测试"
	@echo ""