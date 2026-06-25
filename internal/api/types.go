package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-resty/resty/v2"
	"litellm-cli/internal/config"
)

type Client struct {
	resty     *resty.Client
	config    *config.Config
	jwtToken  string
}

func NewClient(cfg *config.Config) *Client {
	client := resty.New()
	client.SetBaseURL(cfg.BaseURL)
	client.SetHeader("Authorization", "Bearer "+cfg.APIKey)
	client.SetHeader("Content-Type", "application/json")
	// 禁用代理，避免开发代理导致 EOF
	client.SetTransport(&http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return nil, nil
		},
	})

	return &Client{
		resty:    client,
		config:   cfg,
		jwtToken: cfg.JWTToken,
	}
}

func (c *Client) Get(path string, result interface{}) error {
	return c.getWithCookie(path, result, c.jwtToken)
}

func (c *Client) GetWithCookie(path string, result interface{}, jwtToken string) error {
	return c.getWithCookie(path, result, jwtToken)
}

func (c *Client) getWithCookie(path string, result interface{}, jwtToken string) error {
	req := c.resty.R()
	if jwtToken != "" {
		req.SetHeader("Cookie", "token="+jwtToken)

		// 从 JWT 中提取 key 作为 Authorization Bearer
		if key, err := extractKeyFromJWT(jwtToken); err == nil && key != "" {
			req.SetHeader("Authorization", "Bearer "+key)
		}
	}

	resp, err := req.Get(path)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return c.parseError(resp.Body())
	}

	if result != nil {
		if err := json.Unmarshal(resp.Body(), result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}

	return nil
}

func (c *Client) parseError(body []byte) error {
	var errResp map[string]interface{}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("请求失败，状态码: %s", string(body))
	}

	if errObj, ok := errResp["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok {
			return fmt.Errorf("%s", msg)
		}
	}

	return fmt.Errorf("请求失败: %s", string(body))
}

// UserDailyActivityResponse represents /user/daily/activity response
type UserDailyActivityResponse struct {
	Results   []UserDailyActivity `json:"results"`
	Metadata  Metadata            `json:"metadata"`
}

type UserDailyActivity struct {
	Date     string         `json:"date"`
	Metrics  ActivityMetrics `json:"metrics"`
	Breakdown Breakdown    `json:"breakdown"`
}

type ActivityMetrics struct {
	Spend                 float64 `json:"spend"`
	PromptTokens          int64   `json:"prompt_tokens"`
	CompletionTokens      int64   `json:"completion_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	SuccessfulRequests    int64   `json:"successful_requests"`
	FailedRequests       int64   `json:"failed_requests"`
	APIRequests          int64   `json:"api_requests"`
}

type Breakdown struct {
	Models map[string]ModelBreakdown `json:"models"`
	APIKeys map[string]APIKeyMetrics `json:"api_keys"`
}

type ModelBreakdown struct {
	Metrics ActivityMetrics `json:"metrics"`
}

type APIKeyMetrics struct {
	Metrics  ActivityMetrics `json:"metrics"`
	Metadata map[string]string `json:"metadata"`
}

type Metadata struct {
	TotalSpend            float64 `json:"total_spend"`
	TotalPromptTokens     int64   `json:"total_prompt_tokens"`
	TotalCompletionTokens int64   `json:"total_completion_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	TotalAPIRequests      int64   `json:"total_api_requests"`
}

// GetUserDailyActivity 获取用户每日活动
func (c *Client) GetUserDailyActivity(startDate, endDate string) (*UserDailyActivityResponse, error) {
	var result UserDailyActivityResponse
	err := c.Get(fmt.Sprintf("/user/daily/activity?start_date=%s&end_date=%s", startDate, endDate), &result)
	return &result, err
}

// TeamDailyActivityResponse represents /team/daily/activity response
type TeamDailyActivityResponse struct {
	Results []TeamDailyActivity `json:"results"`
}

type TeamDailyActivity struct {
	Date     string         `json:"date"`
	Metrics  ActivityMetrics `json:"metrics"`
	Breakdown Breakdown    `json:"breakdown"`
}

// GetTeamDailyActivity 获取团队每日活动
func (c *Client) GetTeamDailyActivity(startDate, endDate string) (*TeamDailyActivityResponse, error) {
	var result TeamDailyActivityResponse
	err := c.Get(fmt.Sprintf("/team/daily/activity?start_date=%s&end_date=%s", startDate, endDate), &result)
	return &result, err
}

// SpendLogsResponse represents /spend/logs response - 使用 map 处理动态 key
type SpendLogsResponse []map[string]interface{}

// GetSpendLogs 获取消费日志
func (c *Client) GetSpendLogs(startDate, endDate string) (*SpendLogsResponse, error) {
	var result SpendLogsResponse
	err := c.Get(fmt.Sprintf("/spend/logs?start_date=%s&end_date=%s", startDate, endDate), &result)
	return &result, err
}

