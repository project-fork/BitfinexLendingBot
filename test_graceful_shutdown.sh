#!/bin/bash

# 测试优雅关闭功能

echo "🧪 测试优雅关闭功能..."

# 检查配置文件是否存在
if [ ! -f "config.yaml" ]; then
    echo "⚠️  config.yaml 不存在，复制示例配置..."
    cp config.yaml.example config.yaml
fi

# 编译程序
echo "🔨 编译程序..."
go build -o test-graceful-shutdown .

if [ $? -ne 0 ]; then
    echo "❌ 编译失败"
    exit 1
fi

echo "✅ 编译成功"

# 启动程序（背景运行）
echo "🚀 启动程序..."
./test-graceful-shutdown -c config.yaml &
PID=$!

echo "📝 程序 PID: $PID"

# 等待几秒让程序完全启动
echo "⏳ 等待程序启动..."
sleep 5

# 检查程序是否还在运行
if ! kill -0 $PID 2>/dev/null; then
    echo "❌ 程序未能正常启动"
    exit 1
fi

echo "✅ 程序已启动"

# 发送 SIGINT 信号 (Ctrl+C)
echo "📤 发送 SIGINT 信号进行优雅关闭..."
kill -INT $PID

# 等待程序关闭，最多等待 15 秒
echo "⏳ 等待程序关闭（最多15秒）..."
for i in {1..15}; do
    if ! kill -0 $PID 2>/dev/null; then
        echo "✅ 程序在 ${i} 秒内优雅关闭"
        break
    fi
    sleep 1
    if [ $i -eq 15 ]; then
        echo "❌ 程序未在 15 秒内关闭，强制终止..."
        kill -KILL $PID 2>/dev/null
        echo "❌ 优雅关闭测试失败"
        exit 1
    fi
done

echo "🎉 优雅关闭测试成功！"

# 清理
rm -f test-graceful-shutdown

echo "✨ 测试完成"