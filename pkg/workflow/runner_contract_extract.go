package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
)

type compiledExtractionPattern struct {
	re    *regexp.Regexp
	valid bool
}

var extractionPatternCache sync.Map

// ContractExtractNodeRunner extracts a value from upstream text using regex patterns
// defined in the node's metadata, falling back to an LLM call only when no pattern matches.
//
// The engine is pattern-agnostic — all extraction patterns come from the seed config
// via metadata["extraction_patterns"] ([]string of regex patterns tried in order).
type ContractExtractNodeRunner struct {
	llmRunner *LLMCallNodeRunner
}

// NewContractExtractNodeRunner creates a runner backed by the given LLM client
// for fallback extraction.
func NewContractExtractNodeRunner(llmClient *providers.Client) *ContractExtractNodeRunner {
	return &ContractExtractNodeRunner{
		llmRunner: &LLMCallNodeRunner{llmClient: llmClient},
	}
}

// NodeType returns NodeTypeContractExtract.
func (r *ContractExtractNodeRunner) NodeType() NodeType { return NodeTypeContractExtract }

// Execute tries regex extraction from the upstream source variable, returning
// immediately on success. Falls back to an LLM-based extraction call otherwise.
func (r *ContractExtractNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	// Read required source_variable from metadata.
	sourceVar, _ := sc.Node.Metadata["source_variable"].(string)
	if sourceVar == "" {
		err := NewNonRetryableError(
			fmt.Errorf("contract_extract node %q missing required metadata key 'source_variable'", sc.NodeID),
			"INVALID_CONFIG",
		)
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	// Read extraction patterns from metadata.
	patterns := parseExtractionPatterns(sc.Node.Metadata)

	// If no patterns configured, go straight to LLM.
	if len(patterns) == 0 {
		return r.executeLLMFallback(sc)
	}

	// Resolve the raw upstream text from workflow context.
	rawValue, ok := sc.WorkflowContext[sourceVar]
	if !ok {
		err := NewNonRetryableError(
			fmt.Errorf("contract_extract node %q source variable %q not found in workflow context", sc.NodeID, sourceVar),
			"INVALID_SOURCE",
		)
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if rawValue == nil {
		err := NewNonRetryableError(
			fmt.Errorf("contract_extract node %q source variable %q is nil", sc.NodeID, sourceVar),
			"INVALID_SOURCE",
		)
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	sourceText := strings.TrimSpace(fmt.Sprintf("%v", rawValue))
	if sourceText == "" {
		err := NewNonRetryableError(
			fmt.Errorf("contract_extract node %q source variable %q is empty", sc.NodeID, sourceVar),
			"INVALID_SOURCE",
		)
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	// Try each pattern in order.
	for _, pattern := range patterns {
		re, ok := compileExtractionPattern(pattern)
		if !ok {
			continue // skip invalid patterns
		}
		if label := matchFirstCaptureGroup(re, sourceText); label != "" {
			return &NodeResult{
				NodeID:       sc.NodeID,
				Success:      true,
				Output:       label,
				TokensInput:  0,
				TokensOutput: 0,
				Cost:         0,
				LatencyMs:    float64(time.Since(startTime).Milliseconds()),
				Metadata: MergeMetadata(sc.Node.Metadata, map[string]interface{}{
					"extraction_method": "regex",
					"source_variable":   sourceVar,
				}),
			}, nil
		}
	}

	// No pattern matched — fall back to LLM-based extraction.
	return r.executeLLMFallback(sc)
}

// executeLLMFallback delegates to the LLM runner, tagging the result as a fallback.
func (r *ContractExtractNodeRunner) executeLLMFallback(sc *NodeContext) (*NodeResult, error) {
	// Shallow-copy the node with type=prompt so LLMCallNodeRunner handles it.
	promptNode := *sc.Node
	promptNode.Type = NodeTypePrompt

	fallbackSC := &NodeContext{
		Ctx:             sc.Ctx,
		Node:            &promptNode,
		NodeID:          sc.NodeID,
		Attempt:         sc.Attempt,
		ParentSpanID:    sc.ParentSpanID,
		WorkflowContext: sc.WorkflowContext,
		ExecCtx:         sc.ExecCtx,
		CostTracker:     sc.CostTracker,
	}

	result, err := r.llmRunner.Execute(fallbackSC)
	if result != nil {
		result.Metadata = MergeMetadata(result.Metadata, map[string]interface{}{
			"extraction_method": "llm_fallback",
		})
	}
	return result, err
}

func compileExtractionPattern(pattern string) (*regexp.Regexp, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, false
	}
	if cached, ok := extractionPatternCache.Load(pattern); ok {
		entry, _ := cached.(compiledExtractionPattern)
		return entry.re, entry.valid
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		extractionPatternCache.Store(pattern, compiledExtractionPattern{valid: false})
		return nil, false
	}
	entry := compiledExtractionPattern{re: re, valid: true}
	extractionPatternCache.Store(pattern, entry)
	return re, true
}

// parseExtractionPatterns reads extraction_patterns ([]string) from node metadata.
func parseExtractionPatterns(metadata map[string]interface{}) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["extraction_patterns"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		patterns := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				patterns = append(patterns, s)
			}
		}
		return patterns
	}
	return nil
}

// matchFirstCaptureGroup applies a regex and returns the uppercase first capture group
// trimmed of whitespace. Returns empty string if no match or no capture group.
func matchFirstCaptureGroup(re *regexp.Regexp, text string) string {
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(match[1]))
}
