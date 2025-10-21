#!/bin/bash

echo "=== AI助手Redis调试脚本 ==="
echo ""

# 检查Redis服务
echo "1. 检查Redis服务..."
if redis-cli ping > /dev/null 2>&1; then
    echo "   ✅ Redis服务正常"
else
    echo "   ❌ Redis服务异常"
    exit 1
fi

echo ""
echo "2. 检查AI助手系统日志中的关键信息..."
echo "   请查看AI助手系统日志，寻找以下关键信息:"
echo ""
echo "   Redis连接相关:"
echo "   - 'Redis client initialized successfully'"
echo "   - 'Redis client connected'"
echo "   - 'Failed to create Redis client'"
echo ""
echo "   STT识别相关:"
echo "   - 'CachedTranscript' (应该看到客户说话的原文)"
echo "   - 'SkippedCaching' (如果看到这个，说明STT结果没有被缓存)"
echo ""
echo "   翻译和Redis推送相关:"
echo "   - 'AttemptingRedisPush' (尝试推送到Redis)"
echo "   - 'RedisPushDetails' (Redis推送的详细信息)"
echo "   - 'PushedToRedis' (成功推送到Redis)"
echo "   - 'Failed to push client message' (推送失败)"
echo "   - 'Redis client is nil' (Redis客户端为空)"
echo "   - 'lastTranscript is empty' (缓存的原文为空)"

echo ""
echo "3. 手动测试Redis功能..."
echo "   运行Redis测试脚本:"
echo "   chmod +x test-redis-functionality.sh"
echo "   ./test-redis-functionality.sh"

echo ""
echo "4. 检查Redis队列内容..."
echo "   使用以下命令检查Redis队列:"
echo "   redis-cli LRANGE 'client.meng.astercc.com:AI:AGENT:1100:RECEIVE:*' 0 -1"
echo "   redis-cli LRANGE 'client.meng.astercc.com:AI:AGENT:1100:RESPONSE:*' 0 -1"

echo ""
echo "5. 常见问题排查..."
echo ""
echo "   问题1: Redis客户端初始化失败"
echo "   解决: 检查REDIS_ADDR环境变量，确保Redis服务运行"
echo ""
echo "   问题2: STT结果没有被缓存"
echo "   解决: 检查STT回调是否被调用，level是否为'passive'"
echo ""
echo "   问题3: 翻译回调没有被调用"
echo "   解决: 检查Google Translate配置和网络连接"
echo ""
echo "   问题4: Redis推送失败"
echo "   解决: 检查Redis连接、权限和队列名称格式"

echo ""
echo "6. 实时监控Redis队列..."
echo "   在另一个终端运行以下命令监控Redis:"
echo "   redis-cli MONITOR"
echo ""
echo "   或者监控特定队列:"
echo "   redis-cli BLPOP 'client.meng.astercc.com:AI:AGENT:1100:RECEIVE:*' 0"
