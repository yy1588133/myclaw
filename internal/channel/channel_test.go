package channel

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

func TestBaseChannel_Name(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch := NewBaseChannel("test", b, nil)
	if ch.Name() != "test" {
		t.Errorf("Name = %q, want test", ch.Name())
	}
}

func TestBaseChannel_IsAllowed_NoFilter(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch := NewBaseChannel("test", b, nil)
	if !ch.IsAllowed("anyone") {
		t.Error("should allow anyone when allowFrom is empty")
	}
}

func TestBaseChannel_IsAllowed_WithFilter(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch := NewBaseChannel("test", b, []string{"user1", "user2"})

	if !ch.IsAllowed("user1") {
		t.Error("should allow user1")
	}
	if !ch.IsAllowed("user2") {
		t.Error("should allow user2")
	}
	if ch.IsAllowed("user3") {
		t.Error("should reject user3")
	}
}

func TestNewTelegramChannel_NoToken(t *testing.T) {
	b := bus.NewMessageBus(10)
	_, err := NewTelegramChannel(config.TelegramConfig{}, b)
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestNewTelegramChannel_Valid(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, err := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name() != "telegram" {
		t.Errorf("Name = %q, want telegram", ch.Name())
	}
}

func TestToTelegramHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"**bold**", "<b>bold</b>"},
		{"`code`", "<code>code</code>"},
		{"a & b", "a &amp; b"},
		{"<tag>", "&lt;tag&gt;"},
	}

	for _, tt := range tests {
		got := toTelegramHTML(tt.input)
		if got != tt.want {
			t.Errorf("toTelegramHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChannelManager_Empty(t *testing.T) {
	b := bus.NewMessageBus(10)
	m, err := NewChannelManager(config.ChannelsConfig{}, b)
	if err != nil {
		t.Fatalf("NewChannelManager error: %v", err)
	}
	if len(m.EnabledChannels()) != 0 {
		t.Errorf("expected 0 enabled channels, got %d", len(m.EnabledChannels()))
	}
}

func TestToTelegramHTML_CodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"code block with language",
			"```go\nfunc main() {}\n```",
			"<pre>func main() {}\n</pre>",
		},
		{
			"code block without language",
			"```\ncode here\n```",
			"<pre>\ncode here\n</pre>",
		},
		{
			"italic text",
			"*italic*",
			"<i>italic</i>",
		},
		{
			"mixed bold and italic",
			"**bold** and *italic*",
			"<b>bold</b> and <i>italic</i>",
		},
		{
			"unclosed code block",
			"```code",
			"<code></code>`code", // best-effort: processes inline code
		},
		{
			"unclosed inline code",
			"`code",
			"`code", // no closing backtick, unchanged
		},
		{
			"unclosed bold",
			"**bold",
			"<i></i>bold", // best-effort: processes single * as italic
		},
		{
			"unclosed italic",
			"*italic",
			"*italic", // no closing *, unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toTelegramHTML(tt.input)
			if got != tt.want {
				t.Errorf("toTelegramHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTelegramChannel_Stop_NotStarted(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	// Should not panic when stopping before starting
	err := ch.Stop()
	if err != nil {
		t.Errorf("Stop error: %v", err)
	}
}

func TestTelegramChannel_Send_NilBot(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: "test"})
	if err == nil {
		t.Error("expected error when bot is nil")
	}
}

func TestTelegramChannel_WithProxy(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, err := NewTelegramChannel(config.TelegramConfig{
		Token: "fake-token",
		Proxy: "http://proxy.local:8080",
	}, b)
	if err != nil {
		t.Fatalf("NewTelegramChannel error: %v", err)
	}
	if ch.proxy != "http://proxy.local:8080" {
		t.Errorf("proxy = %q, want http://proxy.local:8080", ch.proxy)
	}
}

func TestChannelManager_StartAll_Empty(t *testing.T) {
	b := bus.NewMessageBus(10)
	m, _ := NewChannelManager(config.ChannelsConfig{}, b)

	ctx := context.Background()
	if err := m.StartAll(ctx); err != nil {
		t.Errorf("StartAll error: %v", err)
	}
}

func TestChannelManager_StopAll_Empty(t *testing.T) {
	b := bus.NewMessageBus(10)
	m, _ := NewChannelManager(config.ChannelsConfig{}, b)

	if err := m.StopAll(); err != nil {
		t.Errorf("StopAll error: %v", err)
	}
}

// mockChannel implements Channel interface for testing
type mockChannel struct {
	name     string
	started  bool
	stopped  bool
	startErr error
	stopErr  error
	sentMsgs []bus.OutboundMessage
}

func (m *mockChannel) Name() string { return m.name }

func (m *mockChannel) Start(ctx context.Context) error {
	m.started = true
	return m.startErr
}

func (m *mockChannel) Stop() error {
	m.stopped = true
	return m.stopErr
}

func (m *mockChannel) Send(msg bus.OutboundMessage) error {
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func TestChannelManager_WithMockChannel(t *testing.T) {
	b := bus.NewMessageBus(10)

	mock := &mockChannel{name: "mock"}

	m := &ChannelManager{
		channels: map[string]Channel{"mock": mock},
		bus:      b,
	}

	// Test StartAll
	ctx := context.Background()
	if err := m.StartAll(ctx); err != nil {
		t.Errorf("StartAll error: %v", err)
	}
	if !mock.started {
		t.Error("mock channel should be started")
	}

	// Test EnabledChannels
	channels := m.EnabledChannels()
	if len(channels) != 1 || channels[0] != "mock" {
		t.Errorf("EnabledChannels = %v, want [mock]", channels)
	}

	// Test StopAll
	if err := m.StopAll(); err != nil {
		t.Errorf("StopAll error: %v", err)
	}
	if !mock.stopped {
		t.Error("mock channel should be stopped")
	}
}

func TestChannelManager_StartAll_Error(t *testing.T) {
	b := bus.NewMessageBus(10)

	mock := &mockChannel{name: "mock", startErr: fmt.Errorf("start failed")}

	m := &ChannelManager{
		channels: map[string]Channel{"mock": mock},
		bus:      b,
	}

	ctx := context.Background()
	err := m.StartAll(ctx)
	if err == nil {
		t.Error("expected error from StartAll")
	}
}

func TestChannelManager_StopAll_Error(t *testing.T) {
	b := bus.NewMessageBus(10)

	mock := &mockChannel{name: "mock", stopErr: fmt.Errorf("stop failed")}

	m := &ChannelManager{
		channels: map[string]Channel{"mock": mock},
		bus:      b,
	}

	// Should not return error (errors are logged)
	if err := m.StopAll(); err != nil {
		t.Errorf("StopAll should not return error: %v", err)
	}
}

func TestTelegramChannel_Send_InvalidChatID(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	// Set bot to mock
	ch.SetBot(newMockBot())

	err := ch.Send(bus.OutboundMessage{ChatID: "not-a-number", Content: "test"})
	if err == nil {
		t.Error("expected error for invalid chat ID")
	}
}

func TestTelegramChannel_HandleMessage_Allowed(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123, UserName: "testuser"},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "hello",
		Date: 1234567890,
	}

	ch.handleMessage(msg)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "hello" {
			t.Errorf("content = %q, want hello", inbound.Content)
		}
		if inbound.SenderID != "123" {
			t.Errorf("senderID = %q, want 123", inbound.SenderID)
		}
		if inbound.ChatID != "456" {
			t.Errorf("chatID = %q, want 456", inbound.ChatID)
		}
	default:
		t.Error("expected inbound message")
	}
}

