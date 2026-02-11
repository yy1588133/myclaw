package channel

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "modernc.org/sqlite"
)

const whatsappChannelName = "whatsapp"

const (
	whatsappInboundImageTimeout = 20 * time.Second
	whatsappSendTimeout         = 30 * time.Second
)

type WhatsAppChannel struct {
	BaseChannel
	cfg            config.WhatsAppConfig
	client         *whatsmeow.Client
	storeContainer *sqlstore.Container
	cancel         context.CancelFunc
	handlerID      uint32
}

func NewWhatsApp(cfg config.WhatsAppConfig, msgBus *bus.MessageBus) (*WhatsAppChannel, error) {
	storePath := strings.TrimSpace(cfg.StorePath)
	if storePath == "" {
		storePath = filepath.Join(config.ConfigDir(), "whatsapp-store.db")
	}

	if err := os.MkdirAll(filepath.Dir(storePath), 0755); err != nil {
		return nil, fmt.Errorf("create whatsapp store dir: %w", err)
	}

	storeDSN := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", filepath.ToSlash(storePath))
	container, err := sqlstore.New(context.Background(), "sqlite", storeDSN, waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("init whatsapp session store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		_ = container.Close()
		return nil, fmt.Errorf("get whatsapp device: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	ch := &WhatsAppChannel{
		BaseChannel:    NewBaseChannel(whatsappChannelName, msgBus, cfg.AllowFrom),
		cfg:            cfg,
		client:         client,
		storeContainer: container,
	}
	ch.handlerID = ch.client.AddEventHandler(ch.handleEvent)

	return ch, nil
}

func (w *WhatsAppChannel) Name() string {
	return whatsappChannelName
}

func (w *WhatsAppChannel) Start(ctx context.Context) error {
	if w.client == nil {
		return fmt.Errorf("whatsapp client not initialized")
	}

	ctx, w.cancel = context.WithCancel(ctx)

	if w.client.Store.ID == nil {
		qrChan, err := w.client.GetQRChannel(ctx)
		if err != nil {
			w.cancel()
			return fmt.Errorf("get whatsapp qr channel: %w", err)
		}
		go w.consumeQR(ctx, qrChan)
	}

	if err := w.client.Connect(); err != nil {
		w.cancel()
		return fmt.Errorf("connect whatsapp: %w", err)
	}

	go func() {
		<-ctx.Done()
		w.client.Disconnect()
	}()

	log.Printf("[whatsapp] connected")
	return nil
}

func (w *WhatsAppChannel) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}

	if w.client != nil {
		if w.handlerID != 0 {
			w.client.RemoveEventHandler(w.handlerID)
			w.handlerID = 0
		}
		w.client.Disconnect()
	}

	if w.storeContainer != nil {
		if err := w.storeContainer.Close(); err != nil {
			return fmt.Errorf("close whatsapp store: %w", err)
		}
		w.storeContainer = nil
	}

	log.Printf("[whatsapp] stopped")
	return nil
}

func (w *WhatsAppChannel) Send(msg bus.OutboundMessage) error {
	if w.client == nil {
		return fmt.Errorf("whatsapp client not initialized")
	}

	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		chatID = strings.TrimSpace(w.cfg.JID)
	}
	if chatID == "" {
		return fmt.Errorf("whatsapp chat id is required")
	}

	chatJID, err := parseWhatsAppJID(chatID)
	if err != nil {
		return fmt.Errorf("parse whatsapp chat id %q: %w", chatID, err)
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), whatsappSendTimeout)
	defer cancel()

	_, err = w.client.SendMessage(ctx, chatJID, &waE2E.Message{
		Conversation: proto.String(content),
	})
	if err != nil {
		return fmt.Errorf("send whatsapp message: %w", err)
	}

	return nil
}

func (w *WhatsAppChannel) consumeQR(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-qrChan:
			if !ok {
				return
			}

			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				log.Printf("[whatsapp] scan the QR code below to login")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			default:
				if evt.Error != nil {
					log.Printf("[whatsapp] login event=%s error=%v", evt.Event, evt.Error)
				} else {
					log.Printf("[whatsapp] login event=%s", evt.Event)
				}
			}
		}
	}
}

func (w *WhatsAppChannel) handleEvent(evt interface{}) {
	switch e := evt.(type) {
	case *events.Message:
		w.handleMessage(e)
	}
}

func (w *WhatsAppChannel) handleMessage(evt *events.Message) {
	if evt == nil || evt.Message == nil || evt.Info.IsFromMe {
		return
	}

	rawSender := evt.Info.Sender.String()
	sender := evt.Info.Sender.ToNonAD().String()
	if !w.IsAllowed(sender) && !w.IsAllowed(rawSender) {
		log.Printf("[whatsapp] rejected message from %s", sender)
		return
	}

	content, blocks := w.extractContent(evt)
	if content == "" && len(blocks) == 0 {
		return
	}

	w.bus.Inbound <- bus.InboundMessage{
		Channel:       whatsappChannelName,
		SenderID:      sender,
		ChatID:        evt.Info.Chat.String(),
		Content:       content,
		Timestamp:     evt.Info.Timestamp,
		ContentBlocks: blocks,
		Metadata: map[string]any{
			"message_id": evt.Info.ID,
			"chat_jid":   evt.Info.Chat.String(),
			"sender_jid": evt.Info.Sender.String(),
			"push_name":  evt.Info.PushName,
		},
	}
}

func (w *WhatsAppChannel) extractContent(evt *events.Message) (string, []model.ContentBlock) {
	msg := evt.Message
	content := strings.TrimSpace(msg.GetConversation())
	if content == "" && msg.GetExtendedTextMessage() != nil {
		content = strings.TrimSpace(msg.GetExtendedTextMessage().GetText())
	}

	contentBlocks := make([]model.ContentBlock, 0, 1)
	if image := msg.GetImageMessage(); image != nil {
		if content == "" {
			content = strings.TrimSpace(image.GetCaption())
		}

		ctx, cancel := context.WithTimeout(context.Background(), whatsappInboundImageTimeout)
		data, err := w.client.Download(ctx, image)
		cancel()
		if err != nil {
			log.Printf("[whatsapp] download image failed: %v", err)
		} else if len(data) > 0 {
			mediaType := strings.TrimSpace(image.GetMimetype())
			if mediaType == "" {
				mediaType = http.DetectContentType(data)
			}
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

	return content, contentBlocks
}

func parseWhatsAppJID(raw string) (types.JID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EmptyJID, fmt.Errorf("empty jid")
	}

	if strings.Contains(raw, "@") {
		return types.ParseJID(raw)
	}

	user := strings.TrimPrefix(raw, "+")
	if isDigitsOnly(user) {
		return types.NewJID(user, types.DefaultUserServer), nil
	}

	return types.ParseJID(raw)
}

func isDigitsOnly(val string) bool {
	if val == "" {
		return false
	}
	for _, r := range val {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
