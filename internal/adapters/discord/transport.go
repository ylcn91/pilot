package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/logging"
)

// GatewayClient connects to Discord Gateway and handles event streaming.
type GatewayClient struct {
	botToken      string
	intents       int
	conn          *websocket.Conn
	sessionID     string
	botUserID     string
	seq           *int
	heartbeatTick *time.Ticker
	stopCh        chan struct{}
	mu            sync.Mutex
	hbWG          sync.WaitGroup
	closeOnce     sync.Once
	log           *slog.Logger
}

// NewGatewayClient creates a new Discord Gateway client.
func NewGatewayClient(botToken string, intents int) *GatewayClient {
	return &GatewayClient{
		botToken: botToken,
		intents:  intents,
		stopCh:   make(chan struct{}),
		log:      logging.WithComponent("discord.gateway"),
	}
}

// Connect establishes a WebSocket connection to Discord Gateway.
func (g *GatewayClient) Connect(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get gateway URL
	client := NewClient(g.botToken)
	gatewayURL, err := client.GetGatewayURL(ctx)
	if err != nil {
		return fmt.Errorf("get gateway url: %w", err)
	}

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, gatewayURL+"?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}

	g.conn = conn
	g.log.Info("Connected to Discord Gateway")

	// Wait for HELLO and send IDENTIFY
	if err := g.handleHello(ctx); err != nil {
		_ = g.conn.Close()
		g.conn = nil
		return fmt.Errorf("handle hello: %w", err)
	}

	return nil
}

// handleHello receives HELLO opcode and starts heartbeat loop.
func (g *GatewayClient) handleHello(ctx context.Context) error {
	// Set read deadline for HELLO
	deadline := time.Now().Add(10 * time.Second)
	_ = g.conn.SetReadDeadline(deadline)
	defer func() { _ = g.conn.SetReadDeadline(time.Time{}) }()

	var event GatewayEvent
	if err := g.conn.ReadJSON(&event); err != nil {
		return fmt.Errorf("read hello: %w", err)
	}

	if event.Op != OpcodeHello {
		return fmt.Errorf("expected hello opcode %d, got %d", OpcodeHello, event.Op)
	}

	var hello Hello
	data, _ := json.Marshal(event.D)
	if err := json.Unmarshal(data, &hello); err != nil {
		return fmt.Errorf("parse hello: %w", err)
	}

	// Send IDENTIFY
	identifyData := IdentifyData{
		Token:   g.botToken,
		Intents: g.intents,
		Properties: map[string]string{
			"os":      "linux",
			"browser": "pilot",
			"device":  "pilot",
		},
	}

	identify := Identify{
		Op: OpcodeIdentify,
		D:  identifyData,
	}

	if err := g.conn.WriteJSON(identify); err != nil {
		return fmt.Errorf("send identify: %w", err)
	}

	g.log.Info("Sent IDENTIFY", slog.Int("heartbeat_interval", hello.HeartbeatInterval))

	// Start heartbeat loop
	g.heartbeatTick = time.NewTicker(time.Duration(hello.HeartbeatInterval) * time.Millisecond)
	g.hbWG.Add(1)
	go g.heartbeatLoop()

	return nil
}

// heartbeatLoop sends periodic heartbeat messages.
func (g *GatewayClient) heartbeatLoop() {
	defer g.hbWG.Done()
	defer g.heartbeatTick.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-g.heartbeatTick.C:
			g.mu.Lock()
			if g.conn == nil {
				g.mu.Unlock()
				return
			}

			hb := Heartbeat{
				Op: OpcodeHeartbeat,
				D:  g.seq,
			}

			_ = g.conn.WriteJSON(hb)
			g.mu.Unlock()
		}
	}
}

// BotUserID returns the bot's user ID extracted from the READY event.
func (g *GatewayClient) BotUserID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.botUserID
}

// Close closes the WebSocket connection. Safe to call multiple times.
func (g *GatewayClient) Close() error {
	var closeErr error
	g.closeOnce.Do(func() {
		// Signal the heartbeat goroutine to stop and wait for it to fully
		// exit before touching the connection. The heartbeat loop acquires
		// g.mu on each tick, so the wait must happen outside the lock to
		// avoid deadlock, and it guarantees no WriteJSON races a conn.Close.
		close(g.stopCh)
		g.hbWG.Wait()

		g.mu.Lock()
		defer g.mu.Unlock()

		if g.heartbeatTick != nil {
			g.heartbeatTick.Stop()
		}

		if g.conn != nil {
			closeErr = g.conn.Close()
		}
	})
	return closeErr
}