func TestTelegramChannel_HandleMessage_Rejected(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{
		Token:     "fake-token",
		AllowFrom: []string{"999"}, // Only allow user 999
	}, b)

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123, UserName: "testuser"}, // User 123 not allowed
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "hello",
	}

	ch.handleMessage(msg)

	// Should not receive any message
	select {
	case <-b.Inbound:
		t.Error("should not receive message from rejected user")
	default:
		// OK - no message sent
	}
}

func TestTelegramChannel_HandleMessage_EmptyText(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123},
		Chat: &tgbotapi.Chat{ID: 456},
		Text: "", // Empty text
	}

	ch.handleMessage(msg)

	// Should not receive any message
	select {
	case <-b.Inbound:
		t.Error("should not send message with empty content")
	default:
		// OK
	}
}

func TestTelegramChannel_HandleMessage_Caption(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	msg := &tgbotapi.Message{
		From:    &tgbotapi.User{ID: 123},
		Chat:    &tgbotapi.Chat{ID: 456},
		Text:    "",
		Caption: "image caption", // Caption instead of text
	}

	ch.handleMessage(msg)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "image caption" {
			t.Errorf("content = %q, want 'image caption'", inbound.Content)
		}
	default:
		t.Error("expected inbound message")
	}
}

func TestTelegramChannel_HandleMessage_Photo(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	mockBot := newMockBot()
	mockBot.files["photo-large"] = tgbotapi.File{FileID: "photo-large", FilePath: "photos/large.jpg"}
	ch.SetBot(mockBot)

	photoData := []byte{0xff, 0xd8, 0xff, 0xd9}
	ch.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(photoData)),
			Header:     make(http.Header),
		}, nil
	})}

	msg := &tgbotapi.Message{
		From:    &tgbotapi.User{ID: 123},
		Chat:    &tgbotapi.Chat{ID: 456},
		Caption: "photo caption",
		Photo: []tgbotapi.PhotoSize{
			{FileID: "photo-small"},
			{FileID: "photo-large"},
		},
	}

	ch.handleMessage(msg)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "photo caption" {
			t.Errorf("content = %q, want 'photo caption'", inbound.Content)
		}
		if len(inbound.ContentBlocks) != 1 {
			t.Fatalf("content blocks len = %d, want 1", len(inbound.ContentBlocks))
		}
		block := inbound.ContentBlocks[0]
		if block.Type != model.ContentBlockImage {
			t.Errorf("content block type = %q, want %q", block.Type, model.ContentBlockImage)
		}
		if block.MediaType != "image/jpeg" {
			t.Errorf("content block media type = %q, want image/jpeg", block.MediaType)
		}
		if block.Data != base64.StdEncoding.EncodeToString(photoData) {
			t.Errorf("content block data mismatch")
		}
	default:
		t.Error("expected inbound message")
	}
}

