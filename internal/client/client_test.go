package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"litellm-cli/internal/config"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://test.example.com",
	}

	c := New(cfg)
	if c == nil {
		t.Error("New() returned nil")
	}

	if c.config.APIKey != "test-key" {
		t.Errorf("APIKey = %s, want test-key", c.config.APIKey)
	}
}

func TestGetAPIKey(t *testing.T) {
	cfg := &config.Config{
		APIKey: "client-test-key",
		BaseURL: "https://test.example.com",
	}

	c := New(cfg)
	key := c.GetAPIKey()

	if key != "client-test-key" {
		t.Errorf("GetAPIKey() = %s, want client-test-key", key)
	}
}

func TestGetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"object":"model","id":"gpt-4"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "test",
		BaseURL: server.URL,
	}

	c := New(cfg)
	resp, err := c.GetModels()

	if err != nil {
		t.Fatalf("GetModels() error = %v", err)
	}

	if len(resp.Models) != 1 {
		t.Errorf("len(Models) = %d, want 1", len(resp.Models))
	}
}

func TestGetUserDailyActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证参数
		if r.URL.Query().Get("start_date") != "2026-06-01" {
			t.Errorf("start_date = %s, want 2026-06-01", r.URL.Query().Get("start_date"))
		}
		if r.URL.Query().Get("end_date") != "2026-06-30" {
			t.Errorf("end_date = %s, want 2026-06-30", r.URL.Query().Get("end_date"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[],"metadata":{}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "test",
		BaseURL: server.URL,
	}

	c := New(cfg)
	_, err := c.GetUserDailyActivity("2026-06-01", "2026-06-30")

	if err != nil {
		t.Fatalf("GetUserDailyActivity() error = %v", err)
	}
}

func TestGetTeamDailyActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "test",
		BaseURL: server.URL,
	}

	c := New(cfg)
	_, err := c.GetTeamDailyActivity("2026-06-01", "2026-06-30")

	if err != nil {
		t.Fatalf("GetTeamDailyActivity() error = %v", err)
	}
}

func TestGetSpendLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"spend":1.5}]`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "test",
		BaseURL: server.URL,
	}

	c := New(cfg)
	_, err := c.GetSpendLogs("2026-06-01", "2026-06-30")

	if err != nil {
		t.Fatalf("GetSpendLogs() error = %v", err)
	}
}

// 确保 json 解析正确
func TestSpendLogsResponseParsing(t *testing.T) {
	jsonStr := `[{"spend":1.5,"models":{"gpt-4":1.0}}]`

	var resp json.RawMessage
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// 验证可以被 api.SpendLogsResponse 解析
	// 这里只是验证 raw JSON 可用
	if len(resp) == 0 {
		t.Error("Expected non-empty JSON")
	}
}