package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Writer returns an io.Writer for the given output target.
// "-" means stdout; any other string is treated as a file path.
func Writer(target string) (io.Writer, func(), error) {
	if target == "-" || target == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(target)
	if err != nil {
		return nil, nil, fmt.Errorf("open output file %q: %w", target, err)
	}
	return f, func() { _ = f.Close() }, nil
}

// TableFn renders data as a human-readable table. The format parameter
// is "table" for aligned columns or "md" for GitHub-flavored markdown.
type TableFn func(w io.Writer, data interface{}, format string)

// Render outputs data in the requested format.
func Render(w io.Writer, format string, data interface{}, tableFn TableFn) error {
	switch format {
	case "json":
		return renderJSON(w, data)
	case "jsonl":
		return renderJSONL(w, data)
	case "table", "md":
		if tableFn != nil {
			tableFn(w, data, format)
			return nil
		}
		return renderJSON(w, data)
	default:
		return renderJSON(w, data)
	}
}

func renderJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func renderJSONL(w io.Writer, data interface{}) error {
	// If data is a slice, output one JSON object per line.
	switch v := data.(type) {
	case []interface{}:
		for _, item := range v {
			if err := json.NewEncoder(w).Encode(item); err != nil {
				return err
			}
		}
		return nil
	default:
		// Single object — just output as one line.
		return json.NewEncoder(w).Encode(data)
	}
}

// Table helpers for simple tabular output.

// PrintTable renders a simple aligned table.
func PrintTable(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 && len(headers) == 0 {
		return
	}

	// Compute column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header.
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprintf(w, "%-*s", widths[i], h)
	}
	fmt.Fprintln(w)

	// Separator.
	for i, width := range widths {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, strings.Repeat("-", width))
	}
	fmt.Fprintln(w)

	// Rows.
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			if i < len(widths) {
				fmt.Fprintf(w, "%-*s", widths[i], cell)
			} else {
				fmt.Fprint(w, cell)
			}
		}
		fmt.Fprintln(w)
	}
}

// PrintMarkdownTable renders a GitHub-flavored markdown table.
func PrintMarkdownTable(w io.Writer, headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}

	// Header.
	fmt.Fprint(w, "|")
	for _, h := range headers {
		fmt.Fprintf(w, " %s |", h)
	}
	fmt.Fprintln(w)

	// Separator.
	fmt.Fprint(w, "|")
	for range headers {
		fmt.Fprint(w, " --- |")
	}
	fmt.Fprintln(w)

	// Rows.
	for _, row := range rows {
		fmt.Fprint(w, "|")
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			fmt.Fprintf(w, " %s |", cell)
		}
		fmt.Fprintln(w)
	}
}
