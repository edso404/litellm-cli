package api

import "fmt"

// TeamAvailableResponse represents /team/available response (can be array)
type TeamAvailableResponse []TeamBasicInfo

type TeamBasicInfo struct {
	TeamID    string `json:"team_id"`
	TeamAlias string `json:"team_alias,omitempty"`
	TeamName  string `json:"team_name,omitempty"`
	CreatedAt string `json:"created_at"`
	Members   int64  `json:"members"`
}

// GetTeamAvailable 获取可用团队
func (c *Client) GetTeamAvailable() (*TeamAvailableResponse, error) {
	var result TeamAvailableResponse
	err := c.Get("/team/available", &result)
	return &result, err
}

// TeamInfoResponse represents /team/info response
type TeamInfoResponse struct {
	TeamID   string     `json:"team_id"`
	TeamInfo TeamDetail `json:"team_info"`
	Keys     []KeySpend `json:"keys"`
}

type TeamDetail struct {
	TeamAlias        string       `json:"team_alias,omitempty"`
	TeamID           string       `json:"team_id"`
	MembersWithRoles []MemberInfo `json:"members_with_roles"`
	Spend            float64      `json:"spend"`
}

// GetTeamInfo 获取团队详情（包含成员用量）
func (c *Client) GetTeamInfo(teamID string) (*TeamInfoResponse, error) {
	var result TeamInfoResponse
	err := c.GetWithCookie(fmt.Sprintf("/team/info?team_id=%s", teamID), &result, c.jwtToken)
	return &result, err
}
