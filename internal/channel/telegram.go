package channel

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

const telegramChannelName = "telegram"

// TelegramBot interface for mocking telegram bot API
type TelegramBot interface {
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	StopReceivingUpdates()
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	GetSelf() tgbotapi.User
	GetFile(config tgbotapi.FileConfig) (tgbotapi.File, error)
}

// tgBotWrapper wraps tgbotapi.BotAPI to implement TelegramBot interface
type tgBotWrapper struct {
	bot *tgbotapi.BotAPI
}

func (w *tgBotWrapper) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return w.bot.GetUpdatesChan(config)
}

func (w *tgBotWrapper) StopReceivingUpdates() {
	w.bot.StopReceivingUpdates()
}

func (w *tgBotWrapper) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	return w.bot.Send(c)
}

func (w *tgBotWrapper) GetSelf() tgbotapi.User {
	return w.bot.Self
}

func (w *tgBotWrapper) GetFile(config tgbotapi.FileConfig) (tgbotapi.File, error) {
	return w.bot.GetFile(config)
}

// BotFactory creates TelegramBot instances (allows mocking)
type BotFactory func(token, apiEndpoint string, client *http.Client) (TelegramBot, error)

// defaultBotFactory creates real telegram bot
var defaultBotFactory BotFactory = func(token, apiEndpoint string, client *http.Client) (TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPIWithClient(token, apiEndpoint, client)
	if err != nil {
		return nil, err
	}
	return &tgBotWrapper{bot: bot}, nil
}

type TelegramChannel struct {
	BaseChannel
	token      string
	bot        TelegramBot
	proxy      string
	httpClient *http.Client
	cancel     context.CancelFunc
	botFactory BotFactory
}

func NewTelegramChannel(cfg config.TelegramConfig, b *bus.MessageBus) (*TelegramChannel, error) {
	return NewTelegramChannelWithFactory(cfg, b, defaultBotFactory)
}

// NewTelegramChannelWithFactory creates a TelegramChannel with custom bot factory (for testing)
func NewTelegramChannelWithFactory(cfg config.TelegramConfig, b *bus.MessageBus, factory BotFactory) (*TelegramChannel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram token is required")
	}

	ch := &TelegramChannel{
		BaseChannel: NewBaseChannel(telegramChannelName, b, cfg.AllowFrom),
		token:       cfg.Token,
		proxy:       cfg.Proxy,
		httpClient:  http.DefaultClient,
		botFactory:  factory,
	}
	return ch, nil
}

func (t *TelegramChannel) initBot() error {
	var client *http.Client
	if t.proxy != "" {
		proxyURL, err := url.Parse(t.proxy)
		if err != nil {
			return fmt.Errorf("parse proxy url: %w", err)
		}
		client = &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}
	} else {
		client = http.DefaultClient
	}
	t.httpClient = client

	bot, err := t.botFactory(t.token, tgbotapi.APIEndpoint, client)
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}
	t.bot = bot
	log.Printf("[telegram] authorized as @%s", bot.GetSelf().UserName)
	return nil
}

func (t *TelegramChannel) Start(ctx context.Context) error {
	if err := t.initBot(); err != nil {
		return err
	}

	ctx, t.cancel = context.WithCancel(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := t.bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case update := <-updates:
				if update.Message == nil {
					continue
				}
				t.handleMessage(update.Message)
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Printf("[telegram] polling started")
	return nil
}

func (t *TelegramChannel) handleMessage(msg *tgbotapi.Message) {
	senderID := strconv.FormatInt(msg.From.ID, 10)

	if !t.IsAllowed(senderID) {
		log.Printf("[telegram] rejected message from %s (%s)", senderID, msg.From.UserName)
		return
	}

	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}

	contentBlocks := make([]model.ContentBlock, 0, 2)

	if len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		data, err := t.downloadFileData(photo.FileID)
		if err != nil {
			log.Printf("[telegram] download photo %s failed: %v", photo.FileID, err)
		} else {
			mediaType := http.DetectContentType(data)
			if mediaType == "application/octet-stream" {
				mediaType = "image/jpeg"
			}
			contentBlocks = append(contentBlocks, model.ContentBlock{
				Type:      model.ContentBlockImage,
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(data),
			})
		}
	}

	if msg.Document != nil {
		data, err := t.downloadFileData(msg.Document.FileID)
		if err != nil {
			log.Printf("[telegram] download document %s failed: %v", msg.Document.FileID, err)
		} else {
			mediaType := msg.Document.MimeType
			if mediaType == "" {
				mediaType = http.DetectContentType(data)
			}
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			// Use image block type for image MIME types sent as documents
			blockType := model.ContentBlockDocument
			if strings.HasPrefix(mediaType, "image/") {
				blockType = model.ContentBlockImage
			}
			contentBlocks = append(contentBlocks, model.ContentBlock{
				Type:      blockType,
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(data),
			})
		}
	}

	if content == "" && len(contentBlocks) == 0 {
		return
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	t.bus.Inbound <- bus.InboundMessage{
		Channel:       telegramChannelName,
		SenderID:      senderID,
		ChatID:        chatID,
		Content:       content,
		Timestamp:     time.Unix(int64(msg.Date), 0),
		ContentBlocks: contentBlocks,
		Metadata: map[string]any{
			"username":   msg.From.UserName,
			"first_name": msg.From.FirstName,
			"message_id": msg.MessageID,
		},
	}
}

