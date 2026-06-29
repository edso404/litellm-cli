package logs

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"litellm-cli/internal/api"
	"litellm-cli/internal/client"
	"litellm-cli/internal/config"
	"litellm-cli/internal/testutils"
)

func init() {
	// 强制色彩 Profile 锁定，杜绝 CI 中的色彩漂移
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// mockTransport 拦截底层 Resty 请求并返回内存中的响应
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

// normalizeOutput 用正则擦除动态变化的时间戳，确保快照 100% 确定
func normalizeOutput(s string) string {
	re := regexp.MustCompile(`时间: \d{2}:\d{2}:\d{2}`)
	return re.ReplaceAllString(s, "时间: 15:04:05")
}

func TestLogsTUI_HappyPath(t *testing.T) {
	// 强制色彩 Profile 锁定
	lipgloss.SetColorProfile(termenv.TrueColor)

	// Mock v2 列表响应
	mockUIResp := api.SpendLogsUIResponse{
		Data: []api.SpendLogEntry{
			{
				ID:               "req-1",
				CallType:         "completion",
				APIKey:           "sk-1234",
				Model:            "gpt-4",
				ModelGroup:       "gpt-4-group",
				Status:           "success",
				StartTime:        "2026-06-26T06:00:00Z",
				EndTime:          "2026-06-26T06:00:02Z",
				TotalSpend:       0.0024,
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
				Latency:          2.0,
			},
			{
				ID:               "req-2",
				CallType:         "completion",
				APIKey:           "sk-5678",
				Model:            "claude-3",
				ModelGroup:       "claude-3-group",
				Status:           "success",
				StartTime:        "2026-06-26T06:05:00Z",
				EndTime:          "2026-06-26T06:05:01Z",
				TotalSpend:       0.015,
				PromptTokens:     200,
				CompletionTokens: 100,
				TotalTokens:      300,
				Latency:          1.0,
			},
		},
		Total:      2,
		Page:       1,
		PageSize:   10,
		TotalPages: 1,
	}
	mockUIRespBytes, _ := json.Marshal(mockUIResp)

	// Mock 详情响应 (针对 req-2)
	mockDetail := map[string]interface{}{
		"request_id": "req-2",
		"model":      "claude-3",
		"spend":      0.015,
		"latency":    1.0,
		"status":     "success",
		"proxy_server_request": map[string]interface{}{
			"system": []interface{}{
				"You are a translation assistant.",
			},
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "Translate Hello to Chinese"},
			},
			"tools": []interface{}{},
		},
		"response": map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "<think>\n这里是模拟的思考过程，它使用了 **粗体** 进行测试。\n</think>\n你好，这部分是 **Markdown 正文**，包含一个 `代码块`。",
					},
					"finish_reason": "stop",
				},
			},
		},
	}
	mockDetailBytes, _ := json.Marshal(mockDetail)

	transport := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			if req.URL.Path == "/spend/logs/ui" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(mockUIRespBytes))),
					Header:     header,
				}, nil
			}

			if strings.HasPrefix(req.URL.Path, "/spend/logs/ui/") {
				reqID := strings.TrimPrefix(req.URL.Path, "/spend/logs/ui/")
				if reqID == "req-2" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(string(mockDetailBytes))),
						Header:     header,
					}, nil
				}
			}

			return nil, errors.New("unhandled mock path: " + req.URL.Path)
		},
	}

	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://mock-api.litellm.local",
	}
	c := client.New(cfg, api.WithTransport(transport))

	// 1. 初始化 Model 并设置固定窗口大小以防止漂移
	m := NewModel(c, 5, "")

	// 投递窗口大小消息，锁死长宽
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newModel.(*Model)

	// 2. 模拟网络响应，投递首次加载成功的 LogsLoadedMsg
	newModel, _ = m.Update(LogsLoadedMsg{Response: &mockUIResp})
	m = newModel.(*Model)

	// 验证列表视图状态：断言选中行
	if m.selectedIndex != 0 {
		t.Fatalf("expected selectedIndex to be 0, got %d", m.selectedIndex)
	}
	testutils.AssertTUISnapshot(t, "logs_list_default", normalizeOutput(m.View()))

	// 3. 投递键盘消息向下滚动：'j'
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = newModel.(*Model)

	// 断言选中行递增为 1 (指向 req-2)
	if m.selectedIndex != 1 {
		t.Fatalf("expected selectedIndex to be 1, got %d", m.selectedIndex)
	}
	testutils.AssertTUISnapshot(t, "logs_list_scrolled", normalizeOutput(m.View()))

	// 4. 投递 'enter' 键盘消息触发详情加载
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)

	if m.viewMode != "detail" {
		t.Fatalf("expected viewMode to be 'detail', got '%s'", m.viewMode)
	}
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd for loading detail")
	}

	// 执行 cmd 获取 DetailLoadedMsg
	msg := cmd()
	detailMsg, ok := msg.(DetailLoadedMsg)
	if !ok {
		t.Fatalf("expected DetailLoadedMsg, got %T", msg)
	}

	// 将 DetailLoadedMsg 投递给 Model
	newModel, _ = m.Update(detailMsg)
	m = newModel.(*Model)

	// 验证详情视图
	if m.detailError != "" {
		t.Fatalf("expected no detailError, got '%s'", m.detailError)
	}
	if m.detailData == nil {
		t.Fatal("expected non-nil detailData")
	}
	testutils.AssertTUISnapshot(t, "logs_detail_view", normalizeOutput(m.View()))

	// 4.5. 模拟将焦点移至 choices 卡片并切入 choices 标签页
	// 初始为 0 (system)，按 3 次 Tab 将 focusedSection 移动到 3 (choices)
	for i := 0; i < 3; i++ {
		newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = newModel.(*Model)
	}
	// 按 Enter 键切入 choices 标签页列表
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if m.detailState.activeTab != "choices" {
		t.Fatalf("expected activeTab to be 'choices', got '%s'", m.detailState.activeTab)
	}
	// 验证 Choices Summary 列表状态，并保存快照
	testutils.AssertTUISnapshot(t, "logs_choices_list", normalizeOutput(m.View()))

	// 4.6. 按 Enter 进入第一个 Choice 的单项详情页面
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if !m.detailState.itemDetailMode {
		t.Fatal("expected itemDetailMode to be true after pressing Enter on choices list")
	}
	// 验证 Choice 详情视图（此时包含了用 Markdown 渲染的“思考过程”和“正文内容”），并保存快照！
	testutils.AssertTUISnapshot(t, "logs_choice_detail_view", normalizeOutput(m.View()))

	// == 额外测试：响应式折叠/展开/聚焦切换/复制交互状态机 ==

	// 1. 模拟按 Enter 键展开当前聚焦的 thinking 块
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if m.detailState.blockCollapsed["thinking"] {
		t.Fatal("expected thinking block to be expanded after pressing Enter")
	}
	testutils.AssertTUISnapshot(t, "logs_choice_detail_thinking_expanded", normalizeOutput(m.View()))

	// 2. 模拟再按一次 Enter 键折叠 thinking 块
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if !m.detailState.blockCollapsed["thinking"] {
		t.Fatal("expected thinking block to be collapsed after pressing Enter again")
	}

	// 3. 模拟按 Tab 键将块聚焦切换到 content (响应内容)
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = newModel.(*Model)
	if m.detailState.focusedBlock != 1 {
		t.Fatalf("expected focusedBlock to be 1, got %d", m.detailState.focusedBlock)
	}
	testutils.AssertTUISnapshot(t, "logs_choice_detail_content_focused", normalizeOutput(m.View()))

	// 4. 模拟按 C 键触发复制
	newModel, copyCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = newModel.(*Model)
	if m.detailState.copiedNotification == "" {
		t.Fatal("expected copiedNotification to be populated after pressing C")
	}
	if copyCmd == nil {
		t.Fatal("expected non-nil copyCmd")
	}
	// 验证复制提示条的展示，并记录快照
	testutils.AssertTUISnapshot(t, "logs_choice_detail_copy_notified", normalizeOutput(m.View()))

	// 5. 模拟按 Enter 键展开当前聚焦的 content 块
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if m.detailState.blockCollapsed["content"] {
		t.Fatal("expected content block to be expanded after pressing Enter")
	}
	testutils.AssertTUISnapshot(t, "logs_choice_detail_content_expanded", normalizeOutput(m.View()))

	// 6. 模拟再按一次 Enter 键折叠 content 块
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(*Model)
	if !m.detailState.blockCollapsed["content"] {
		t.Fatal("expected content block to be collapsed after pressing Enter again")
	}

	// 按 ESC 退出单项详情，回到 Choices 列表
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(*Model)
	if m.detailState.itemDetailMode {
		t.Fatal("expected itemDetailMode to be false after pressing ESC on choice detail")
	}

	// 按 ESC 退出 Choices 列表，回到 main 视图
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(*Model)
	if m.detailState.activeTab != "main" {
		t.Fatalf("expected activeTab to return to 'main', got '%s'", m.detailState.activeTab)
	}

	// 5. 投递 'esc' 键盘消息，退回列表视图
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(*Model)

	if m.viewMode != "list" {
		t.Fatalf("expected viewMode to return to 'list', got '%s'", m.viewMode)
	}
	testutils.AssertTUISnapshot(t, "logs_list_returned", normalizeOutput(m.View()))
}

