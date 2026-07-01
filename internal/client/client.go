package client

import (
	"litellm-cli/internal/api"
	"litellm-cli/internal/config"
)

type Client struct {
	api    *api.Client
	config *config.Config
}

func New(cfg *config.Config, opts ...api.ClientOption) *Client {
	return &Client{
		api:    api.NewClient(cfg, opts...),
		config: cfg,
	}
}

func (c *Client) GetUserDailyActivity(startDate, endDate string, pageSize int, page int) (*api.UserDailyActivityResponse, error) {
	return c.api.GetUserDailyActivity(startDate, endDate, pageSize, page)
}

func (c *Client) GetTeamDailyActivity(startDate, endDate string) (*api.TeamDailyActivityResponse, error) {
	return c.api.GetTeamDailyActivity(startDate, endDate)
}

func (c *Client) GetSpendLogs(startDate, endDate string) (*api.SpendLogsResponse, error) {
	return c.api.GetSpendLogs(startDate, endDate)
}

func (c *Client) GetSpendLogsUI(startDateTime, endDateTime string) (*api.SpendLogsUIResponse, error) {
	return c.api.GetSpendLogsUI(startDateTime, endDateTime)
}

func (c *Client) GetSpendLogDetail(requestID string) (map[string]interface{}, error) {
	return c.api.GetSpendLogDetail(requestID)
}

func (c *Client) GetModels() (*api.ModelsResponse, error) {
	return c.api.GetModels()
}

func (c *Client) GetKeyInfo(apiKey string) (*api.KeyInfoResponse, error) {
	return c.api.GetKeyInfo(apiKey)
}

func (c *Client) GetAPIKey() string {
	return c.config.APIKey
}

// API returns the underlying API client
func (c *Client) API() *api.Client {
	return c.api
}

func (c *Client) GetTeamAvailable() (*api.TeamAvailableResponse, error) {
	return c.api.GetTeamAvailable()
}

func (c *Client) GetUserInfo() (*api.UserInfoResponse, error) {
	return c.api.GetUserInfo()
}

func (c *Client) GetTeamList(userID string) (*api.TeamListResponse, error) {
	return c.api.GetTeamList(userID)
}

func (c *Client) GetTeamInfo(teamID string) (*api.TeamInfoResponse, error) {
	return c.api.GetTeamInfo(teamID)
}
