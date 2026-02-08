package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

const feishuChannelName = "feishu"

// FeishuClient interface for sending messages (allows mocking)
type FeishuClient interface {
	SendMessage(ctx context.Context, chatID, content string) error
	GetTenantAccessToken(ctx context.Context) (string, error)
}

// defaultFeishuClient implements FeishuClient using Feishu Open API
type defaultFeishuClient struct {
	appID     string
	appSecret string
	mu        sync.RWMutex
	token     string
	tokenExp  time.Time
}

func (c *defaultFeishuClient) GetTenantAccessToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.token != "" && time.Now().Before(c.tokenExp) {
		token := c.token
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}

	body := fmt.Sprintf(`{"app_id":"%s","app_secret":"%s"}`, c.appID, c.appSecret)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get tenant token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("feishu token error: %s", result.Msg)
	}

	c.token = result.TenantAccessToken
	c.tokenExp = time.Now().Add(time.Duration(result.Expire-60) * time.Second)
	return c.token, nil
}

func (c *defaultFeishuClient) SendMessage(ctx context.Context, chatID, content string) error {
	token, err := c.GetTenantAccessToken(ctx)
	if err != nil {
		return err
	}

	// Use json.Marshal for proper escaping of content
	textJSON, err := json.Marshal(map[string]string{"text": content})
	if err != nil {
		return fmt.Errorf("marshal text content: %w", err)
	}

	payload := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(textJSON),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id",
		strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send feishu message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode send response: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("feishu send error: %s", result.Msg)
	}
	return nil
}

// FeishuClientFactory creates FeishuClient instances
type FeishuClientFactory func(appID, appSecret string) FeishuClient

var defaultFeishuClientFactory FeishuClientFactory = func(appID, appSecret string) FeishuClient {
	return &defaultFeishuClient{appID: appID, appSecret: appSecret}
}

type FeishuChannel struct {
	BaseChannel
	cfg           config.FeishuConfig
	client        FeishuClient
	server        *http.Server
	cancel        context.CancelFunc
	clientFactory FeishuClientFactory
}

func NewFeishuChannel(cfg config.FeishuConfig, b *bus.MessageBus) (*FeishuChannel, error) {
	return NewFeishuChannelWithFactory(cfg, b, defaultFeishuClientFactory)
}

func NewFeishuChannelWithFactory(cfg config.FeishuConfig, b *bus.MessageBus, factory FeishuClientFactory) (*FeishuChannel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}

	ch := &FeishuChannel{
		BaseChannel:   NewBaseChannel(feishuChannelName, b, cfg.AllowFrom),
		cfg:           cfg,
		clientFactory: factory,
	}
	return ch, nil
}

func (f *FeishuChannel) Start(ctx context.Context) error {
	f.client = f.clientFactory(f.cfg.AppID, f.cfg.AppSecret)

	ctx, f.cancel = context.WithCancel(ctx)

	port := f.cfg.Port
	if port == 0 {
		port = 9876
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/feishu/webhook", f.handleWebhook)

	f.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Printf("[feishu] webhook server listening on :%d", port)
		if err := f.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[feishu] server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		f.server.Close()
	}()

	return nil
}

func (f *FeishuChannel) Stop() error {
	if f.cancel != nil {
		f.cancel()
	}
	if f.server != nil {
		f.server.Close()
	}
	log.Printf("[feishu] stopped")
	return nil
}

func (f *FeishuChannel) Send(msg bus.OutboundMessage) error {
	if f.client == nil {
		return fmt.Errorf("feishu client not initialized")
	}
	return f.client.SendMessage(context.Background(), msg.ChatID, msg.Content)
}

func (f *FeishuChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	var event struct {
		Challenge string `json:"challenge"`
		Type      string `json:"type"`
		Header    struct {
			EventType string `json:"event_type"`
			Token     string `json:"token"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				ChatID      string `json:"chat_id"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
			} `json:"message"`
		} `json:"event"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// URL verification challenge
	if event.Challenge != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
		return
	}

	// Verify token
	if f.cfg.VerificationToken != "" && event.Header.Token != f.cfg.VerificationToken {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)

	// Only handle message events
	if event.Header.EventType != "im.message.receive_v1" {
		return
	}

	senderID := event.Event.Sender.SenderID.OpenID
	if !f.IsAllowed(senderID) {
		log.Printf("[feishu] rejected message from %s", senderID)
		return
	}

	if event.Event.Message.MessageType != "text" {
		return
	}

	var textContent struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(event.Event.Message.Content), &textContent); err != nil {
		log.Printf("[feishu] parse content error: %v", err)
		return
	}

	if textContent.Text == "" {
		return
	}

	f.bus.Inbound <- bus.InboundMessage{
		Channel:   feishuChannelName,
		SenderID:  senderID,
		ChatID:    event.Event.Message.ChatID,
		Content:   textContent.Text,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"message_type": event.Event.Message.MessageType,
		},
	}
}
