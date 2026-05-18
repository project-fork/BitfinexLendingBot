# BitfinexLendingBot Makefile

.PHONY: build test clean run dev format lint help install

# 默认目标
.DEFAULT_GOAL := help

# 变量定义
BINARY_NAME=bitfinex-lending-bot
BUILD_DIR=build
CONFIG_FILE=config.yaml

# 颜色定义
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

## build: 编译应用程序
build:
	@echo "$(GREEN)🔨 编译应用程序...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "$(GREEN)✓ 编译完成: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

## build: 编译linux应用程序
build-linux:
	@echo "$(GREEN)🔨 编译应用程序...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "$(GREEN)✓ 编译完成: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

## test: 运行测试套件
test:
	@echo "$(GREEN)🧪 运行测试套件...$(NC)"
	@./test.sh

## test-quick: 快速测试（不包含覆盖率）
test-quick:
	@echo "$(GREEN)⚡ 快速测试...$(NC)"
	@go test ./... -short

## test-verbose: 详细测试输出
test-verbose:
	@echo "$(GREEN)🔍 详细测试...$(NC)"
	@go test -v ./...

## run: 运行应用程序（生产模式）
run: build
	@echo "$(GREEN)🚀 运行应用程序...$(NC)"
	@./$(BUILD_DIR)/$(BINARY_NAME) -c $(CONFIG_FILE)

## dev: 开发模式运行（测试模式）
dev:
	@echo "$(YELLOW)🧪 开发模式运行（测试模式）...$(NC)"
	@go run . -c $(CONFIG_FILE)

## format: 格式化代码
format:
	@echo "$(GREEN)🎨 格式化代码...$(NC)"
	@gofmt -w .
	@echo "$(GREEN)✓ 代码格式化完成$(NC)"

## lint: 代码静态分析
lint:
	@echo "$(GREEN)🔍 代码静态分析...$(NC)"
	@go vet ./...
	@echo "$(GREEN)✓ 静态分析完成$(NC)"

## mod-tidy: 整理模组依赖
mod-tidy:
	@echo "$(GREEN)📦 整理模组依赖...$(NC)"
	@go mod tidy
	@go mod vendor
	@echo "$(GREEN)✓ 依赖整理完成$(NC)"

## clean: 清理编译文件
clean:
	@echo "$(GREEN)🧹 清理编译文件...$(NC)"
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "$(GREEN)✓ 清理完成$(NC)"

## install: 安装到系统路径
install: build
	@echo "$(GREEN)📥 安装应用程序...$(NC)"
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "$(GREEN)✓ 安装完成: /usr/local/bin/$(BINARY_NAME)$(NC)"

## uninstall: 从系统路径卸载
uninstall:
	@echo "$(GREEN)📤 卸载应用程序...$(NC)"
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "$(GREEN)✓ 卸载完成$(NC)"

## docker-build: 构建 Docker 镜像
docker-build:
	@echo "$(GREEN)🐳 构建 Docker 镜像...$(NC)"
	@docker build -t $(BINARY_NAME):latest .
	@echo "$(GREEN)✓ Docker 镜像构建完成$(NC)"

## config-example: 复制配置示例
config-example:
	@echo "$(GREEN)📋 复制配置示例...$(NC)"
	@cp config.yaml.example config.yaml
	@echo "$(YELLOW)⚠ 请编辑 config.yaml 填入您的 API 密钥$(NC)"

## security-check: 安全检查
security-check:
	@echo "$(GREEN)🔒 安全检查...$(NC)"
	@echo "检查是否有敏感信息..."
	@! git log --oneline | grep -i "api\|key\|secret\|token" || echo "$(YELLOW)⚠ 发现可能包含敏感信息的提交$(NC)"
	@! find . -name "*.go" -o -name "*.yaml" -o -name "*.yml" | xargs grep -l "api.*key\|secret.*key" | grep -v "_test.go" | grep -v "config.yaml.example" || echo "$(YELLOW)⚠ 发现可能包含敏感信息的文件$(NC)"
	@echo "$(GREEN)✓ 安全检查完成$(NC)"

## deps: 检查和更新依赖
deps:
	@echo "$(GREEN)🔍 检查依赖...$(NC)"
	@go list -u -m all
	@echo "$(GREEN)📥 下载依赖...$(NC)"
	@go mod download

## release: 构建发布版本
release: clean test build
	@echo "$(GREEN)🚀 构建发布版本...$(NC)"
	@mkdir -p $(BUILD_DIR)/release
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(BUILD_DIR)/release/
	@cp config.yaml.example $(BUILD_DIR)/release/
	@cp README.md $(BUILD_DIR)/release/
	@cp SECURITY.md $(BUILD_DIR)/release/
	@cd $(BUILD_DIR)/release && tar -czf ../$(BINARY_NAME)-release.tar.gz .
	@echo "$(GREEN)✓ 发布包已生成: $(BUILD_DIR)/$(BINARY_NAME)-release.tar.gz$(NC)"

## help: 显示帮助信息
help:
	@echo "$(GREEN)BitfinexLendingBot - 可用命令:$(NC)"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@echo ""
	@echo "$(YELLOW)使用示例:$(NC)"
	@echo "  make config-example  # 创建配置文件"
	@echo "  make dev            # 开发模式运行"
	@echo "  make test           # 运行完整测试"
	@echo "  make build          # 编译应用程序"
	@echo "  make run            # 运行应用程序"