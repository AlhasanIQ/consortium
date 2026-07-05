package api

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients generally omit Origin.
			return true
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}

		if strings.EqualFold(originURL.Host, r.Host) {
			return true
		}

		return appenv.OriginAllowed(origin)
	},
}

// ExecutionMessage represents a WebSocket message during workflow execution.
// Type should be one of the canonical event constants: EventStatus, EventJobCreated,
// EventNodeStart, EventNodeComplete, EventNodeFailed, EventComplete, EventError, EventCancelled.
type ExecutionMessage struct {
	Type      string                 `json:"type"` // One of Event* constants
	JobID     string                 `json:"job_id,omitempty"`
	Message   string                 `json:"message"` // Human-readable message
	NodeID    string                 `json:"node_id,omitempty"`
	Output    string                 `json:"output,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Code      string                 `json:"code,omitempty"` // Machine-readable error code
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"` // Additional data
}

func (a *WorkflowAPI) sendMessage(conn *websocket.Conn, msg ExecutionMessage) error {
	// Set write deadline to prevent hanging
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		log.Printf("Error setting write deadline: %v", err)
		return err
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Error sending WebSocket message [%s]: %v", msg.Type, err)
		return err
	}
	return nil
}
