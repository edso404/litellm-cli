package config

import (
	"os"
	"testing"
)

func TestGetBaseURLDefault(t *testing.T) {
	// 验证默认值
	expected := "http://localhost:4000"
	actual := GetBaseURL()

	if actual != expected {
		t.Errorf("GetBaseURL() = %s, want %s", actual, expected)
	}
}

func TestGetAPIKeyFromEnv(t *testing.T) {
	// 设置环境变量
	testKey := "test-env-key-12345"
	os.Setenv("LITELLM_API_KEY", testKey)
	defer os.Unsetenv("LITELLM_API_KEY")

	// 获取并验证
	key := GetAPIKey()
	if key != testKey {
		t.Errorf("GetAPIKey() = %s, want %s", key, testKey)
	}
}

func TestConfigFields(t *testing.T) {
	// 测试 Config 结构
	cfg := &Config{
		APIKey:  "test-key",
		BaseURL: "https://test.example.com",
	}

	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %s, want test-key", cfg.APIKey)
	}

	if cfg.BaseURL != "https://test.example.com" {
		t.Errorf("BaseURL = %s, want https://test.example.com", cfg.BaseURL)
	}
}