func (t *TelegramChannel) downloadFileData(fileID string) ([]byte, error) {
	if t.bot == nil {
		return nil, fmt.Errorf("telegram bot not initialized")
	}

	file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get telegram file: %w", err)
	}

	client := t.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Get(file.Link(t.token))
	if err != nil {
		return nil, fmt.Errorf("download telegram file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download telegram file: unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read telegram file body: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("telegram file is empty")
	}

	return data, nil
}

func (t *TelegramChannel) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}
	if t.bot != nil {
		t.bot.StopReceivingUpdates()
	}
	log.Printf("[telegram] stopped")
	return nil
}

// SetBot sets the bot (for testing)
func (t *TelegramChannel) SetBot(bot TelegramBot) {
	t.bot = bot
}

func (t *TelegramChannel) Send(msg bus.OutboundMessage) error {
	if t.bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}

	chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat id %q: %w", msg.ChatID, err)
	}

	content := toTelegramHTML(msg.Content)

	// Telegram has a 4096 char limit per message
	const maxLen = 4000
	for len(content) > 0 {
		chunk := content
		if len(chunk) > maxLen {
			// Try to split at last newline before maxLen
			idx := strings.LastIndex(chunk[:maxLen], "\n")
			if idx > 0 {
				chunk = chunk[:idx]
			} else {
				chunk = chunk[:maxLen]
			}
		}
		content = content[len(chunk):]

		tgMsg := tgbotapi.NewMessage(chatID, chunk)
		tgMsg.ParseMode = tgbotapi.ModeHTML
		if _, err := t.bot.Send(tgMsg); err != nil {
			// Retry without HTML parse mode
			tgMsg.ParseMode = ""
			tgMsg.Text = msg.Content
			if _, err2 := t.bot.Send(tgMsg); err2 != nil {
				return fmt.Errorf("send telegram message: %w", err2)
			}
			return nil
		}
	}
	return nil
}

// toTelegramHTML converts basic markdown to Telegram HTML.
func toTelegramHTML(s string) string {
	// Escape HTML entities first
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")

	// Code blocks: ```...``` -> <pre>...</pre>
	for {
		start := strings.Index(s, "```")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+3:], "```")
		if end == -1 {
			break
		}
		end += start + 3
		code := s[start+3 : end]
		// Strip optional language tag on first line
		if nl := strings.Index(code, "\n"); nl >= 0 {
			firstLine := strings.TrimSpace(code[:nl])
			if len(firstLine) > 0 && !strings.Contains(firstLine, " ") {
				code = code[nl+1:]
			}
		}
		s = s[:start] + "<pre>" + code + "</pre>" + s[end+3:]
	}

	// Inline code: `...` -> <code>...</code>
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		s = s[:start] + "<code>" + s[start+1:end] + "</code>" + s[end+1:]
	}

	// Bold: **...** -> <b>...</b>
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		s = s[:start] + "<b>" + s[start+2:end] + "</b>" + s[end+2:]
	}

	// Italic: *...* -> <i>...</i> (after bold to avoid conflicts)
	for {
		start := strings.Index(s, "*")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "*")
		if end == -1 {
			break
		}
		end += start + 1
		s = s[:start] + "<i>" + s[start+1:end] + "</i>" + s[end+1:]
	}

	return s
}