func TestTelegramChannel_HandleMessage_PhotoWithCaption(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	mockBot := newMockBot()
	mockBot.files["photo-large"] = tgbotapi.File{FileID: "photo-large", FilePath: "photos/large.jpg"}
	ch.SetBot(mockBot)

	photoData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file/botfake-token/photos/large.jpg" {
			t.Fatalf("download path = %q, want /file/botfake-token/photos/large.jpg", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(photoData)
	}))
	defer downloadServer.Close()

	serverURL, err := url.Parse(downloadServer.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	ch.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clonedReq := req.Clone(req.Context())
		clonedReq.URL.Scheme = serverURL.Scheme
		clonedReq.URL.Host = serverURL.Host
		return transport.RoundTrip(clonedReq)
	})}

	msg := &tgbotapi.Message{
		From:    &tgbotapi.User{ID: 123},
		Chat:    &tgbotapi.Chat{ID: 456},
		Caption: "photo caption via server",
		Photo: []tgbotapi.PhotoSize{
			{FileID: "photo-small"},
			{FileID: "photo-large"},
		},
	}

	ch.handleMessage(msg)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "photo caption via server" {
			t.Errorf("content = %q, want 'photo caption via server'", inbound.Content)
		}
		if len(inbound.ContentBlocks) != 1 {
			t.Fatalf("content blocks len = %d, want 1", len(inbound.ContentBlocks))
		}
		block := inbound.ContentBlocks[0]
		if block.Type != model.ContentBlockImage {
			t.Errorf("content block type = %q, want %q", block.Type, model.ContentBlockImage)
		}
		if block.MediaType != "image/png" {
			t.Errorf("content block media type = %q, want image/png", block.MediaType)
		}
		if block.Data != base64.StdEncoding.EncodeToString(photoData) {
			t.Errorf("content block data mismatch")
		}
	case <-time.After(time.Second):
		t.Error("expected inbound message")
	}
}

