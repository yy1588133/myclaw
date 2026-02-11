package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/config"
)

func TestNewWebUIChannel(t *testing.T) {
	b := bus.NewMessageBus(10)
	cfg := config.WebUIConfig{Enabled: true}
	gwCfg := config.GatewayConfig{Port: 0}

	ch, err := NewWebUIChannel(cfg, gwCfg, b)
	if err != nil {
		t.Fatalf("NewWebUIChannel: %v", err)
	}
	if ch.Name() != "webui" {
		t.Errorf("Name() = %q, want %q", ch.Name(), "webui")
	}
}

func TestWebUIChannel_StartStop(t *testing.T) {
	b := bus.NewMessageBus(10)
	cfg := config.WebUIConfig{Enabled: true}
	gwCfg := config.GatewayConfig{Port: 19876}

	ch, err := NewWebUIChannel(cfg, gwCfg, b)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:19876/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}

	if err := ch.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestWebUIChannel_WebSocket(t *testing.T) {
	b := bus.NewMessageBus(10)
	cfg := config.WebUIConfig{Enabled: true}
	gwCfg := config.GatewayConfig{Port: 19877}

	ch, err := NewWebUIChannel(cfg, gwCfg, b)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ch.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, _, err := websocket.Dial(ctx, "ws://localhost:19877/ws", nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	msg := wsMessage{Type: "message", Content: "hello from test"}
	data, _ := json.Marshal(msg)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("ws write: %v", err)
	}

	select {
	case inbound := <-b.Inbound:
		if inbound.Channel != "webui" {
			t.Errorf("channel = %q, want %q", inbound.Channel, "webui")
		}
		if inbound.Content != "hello from test" {
			t.Errorf("content = %q, want %q", inbound.Content, "hello from test")
		}
		if !strings.HasPrefix(inbound.ChatID, "webui-") {
			t.Errorf("chatID = %q, want prefix %q", inbound.ChatID, "webui-")
		}

		if err := ch.Send(bus.OutboundMessage{
			Channel: "webui",
			ChatID:  inbound.ChatID,
			Content: "reply from bot",
		}); err != nil {
			t.Fatalf("Send: %v", err)
		}

		_, respData, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var resp wsMessage
		if err := json.Unmarshal(respData, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Type != "message" {
			t.Errorf("resp type = %q, want %q", resp.Type, "message")
		}
		if resp.Content != "reply from bot" {
			t.Errorf("resp content = %q, want %q", resp.Content, "reply from bot")
		}

	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestWebUIChannel_SendBroadcast(t *testing.T) {
	b := bus.NewMessageBus(10)
	cfg := config.WebUIConfig{Enabled: true}
	gwCfg := config.GatewayConfig{Port: 19878}

	ch, err := NewWebUIChannel(cfg, gwCfg, b)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ch.Stop()

	time.Sleep(100 * time.Millisecond)

	conn1, _, err := websocket.Dial(ctx, "ws://localhost:19878/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.CloseNow()

	conn2, _, err := websocket.Dial(ctx, "ws://localhost:19878/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.CloseNow()

	time.Sleep(100 * time.Millisecond)

	if err := ch.Send(bus.OutboundMessage{
		Channel: "webui",
		ChatID:  "unknown-id",
		Content: "broadcast msg",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			t.Fatalf("client %d read: %v", i+1, err)
		}
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("client %d unmarshal: %v", i+1, err)
		}
		if msg.Content != "broadcast msg" {
			t.Errorf("client %d content = %q, want %q", i+1, msg.Content, "broadcast msg")
		}
	}
}
