#!/bin/bash

echo "=== AI助手系统快速修复脚本 ==="
echo ""

# 检查当前环境
echo "1. 检查当前环境..."
if [ -f "env.sh" ]; then
    echo "   ✅ 环境配置文件存在"
    source env.sh
else
    echo "   ⚠️  环境配置文件不存在，使用默认配置"
    export AGENT_URI_FORMAT="PJSIP"
fi

echo "   当前AGENT_URI_FORMAT: ${AGENT_URI_FORMAT:-PJSIP}"
echo ""

# 提供不同的解决方案
echo "2. 解决方案选择..."
echo ""
echo "请选择解决方案:"
echo "1) 配置PJSIP端点 (推荐)"
echo "2) 使用SIP协议"
echo "3) 使用Local通道"
echo "4) 使用PJSIP本地分机"
echo ""

read -p "请输入选择 (1-4): " choice

case $choice in
    1)
        echo ""
        echo "=== 方案1: 配置PJSIP端点 ==="
        echo ""
        echo "请在Asterisk的pjsip.conf中添加以下配置:"
        echo ""
        echo "[1100]"
        echo "type=endpoint"
        echo "context=from_router"
        echo "disallow=all"
        echo "allow=ulaw"
        echo "auth=1100"
        echo "aors=1100"
        echo ""
        echo "[1100]"
        echo "type=auth"
        echo "auth_type=userpass"
        echo "password=1100"
        echo "username=1100"
        echo ""
        echo "[1100]"
        echo "type=aor"
        echo "max_contacts=1"
        echo ""
        echo "配置完成后，重新加载PJSIP:"
        echo "asterisk -rx 'module reload res_pjsip.so'"
        ;;
    2)
        echo ""
        echo "=== 方案2: 使用SIP协议 ==="
        echo ""
        echo "设置环境变量:"
        echo "export AGENT_URI_FORMAT=SIP"
        echo ""
        echo "或者修改env.sh文件:"
        echo "AGENT_URI_FORMAT=SIP"
        ;;
    3)
        echo ""
        echo "=== 方案3: 使用Local通道 ==="
        echo ""
        echo "设置环境变量:"
        echo "export AGENT_URI_FORMAT=LOCAL"
        echo ""
        echo "或者修改env.sh文件:"
        echo "AGENT_URI_FORMAT=LOCAL"
        echo ""
        echo "注意: 这需要1100分机在from_router上下文中配置"
        ;;
    4)
        echo ""
        echo "=== 方案4: 使用PJSIP本地分机 ==="
        echo ""
        echo "设置环境变量:"
        echo "export AGENT_URI_FORMAT=PJSIP_LOCAL"
        echo ""
        echo "或者修改env.sh文件:"
        echo "AGENT_URI_FORMAT=PJSIP_LOCAL"
        echo ""
        echo "注意: 这需要1100分机在本地PJSIP中配置"
        ;;
    *)
        echo "无效选择"
        exit 1
        ;;
esac

echo ""
echo "=== 测试步骤 ==="
echo ""
echo "1. 应用选择的解决方案"
echo "2. 重新启动AI助手系统:"
echo "   source env.sh"
echo "   go run ."
echo ""
echo "3. 测试通话:"
echo "   客户1009拨打8888"
echo "   系统应该自动呼叫1100"
echo ""
echo "4. 如果仍然失败，请检查Asterisk日志:"
echo "   asterisk -rx 'core show channels'"
echo "   asterisk -rx 'pjsip show endpoints'"
echo ""

# 提供快速测试命令
echo "=== 快速测试命令 ==="
echo ""
echo "测试PJSIP端点:"
echo "asterisk -rx 'pjsip show endpoint 1100'"
echo ""
echo "测试SIP呼叫:"
echo "asterisk -rx 'originate PJSIP/1009 extension 8888@from_router'"
echo ""
echo "查看呼叫日志:"
echo "asterisk -rx 'core show channels'"
