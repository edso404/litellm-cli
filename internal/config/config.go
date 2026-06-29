package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

func init() {
	// 自动读取环境变量
	viper.SetEnvPrefix("LITELLM")
	viper.AutomaticEnv()
}

type Config struct {
	APIKey      string
	BaseURL     string
	Username    string
	Password    string
	JWTToken    string // 用于 cookie 认证
	UserID      string // 从 JWT 提取的用户ID
	TokenExpiry time.Time
}

func Load() (*Config, error) {
	apiKey := viper.GetString("api_key")
	baseURL := viper.GetString("base_url")
	username := viper.GetString("username")

	// 从环境变量覆盖
	if apiKey == "" {
		apiKey = os.Getenv("LITELLM_API_KEY")
	}
	if baseURL == "" {
		baseURL = os.Getenv("LITELLM_BASE_URL")
	}
	if username == "" {
		username = os.Getenv("LITELLM_USERNAME")
	}
	password := os.Getenv("LITELLM_PASSWORD")

	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	return &Config{
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Username: username,
		Password: password,
	}, nil
}

// LoadWithAutoLogin 如果没有 API key 但有 username/password，自动登录
func LoadWithAutoLogin() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	// 如果有 username/password，检查缓存或自动登录
	if cfg.Username != "" && cfg.Password != "" {
		// 检查缓存
		if cached, jwtToken, userID := loadCachedToken(cfg.Username); cached != "" {
			cfg.JWTToken = jwtToken
			cfg.UserID = userID
			// 如果没有 API key，用缓存的 key
			if cfg.APIKey == "" {
				cfg.APIKey = cached
			}
		} else {
			// 没有缓存，需要自动登录获取 JWT
			if cfg.APIKey == "" {
				// 没有 API key，需要完整登录
				key, jwtToken, userID, err := login(cfg.Username, cfg.Password, cfg.BaseURL)
				if err != nil {
					return nil, fmt.Errorf("自动登录失败: %v", err)
				}
				// 缓存 token
				saveCachedToken(cfg.Username, key, jwtToken, userID)
				cfg.APIKey = key
				cfg.JWTToken = jwtToken
				cfg.UserID = userID
			} else {
				// 有 API key 但没有 JWT，需要用密码登录获取 JWT
				// 由于 /v2/login 返回的 key 可能不同，这里还是需要登录
				key, jwtToken, userID, err := login(cfg.Username, cfg.Password, cfg.BaseURL)
				if err != nil {
					// 登录失败不报错，只记录日志，但继续使用提供的 APIKey
					fmt.Printf("获取 JWT 失败: %v, 使用提供的 APIKey\n", err)
				} else {
					// 缓存 token
					saveCachedToken(cfg.Username, key, jwtToken, userID)
					cfg.JWTToken = jwtToken
					cfg.UserID = userID
				}
			}
		}
	}

	// 仍然没有 key
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API Key 未设置，请通过 --api-key、LITELLM_API_KEY 环境变量配置，或设置 LITELLM_USERNAME 和 LITELLM_PASSWORD")
	}

	return cfg, nil
}

func login(username, password, baseURL string) (string, string, string, error) {
	loginURL := baseURL + "/v2/login"
	// 使用 JSON 格式
	data := fmt.Sprintf(`{"username":"%s","password":"%s"}`, username, password)

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("登录失败: %s", string(body))
	}

	// 从 Set-Cookie 提取 token
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "token" {
			key, userID, err := extractKeyFromJWT(cookie.Value)
			return key, cookie.Value, userID, err
		}
	}

	return "", "", "", fmt.Errorf("未找到 token")
}

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

// Token 缓存
type tokenCache struct {
	Key      string `json:"key"`
	JWTToken string `json:"jwt_token"`
	UserID   string `json:"user_id"`
	Time     string `json:"time"`
	Username string `json:"username"`
}

func getCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".litellm-cli-cache")
}

func saveCachedToken(username, key, jwtToken, userID string) {
	cache := tokenCache{
		Key:      key,
		JWTToken: jwtToken,
		UserID:   userID,
		Time:     time.Now().Format(time.RFC3339),
		Username: username,
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(getCachePath(), data, 0600)
}

// SaveTokenCache 公开的保存 token 方法（供 login 命令使用）
func SaveTokenCache(username, key, jwtToken, userID string) {
	saveCachedToken(username, key, jwtToken, userID)
}

func loadCachedToken(username string) (string, string, string) {
	data, err := os.ReadFile(getCachePath())
	if err != nil {
		return "", "", ""
	}

	var cache tokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return "", "", ""
	}

	// 检查是否是同一用户
	if cache.Username != username {
		return "", "", ""
	}

	// 检查是否过期 (24小时)
	if cache.Time != "" {
		if t, err := time.Parse(time.RFC3339, cache.Time); err == nil {
			if time.Since(t) > 24*time.Hour {
				return "", "", "" // 过期
			}
		}
	}

	return cache.Key, cache.JWTToken, cache.UserID
}

// 兼容旧接口
func GetAPIKey() string {
	if key := viper.GetString("api_key"); key != "" {
		return key
	}
	return os.Getenv("LITELLM_API_KEY")
}

func GetBaseURL() string {
	if url := viper.GetString("base_url"); url != "" {
		return url
	}
	return "http://localhost:4000"
}
