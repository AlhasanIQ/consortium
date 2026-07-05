package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketMessageSending(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		api := &WorkflowAPI{}

		// Send test message
		msg := ExecutionMessage{
			Type:      "test",
			Message:   "Test message",
			Timestamp: time.Now(),
		}
		api.sendMessage(conn, msg)

		// Keep connection alive briefly
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Read message
	var msg ExecutionMessage
	err = conn.ReadJSON(&msg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify
	if msg.Type != "test" {
		t.Errorf("Expected type 'test', got '%s'", msg.Type)
	}
	if msg.Message != "Test message" {
		t.Errorf("Expected message 'Test message', got '%s'", msg.Message)
	}
}

func TestWebSocketMessageSending_ClosedConnectionReturnsError(t *testing.T) {
	// Create a server that upgrades and keeps the socket briefly alive.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		time.Sleep(300 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Close client-side conn, then sending should fail.
	if err := conn.Close(); err != nil {
		t.Fatalf("failed to close client connection: %v", err)
	}

	api := &WorkflowAPI{}
	sendErr := api.sendMessage(conn, ExecutionMessage{
		Type:      "test",
		Message:   "should fail",
		Timestamp: time.Now(),
	})
	if sendErr == nil {
		t.Fatal("expected sendMessage to return error on closed connection")
	}
}
