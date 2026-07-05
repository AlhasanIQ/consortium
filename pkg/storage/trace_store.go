package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/alhasaniq/consortium/pkg/trace"
)

// WriteSpan persists a trace span. Implements trace.Writer.
func (s *Storage) WriteSpan(ctx context.Context, span *trace.Span) error {
	if span == nil {
		return fmt.Errorf("span is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	attrsJSON, err := marshalSpanAttributes(span.Attributes)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO job_node_traces (job_id, node_id, span_id, parent_span_id, kind, status, started_at, duration_ms, attributes, tool_call_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var parentSpanID, toolCallID *string
	if span.ParentSpanID != "" {
		parentSpanID = &span.ParentSpanID
	}
	if span.ToolCallID != "" {
		toolCallID = &span.ToolCallID
	}

	const maxWriteAttempts = 3
	backoff := 10 * time.Millisecond
	for attempt := 1; attempt <= maxWriteAttempts; attempt++ {
		_, err = s.db.ExecContext(ctx, query,
			span.JobID,
			span.NodeID,
			span.SpanID,
			parentSpanID,
			span.Kind,
			span.Status,
			span.StartedAt.UTC().Format(time.RFC3339Nano),
			span.DurationMs,
			attrsJSON,
			toolCallID,
		)
		if err == nil {
			return nil
		}

		if attempt == maxWriteAttempts || !isRetryableTraceWriteError(err) || ctx.Err() != nil {
			return fmt.Errorf("insert trace span: %w", err)
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("insert trace span: %w", ctx.Err())
		case <-timer.C:
		}
		backoff *= 2
	}

	return nil
}

// GetJobSpans returns all trace spans for a job, ordered by started_at.
func (s *Storage) GetJobSpans(jobID string) ([]trace.Span, error) {
	query := `
		SELECT id, job_id, node_id, span_id, COALESCE(parent_span_id, ''), kind, status,
		       started_at, COALESCE(duration_ms, 0), attributes, COALESCE(tool_call_id, '')
		FROM job_node_traces
		WHERE job_id = ?
		ORDER BY started_at ASC, id ASC`

	rows, err := s.db.Query(query, jobID)
	if err != nil {
		return nil, fmt.Errorf("query trace spans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanSpans(rows)
}

// GetJobSpansByNode returns trace spans grouped by node_id.
func (s *Storage) GetJobSpansByNode(jobID string) ([]trace.NodeGroup, error) {
	spans, err := s.GetJobSpans(jobID)
	if err != nil {
		return nil, err
	}

	// Group by node_id preserving order of first appearance
	groupMap := make(map[string]int) // node_id -> index in result
	var groups []trace.NodeGroup

	for _, span := range spans {
		idx, exists := groupMap[span.NodeID]
		if !exists {
			idx = len(groups)
			groupMap[span.NodeID] = idx
			groups = append(groups, trace.NodeGroup{NodeID: span.NodeID})
		}
		groups[idx].Spans = append(groups[idx].Spans, span)
	}

	return groups, nil
}

func scanSpans(rows interface {
	Next() bool
	Scan(dest ...any) error
}) ([]trace.Span, error) {
	var spans []trace.Span
	for rows.Next() {
		var span trace.Span
		var startedAtStr, attrsJSON string

		err := rows.Scan(
			&span.ID,
			&span.JobID,
			&span.NodeID,
			&span.SpanID,
			&span.ParentSpanID,
			&span.Kind,
			&span.Status,
			&startedAtStr,
			&span.DurationMs,
			&attrsJSON,
			&span.ToolCallID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trace span: %w", err)
		}

		// Parse timestamp
		t, err := time.Parse(time.RFC3339Nano, startedAtStr)
		if err != nil {
			// Try other common formats from SQLite
			t, err = time.Parse("2006-01-02 15:04:05", startedAtStr)
			if err != nil {
				t, _ = time.Parse("2006-01-02T15:04:05Z", startedAtStr)
			}
		}
		span.StartedAt = t

		// Parse attributes JSON
		if attrsJSON != "" && attrsJSON != "{}" {
			if err := json.Unmarshal([]byte(attrsJSON), &span.Attributes); err != nil {
				span.Attributes = map[string]any{"_raw": attrsJSON}
			}
		} else {
			span.Attributes = map[string]any{}
		}

		spans = append(spans, span)
	}

	if rowErrProvider, ok := rows.(interface{ Err() error }); ok {
		if err := rowErrProvider.Err(); err != nil {
			return nil, fmt.Errorf("iterate trace spans: %w", err)
		}
	}

	return spans, nil
}

func marshalSpanAttributes(attrs map[string]any) (string, error) {
	if attrs == nil {
		return "{}", nil
	}
	data, err := json.Marshal(attrs)
	if err == nil {
		return string(data), nil
	}

	sanitized, ok := sanitizeForJSON(attrs).(map[string]any)
	if !ok {
		sanitized = map[string]any{
			"_attrs_error": "could not sanitize attributes map",
		}
	}
	data, retryErr := json.Marshal(sanitized)
	if retryErr != nil {
		return "", fmt.Errorf("marshal span attributes: %w (sanitize failed: %v)", err, retryErr)
	}
	return string(data), nil
}

func sanitizeForJSON(v any) any {
	switch t := v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return t
	case float32:
		f := float64(t)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Sprintf("%v", f)
		}
		return t
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fmt.Sprintf("%v", t)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitizeForJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = sanitizeForJSON(t[i])
		}
		return out
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		return sanitizeForJSON(rv.Elem().Interface())
	case reflect.Map:
		out := map[string]any{}
		iter := rv.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			out[key] = sanitizeForJSON(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = sanitizeForJSON(rv.Index(i).Interface())
		}
		return out
	default:
		// Last resort for unsupported JSON types (channels/functions/complex structs).
		return fmt.Sprint(v)
	}
}

func isRetryableTraceWriteError(err error) bool {
	return IsRetryableSQLiteError(err)
}
