package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"litellm-cli/internal/config"
)

var (
	loginUser     string
	loginPassword string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "用户名密码登录获取 token",
	Run:   runLogin,
}

func init() {
	loginCmd.Flags().StringVarP(&loginUser, "username", "u", "", "用户名")
	loginCmd.Flags().StringVarP(&loginPassword, "password", "p", "", "密码")
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) {
	if loginUser == "" || loginPassword == "" {
		log.Fatal("请提供用户名和密码: -u <username> -p <password>")
	}

	baseURL := config.GetBaseURL()

	// 构建登录请求
	loginURL := baseURL + "/v2/login"
	data := url.Values{}
	data.Set("username", loginUser)
	data.Set("password", loginPassword)

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		log.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("登录请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("登录失败: %s", string(body))
	}

	// 从 Set-Cookie 提取 token
	token := ""
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "token" {
			token = cookie.Value
			break
		}
	}

	if token == "" {
		log.Fatal("未找到 token，请检查登录是否成功")
	}

	// JWT decode 提取 key
	apiKey, err := extractKeyFromJWT(token)
	if err != nil {
		log.Fatalf("解析 token 失败: %v", err)
	}

	// 显示结果
	fmt.Println("\n✅ 登录成功!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Token: %s...\n", token[:50])
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n使用方式:")
	fmt.Printf("  方式1: export LITELLM_API_KEY=%s\n", apiKey)
	fmt.Printf("  方式2: litellm-cli --api-key=%s stats\n", apiKey)
	fmt.Printf("  方式3: 写入配置文件 ~/.litellm-cli.yaml:\n")
	fmt.Printf("           api_key: %s\n", apiKey)
}

func extractKeyFromJWT(tokenString string) (string, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("无效的 JWT token")
	}

	// 解码 payload (第二部分)
	// JWT 使用 URL-safe base64，需要转换
	payload := parts[1]
	// 添加 padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("解码失败: %v", err)
	}

	// 解析 JSON
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %v", err)
	}

	// 提取 key
	key, ok := claims["key"].(string)
	if !ok {
		return "", fmt.Errorf("token 中未找到 key")
	}

	return key, nil
}