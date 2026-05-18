#!/bin/bash

# BitfinexLendingBot 测试脚本

set -e

echo "🧪 开始运行测试套件..."

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 函数：打印带颜色的消息
print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

# 检查 Go 版本
echo "🔍 检查 Go 环境..."
if ! command -v go &> /dev/null; then
    print_error "Go 未安装或不在 PATH 中"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
print_status "Go 版本: $GO_VERSION"

# 运行 go mod tidy
echo "📦 整理模组依赖..."
go mod tidy
print_status "依赖整理完成"

# 运行 go vet
echo "🔍 运行静态分析 (go vet)..."
if go vet ./...; then
    print_status "静态分析通过"
else
    print_error "静态分析发现问题"
    exit 1
fi

# 运行测试
echo "🧪 运行单元测试..."
if go test -v ./... -race -coverprofile=coverage.out; then
    print_status "所有测试通过"
else
    print_error "测试失败"
    exit 1
fi

# 生成测试覆盖率报告
echo "📊 生成测试覆盖率报告..."
if go tool cover -html=coverage.out -o coverage.html; then
    print_status "覆盖率报告已生成: coverage.html"
else
    print_warning "无法生成覆盖率报告"
fi

# 显示简要覆盖率统计
if go tool cover -func=coverage.out | tail -1; then
    print_status "测试覆盖率统计完成"
fi

# 运行格式检查
echo "🎨 检查代码格式..."
UNFORMATTED=$(gofmt -l .)
if [ -z "$UNFORMATTED" ]; then
    print_status "代码格式正确"
else
    print_warning "以下文件需要格式化:"
    echo "$UNFORMATTED"
    echo "运行 'gofmt -w .' 来修复格式问题"
fi

# 检查是否有未提交的 go.mod 或 go.sum 变更
echo "📋 检查模组文件变更..."
if git diff --exit-code go.mod go.sum; then
    print_status "模组文件无变更"
else
    print_warning "go.mod 或 go.sum 有变更，请检查并提交"
fi

# 编译检查
echo "🔨 检查编译..."
if go build -o /tmp/bitfinex-lending-bot-test .; then
    print_status "编译成功"
    rm -f /tmp/bitfinex-lending-bot-test
else
    print_error "编译失败"
    exit 1
fi

echo ""
echo "🎉 所有检查完成！"
echo ""
echo "📋 测试总结:"
echo "   ✓ 静态分析通过"
echo "   ✓ 单元测试通过"
echo "   ✓ 编译成功"
echo "   📊 覆盖率报告: coverage.html"
echo ""
echo "💡 提示："
echo "   - 运行 'go test -v ./...' 来重新运行测试"
echo "   - 运行 'go test -bench=.' 来运行性能测试（如果有的话）"
echo "   - 查看 coverage.html 了解详细的测试覆盖率"