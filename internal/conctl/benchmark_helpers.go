package conctl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// fetchBenchmarkItemDetail fetches a benchmark item's full detail.
func fetchBenchmarkItemDetail(rc *RunContext, runID, itemID string) (map[string]interface{}, error) {
	var data map[string]interface{}
	path := fmt.Sprintf("/api/admin/benchmarks/%s/items", runID)
	query := url.Values{}
	query.Set("item_id", itemID)
	if err := rc.Client.GetJSON(rc.Ctx, path, query, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// normalizeBenchmarkItemID normalizes user-provided item identifiers.
// Accepts "row-17", "Row-17", or plain numeric "17" and returns "row-17".
func normalizeBenchmarkItemID(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	lower := strings.ToLower(itemID)
	if strings.HasPrefix(lower, "row-") {
		suffix := strings.TrimSpace(strings.TrimPrefix(lower, "row-"))
		if suffix == "" {
			return "row-"
		}
		return "row-" + suffix
	}
	if isDigits(itemID) {
		return "row-" + itemID
	}
	return itemID
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// fetchBenchmarkAnalysisItem fetches run analysis and returns the matching item entry.
// Returns nil if analysis is unavailable or the item isn't found (non-fatal).
func fetchBenchmarkAnalysisItem(rc *RunContext, runID, itemID string) map[string]interface{} {
	var analysisData map[string]interface{}
	if err := rc.Client.GetJSON(rc.Ctx, "/api/admin/benchmarks/"+runID+"/analysis", nil, &analysisData); err != nil {
		return nil
	}
	return findAnalysisItem(analysisData, itemID)
}

// findAnalysisItem searches analysis data for a specific item_id.
func findAnalysisItem(analysisData map[string]interface{}, itemID string) map[string]interface{} {
	if analysisData == nil {
		return nil
	}
	items, ok := analysisData["items"].([]interface{})
	if !ok {
		return nil
	}
	for _, ai := range items {
		aim, ok := ai.(map[string]interface{})
		if !ok {
			continue
		}
		if stringVal(aim, "item_id") == itemID {
			return aim
		}
	}
	return nil
}

// renderQuestion prints the question section with choices.
func renderQuestion(w io.Writer, dsItem map[string]interface{}, maxQuestionLen int) {
	if dsItem == nil {
		return
	}
	fmt.Fprintf(w, "Question: %s\n", truncate(stringVal(dsItem, "question"), maxQuestionLen))
	if choices, ok := dsItem["choices"].([]interface{}); ok {
		for i, c := range choices {
			label := fmt.Sprintf("%d", i)
			if i < 26 {
				label = string(rune('A' + i))
			}
			fmt.Fprintf(w, "  %s) %s\n", label, truncate(fmt.Sprintf("%v", c), 100))
		}
	}
	fmt.Fprintf(w, "Correct Answer: %s\n", stringVal(dsItem, "answer_label"))
	fmt.Fprintln(w)
}

// renderItemOutcome prints the item verdict/outcome section.
// category is optional (pass "" to omit).
func renderItemOutcome(w io.Writer, detail map[string]interface{}, category string) {
	if detail == nil {
		return
	}
	item, _ := detail["item"].(map[string]interface{})
	if item == nil {
		return
	}
	correct := "INCORRECT"
	if boolVal(item, "correct") {
		correct = "CORRECT"
	}
	if category != "" {
		fmt.Fprintf(w, "Verdict:  %s  Predicted: %s  Expected: %s\n",
			correct, stringVal(item, "predicted"), stringVal(item, "answer_label"))
		fmt.Fprintf(w, "Category: %s\n", category)
	} else {
		fmt.Fprintf(w, "Predicted: %s  Result: %s\n", stringVal(item, "predicted"), correct)
	}
	fmt.Fprintf(w, "Subject: %s  Attempts: %.0f  Cost: %s\n",
		stringVal(item, "subject"),
		floatVal(item, "attempts"),
		formatCostVal(floatVal(item, "cost_usd")))
	// Show contract diagnostic from last attempt if present.
	if attempts, ok := detail["attempts"].([]interface{}); ok && len(attempts) > 0 {
		lastAttempt, _ := attempts[len(attempts)-1].(map[string]interface{})
		if diag := stringVal(lastAttempt, "contract_diagnostic"); diag != "" {
			fmt.Fprintf(w, "Diagnostic: %s\n", truncate(diag, 120))
		}
	}
	fmt.Fprintln(w)
}

// nodeRowOpts controls how renderNodeRows builds table rows.
type nodeRowOpts struct {
	// ExpectedAnswer enables (Y)/(N) correctness markers on answers.
	ExpectedAnswer string
	// IncludeTokens appends a TOKENS column (sum of tokens_input + tokens_output).
	IncludeTokens bool
	// FullAnswer uses extractAnswer (full output) instead of extractAnswerShort (truncated).
	FullAnswer bool
	// AnnotateStatus adds [replay] and [extraction method] markers to the status column.
	// Defaults to true when using renderNodeRows (the variadic shorthand).
	AnnotateStatus bool
}

// renderNodeRowsOpts builds table rows from workflow nodes, skipping aggregation child nodes.
func renderNodeRowsOpts(nodes []interface{}, opts nodeRowOpts) [][]string {
	var rows [][]string
	for _, n := range nodes {
		nm, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		if stringVal(nm, "parent_node_id") != "" {
			continue
		}
		var answer string
		if opts.FullAnswer {
			answer = extractAnswer(stringVal(nm, "output"))
		} else {
			answer = extractAnswerShort(stringVal(nm, "output"))
		}
		if opts.ExpectedAnswer != "" && answer != "-" && answer != "" {
			if strings.EqualFold(answer, opts.ExpectedAnswer) {
				answer += " (Y)"
			} else {
				answer += " (N)"
			}
		}
		status := stringVal(nm, "status")
		if opts.AnnotateStatus {
			if isNodeReplayed(nm) {
				status += " [replay]"
			}
			if em := nodeExtractionMethod(nm); em != "" {
				status += " [" + em + "]"
			}
		}
		row := []string{
			stringVal(nm, "node_id"),
			stringVal(nm, "node_type"),
			truncateModel(stringVal(nm, "model")),
			status,
			answer,
			fmt.Sprintf("%.0fms", floatVal(nm, "latency_ms")),
		}
		if opts.IncludeTokens {
			row = append(row, fmt.Sprintf("%.0f", floatVal(nm, "tokens_input")+floatVal(nm, "tokens_output")))
		}
		rows = append(rows, row)
	}
	return rows
}

// renderNodeRows builds table rows from workflow nodes, skipping aggregation child nodes.
// Returns rows suitable for printTableAuto with nodeTableHeaders.
// When expectedAnswer is provided, appends (Y) or (N) correctness markers to answers.
func renderNodeRows(nodes []interface{}, expectedAnswer ...string) [][]string {
	expected := ""
	if len(expectedAnswer) > 0 {
		expected = expectedAnswer[0]
	}
	return renderNodeRowsOpts(nodes, nodeRowOpts{
		ExpectedAnswer: expected,
		AnnotateStatus: true,
	})
}

// nodeTableHeaders is the standard header for node tables.
var nodeTableHeaders = []string{"NODE", "TYPE", "MODEL", "STATUS", "ANSWER", "LATENCY"}

// nodeTableHeadersWithTokens is nodeTableHeaders plus a TOKENS column.
var nodeTableHeadersWithTokens = []string{"NODE", "TYPE", "MODEL", "STATUS", "ANSWER", "LATENCY", "TOKENS"}

// isNodeReplayed checks if a node's metadata indicates it was replayed from a baseline.
// The metadata field on WorkflowNode is a JSON string; on NodeExecutionAttempt it's a parsed map.
func isNodeReplayed(nm map[string]interface{}) bool {
	// Attempt metadata is already a parsed map.
	if meta, ok := nm["metadata"].(map[string]interface{}); ok {
		if replayed, _ := meta["replayed"].(bool); replayed {
			return true
		}
	}
	// WorkflowNode metadata is a JSON string.
	if metaStr, ok := nm["metadata"].(string); ok && metaStr != "" {
		var meta map[string]interface{}
		if json.Unmarshal([]byte(metaStr), &meta) == nil {
			if replayed, _ := meta["replayed"].(bool); replayed {
				return true
			}
		}
	}
	return false
}

// nodeExtractionMethod returns "regex" or "llm" if the node metadata contains
// an extraction_method key (set by contract_extract nodes). Returns "" otherwise.
func nodeExtractionMethod(nm map[string]interface{}) string {
	extract := func(meta map[string]interface{}) string {
		if v, ok := meta["extraction_method"].(string); ok {
			switch v {
			case "regex":
				return "regex"
			case "llm_fallback":
				return "llm"
			}
		}
		return ""
	}
	// Attempt metadata is already a parsed map.
	if meta, ok := nm["metadata"].(map[string]interface{}); ok {
		if em := extract(meta); em != "" {
			return em
		}
	}
	// WorkflowNode metadata is a JSON string.
	if metaStr, ok := nm["metadata"].(string); ok && metaStr != "" {
		var meta map[string]interface{}
		if json.Unmarshal([]byte(metaStr), &meta) == nil {
			return extract(meta)
		}
	}
	return ""
}

// buildCategoryItemSet returns a set of item_ids matching the given analysis category.
func buildCategoryItemSet(analysisData map[string]interface{}, category string) map[string]string {
	result := make(map[string]string)
	items, ok := analysisData["items"].([]interface{})
	if !ok {
		return result
	}
	for _, item := range items {
		im, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		cat := stringVal(im, "category")
		if cat == category {
			result[stringVal(im, "item_id")] = cat
		}
	}
	return result
}

// filterItemsByCategory filters benchmark items in-place to only those in categorySet,
// and injects _showCategory metadata so the table renderer adds a CATEGORY column.
func filterItemsByCategory(dataMap map[string]interface{}, categorySet map[string]string) {
	items, ok := dataMap["Items"].([]interface{})
	if !ok {
		return
	}
	var filtered []interface{}
	for _, item := range items {
		im, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemID := stringVal(im, "item_id")
		if cat, found := categorySet[itemID]; found {
			im["_category"] = cat
			filtered = append(filtered, item)
		}
	}
	dataMap["Items"] = filtered
	dataMap["_showCategory"] = true
}

// printNodeDetails prints prompt and/or raw output for each node (skipping child aggregation nodes).
func printNodeDetails(w io.Writer, nodes []interface{}, showPrompts, showOutput bool) {
	for _, n := range nodes {
		nm, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		if stringVal(nm, "parent_node_id") != "" {
			continue
		}
		nodeID := stringVal(nm, "node_id")
		if showPrompts {
			if prompt := stringVal(nm, "prompt"); prompt != "" {
				fmt.Fprintf(w, "\n  [%s] Prompt:\n    %s\n", nodeID, prompt)
			}
		}
		if showOutput {
			if output := stringVal(nm, "output"); output != "" {
				fmt.Fprintf(w, "\n  [%s] Output:\n    %s\n", nodeID, output)
			}
		}
	}
}
