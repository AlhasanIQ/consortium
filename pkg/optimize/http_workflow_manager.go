package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

type HTTPWorkflowManager struct {
	Client *HTTPClient
}

func NewHTTPWorkflowManager(baseURL string, timeout time.Duration) *HTTPWorkflowManager {
	return &HTTPWorkflowManager{Client: NewHTTPClient(baseURL, timeout)}
}

func (m *HTTPWorkflowManager) CreateTemporaryWorkflow(ctx context.Context, id string, workflowJSON json.RawMessage, name string, description string) error {
	if m.Client == nil {
		return fmt.Errorf("http client is nil")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &payload); err != nil {
		return fmt.Errorf("parse workflow JSON: %w", err)
	}
	payload["id"] = id
	if name != "" {
		payload["name"] = name
	}
	if description != "" {
		payload["description"] = description
	}
	var response map[string]interface{}
	if err := m.Client.PostJSON(ctx, "/api/workflows", payload, &response); err != nil {
		return err
	}
	return nil
}

func (m *HTTPWorkflowManager) DeleteTemporaryWorkflow(ctx context.Context, id string) error {
	if m.Client == nil {
		return fmt.Errorf("http client is nil")
	}
	var response map[string]interface{}
	if err := m.Client.DeleteJSON(ctx, "/api/workflows/"+url.PathEscape(id), &response); err != nil {
		return err
	}
	return nil
}

var _ WorkflowManager = (*HTTPWorkflowManager)(nil)
