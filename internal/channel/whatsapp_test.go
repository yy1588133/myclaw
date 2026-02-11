package channel

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestNewWhatsApp_Disabled(t *testing.T) {
	b := bus.NewMessageBus(10)

	m, err := NewChannelManager(config.ChannelsConfig{
		WhatsApp: config.WhatsAppConfig{
			Enabled:   false,
			StorePath: filepath.Join("/dev/null", "whatsapp-store.db"),
		},
	}, b)
	if err != nil {
		t.Fatalf("NewChannelManager error: %v", err)
	}

	for _, name := range m.EnabledChannels() {
		if name == whatsappChannelName {
			t.Fatalf("%s channel should not be created when disabled", whatsappChannelName)
		}
	}
}

func TestNewWhatsApp_Valid(t *testing.T) {
	b := bus.NewMessageBus(10)
	storePath := filepath.Join(t.TempDir(), "whatsapp-store.db")

	ch, err := NewWhatsApp(config.WhatsAppConfig{
		Enabled:   true,
		StorePath: storePath,
	}, b)
	if err != nil {
		t.Fatalf("NewWhatsApp error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	if ch.Name() != whatsappChannelName {
		t.Errorf("Name = %q, want %s", ch.Name(), whatsappChannelName)
	}
	if ch.client == nil {
		t.Fatal("expected non-nil whatsapp client")
	}

	if err := ch.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}

func TestWhatsAppChannel_Name(t *testing.T) {
	ch := &WhatsAppChannel{}
	if ch.Name() != whatsappChannelName {
		t.Errorf("Name = %q, want %s", ch.Name(), whatsappChannelName)
	}
}

func TestWhatsAppChannel_Send_NilClient(t *testing.T) {
	ch := &WhatsAppChannel{}
	err := ch.Send(bus.OutboundMessage{ChatID: "8613800138000", Content: "hello"})
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("error = %v, want contains %q", err, "not initialized")
	}
}

func TestWhatsAppChannel_AllowFrom(t *testing.T) {
	deviceJID, err := types.ParseJID("8613800138000:2@s.whatsapp.net")
	if err != nil {
		t.Fatalf("parse device jid: %v", err)
	}

	makeEvent := func(sender types.JID) *events.Message {
		return &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{
					Sender: sender,
					Chat:   types.NewJID("8613800138000", types.DefaultUserServer),
				},
				ID:        types.MessageID("msg-1"),
				Timestamp: time.Now(),
			},
			Message: &waE2E.Message{
				Conversation: proto.String("hello"),
			},
		}
	}

	dispatched := func(allowFrom []string, sender types.JID) bool {
		b := bus.NewMessageBus(1)
		ch := &WhatsAppChannel{BaseChannel: NewBaseChannel(whatsappChannelName, b, allowFrom)}
		ch.handleMessage(makeEvent(sender))

		select {
		case <-b.Inbound:
			return true
		default:
			return false
		}
	}

	tests := []struct {
		name      string
		allowFrom []string
		sender    types.JID
		want      bool
	}{
		{
			name:      "empty whitelist allows all",
			allowFrom: nil,
			sender:    types.NewJID("8613800138000", types.DefaultUserServer),
			want:      true,
		},
		{
			name:      "allow non-ad jid matches sender to non-ad",
			allowFrom: []string{"8613800138000@s.whatsapp.net"},
			sender:    deviceJID,
			want:      true,
		},
		{
			name:      "allow ad jid matches raw sender jid",
			allowFrom: []string{"8613800138000:2@s.whatsapp.net"},
			sender:    deviceJID,
			want:      true,
		},
		{
			name:      "plus-prefixed whitelist is not normalized",
			allowFrom: []string{"+8613800138000"},
			sender:    types.NewJID("8613800138000", types.DefaultUserServer),
			want:      false,
		},
		{
			name:      "unknown sender rejected",
			allowFrom: []string{"8613900000000@s.whatsapp.net"},
			sender:    types.NewJID("8613800138000", types.DefaultUserServer),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dispatched(tt.allowFrom, tt.sender)
			if got != tt.want {
				t.Fatalf("allowFrom=%v sender=%q => dispatched=%v, want %v", tt.allowFrom, tt.sender.String(), got, tt.want)
			}
		})
	}
}

func TestWhatsAppChannel_ParseJID(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "plus prefixed phone number",
			raw:  "+8613800138000",
			want: "8613800138000@s.whatsapp.net",
		},
		{
			name: "plain phone number",
			raw:  "8613800138000",
			want: "8613800138000@s.whatsapp.net",
		},
		{
			name: "full user jid",
			raw:  "8613800138000@s.whatsapp.net",
			want: "8613800138000@s.whatsapp.net",
		},
		{
			name: "device jid",
			raw:  "8613800138000:2@s.whatsapp.net",
			want: "8613800138000:2@s.whatsapp.net",
		},
		{
			name:    "empty input",
			raw:     " ",
			wantErr: true,
		},
		{
			name:    "invalid jid",
			raw:     "a:b:c@s.whatsapp.net",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jid, err := parseWhatsAppJID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseWhatsAppJID(%q) expected error", tt.raw)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseWhatsAppJID(%q) error: %v", tt.raw, err)
			}
			if jid.String() != tt.want {
				t.Fatalf("parseWhatsAppJID(%q) = %q, want %q", tt.raw, jid.String(), tt.want)
			}
		})
	}
}

func TestWhatsAppChannel_Stop_NotStarted(t *testing.T) {
	ch := &WhatsAppChannel{}
	if err := ch.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
}
