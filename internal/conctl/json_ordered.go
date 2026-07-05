package conctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Seed-canonical key orderings for workflow JSON objects.
// Keys not in the priority list are appended alphabetically.
var (
	workflowKeyOrder = []string{
		"version", "id", "name", "description",
		"created_at", "updated_at",
		"nodes", "edges", "metadata",
	}
	nodeKeyOrder     = []string{"id", "type", "position", "data"}
	edgeKeyOrder     = []string{"id", "source", "target"}
	positionKeyOrder = []string{"x", "y"}
	dataKeyOrder     = []string{"type", "label", "config"}
	configKeyOrder   = []string{
		"name", "description",
		"childWorkflowId", "inputTemplate", "outputKey", "await",
		"provider", "model",
		"openRouterProvider", "openRouterReasoning",
		"schema", "systemPrompt", "userPrompt",
		"temperature",
		"outputFormat", "aggregationMethod", "aggregationConfig",
		"timeoutSeconds", "maxTokens",
		"childWorkflowVar",
	}
	metadataKeyOrder          = []string{"tags"}
	routerProviderKeyOrder    = []string{"order", "allow_fallbacks"}
	reasoningKeyOrder         = []string{"effort"}
	aggregationConfigKeyOrder = []string{
		"model", "system_prompt", "temperature",
		"openRouterReasoning",
	}
	schemaEntryKeyOrder = []string{"type", "required"}
)

// marshalWorkflowOrdered produces JSON with seed-canonical key ordering.
// The output matches the key ordering convention used in pkg/seeds/data/.
func marshalWorkflowOrdered(raw []byte) ([]byte, error) {
	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	var buf bytes.Buffer
	writeOrdered(&buf, data, "workflow", 0)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// writeOrdered recursively writes a JSON value with canonical key ordering.
// context selects the appropriate key priority list for map objects.
func writeOrdered(buf *bytes.Buffer, v interface{}, context string, indent int) {
	switch val := v.(type) {
	case map[string]interface{}:
		order := keyOrderForContext(context)
		writeObjectOrdered(buf, val, order, context, indent)
	case []interface{}:
		writeArrayOrdered(buf, val, context, indent)
	case string:
		marshalString(buf, val)
	case float64:
		writeFloat(buf, val)
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		data, _ := json.Marshal(val)
		buf.Write(data)
	}
}

// marshalString writes a JSON string without HTML-escaping <, >, &.
func marshalString(buf *bytes.Buffer, s string) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	// Encoder appends a newline; trim it.
	out := b.Bytes()
	buf.Write(bytes.TrimRight(out, "\n"))
}

func writeFloat(buf *bytes.Buffer, val float64) {
	if val == float64(int64(val)) && val >= -1e15 && val <= 1e15 {
		buf.WriteString(strconv.FormatInt(int64(val), 10))
	} else {
		buf.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
	}
}

func writeObjectOrdered(buf *bytes.Buffer, m map[string]interface{}, keyOrder []string, context string, indent int) {
	if len(m) == 0 {
		buf.WriteString("{}")
		return
	}

	keys := orderedKeys(m, keyOrder)

	buf.WriteString("{\n")
	prefix := strings.Repeat("  ", indent+1)
	for i, k := range keys {
		buf.WriteString(prefix)
		kJSON, _ := json.Marshal(k)
		buf.Write(kJSON)
		buf.WriteString(": ")

		childCtx := childContext(context, k)
		writeOrdered(buf, m[k], childCtx, indent+1)
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(strings.Repeat("  ", indent))
	buf.WriteByte('}')
}

func writeArrayOrdered(buf *bytes.Buffer, arr []interface{}, context string, indent int) {
	if len(arr) == 0 {
		buf.WriteString("[]")
		return
	}

	buf.WriteString("[\n")
	prefix := strings.Repeat("  ", indent+1)
	for i, v := range arr {
		buf.WriteString(prefix)
		writeOrdered(buf, v, context, indent+1)
		if i < len(arr)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(strings.Repeat("  ", indent))
	buf.WriteByte(']')
}

// orderedKeys returns map keys in priority order, then remaining keys alphabetically.
func orderedKeys(m map[string]interface{}, priority []string) []string {
	var ordered []string
	seen := make(map[string]bool, len(priority))
	for _, k := range priority {
		if _, ok := m[k]; ok {
			ordered = append(ordered, k)
			seen[k] = true
		}
	}
	var extra []string
	for k := range m {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(ordered, extra...)
}

// keyOrderForContext returns the key priority list for a given context.
func keyOrderForContext(context string) []string {
	switch context {
	case "workflow":
		return workflowKeyOrder
	case "nodes":
		return nodeKeyOrder
	case "edges":
		return edgeKeyOrder
	case "position":
		return positionKeyOrder
	case "data":
		return dataKeyOrder
	case "config":
		return configKeyOrder
	case "metadata":
		return metadataKeyOrder
	case "openRouterProvider":
		return routerProviderKeyOrder
	case "openRouterReasoning":
		return reasoningKeyOrder
	case "aggregationConfig":
		return aggregationConfigKeyOrder
	case "schema_entry":
		return schemaEntryKeyOrder
	default:
		return nil
	}
}

// childContext determines the context name for a child key's value.
func childContext(parentContext, key string) string {
	switch key {
	case "nodes":
		return "nodes"
	case "edges":
		return "edges"
	case "position":
		return "position"
	case "data":
		return "data"
	case "config":
		return "config"
	case "metadata":
		return "metadata"
	case "openRouterProvider":
		return "openRouterProvider"
	case "openRouterReasoning":
		return "openRouterReasoning"
	case "aggregationConfig":
		return "aggregationConfig"
	case "schema":
		return "schema"
	}
	if parentContext == "schema" {
		return "schema_entry"
	}
	return key
}
