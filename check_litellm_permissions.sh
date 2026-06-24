#!/bin/bash

# LiteLLM API 权限探测脚本
# 用法: ./check_permissions.sh <API_KEY> [BASE_URL]

API_KEY="${1:-$LITELLM_API_KEY}"
BASE_URL="${2:-http://localhost:4000}"

if [ -z "$API_KEY" ]; then
    echo "请传入 API_KEY 或设置 LITELLM_API_KEY 环境变量"
    echo "用法: ./check_permissions.sh <API_KEY> [BASE_URL]"
    exit 1
fi

echo "============================================"
echo "LiteLLM API 权限探测"
echo "API Key: ${API_KEY:0:10}..."
echo "Base URL: $BASE_URL"
echo "============================================"
echo ""

# 定义要测试的接口列表
# 格式: "接口路径|描述|请求方法|可选参数"
declare -a ENDPOINTS=(
    "/spend/logs/v2|消费日志 (v2)|GET"
    "/global/spend/report|消费报表|GET"
    "/team/list|团队列表|GET"
    "/team/info?team_id=test|团队详情|GET"
    "/user/list|用户列表|GET"
    "/user/info?user_id=test|用户详情|GET"
    "/key/list|Key 列表|GET"
    "/key/info?api_key=$API_KEY|Key 详情|GET"
    "/models|模型列表|GET"
    "/model/info?model=gpt-4|模型详情|GET"
    "/budget/list|预算列表|GET"
    "/tag/summary|Tag 汇总|GET"
    "/tag/dau|每日活跃用户|GET"
    "/tag/wau|每周活跃用户|GET"
    "/tag/mau|每月活跃用户|GET"
    "/audit|审计日志|GET"
    "/health/history|健康历史|GET"
    "/settings|全局设置|GET"
    "/router/settings|路由设置|GET"
    "/cache/settings|缓存设置|GET"
    "/callbacks/list|回调列表|GET"
)

check_endpoint() {
    local endpoint=$1
    local method=${3:-GET}
    local result

    if [ "$method" = "GET" ]; then
        result=$(curl -s -o /dev/null -w "%{http_code}" \
            -H "Authorization: Bearer $API_KEY" \
            -H "Content-Type: application/json" \
            "${BASE_URL}${endpoint}")
    else
        result=$(curl -s -o /dev/null -w "%{http_code}" \
            -X "$method" \
            -H "Authorization: Bearer $API_KEY" \
            -H "Content-Type: application/json" \
            "${BASE_URL}${endpoint}")
    fi

    echo "$result"
}

# 分类存储结果
declare -a SUCCESS=()
declare -a FORBIDDEN=()
declare -a UNAUTHORIZED=()
declare -a OTHER=()

for item in "${ENDPOINTS[@]}"; do
    IFS='|' read -r path desc method <<< "$item"

    code=$(check_endpoint "$path" "$BASE_URL" "$method")

    case $code in
        200|201)
            SUCCESS+=("$desc ($path) - ✅ $code")
            ;;
        401)
            UNAUTHORIZED+=("$desc ($path) - 🔐 $code")
            ;;
        403)
            FORBIDDEN+=("$desc ($path) - 🚫 $code")
            ;;
        404)
            OTHER+=("$desc ($path) - ❌ $code (not found)")
            ;;
        *)
            OTHER+=("$desc ($path) - ❓ $code")
            ;;
    esac
done

echo "============================================"
echo "📊 探测结果汇总"
echo "============================================"

if [ ${#SUCCESS[@]} -gt 0 ]; then
    echo ""
    echo "✅ 可访问 (${#SUCCESS[@]}):"
    for item in "${SUCCESS[@]}"; do
        echo "   $item"
    done
fi

if [ ${#FORBIDDEN[@]} -gt 0 ]; then
    echo ""
    echo "🚫 权限不足 (${#FORBIDDEN[@]}):"
    for item in "${FORBIDDEN[@]}"; do
        echo "   $item"
    done
fi

if [ ${#UNAUTHORIZED[@]} -gt 0 ]; then
    echo ""
    echo "🔐 未授权 (${#UNAUTHORIZED[@]}):"
    for item in "${UNAUTHORIZED[@]}"; do
        echo "   $item"
    done
fi

if [ ${#OTHER[@]} -gt 0 ]; then
    echo ""
    echo "❓ 其他 (${#OTHER[@]}):"
    for item in "${OTHER[@]}"; do
        echo "   $item"
    done
fi

echo ""
echo "============================================"
echo "建议: 使用可访问的接口来规划 CLI 功能"
echo "============================================"