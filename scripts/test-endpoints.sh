#!/bin/bash

# API 端点测试脚本

BASE_URL="${LITELLM_BASE_URL:-http://localhost:4000}"

# 检查是否使用 JWT key
if [ -n "$LITELLM_USERNAME" ] && [ -n "$LITELLM_PASSWORD" ]; then
    echo "=== 使用 JWT 登录 ==="

    # 登录获取 token (从 cookie 中提取)
    COOKIE_FILE=$(mktemp)

    RESPONSE=$(curl -s -X POST "$BASE_URL/v2/login" \
        -d "username=$LITELLM_USERNAME" \
        -d "password=$LITELLM_PASSWORD" \
        -w "\n%{http_code}" \
        -c "$COOKIE_FILE" \
        -o /dev/null)

    HTTP_CODE=$(echo "$RESPONSE" | tail -1)

    if [ "$HTTP_CODE" != "200" ]; then
        echo "登录失败: HTTP $HTTP_CODE"
        rm -f "$COOKIE_FILE"
        exit 1
    fi

    # 从 cookie 文件提取 token
    TOKEN=$(grep "token" "$COOKIE_FILE" | awk '{print $7}')
    rm -f "$COOKIE_FILE"

    if [ -z "$TOKEN" ]; then
        echo "未获取到 token"
        exit 1
    fi

    # 从 JWT 提取 key
    JWT_KEY=$(echo "$TOKEN" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq -r '.key' 2>/dev/null)

    if [ -z "$JWT_KEY" ] || [ "$JWT_KEY" = "null" ]; then
        echo "未能从 JWT 提取 key"
        exit 1
    fi

    echo "JWT Key: $JWT_KEY"
    API_KEY="$JWT_KEY"
    echo ""
elif [ -n "$LITELLM_API_KEY" ]; then
    echo "=== 使用普通 API Key ==="
    API_KEY="$LITELLM_API_KEY"
else
    echo "请设置以下环境变量之一:"
    echo "  - LITELLM_USERNAME + LITELLM_PASSWORD (自动登录)"
    echo "  - LITELLM_API_KEY (直接使用)"
    exit 1
fi

echo "API Key: ${API_KEY:0:20}..."
echo ""

# 测试各个端点
echo "=== 测试端点可用性 ==="

endpoints=(
    "/key/info?api_key=$API_KEY:Key Info"
    "/models:Models"
    "/user/daily/activity?start_date=2024-01-01&end_date=2024-01-02:User Daily Activity"
    "/team/daily/activity?start_date=2024-01-01&end_date=2024-01-02:Team Daily Activity"
    "/spend/logs?start_date=2024-01-01&end_date=2024-01-02:Spend Logs"
    "/spend/logs/ui?start_date=2024-01-01&end_date=2024-01-02:Spend Logs UI"
    "/user/info:User Info"
    "/team/info:Team Info"
    "/spend/summary:Spend Summary"
)

for item in "${endpoints[@]}"; do
    endpoint="${item%%:*}"
    name="${item##*:}"

    RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $API_KEY" \
        "$BASE_URL$endpoint")

    if [ "$RESPONSE" = "200" ]; then
        echo "✓ $name: 200 OK"
    else
        echo "✗ $name: $RESPONSE"
    fi
done