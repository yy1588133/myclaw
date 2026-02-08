package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

// mockFeishuClient implements FeishuClient for testing
type mockFeishuClient struct {
	sentMessages []struct{ chatID, content string }
	sendErr      error
	token        string
	tokenErr     error
}

func (m *mockFeishuClient) SendMessage(ctx context.Context, chatID, content string) error {
	m.sentMessages = append(m.sentMessages, struct{ chatID, content string }{chatID, content})
	return m.sendErr
}

func (m *mockFeishuClient) GetTenantAccessToken(ctx context.Context) (string, error) {
	return m.token, m.tokenErr
}

func mockFeishuClientFactory(client *mockFeishuClient) FeishuClientFactory {
	return func(appID, appSecret string) FeishuClient {
		return client
	}
}

func TestNewFeishuChannel_Valid(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, err := NewFeishuChannel(config.FeishuConfig{
		AppID:     "cli_test",
		AppSecret: "secret",
	}, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name() != "feishu" {
		t.Errorf("Name = %q, want feishu", ch.Name())
	}
}

func TestNewFeishuChannel_MissingAppID(t *testing.T) {
	b := bus.NewMessageBus(10)
	_, err := NewFeishuChannel(config.FeishuConfig{
		AppSecret: "secret",
	}, b)
	if err == nil {
		t.Error("expected error for missing app_id")
	}
}

func TestNewFeishuChannel_MissingAppSecret(t *testing.T) {
	b := bus.NewMessageBus(10)
	_, err := NewFeishuChannel(config.FeishuConfig{
		AppID: "cli_test",
	}, b)
	if err == nil {
		t.Error("expected error for missing app_secret")
	}
}

func TestFeishuChannel_Send_NilClient(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewFeishuChannel(config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	}, b)

	err := ch.Send(bus.OutboundMessage{ChatID: "chat_123", Content: "hello"})
	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestFeishuChannel_Send_Success(t *testing.T) {
	b := bus.NewMessageBus(10)
	mock := &mockFeishuClient{token: "test-token"}

	ch, _ := NewFeishuChannelWithFactory(config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	}, b, mockFeishuClientFactory(mock))

	ch.client = mock

	err := ch.Send(bus.OutboundMessage{ChatID: "chat_123", Content: "hello"})
	if err != nil {
		t.Errorf("Send error: %v", err)
	}
	if len(mock.sentMessages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(mock.sentMessages))
	}
	if mock.sentMessages[0].chatID != "chat_123" {
		t.Errorf("chatID = %q, want chat_123", mock.sentMessages[0].chatID)
	}
}

