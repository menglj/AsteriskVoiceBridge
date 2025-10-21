#!/bin/bash

echo "=== AI助手系统依赖修复脚本 ==="
echo ""

# 检查Go环境
if ! command -v go &> /dev/null; then
    echo "错误: 未找到Go环境，请先安装Go"
    exit 1
fi

echo "1. 清理旧的依赖文件..."
rm -f go.sum

echo "2. 下载依赖包..."
go mod download

echo "3. 验证模块..."
go mod verify

echo "4. 整理依赖..."
go mod tidy

echo "5. 检查Redis模块..."
if [ -d "redis" ]; then
    echo "   Redis模块存在"
    cd redis
    go mod tidy
    cd ..
else
    echo "   错误: Redis模块不存在"
    exit 1
fi

echo "6. 尝试编译..."
if go build -o /tmp/test-build .; then
    echo "   ✅ 编译成功"
    rm -f /tmp/test-build
else
    echo "   ❌ 编译失败"
    echo "   请检查错误信息并修复"
    exit 1
fi

echo ""
echo "=== 依赖修复完成 ==="
echo "现在可以运行: go install ."
echo "或者运行: go run ."