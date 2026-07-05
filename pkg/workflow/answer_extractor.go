package workflow

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ExtractionStrategy defines how to extract a discrete answer from LLM output.
type ExtractionStrategy string

const (
	ExtractRegex       ExtractionStrategy = "regex"
	ExtractFirstLetter ExtractionStrategy = "first_letter"
	ExtractJSONField   ExtractionStrategy = "json_field"
)

// DefaultExtractionPattern extracts explicit final answers from MCQA and short
// open-ended responses. The first non-empty capture group is returned, so
// "Answer: A because..." normalizes to "A" while "Answer: 27." returns "27".
const DefaultExtractionPattern = `(?im)(?:^|\n|[^\n]{0,60}\b)(?:final answer|answer)\s*(?:(?:is)?\s*[:\-]\s*(?:\(?([A-Z])\)?\s*(?:[.)]?\s*$|\s+(?:is|was|are|because|since|as|therefore|so)\b)|\(?([^\n)]{1,80}?)\)?\s*(?:[.;]\s*$|$|\n|[.;]\s+(?:because|since|as|this|the|it)\b|,?\s+(?:because|since|as|therefore|so)\b))|is\s*:?\s*(?:\(?([A-Z])\)?\s*(?:[.)]?\s*$|\s+(?:is|was|are|because|since|as|therefore|so)\b)|([-+]?(?:\d+(?:\.\d+)?|[A-Z]\s*=\s*[-+]?\d+(?:\.\d+)?))\s*(?:[.;]?\s*$|,?\s+(?:because|since|as|therefore|so)\b)))`

// ExtractorConfig configures answer extraction.
type ExtractorConfig struct {
	Strategy ExtractionStrategy `json:"strategy"`
	Pattern  string             `json:"pattern"` // regex pattern or JSON field name
}

// DefaultExtractorConfig returns the default extractor config for MCQA-style answers.
func DefaultExtractorConfig() ExtractorConfig {
	return ExtractorConfig{
		Strategy: ExtractRegex,
		Pattern:  DefaultExtractionPattern,
	}
}

// ParseExtractorConfig extracts an ExtractorConfig from an aggregation config map.
func ParseExtractorConfig(config map[string]interface{}) ExtractorConfig {
	cfg := ExtractorConfig{}
	if strategy, ok := config["extraction_strategy"].(string); ok && strategy != "" {
		cfg.Strategy = ExtractionStrategy(strategy)
	}
	if pattern, ok := config["extraction_pattern"].(string); ok && pattern != "" {
		cfg.Pattern = pattern
	}
	return cfg
}

// ExtractAnswer extracts a discrete answer from the given text using the configured strategy.
// Returns empty string if no answer could be extracted.
func ExtractAnswer(text string, cfg ExtractorConfig) string {
	switch cfg.Strategy {
	case ExtractFirstLetter:
		return extractFirstLetter(text)
	case ExtractJSONField:
		return extractJSONField(text, cfg.Pattern)
	case ExtractRegex:
		return extractRegex(text, cfg.Pattern)
	default:
		return ""
	}
}

// extractRegex extracts an answer using a regex pattern.
// The first capture group is returned as the answer.
func extractRegex(text, pattern string) string {
	if pattern == "" {
		return ""
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	for _, match := range matches[1:] {
		if strings.TrimSpace(match) != "" {
			return strings.ToUpper(strings.TrimSpace(match))
		}
	}
	return ""
}

// extractFirstLetter extracts the first uppercase letter in the text.
func extractFirstLetter(text string) string {
	for _, r := range text {
		if unicode.IsUpper(r) && unicode.IsLetter(r) {
			return string(r)
		}
	}
	return ""
}

// extractJSONField parses JSON from the text and extracts a named field.
func extractJSONField(text, fieldName string) string {
	if fieldName == "" {
		fieldName = "answer"
	}

	// Try to find JSON in the text (may be wrapped in markdown code fences)
	jsonStr := text
	if idx := strings.Index(text, "{"); idx >= 0 {
		if end := strings.LastIndex(text, "}"); end > idx {
			jsonStr = text[idx : end+1]
		}
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return ""
	}

	if val, ok := data[fieldName]; ok {
		switch v := val.(type) {
		case string:
			return strings.ToUpper(strings.TrimSpace(v))
		case float64:
			return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	return ""
}

// ComputeAgreement extracts answers from all agent outputs and computes agreement.
// Returns agreementRatio (0.0-1.0), consensusAnswer, and dissentingIDs.
// If no answers can be extracted, returns 0.0 (meaning "unknown").
func ComputeAgreement(inputs []AgentOutput, cfg ExtractorConfig) (float64, string, []string) {
	if len(inputs) == 0 {
		return 0, "", nil
	}

	// Extract answers from each agent
	answers := make(map[string]string) // agentID -> extracted answer
	for _, input := range inputs {
		answer := ExtractAnswer(input.Output, cfg)
		if answer != "" {
			answers[input.AgentID] = answer
		}
	}

	if len(answers) == 0 {
		return 0, "", nil
	}

	// Count votes per answer
	votes := make(map[string]int)
	for _, answer := range answers {
		votes[answer]++
	}

	// Find the consensus (most common answer) — deterministic tie-break by alphabetical order
	type answerCount struct {
		answer string
		count  int
	}
	sorted := make([]answerCount, 0, len(votes))
	for answer, count := range votes {
		sorted = append(sorted, answerCount{answer, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].answer < sorted[j].answer
	})
	consensus := sorted[0].answer
	maxVotes := sorted[0].count

	// Build dissenting list
	var dissenting []string
	for _, input := range inputs {
		answer, extracted := answers[input.AgentID]
		if !extracted {
			continue // couldn't extract, not counted
		}
		if answer != consensus {
			dissenting = append(dissenting, input.AgentID)
		}
	}

	ratio := float64(maxVotes) / float64(len(answers))
	return ratio, consensus, dissenting
}
