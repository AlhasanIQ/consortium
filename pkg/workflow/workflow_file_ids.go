package workflow

import "github.com/google/uuid"

// NormalizeWorkflowFileIDs ensures IDs are present on workflow file payloads
// (workflow ID, node IDs, and edge IDs). Existing IDs are preserved.
func NormalizeWorkflowFileIDs(workflowFile map[string]interface{}) error {
	// Only generate workflow ID if missing or empty.
	if id, ok := workflowFile["id"].(string); !ok || id == "" {
		workflowFile["id"] = uuid.New().String()
	}

	// Normalize node IDs.
	if nodes, ok := workflowFile["nodes"].([]interface{}); ok {
		for _, nodeInterface := range nodes {
			node, ok := nodeInterface.(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := node["id"].(string); !ok || id == "" {
				node["id"] = uuid.New().String()
			}
		}
	}

	// Normalize edge IDs.
	if edges, ok := workflowFile["edges"].([]interface{}); ok {
		for _, edgeInterface := range edges {
			edge, ok := edgeInterface.(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := edge["id"].(string); !ok || id == "" {
				edge["id"] = uuid.New().String()
			}
		}
	}

	return nil
}
