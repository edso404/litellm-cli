package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"litellm-cli/internal/config"
)

func TestClientGet(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %s, want Bearer test-key", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}

	client := NewClient(cfg)

	var result map[string]string
	err := client.Get("/", &result)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("status = %s, want ok", result["status"])
	}
}

func TestClientGetError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIKey:  "invalid-key",
		BaseURL: server.URL,
	}

	client := NewClient(cfg)

	err := client.Get("/", nil)
	if err == nil {
		t.Error("Get() expected error for 401, got nil")
	}
}

func TestParseError(t *testing.T) {
	body := []byte(`{"error":{"message":"test error","type":"auth_error","code":"401"}}`)
	cfg := &config.Config{
		APIKey:  "test",
		BaseURL: "http://test",
	}
	client := NewClient(cfg)

	err := client.parseError(body)
	if err == nil {
		t.Error("parseError() expected error, got nil")
	}
}

func TestUserDailyActivityResponse(t *testing.T) {
	jsonStr := `{
		"results": [{
			"date": "2026-06-24",
			"metrics": {
				"spend": 1.5,
				"prompt_tokens": 1000,
				"completion_tokens": 500,
				"total_tokens": 1500,
				"successful_requests": 10,
				"failed_requests": 1,
				"api_requests": 11
			}
		}]
	}`

	var resp UserDailyActivityResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(resp.Results) != 1 {
		t.Errorf("len(Results) = %d, want 1", len(resp.Results))
	}

	if resp.Results[0].Metrics.Spend != 1.5 {
		t.Errorf("Spend = %f, want 1.5", resp.Results[0].Metrics.Spend)
	}
}

func TestModelsResponse(t *testing.T) {
	jsonStr := `{
		"data": [
			{"object": "model", "id": "gpt-4"},
			{"object": "model", "id": "gpt-3.5-turbo"}
		]
	}`

	var resp ModelsResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(resp.Models) != 2 {
		t.Errorf("len(Models) = %d, want 2", len(resp.Models))
	}

	if resp.Models[0].ID != "gpt-4" {
		t.Errorf("Model[0].ID = %s, want gpt-4", resp.Models[0].ID)
	}
}

func TestKeyInfoResponse(t *testing.T) {
	jsonStr := `{
		"key": "test-key-hash",
		"info": {
			"key_name": "sk-test",
			"key_alias": "test-alias",
			"spend": 10.5,
			"models": ["gpt-4"],
			"user_id": "user-123",
			"team_id": "team-456"
		}
	}`

	var resp KeyInfoResponse
	err := json.Unmarshal([]byte(jsonStr), &resp)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if resp.Info.Spend != 10.5 {
		t.Errorf("Spend = %f, want 10.5", resp.Info.Spend)
	}

	if resp.Info.KeyAlias != "test-alias" {
		t.Errorf("KeyAlias = %s, want test-alias", resp.Info.KeyAlias)
	}
}