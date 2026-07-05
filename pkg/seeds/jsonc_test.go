package seeds

import (
	"encoding/json"
	"testing"
)

func TestStripJSONComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result string)
	}{
		{
			name:  "no comments",
			input: `{"key": "value"}`,
			check: func(t *testing.T, result string) {
				if result != `{"key": "value"}` {
					t.Errorf("got %q", result)
				}
			},
		},
		{
			name: "line comment",
			input: `{
				// this is a comment
				"key": "value"
			}`,
			check: validJSON,
		},
		{
			name: "block comment",
			input: `{
				/* block comment */
				"key": "value"
			}`,
			check: validJSON,
		},
		{
			name: "inline line comment",
			input: `{
				"key": "value" // inline comment
			}`,
			check: validJSON,
		},
		{
			name:  "comment-like content inside string preserved",
			input: `{"url": "https://example.com", "note": "use // for comments"}`,
			check: func(t *testing.T, result string) {
				var m map[string]string
				if err := json.Unmarshal([]byte(result), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if m["url"] != "https://example.com" {
					t.Errorf("url mangled: %q", m["url"])
				}
				if m["note"] != "use // for comments" {
					t.Errorf("note mangled: %q", m["note"])
				}
			},
		},
		{
			name: "trailing comma in object",
			input: `{
				"a": 1,
				"b": 2,
			}`,
			check: validJSON,
		},
		{
			name: "trailing comma in array",
			input: `{
				"arr": [1, 2, 3,]
			}`,
			check: validJSON,
		},
		{
			name: "trailing comma with comment before closing brace",
			input: `{
				"a": 1,
				// comment here
			}`,
			check: validJSON,
		},
		{
			name:  "escaped quote in string",
			input: `{"msg": "he said \"hello\" // not a comment"}`,
			check: func(t *testing.T, result string) {
				var m map[string]string
				if err := json.Unmarshal([]byte(result), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if m["msg"] != `he said "hello" // not a comment` {
					t.Errorf("msg mangled: %q", m["msg"])
				}
			},
		},
		{
			name: "realistic seed snippet",
			input: `{
				"model": "deepseek/deepseek-v4-flash",
				"openRouterProvider": {
					// Provider preferences are preserved when present.
					"order": ["deepseek", "openrouter"],
					"allow_fallbacks": true,
				},
				// 360s timeout reduces long-tail retries under high concurrency.
				"timeoutSeconds": 360,
				"maxTokens": -1,
			}`,
			check: func(t *testing.T, result string) {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(result), &m); err != nil {
					t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
				}
				if m["model"] != "deepseek/deepseek-v4-flash" {
					t.Errorf("model: %v", m["model"])
				}
				if m["timeoutSeconds"] != float64(360) {
					t.Errorf("timeoutSeconds: %v", m["timeoutSeconds"])
				}
			},
		},
		{
			name: "block comment spanning multiple lines",
			input: `{
				/*
				 * Multi-line
				 * block comment
				 */
				"key": "value"
			}`,
			check: validJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripJSONComments(tt.input)
			tt.check(t, result)
		})
	}
}

func TestAllSeedsParsToValidWorkflows(t *testing.T) {
	allSeeds := GetAllJSON()
	if len(allSeeds) == 0 {
		t.Fatal("Expected at least one seed workflow")
	}

	for i, seedJSON := range allSeeds {
		var wf struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Nodes []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"nodes"`
			Edges []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"edges"`
		}

		if err := json.Unmarshal([]byte(seedJSON), &wf); err != nil {
			t.Errorf("Seed %d failed to parse: %v\nFirst 200 chars: %.200s", i, err, seedJSON)
			continue
		}

		if wf.ID == "" {
			t.Errorf("Seed %d missing 'id' field", i)
		}
		if wf.Name == "" {
			t.Errorf("Seed %d (%s) missing 'name' field", i, wf.ID)
		}
		if len(wf.Nodes) == 0 {
			t.Errorf("Seed %d (%s) has no nodes", i, wf.ID)
		}

		// Verify all edge targets reference existing node IDs
		nodeIDs := make(map[string]bool)
		for _, n := range wf.Nodes {
			if n.ID == "" {
				t.Errorf("Seed %d (%s) has a node with empty ID", i, wf.ID)
			}
			nodeIDs[n.ID] = true
		}
		for _, e := range wf.Edges {
			if !nodeIDs[e.Source] {
				t.Errorf("Seed %s: edge source %q not in nodes", wf.ID, e.Source)
			}
			if !nodeIDs[e.Target] {
				t.Errorf("Seed %s: edge target %q not in nodes", wf.ID, e.Target)
			}
		}
	}
}

func validJSON(t *testing.T, result string) {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		t.Errorf("invalid JSON: %v\nresult: %s", err, result)
	}
}
