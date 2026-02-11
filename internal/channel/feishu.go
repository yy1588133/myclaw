package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

const feishuChannelName = "feishu"

const (
	feishuInboundImageMaxBytes = 10 << 20 // 10MB
	feishuInboundImageTimeout  = 10 * time.Second
)

type FeishuImageDownloader func(ctx context.Context, tenantAccessToken, imageKey string) (string, string, error)

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
	cfg             config.FeishuConfig
	client          FeishuClient
	server          *http.Server
	cancel          context.CancelFunc
	clientFactory   FeishuClientFactory
	imageDownloader FeishuImageDownloader
}

func NewFeishuChannel(cfg config.FeishuConfig, b *bus.MessageBus) (*FeishuChannel, error) {
	return NewFeishuChannelWithFactory(cfg, b, defaultFeishuClientFactory)
}

func NewFeishuChannelWithFactory(cfg config.FeishuConfig, b *bus.MessageBus, factory FeishuClientFactory) (*FeishuChannel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}

	ch := &FeishuChannel{
		BaseChannel:     NewBaseChannel(feishuChannelName, b, cfg.AllowFrom),
		cfg:             cfg,
		clientFactory:   factory,
		imageDownloader: downloadFeishuImageAsBase64,
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

	messageType := strings.ToLower(strings.TrimSpace(event.Event.Message.MessageType))
	content, contentBlocks, messageMetadata, err := f.parseFeishuInboundMessage(
		context.Background(),
		messageType,
		event.Event.Message.Content,
	)
	if err != nil {
		log.Printf("[feishu] parse message error: %v", err)
		return
	}
	if content == "" && len(contentBlocks) == 0 {
		return
	}

	metadata := map[string]any{"message_type": event.Event.Message.MessageType}
	for k, v := range messageMetadata {
		metadata[k] = v
	}

	f.bus.Inbound <- bus.InboundMessage{
		Channel:       feishuChannelName,
		SenderID:      senderID,
		ChatID:        event.Event.Message.ChatID,
		Content:       content,
		Timestamp:     time.Now(),
		ContentBlocks: contentBlocks,
		Metadata:      metadata,
	}
}

func (f *FeishuChannel) parseFeishuInboundMessage(ctx context.Context, messageType, rawContent string) (string, []model.ContentBlock, map[string]any, error) {
	if messageType == "" {
		return "", nil, nil, nil
	}

	switch messageType {
	case "text":
		var textContent struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(rawContent), &textContent); err != nil {
			return "", nil, nil, fmt.Errorf("parse text content: %w", err)
		}
		content := strings.TrimSpace(textContent.Text)
		if content == "" {
			return "", nil, nil, nil
		}
		return content, nil, nil, nil

	case "image":
		var imageContent struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(rawContent), &imageContent); err != nil {
			return "", nil, nil, fmt.Errorf("parse image content: %w", err)
		}

		imageKey := strings.TrimSpace(imageContent.ImageKey)
		if imageKey == "" {
			return "", nil, nil, fmt.Errorf("missing image_key")
		}

		block, err := f.buildFeishuImageContentBlock(ctx, imageKey)
		if err != nil {
			log.Printf("[feishu] image download warning: %v", err)
		}
		if block == nil {
			return "[image]", nil, map[string]any{"image_key": imageKey}, nil
		}
		return "[image]", []model.ContentBlock{*block}, map[string]any{"image_key": imageKey}, nil

	default:
		return "", nil, nil, nil
	}
}

func (f *FeishuChannel) buildFeishuImageContentBlock(ctx context.Context, imageKey string) (*model.ContentBlock, error) {
	if f.client == nil {
		return nil, fmt.Errorf("feishu client not initialized")
	}

	tenantAccessToken, err := f.client.GetTenantAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get tenant access token: %w", err)
	}

	downloader := f.imageDownloader
	if downloader == nil {
		downloader = downloadFeishuImageAsBase64
	}

	base64Data, mediaType, err := downloader(ctx, tenantAccessToken, imageKey)
	if err != nil {
		return &model.ContentBlock{
			Type: model.ContentBlockImage,
			URL:  buildFeishuImageDownloadURL(imageKey),
		}, fmt.Errorf("download image %q: %w", imageKey, err)
	}

	return &model.ContentBlock{
		Type:      model.ContentBlockImage,
		MediaType: mediaType,
		Data:      base64Data,
	}, nil
}

func buildFeishuImageDownloadURL(imageKey string) string {
	return fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/images/%s?image_type=message", url.PathEscape(strings.TrimSpace(imageKey)))
}

func downloadFeishuImageAsBase64(ctx context.Context, tenantAccessToken, imageKey string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildFeishuImageDownloadURL(imageKey), nil)
	if err != nil {
		return "", "", fmt.Errorf("create image request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tenantAccessToken)

	httpClient := &http.Client{Timeout: feishuInboundImageTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request image: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, feishuInboundImageMaxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read image response: %w", err)
	}
	if int64(len(body)) > feishuInboundImageMaxBytes {
		return "", "", fmt.Errorf("image exceeds %d bytes", feishuInboundImageMaxBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("image request failed with status %d", resp.StatusCode)
	}

	mediaType := normalizeFeishuMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = http.DetectContentType(body)
	}

	// TODO: 如遇 image_type=message 不适配的场景，补充根据会话上下文选择下载参数。
	return base64.StdEncoding.EncodeToString(body), mediaType, nil
}

func normalizeFeishuMediaType(value string) string {
	contentType := strings.TrimSpace(value)
	if contentType == "" {
		return ""
	}
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = contentType[:idx]
	}
	return strings.TrimSpace(contentType)
}
