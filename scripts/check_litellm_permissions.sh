#!/bin/bash

# LiteLLM API 权限探测脚本
# 用法: ./check_permissions.sh <API_KEY> [BASE_URL]

API_KEY="${1:-$LITELLM_API_KEY}"
BASE_URL="${2:-${LITELLM_BASE_URL:-http://localhost:4000}}"

if [ -z "$API_KEY" ]; then
    echo "请传入 API_KEY 或设置 LITELLM_API_KEY 环境变量"
    echo "用法: ./check_permissions_v2.sh <API_KEY> [BASE_URL]"
    exit 1
fi

echo "============================================"
echo "LiteLLM API 权限探测"
echo "API Key: ${API_KEY:0:10}..."
echo "Base URL: $BASE_URL"
echo "============================================"
echo ""

check_endpoint() {
    local endpoint=$1
    local method=${2:-GET}
    local result

    if [ "$method" = "GET" ]; then
        result=$(curl -s -o /dev/null -w "%{http_code}" \
            -H "Authorization: Bearer $API_KEY" \
            -H "Content-Type: application/json" \
            "${BASE_URL}${endpoint}" 2>/dev/null)
    else
        result=$(curl -s -o /dev/null -w "%{http_code}" \
            -X "$method" \
            -H "Authorization: Bearer $API_KEY" \
            -H "Content-Type: application/json" \
            "${BASE_URL}${endpoint}" 2>/dev/null)
    fi

    echo "$result"
}

# 增强接口列表 - 按类别分组
declare -a ENDPOINTS=()

# === 消费/用量 ===
ENDPOINTS+=("/spend/logs|消费日志 (v1)|GET")
ENDPOINTS+=("/spend/logs/v2|消费日志 (v2)|GET")
ENDPOINTS+=("/global/spend/report|消费报表|GET")
ENDPOINTS+=("/global/spend/tags|Tags 消费|GET")

# === 团队 ===
ENDPOINTS+=("/team/list|团队列表|GET")
ENDPOINTS+=("/team/available|可用团队|GET")
ENDPOINTS+=("/team/info?team_id=test|团队详情|GET")

# === 用户 ===
ENDPOINTS+=("/user/list|用户列表|GET")
ENDPOINTS+=("/user/info?user_id=test|用户详情|GET")
ENDPOINTS+=("/user/available_users|可用用户|GET")

# === Key ===
ENDPOINTS+=("/key/list|Key 列表|GET")
ENDPOINTS+=("/key/info?api_key=$API_KEY|Key 详情|GET")
ENDPOINTS+=("/key/aliases|Key 别名|GET")

# === 模型 ===
ENDPOINTS+=("/models|模型列表|GET")
ENDPOINTS+=("/model/info?model=gpt-4o|模型详情|GET")
ENDPOINTS+=("/model_group/info?model_group=gpt-4|模型组详情|GET")

# === 预算 ===
ENDPOINTS+=("/budget/list|预算列表|GET")
ENDPOINTS+=("/budget/info?budget_id=test|预算详情|GET")

# === Tag ===
ENDPOINTS+=("/tag/list|Tag 列表|GET")
ENDPOINTS+=("/tag/summary|Tag 汇总|GET")
ENDPOINTS+=("/tag/dau|每日活跃用户|GET")
ENDPOINTS+=("/tag/wau|每周活跃用户|GET")
ENDPOINTS+=("/tag/mau|每月活跃用户|GET")
ENDPOINTS+=("/tag/daily/activity|Tag 每日活动|GET")
ENDPOINTS+=("/tag/distinct|Tag 唯一值|GET")

# === 活动统计 ===
ENDPOINTS+=("/user/daily/activity|用户每日活动|GET")
ENDPOINTS+=("/team/daily/activity|团队每日活动|GET")
ENDPOINTS+=("/customer/daily/activity|客户每日活动|GET")

# === 审计/健康 ===
ENDPOINTS+=("/audit|审计日志|GET")
ENDPOINTS+=("/health/history|健康历史|GET")
ENDPOINTS+=("/health/latest|最新健康状态|GET")

# === 设置 ===
ENDPOINTS+=("/settings|全局设置|GET")
ENDPOINTS+=("/router/settings|路由设置|GET")
ENDPOINTS+=("/cache/settings|缓存设置|GET")
ENDPOINTS+=("/callbacks/list|回调列表|GET")
ENDPOINTS+=("/budget/settings|预算设置|GET")

# === v2 接口 ===
ENDPOINTS+=("/v2/team/list|团队列表 (v2)|GET")

# 分类存储结果
declare -a SUCCESS=()
declare -a FORBIDDEN=()
declare -a UNAUTHORIZED=()
declare -a OTHER=()

for item in "${ENDPOINTS[@]}"; do
    IFS='|' read -r path desc method <<< "$item"

    code=$(check_endpoint "$path" "$method")

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
        500|502|503)
            OTHER+=("$desc ($path) - 💥 $code (server error)")
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
echo "📌 可用于 CLI 的接口"
echo "============================================"
if [ ${#SUCCESS[@]} -gt 0 ]; then
    echo "以下接口可访问:"
    for item in "${SUCCESS[@]}"; do
        echo "   • $item"
    done
else
    echo "未发现可访问的接口"
fi