func TestWeComCallback_ImageMessage(t *testing.T) {
	imageData := []byte{0xff, 0xd8, 0xff, 0xd9}
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.jpg" {
			t.Fatalf("image path = %q, want /image.jpg", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imageData)
	}))
	defer imageServer.Close()

	ch, b := newTestWeComChannel(t, config.WeComConfig{
		Token:          "verify-token",
		EncodingAESKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
		ReceiveID:      "recv-id-1",
		AllowFrom:      []string{"zhangsan"},
	})

	timestamp := "1739000200"
	nonce := "nonce-image"
	imageURL := imageServer.URL + "/image.jpg"
	plaintext := fmt.Sprintf(`{"msgid":"20001","aibotid":"AIBOTID","chattype":"single","from":{"userid":"zhangsan"},"response_url":"https://example.com/resp","msgtype":"image","image":{"url":"%s"}}`, imageURL)
	encrypt := testWeComEncrypt(t, ch.cfg.EncodingAESKey, ch.receiveID, plaintext)
	signature := testWeComSignature(ch.cfg.Token, timestamp, nonce, encrypt)
	body := testWeComEncryptedRequestBody(t, encrypt)

	req := httptest.NewRequest(http.MethodPost, "/wecom/bot", strings.NewReader(body))
	q := req.URL.Query()
	q.Set("msg_signature", signature)
	q.Set("timestamp", timestamp)
	q.Set("nonce", nonce)
	req.URL.RawQuery = q.Encode()
	w := httptest.NewRecorder()

	ch.handleCallback(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "[image]" {
			t.Errorf("content = %q, want [image]", inbound.Content)
		}
		if len(inbound.ContentBlocks) != 1 {
			t.Fatalf("content blocks len = %d, want 1", len(inbound.ContentBlocks))
		}
		block := inbound.ContentBlocks[0]
		if block.Type != model.ContentBlockImage {
			t.Errorf("content block type = %q, want %q", block.Type, model.ContentBlockImage)
		}
		if block.MediaType != "image/jpeg" {
			t.Errorf("content block media type = %q, want image/jpeg", block.MediaType)
		}
		if block.Data != base64.StdEncoding.EncodeToString(imageData) {
			t.Errorf("content block data mismatch")
		}
		if inbound.Metadata["image_url"] != imageURL {
			t.Errorf("metadata image_url = %v, want %s", inbound.Metadata["image_url"], imageURL)
		}
	case <-time.After(time.Second):
		t.Fatal("expected inbound message")
	}
}

func TestTelegramChannel_HandleMessage_Document(t *testing.T) {
	b := bus.NewMessageBus(10)
	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	mockBot := newMockBot()
	mockBot.files["doc-1"] = tgbotapi.File{FileID: "doc-1", FilePath: "docs/file.pdf"}
	ch.SetBot(mockBot)

	pdfData := []byte("%PDF-1.4\n")
	ch.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(pdfData)),
			Header:     make(http.Header),
		}, nil
	})}

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 123},
		Chat: &tgbotapi.Chat{ID: 456},
		Document: &tgbotapi.Document{
			FileID:   "doc-1",
			MimeType: "application/pdf",
		},
	}

	ch.handleMessage(msg)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "" {
			t.Errorf("content = %q, want empty", inbound.Content)
		}
		if len(inbound.ContentBlocks) != 1 {
			t.Fatalf("content blocks len = %d, want 1", len(inbound.ContentBlocks))
		}
		block := inbound.ContentBlocks[0]
		if block.Type != model.ContentBlockDocument {
			t.Errorf("content block type = %q, want %q", block.Type, model.ContentBlockDocument)
		}
		if block.MediaType != "application/pdf" {
			t.Errorf("content block media type = %q, want application/pdf", block.MediaType)
		}
		if block.Data != base64.StdEncoding.EncodeToString(pdfData) {
			t.Errorf("content block data mismatch")
		}
	default:
		t.Error("expected inbound message")
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// mockTelegramBot implements TelegramBot interface for testing
type mockTelegramBot struct {
	updatesChan chan tgbotapi.Update
	stopped     bool
	sentMsgs    []tgbotapi.Chattable
	sendErr     error
	getFileErr  error
	files       map[string]tgbotapi.File
	self        tgbotapi.User
}