func TestLogsTUI_Unauthorized(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	transport := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"error": {"message": "Unauthorized access"}}`)),
				Header:     header,
			}, nil
		},
	}

	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://mock-api.litellm.local",
	}
	c := client.New(cfg, api.WithTransport(transport))

	m := NewModel(c, 5, "")
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newModel.(*Model)

	// 执行 RefreshCmd 内部命令以捕获错误响应
	cmd := m.RefreshCmd()
	msg := cmd() // 由于 mockTransport 返回 401 错误，这会返回一个带有 Error 的 LogsLoadedMsg

	newModel, _ = m.Update(msg)
	m = newModel.(*Model)

	// 验证 Model 正常展现错误提示且不崩溃
	if !strings.Contains(m.data, "Unauthorized access") {
		t.Errorf("expected error message in m.data, got '%s'", m.data)
	}

	testutils.AssertTUISnapshot(t, "logs_unauthorized", normalizeOutput(m.View()))
}

func TestLogsTUI_Forbidden_v2(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	// 模拟 /spend/logs/ui (v2) 返回 403 Forbidden
	// 并且 /spend/logs (v1) 返回成功的聚合日志响应
	mockV1Resp := `[
		{
			"spend": 0.005,
			"key_alias": "my-test-key",
			"request_id": "req-old-1"
		}
	]`

	transport := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")

			if req.URL.Path == "/spend/logs/ui" {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader(`{"error": {"message": "Forbidden"}}`)),
					Header:     header,
				}, nil
			}

			if req.URL.Path == "/spend/logs" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(mockV1Resp)),
					Header:     header,
				}, nil
			}

			return nil, errors.New("unhandled mock path: " + req.URL.Path)
		},
	}

	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://mock-api.litellm.local",
	}
	c := client.New(cfg, api.WithTransport(transport))

	m := NewModel(c, 5, "")
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newModel.(*Model)

	// 执行 RefreshCmd，底层会自动进行 v2 -> v1 降级
	cmd := m.RefreshCmd()
	msg := cmd()

	// 将降级后的 LogsLoadedMsg 投递给 Model
	newModel, _ = m.Update(msg)
	m = newModel.(*Model)

	// 验证是否进入了 v1 数据状态
	if m.logDataOld == nil {
		t.Fatal("expected logDataOld to be populated after fallback")
	}
	if m.logData != nil {
		t.Fatal("expected logData to be nil after fallback")
	}

	testutils.AssertTUISnapshot(t, "logs_forbidden_v2_fallback", normalizeOutput(m.View()))
}

func TestLogsTUI_InternalServerError(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	// 模拟 v2 和 v1 接口均返回 500 Internal Server Error
	transport := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error": {"message": "Internal Server Error"}}`)),
				Header:     header,
			}, nil
		},
	}

	cfg := &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://mock-api.litellm.local",
	}
	c := client.New(cfg, api.WithTransport(transport))

	m := NewModel(c, 5, "")
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newModel.(*Model)

	// 触发刷新命令并捕获最终的 500 错误
	cmd := m.RefreshCmd()
	msg := cmd()

	newModel, _ = m.Update(msg)
	m = newModel.(*Model)

	// 验证错误横幅正常展示，系统不崩溃
	if !strings.Contains(m.data, "Internal Server Error") {
		t.Errorf("expected Internal Server Error in m.data, got '%s'", m.data)
	}

	testutils.AssertTUISnapshot(t, "logs_internal_server_error", normalizeOutput(m.View()))
}
