package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSessionUpdatePing(t *testing.T) {
	sm := NewSessionManager()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	session := sm.Create(conn)
	originalPing := session.LastPing

	// Wait a bit and update ping
	time.Sleep(10 * time.Millisecond)
	session.UpdatePing()

	if !session.LastPing.After(originalPing) {
		t.Error("LastPing should be updated to a later time")
	}
}

func TestSessionSend(t *testing.T) {
	receivedMessage := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read message from client
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		receivedMessage <- msg
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	sm := NewSessionManager()
	session := sm.Create(conn)

	testMessage := []byte(`{"type":"test","data":"hello"}`)
	err = session.Send(testMessage)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case received := <-receivedMessage:
		if string(received) != string(testMessage) {
			t.Errorf("Expected message %s, got %s", testMessage, received)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestSessionManagerBroadcast(t *testing.T) {
	numClients := 3
	receivedMessages := make(chan []byte, numClients)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read message from server
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		receivedMessages <- msg
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	sm := NewSessionManager()

	// Create multiple client connections
	var conns []*websocket.Conn
	for i := 0; i < numClients; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		conns = append(conns, conn)
		sm.Create(conn)
	}

	// Clean up connections
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	// Broadcast message
	testMessage := []byte(`{"type":"broadcast","data":"hello all"}`)
	sm.Broadcast(testMessage)

	// Wait for all clients to receive the message
	receivedCount := 0
	timeout := time.After(2 * time.Second)

	for receivedCount < numClients {
		select {
		case msg := <-receivedMessages:
			if string(msg) != string(testMessage) {
				t.Errorf("Expected message %s, got %s", testMessage, msg)
			}
			receivedCount++
		case <-timeout:
			t.Errorf("Timeout: only received %d/%d messages", receivedCount, numClients)
			return
		}
	}
}

func TestSessionIDUniqueness(t *testing.T) {
	sm := NewSessionManager()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	numSessions := 100
	ids := make(map[string]bool)

	for i := 0; i < numSessions; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		session := sm.Create(conn)
		if ids[session.ID] {
			t.Errorf("Duplicate session ID generated: %s", session.ID)
		}
		ids[session.ID] = true
	}

	if len(ids) != numSessions {
		t.Errorf("Expected %d unique IDs, got %d", numSessions, len(ids))
	}
}