// SpendLogsUIResponse represents /spend/logs/ui response - 更详细的实时日志
type SpendLogsUIResponse struct {
	Data     []SpendLogEntry `json:"data"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	TotalPages int           `json:"total_pages"`
}

type SpendLogEntry struct {
	ID               string                 `json:"request_id"`
	CallType         string                 `json:"call_type"`
	APIKey           string                 `json:"api_key"`
	Model            string                 `json:"model"`
	ModelGroup       string                 `json:"model_group"`
	Status           string                 `json:"status"`
	StartTime        string                 `json:"startTime"`
	EndTime          string                 `json:"endTime"`
	TotalSpend       float64                `json:"spend"`
	PromptTokens     int64                  `json:"prompt_tokens"`
	CompletionTokens int64                  `json:"completion_tokens"`
	TotalTokens      int64                  `json:"total_tokens"`
	Latency          float64                `json:"latency,omitempty"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	RequestTags      []string               `json:"request_tags,omitempty"`
	TeamID           string                 `json:"team_id,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GetSpendLogsUI 获取 UI 风格的消费日志 (需要 JWT token)
func (c *Client) GetSpendLogsUI(startDateTime, endDateTime string) (*SpendLogsUIResponse, error) {
	var result SpendLogsUIResponse
	err := c.GetWithCookie(fmt.Sprintf("/spend/logs/ui?start_date=%s&end_date=%s", startDateTime, endDateTime), &result, c.jwtToken)
	return &result, err
}

// ModelsResponse represents /models response
type ModelsResponse struct {
	Models []ModelInfo `json:"data"`
}

type ModelInfo struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	ModelName   string `json:"model_name,omitempty"`
}

// GetModels 获取模型列表
func (c *Client) GetModels() (*ModelsResponse, error) {
	var result ModelsResponse
	err := c.Get("/models", &result)
	return &result, err
}

// KeyInfoResponse represents /key/info response
type KeyInfoResponse struct {
	Key  string    `json:"key"`
	Info KeyDetail `json:"info"`
}

type KeyDetail struct {
	KeyName      string             `json:"key_name"`
	KeyAlias     string             `json:"key_alias"`
	Spend        float64            `json:"spend"`
	Models       []string           `json:"models"`
	UserID       string             `json:"user_id"`
	TeamID       string             `json:"team_id"`
	CreatedAt    string             `json:"created_at"`
	LastActive   string             `json:"last_active"`
}

// GetKeyInfo 获取 Key 详情
func (c *Client) GetKeyInfo(apiKey string) (*KeyInfoResponse, error) {
	var result KeyInfoResponse
	err := c.Get("/key/info?api_key="+apiKey, &result)
	return &result, err
}

// UserInfoResponse represents /user/info response
type UserInfoResponse struct {
	UserID         string     `json:"user_id"`
	UserEmail      string     `json:"user_email"`
	UserRole       string     `json:"user_role"`
	OrganizationID string     `json:"organization_id"`
	Teams          []UserTeam `json:"teams"`
}

// UserTeam represents a team in user's teams list
type UserTeam struct {
	TeamID           string        `json:"team_id"`
	TeamAlias        string        `json:"team_alias"`
	Spend            float64       `json:"spend"`
	MembersWithRoles []MemberInfo `json:"members_with_roles"`
	Keys             []KeySpend    `json:"keys"`
}

// GetUserInfo 获取用户信息
func (c *Client) GetUserInfo() (*UserInfoResponse, error) {
	var result UserInfoResponse
	err := c.Get("/user/info", &result)
	return &result, err
}

// TeamListResponse represents /team/list response
type TeamListResponse []TeamInfo

type TeamInfo struct {
	TeamAlias         string       `json:"team_alias"`
	TeamID            string       `json:"team_id"`
	Spend             float64      `json:"spend"`
	MembersWithRoles  []MemberInfo `json:"members_with_roles"`
	Keys              []KeySpend    `json:"keys"`
}

type MemberInfo struct {
	UserID  string `json:"user_id"`
	Email   string `json:"user_email"`
	Role    string `json:"role"`
}

type KeySpend struct {
	KeyName string  `json:"key_name"`
	KeyAlias string `json:"key_alias"`
	UserID  string  `json:"user_id"`
	Spend   float64 `json:"spend"`
}

// GetTeamList 获取团队列表（含用量）
func (c *Client) GetTeamList(userID string) (*TeamListResponse, error) {
	var result TeamListResponse
	err := c.GetWithCookie(fmt.Sprintf("/team/list?user_id=%s", userID), &result, c.jwtToken)
	return &result, err
}

// extractKeyFromJWT 从 JWT token 中提取 key
func extractKeyFromJWT(tokenString string) (string, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}

	payload := parts[1]
	// 添加 padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	// 使用标准 base64 解码
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", err
	}

	key, ok := claims["key"].(string)
	if !ok {
		return "", fmt.Errorf("key not found in token")
	}

	return key, nil
}