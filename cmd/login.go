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

const (
	// apiKeyMaxDisplayLen API Key 最大显示长度
	apiKeyMaxDisplayLen = 20
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

	// 构建登录请求 - 使用 JSON 格式
	loginURL := baseURL + "/v2/login"
	data := fmt.Sprintf(`{"username":"%s","password":"%s"}`, loginUser, loginPassword)

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data))
	if err != nil {
		log.Fatalf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("登录请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("读取响应失败: %v", err)
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Fatalf("解析响应失败: %v", err)
	}

	// 检查是否有重定向 URL，其中包含 token
	redirectURL, ok := result["redirect_url"].(string)
	if !ok || redirectURL == "" {
		printResponse(body)
		log.Fatal("未找到 redirect_url")
	}

	// 从 redirect_url 中提取 token
	token := extractTokenFromURL(redirectURL)
	if token == "" {
		printResponse(body)
		log.Fatal("未找到 token")
	}

	// 从 JWT 中提取 key 和 user_id
	key, userID, err := extractKeyFromJWT(token)
	if err != nil {
		log.Fatalf("解析 JWT 失败: %v", err)
	}

	// 保存到缓存
	config.SaveTokenCache(loginUser, key, token, userID)

	displayKey := key
	if len(key) > apiKeyMaxDisplayLen {
		displayKey = key[:apiKeyMaxDisplayLen] + "..."
	}

	fmt.Println("✅ 登录成功!")
	fmt.Printf("   用户: %s\n", loginUser)
	fmt.Printf("   API Key: %s\n", displayKey)
	fmt.Printf("   JWT Token: 已保存\n")
	fmt.Printf("   User ID: %s\n", userID)
}

// extractTokenFromURL 从重定向 URL 中提取 token
func extractTokenFromURL(redirectURL string) string {
	if !strings.Contains(redirectURL, "token=") {
		return ""
	}
	u, err := url.Parse(redirectURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}

// printResponse 打印原始响应（用于调试）
func printResponse(body []byte) {
	fmt.Printf("响应: %s\n", string(body))
}

// extractKeyFromJWT 从 JWT token 中提取 key 和 user_id
func extractKeyFromJWT(tokenString string) (string, string, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("无效的 JWT token")
	}

	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", "", err
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", "", err
	}

	key, ok := claims["key"].(string)
	if !ok {
		return "", "", fmt.Errorf("token 中未找到 key")
	}

	userID, _ := claims["user_id"].(string)

	return key, userID, nil
}