func TestFeishuChannel_Send_Error(t *testing.T) {
	b := bus.NewMessageBus(10)
	mock := &mockFeishuClient{sendErr: fmt.Errorf("send failed")}

	ch, _ := NewFeishuChannelWithFactory(config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	}, b, mockFeishuClientFactory(mock))
	ch.client = mock

	err := ch.Send(bus.OutboundMessage{ChatID: "chat_123", Content: "hello"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestFeishuChannel_Stop_NotStarted(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewFeishuChannel(config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	}, b)

	err := ch.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

// --- Webhook handler tests ---

func newTestFeishuChannel(t *testing.T, cfg config.FeishuConfig) (*FeishuChannel, *bus.MessageBus) {
	t.Helper()
	b := bus.NewMessageBus(10)
	mock := &mockFeishuClient{token: "test-token"}
	ch, err := NewFeishuChannelWithFactory(cfg, b, mockFeishuClientFactory(mock))
	if err != nil {
		t.Fatalf("NewFeishuChannelWithFactory error: %v", err)
	}
	ch.client = mock
	return ch, b
}

func TestFeishuWebhook_Challenge(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	body := `{"challenge":"test-challenge-token","token":"xxx","type":"url_verification"}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["challenge"] != "test-challenge-token" {
		t.Errorf("challenge = %q, want test-challenge-token", resp["challenge"])
	}
}

func TestFeishuWebhook_MethodNotAllowed(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/feishu/webhook", nil)
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestFeishuWebhook_InvalidJSON(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestFeishuWebhook_InvalidToken(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID:             "cli_test",
		AppSecret:         "secret",
		VerificationToken: "correct-token",
	})

	body := `{"header":{"event_type":"im.message.receive_v1","token":"wrong-token"},"event":{}}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestFeishuWebhook_ValidToken(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID:             "cli_test",
		AppSecret:         "secret",
		VerificationToken: "correct-token",
	})

	body := `{"header":{"event_type":"other.event","token":"correct-token"},"event":{}}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestFeishuWebhook_MessageReceive(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	event := map[string]interface{}{
		"header": map[string]interface{}{
			"event_type": "im.message.receive_v1",
			"token":      "",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]interface{}{
					"open_id": "ou_test123",
				},
			},
			"message": map[string]interface{}{
				"chat_id":      "oc_chat456",
				"message_type": "text",
				"content":      `{"text":"hello myclaw"}`,
			},
		},
	}
	data, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(string(data)))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	select {
	case msg := <-b.Inbound:
		if msg.Content != "hello myclaw" {
			t.Errorf("content = %q, want 'hello myclaw'", msg.Content)
		}
		if msg.SenderID != "ou_test123" {
			t.Errorf("senderID = %q, want ou_test123", msg.SenderID)
		}
		if msg.ChatID != "oc_chat456" {
			t.Errorf("chatID = %q, want oc_chat456", msg.ChatID)
		}
		if msg.Channel != "feishu" {
			t.Errorf("channel = %q, want feishu", msg.Channel)
		}
	case <-time.After(time.Second):
		t.Error("expected inbound message")
	}
}

func TestFeishuWebhook_RejectedSender(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID:     "cli_test",
		AppSecret: "secret",
		AllowFrom: []string{"ou_allowed"},
	})

	event := map[string]interface{}{
		"header": map[string]interface{}{
			"event_type": "im.message.receive_v1",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]interface{}{
					"open_id": "ou_rejected",
				},
			},
			"message": map[string]interface{}{
				"chat_id":      "oc_chat",
				"message_type": "text",
				"content":      `{"text":"hello"}`,
			},
		},
	}
	data, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(string(data)))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	select {
	case <-b.Inbound:
		t.Error("should not receive message from rejected sender")
	default:
		// OK
	}
}

func TestFeishuWebhook_NonTextMessage(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	event := map[string]interface{}{
		"header": map[string]interface{}{
			"event_type": "im.message.receive_v1",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]interface{}{
					"open_id": "ou_test",
				},
			},
			"message": map[string]interface{}{
				"chat_id":      "oc_chat",
				"message_type": "image",
				"content":      `{"image_key":"img_xxx"}`,
			},
		},
	}
	data, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(string(data)))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	select {
	case <-b.Inbound:
		t.Error("should not receive non-text message")
	default:
		// OK
	}
}

func TestFeishuWebhook_EmptyText(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	event := map[string]interface{}{
		"header": map[string]interface{}{
			"event_type": "im.message.receive_v1",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]interface{}{
					"open_id": "ou_test",
				},
			},
			"message": map[string]interface{}{
				"chat_id":      "oc_chat",
				"message_type": "text",
				"content":      `{"text":""}`,
			},
		},
	}
	data, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(string(data)))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	select {
	case <-b.Inbound:
		t.Error("should not receive empty text message")
	default:
		// OK
	}
}

func TestFeishuWebhook_InvalidContent(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	event := map[string]interface{}{
		"header": map[string]interface{}{
			"event_type": "im.message.receive_v1",
		},
		"event": map[string]interface{}{
			"sender": map[string]interface{}{
				"sender_id": map[string]interface{}{
					"open_id": "ou_test",
				},
			},
			"message": map[string]interface{}{
				"chat_id":      "oc_chat",
				"message_type": "text",
				"content":      "not-valid-json",
			},
		},
	}
	data, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(string(data)))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	select {
	case <-b.Inbound:
		t.Error("should not receive message with invalid content JSON")
	default:
		// OK
	}
}

func TestFeishuWebhook_NonMessageEvent(t *testing.T) {
	ch, b := newTestFeishuChannel(t, config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret",
	})

	body := `{"header":{"event_type":"im.chat.member.bot.added_v1"},"event":{}}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	select {
	case <-b.Inbound:
		t.Error("should not receive non-message event")
	default:
		// OK
	}
}

func TestFeishuWebhook_NoVerificationToken(t *testing.T) {
	ch, _ := newTestFeishuChannel(t, config.FeishuConfig{
		AppID:             "cli_test",
		AppSecret:         "secret",
		VerificationToken: "", // No token configured = skip verification
	})

	body := `{"header":{"event_type":"other.event","token":"any-token"},"event":{}}`
	req := httptest.NewRequest(http.MethodPost, "/feishu/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	ch.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (token verification should be skipped)", w.Code)
	}
}

func TestFeishuChannel_StartStop(t *testing.T) {
	b := bus.NewMessageBus(10)
	mock := &mockFeishuClient{token: "test-token"}

	ch, err := NewFeishuChannelWithFactory(config.FeishuConfig{
		AppID: "cli_test", AppSecret: "secret", Port: 0,
	}, b, mockFeishuClientFactory(mock))
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = ch.Start(ctx)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	err = ch.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

func TestChannelManager_FeishuEnabled(t *testing.T) {
	b := bus.NewMessageBus(10)
	m, err := NewChannelManager(config.ChannelsConfig{
		Feishu: config.FeishuConfig{
			Enabled:   true,
			AppID:     "cli_test",
			AppSecret: "secret",
		},
	}, b)
	if err != nil {
		t.Fatalf("NewChannelManager error: %v", err)
	}

	channels := m.EnabledChannels()
	if len(channels) != 1 || channels[0] != "feishu" {
		t.Errorf("EnabledChannels = %v, want [feishu]", channels)
	}
}

func TestChannelManager_FeishuEnabled_MissingConfig(t *testing.T) {
	b := bus.NewMessageBus(10)
	_, err := NewChannelManager(config.ChannelsConfig{
		Feishu: config.FeishuConfig{
			Enabled: true,
			// Missing AppID and AppSecret
		},
	}, b)
	if err == nil {
		t.Error("expected error for missing feishu config")
	}
}