func newMockBot() *mockTelegramBot {
	return &mockTelegramBot{
		updatesChan: make(chan tgbotapi.Update, 10),
		files:       make(map[string]tgbotapi.File),
		self:        tgbotapi.User{UserName: "testbot"},
	}
}

func (m *mockTelegramBot) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return m.updatesChan
}

func (m *mockTelegramBot) StopReceivingUpdates() {
	m.stopped = true
}

func (m *mockTelegramBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.sentMsgs = append(m.sentMsgs, c)
	if m.sendErr != nil {
		return tgbotapi.Message{}, m.sendErr
	}
	return tgbotapi.Message{MessageID: 1}, nil
}

func (m *mockTelegramBot) GetSelf() tgbotapi.User {
	return m.self
}

func (m *mockTelegramBot) GetFile(config tgbotapi.FileConfig) (tgbotapi.File, error) {
	if m.getFileErr != nil {
		return tgbotapi.File{}, m.getFileErr
	}
	file, ok := m.files[config.FileID]
	if !ok {
		return tgbotapi.File{}, fmt.Errorf("file %q not found", config.FileID)
	}
	return file, nil
}

func TestTelegramChannel_InitBot_Success(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	factory := func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
		return mockBot, nil
	}

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{Token: "fake-token"}, b, factory)

	err := ch.initBot()
	if err != nil {
		t.Errorf("initBot error: %v", err)
	}
	if ch.bot == nil {
		t.Error("bot should be set")
	}
}

func TestTelegramChannel_InitBot_Error(t *testing.T) {
	b := bus.NewMessageBus(10)

	factory := func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
		return nil, fmt.Errorf("auth failed")
	}

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{Token: "fake-token"}, b, factory)

	err := ch.initBot()
	if err == nil {
		t.Error("expected error from initBot")
	}
}

func TestTelegramChannel_InitBot_InvalidProxy(t *testing.T) {
	b := bus.NewMessageBus(10)

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{
		Token: "fake-token",
		Proxy: "://invalid-url",
	}, b, defaultBotFactory)

	err := ch.initBot()
	if err == nil {
		t.Error("expected error for invalid proxy URL")
	}
}

func TestTelegramChannel_Start_Success(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	factory := func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
		return mockBot, nil
	}

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{Token: "fake-token"}, b, factory)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ch.Start(ctx)
	if err != nil {
		t.Errorf("Start error: %v", err)
	}

	// Send a test update
	mockBot.updatesChan <- tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 123},
			Chat: &tgbotapi.Chat{ID: 456},
			Text: "test message",
		},
	}

	// Wait for message to be processed
	time.Sleep(100 * time.Millisecond)

	select {
	case inbound := <-b.Inbound:
		if inbound.Content != "test message" {
			t.Errorf("content = %q, want 'test message'", inbound.Content)
		}
	default:
		t.Error("expected inbound message")
	}

	// Test stop
	ch.Stop()
	if !mockBot.stopped {
		t.Error("bot should be stopped")
	}
}

