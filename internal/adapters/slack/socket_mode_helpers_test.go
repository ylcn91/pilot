package slack

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// newTestWSPair creates a connected client/server websocket pair for testing.
func newTestWSPair(t *testing.T) (client *websocket.Conn, server *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	var serverConn *websocket.Conn
	var wg sync.WaitGroup
	wg.Add(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		serverConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		wg.Done()
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	wg.Wait()
	t.Cleanup(func() { _ = serverConn.Close() })

	return clientConn, serverConn
}