func TestTelegramChannel_Start_InitError(t *testing.T) {
	b := bus.NewMessageBus(10)

	factory := func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
		return nil, fmt.Errorf("init failed")
	}

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{Token: "fake-token"}, b, factory)

	err := ch.Start(context.Background())
	if err == nil {
		t.Error("expected error from Start")
	}
}

func TestTelegramChannel_Start_NilMessage(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	factory := func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
		return mockBot, nil
	}

	ch, _ := NewTelegramChannelWithFactory(config.TelegramConfig{Token: "fake-token"}, b, factory)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch.Start(ctx)

	// Send update with nil message (should be ignored)
	mockBot.updatesChan <- tgbotapi.Update{Message: nil}

	time.Sleep(50 * time.Millisecond)

	select {
	case <-b.Inbound:
		t.Error("should not receive message for nil update")
	default:
		// OK
	}
}

func TestTelegramChannel_Send_Success(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	ch.SetBot(mockBot)

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: "hello"})
	if err != nil {
		t.Errorf("Send error: %v", err)
	}

	if len(mockBot.sentMsgs) != 1 {
		t.Errorf("expected 1 sent message, got %d", len(mockBot.sentMsgs))
	}
}

func TestTelegramChannel_Send_LongMessage(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	ch.SetBot(mockBot)

	// Create a message longer than 4000 chars with newlines
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "This is a long line of text that will be repeated.\n"
	}

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: longContent})
	if err != nil {
		t.Errorf("Send error: %v", err)
	}

	// Should split into multiple messages
	if len(mockBot.sentMsgs) < 2 {
		t.Errorf("expected multiple sent messages for long content, got %d", len(mockBot.sentMsgs))
	}
}

func TestTelegramChannel_Send_LongMessageNoNewline(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	ch.SetBot(mockBot)

	// Create a long message without newlines
	longContent := ""
	for i := 0; i < 5000; i++ {
		longContent += "x"
	}

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: longContent})
	if err != nil {
		t.Errorf("Send error: %v", err)
	}

	if len(mockBot.sentMsgs) < 2 {
		t.Errorf("expected multiple messages, got %d", len(mockBot.sentMsgs))
	}
}

func TestTelegramChannel_Send_HTMLError_Retry(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()

	// First call fails (HTML parse error), second succeeds
	callCount := 0
	mockBot.sendErr = nil
	originalSend := mockBot.Send
	_ = originalSend

	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)

	// Create a wrapper that fails first then succeeds
	wrapper := &sendCountingBot{mockBot: mockBot, failFirst: true}
	ch.SetBot(wrapper)
	_ = callCount

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: "test"})
	// Should succeed after retry
	if err != nil {
		t.Errorf("Send should succeed after retry: %v", err)
	}
}

type sendCountingBot struct {
	mockBot   *mockTelegramBot
	failFirst bool
	callCount int
}

func (s *sendCountingBot) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return s.mockBot.updatesChan
}

func (s *sendCountingBot) StopReceivingUpdates() {
	s.mockBot.stopped = true
}

func (s *sendCountingBot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	s.callCount++
	if s.failFirst && s.callCount == 1 {
		return tgbotapi.Message{}, fmt.Errorf("HTML parse error")
	}
	return tgbotapi.Message{MessageID: 1}, nil
}

func (s *sendCountingBot) GetSelf() tgbotapi.User {
	return s.mockBot.self
}

func (s *sendCountingBot) GetFile(config tgbotapi.FileConfig) (tgbotapi.File, error) {
	return s.mockBot.GetFile(config)
}

func TestTelegramChannel_Send_BothFail(t *testing.T) {
	b := bus.NewMessageBus(10)
	mockBot := newMockBot()
	mockBot.sendErr = fmt.Errorf("send failed")

	ch, _ := NewTelegramChannel(config.TelegramConfig{Token: "fake-token"}, b)
	ch.SetBot(mockBot)

	err := ch.Send(bus.OutboundMessage{ChatID: "123", Content: "test"})
	if err == nil {
		t.Error("expected error when both sends fail")
	}
}
