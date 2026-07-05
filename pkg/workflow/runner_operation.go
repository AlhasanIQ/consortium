package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	OperationCollectInputs    = "collect_inputs"
	OperationFormatCandidates = "format_candidates"
	OperationExtractAnswer    = "extract_answer"
	OperationCountVotes       = "count_votes"
	OperationGroupAnswerCamps = "group_answer_camps"
	OperationSelectWinner     = "select_winner"
	OperationReduceScores     = "reduce_scores"
	OperationParseJSONField   = "parse_json_field"
	OperationParseSelection   = "parse_selection"
)

var exactTemplateRegex = regexp.MustCompile(`^\{\{([^}]+)\}\}$`)

// OperationNodeRunner executes deterministic, zero-cost workflow operations.
type OperationNodeRunner struct{}

func (r *OperationNodeRunner) NodeType() NodeType { return NodeTypeOperation }

func (r *OperationNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	if sc == nil || sc.Node == nil {
		return nil, fmt.Errorf("operation node context is nil")
	}
	config := resolveOperationConfig(sc.Node.OperationConfig, sc.WorkflowContext)

	var (
		output string
		err    error
	)
	switch sc.Node.OperationType {
	case OperationCollectInputs:
		output, err = executeCollectInputs(config, sc.WorkflowContext)
	case OperationFormatCandidates:
		output, err = executeFormatCandidates(config, sc.WorkflowContext)
	case OperationExtractAnswer:
		output, err = executeExtractAnswer(config)
	case OperationCountVotes:
		output, err = executeCountVotes(config)
	case OperationGroupAnswerCamps:
		output, err = executeGroupAnswerCamps(config)
	case OperationSelectWinner:
		output, err = executeSelectWinner(config)
	case OperationReduceScores:
		output, err = executeReduceScores(config)
	case OperationParseJSONField:
		output, err = executeParseJSONField(config)
	case OperationParseSelection:
		output, err = executeParseSelection(config)
	default:
		err = fmt.Errorf("unknown operation_type %q", sc.Node.OperationType)
	}
	if err != nil {
		return nil, err
	}
	return &NodeResult{
		NodeID:  sc.NodeID,
		Success: true,
		Output:  output,
	}, nil
}

func resolveOperationConfig(config map[string]interface{}, ctx map[string]interface{}) map[string]interface{} {
	resolved := make(map[string]interface{}, len(config))
	for key, value := range config {
		resolved[key] = resolveOperationValue(value, ctx)
	}
	return resolved
}

func resolveOperationValue(value interface{}, ctx map[string]interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		if match := exactTemplateRegex.FindStringSubmatch(typed); len(match) == 2 {
			key := strings.TrimSpace(match[1])
			if ctxValue, ok := ctx[key]; ok {
				return ctxValue
			}
		}
		return InterpolateVariables(typed, ctx)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = resolveOperationValue(item, ctx)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			out[key] = resolveOperationValue(item, ctx)
		}
		return out
	default:
		return value
	}
}

type collectedItem struct {
	ID     string `json:"id"`
	Output string `json:"output"`
	Model  string `json:"model,omitempty"`
}

func executeCollectInputs(config map[string]interface{}, ctx map[string]interface{}) (string, error) {
	inputs, err := operationInputs(config, ctx)
	if err != nil {
		return "", err
	}
	separator := stringConfig(config, "separator", "\n---\n")
	items := make([]collectedItem, 0, len(inputs))
	parts := make([]string, 0, len(inputs))
	for _, input := range inputs {
		items = append(items, collectedItem{ID: input.AgentID, Output: input.Output, Model: input.Model})
		parts = append(parts, input.Output)
	}
	if boolConfig(config, "raw_text", false) {
		return strings.Join(parts, separator), nil
	}
	return marshalOperationOutput(struct {
		Items []collectedItem `json:"items"`
		Text  string          `json:"text"`
	}{Items: items, Text: strings.Join(parts, separator)})
}

func executeFormatCandidates(config map[string]interface{}, ctx map[string]interface{}) (string, error) {
	inputs, err := operationInputs(config, ctx)
	if err != nil {
		return "", err
	}
	labels := candidateLabels(len(inputs))
	lines := make([]string, 0, len(inputs))
	items := make([]map[string]interface{}, 0, len(inputs))
	blind := boolConfig(config, "blind", false)
	for i, input := range inputs {
		label := labels[i]
		header := fmt.Sprintf("Candidate %s", label)
		if !blind {
			modelSuffix := ""
			if input.Model != "" {
				modelSuffix = fmt.Sprintf(", model %s", input.Model)
			}
			header = fmt.Sprintf("Candidate %s (%s%s)", label, input.AgentID, modelSuffix)
		}
		lines = append(lines, fmt.Sprintf("%s:\n%s", header, input.Output))
		items = append(items, map[string]interface{}{
			"label":  label,
			"id":     input.AgentID,
			"model":  input.Model,
			"output": input.Output,
		})
	}
	text := strings.Join(lines, "\n\n")
	if boolConfig(config, "raw_text", false) {
		return text, nil
	}
	return marshalOperationOutput(map[string]interface{}{
		"candidates": items,
		"text":       text,
	})
}

func executeExtractAnswer(config map[string]interface{}) (string, error) {
	text := stringConfig(config, "text", "")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("extract_answer requires text")
	}
	extractorConfig := ParseExtractorConfig(config)
	if extractorConfig.Strategy == "" && extractorConfig.Pattern == "" {
		extractorConfig = DefaultExtractorConfig()
	}
	answer := ExtractAnswer(text, extractorConfig)
	if boolConfig(config, "raw_answer", false) {
		return answer, nil
	}
	return marshalOperationOutput(map[string]string{"answer": answer})
}

func executeCountVotes(config map[string]interface{}) (string, error) {
	answers, err := stringSliceConfig(config, "answers")
	if err != nil {
		return "", err
	}
	result := tallyVotes(answers)
	return marshalOperationOutput(result)
}

func executeGroupAnswerCamps(config map[string]interface{}) (string, error) {
	items, err := answerItems(config)
	if err != nil {
		return "", err
	}
	campsByAnswer := make(map[string][]string)
	for _, item := range items {
		if item.Answer == "" {
			continue
		}
		campsByAnswer[item.Answer] = append(campsByAnswer[item.Answer], item.ID)
	}
	answers := make([]string, 0, len(campsByAnswer))
	for answer := range campsByAnswer {
		answers = append(answers, answer)
	}
	sort.Strings(answers)
	camps := make([]map[string]interface{}, 0, len(answers))
	for _, answer := range answers {
		sort.Strings(campsByAnswer[answer])
		camps = append(camps, map[string]interface{}{
			"answer":     answer,
			"member_ids": campsByAnswer[answer],
		})
	}
	return marshalOperationOutput(map[string]interface{}{"camps": camps})
}

func executeSelectWinner(config map[string]interface{}) (string, error) {
	if rawVoteResult, ok := config["vote_result"]; ok {
		return executeSelectVoteWinner(config, rawVoteResult)
	}
	if rawWinnerAnswer, ok := config["winner_answer"]; ok {
		return executeSelectAnswerWinner(config, rawWinnerAnswer)
	}
	if rawSelection, ok := config["selection"]; ok {
		return executeSelectSelectionWinner(config, rawSelection)
	}
	scores, err := scoreMapConfig(config, "scores")
	if err != nil {
		return "", err
	}
	outputs := stringMapConfig(config, "outputs")
	if boolConfig(config, "prefer_fallback_output", false) {
		if fallback := strings.TrimSpace(stringFromAny(config["fallback_output"])); fallback != "" {
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": "", "output": fallback, "fallback": true})
		}
	}
	inputs := make([]AgentOutput, 0, len(outputs))
	for id, output := range outputs {
		inputs = append(inputs, AgentOutput{AgentID: id, Output: output})
	}
	winner, output := selectWinner(scores, inputs)
	if output == "" {
		output = outputs[winner]
	}
	if boolConfig(config, "raw_output", false) {
		return output, nil
	}
	return marshalOperationOutput(map[string]interface{}{
		"winner": winner,
		"output": output,
		"scores": scores,
	})
}

func executeSelectSelectionWinner(config map[string]interface{}, rawSelection interface{}) (string, error) {
	outputs := stringMapConfig(config, "outputs")
	if len(outputs) == 0 {
		return "", fmt.Errorf("select_winner selection mode requires outputs")
	}
	labels := stringSliceConfigOrDefault(config, "labels", nil)
	winner := resolveSelectionToCandidate(rawSelection, labels, stringMapConfig(config, "label_to_id"))
	if winner == "" {
		winner = resolveSelectionToCandidate(config["fallback_selection"], labels, stringMapConfig(config, "label_to_id"))
	}
	if winner == "" {
		if fallback := strings.TrimSpace(stringFromAny(config["fallback_output"])); fallback != "" {
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": "", "output": fallback, "fallback": true})
		}
		return "", fmt.Errorf("select_winner selection mode could not resolve winner")
	}
	output, ok := outputs[winner]
	if !ok || strings.TrimSpace(output) == "" {
		if fallback := strings.TrimSpace(stringFromAny(config["fallback_output"])); fallback != "" {
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": winner, "output": fallback, "fallback": true})
		}
		return "", fmt.Errorf("select_winner selection mode could not resolve output for winner %q", winner)
	}
	if boolConfig(config, "raw_output", false) {
		return output, nil
	}
	return marshalOperationOutput(map[string]interface{}{"winner": winner, "output": output})
}

func executeSelectAnswerWinner(config map[string]interface{}, rawWinnerAnswer interface{}) (string, error) {
	items, err := answerItems(config)
	if err != nil {
		return "", err
	}
	winnerAnswer := strings.TrimSpace(stringFromAny(rawWinnerAnswer))
	if winnerAnswer == "" {
		winnerAnswer = strings.TrimSpace(stringFromAny(config["fallback_selection"]))
	}
	if winnerAnswer == "" {
		if fallback := firstFallbackOutput(config); fallback != "" {
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": "", "output": fallback, "fallback": true})
		}
		return "", fmt.Errorf("select_winner answer mode requires winner_answer")
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Answer), winnerAnswer) {
			if boolConfig(config, "raw_output", false) {
				return item.Output, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": item.ID, "answer": item.Answer, "output": item.Output})
		}
	}
	if fallback := firstFallbackOutput(config); fallback != "" {
		if boolConfig(config, "raw_output", false) {
			return fallback, nil
		}
		return marshalOperationOutput(map[string]interface{}{"winner": "", "answer": winnerAnswer, "output": fallback, "fallback": true})
	}
	return "", fmt.Errorf("select_winner answer mode could not resolve answer %q", winnerAnswer)
}

func firstFallbackOutput(config map[string]interface{}) string {
	for _, key := range []string{"fallback_output", "secondary_fallback"} {
		if fallback := strings.TrimSpace(stringFromAny(config[key])); fallback != "" {
			return fallback
		}
	}
	return ""
}

func executeSelectVoteWinner(config map[string]interface{}, rawVoteResult interface{}) (string, error) {
	vote, err := voteResultFromValue(rawVoteResult)
	if err != nil {
		return "", err
	}
	items, err := answerItems(config)
	if err != nil {
		return "", err
	}
	if boolConfig(config, "require_unanimous", false) {
		if !vote.Unanimous {
			return "", nil
		}
		return outputForVoteWinner(config, vote, items)
	}
	tieBreaker := strings.ToLower(strings.TrimSpace(stringConfig(config, "tie_breaker_method", "error")))
	if strings.TrimSpace(vote.Winner) == "" {
		switch tieBreaker {
		case "synthesis":
			fallback := strings.TrimSpace(stringFromAny(config["fallback_output"]))
			if fallback == "" {
				return "", fmt.Errorf("select_winner vote mode has no winning answer and requires fallback_output for synthesis")
			}
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": "", "output": fallback, "tie": false})
		case "first":
			if len(items) == 0 {
				return "", fmt.Errorf("select_winner vote mode has no items")
			}
			if boolConfig(config, "raw_output", false) {
				return items[0].Output, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": items[0].ID, "answer": items[0].Answer, "output": items[0].Output, "fallback": "first"})
		case "error", "":
			return "", fmt.Errorf("select_winner vote mode has no winning answer")
		default:
			return "", fmt.Errorf("select_winner vote mode unsupported tie_breaker_method %q", tieBreaker)
		}
	}
	if vote.Tie {
		switch tieBreaker {
		case "synthesis":
			fallback := strings.TrimSpace(stringFromAny(config["fallback_output"]))
			if fallback == "" {
				return "", fmt.Errorf("select_winner vote mode tie requires fallback_output for synthesis")
			}
			if boolConfig(config, "raw_output", false) {
				return fallback, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": "", "output": fallback, "tie": true})
		case "first":
			if len(items) == 0 {
				return "", fmt.Errorf("select_winner vote mode has no items")
			}
			if boolConfig(config, "raw_output", false) {
				return items[0].Output, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": items[0].ID, "answer": items[0].Answer, "output": items[0].Output, "tie": true})
		case "error", "":
			return "", fmt.Errorf("select_winner vote mode tie for answer %q", vote.Winner)
		default:
			return "", fmt.Errorf("select_winner vote mode unsupported tie_breaker_method %q", tieBreaker)
		}
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Answer), vote.Winner) {
			if boolConfig(config, "raw_output", false) {
				return item.Output, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": item.ID, "answer": item.Answer, "output": item.Output, "tie": false})
		}
	}
	return "", fmt.Errorf("select_winner vote mode could not resolve answer %q", vote.Winner)
}

func outputForVoteWinner(config map[string]interface{}, vote voteResult, items []answerItem) (string, error) {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Answer), vote.Winner) {
			if boolConfig(config, "raw_output", false) {
				return item.Output, nil
			}
			return marshalOperationOutput(map[string]interface{}{"winner": item.ID, "answer": item.Answer, "output": item.Output, "unanimous": vote.Unanimous})
		}
	}
	return "", nil
}

func executeReduceScores(config map[string]interface{}) (string, error) {
	scoreLists, err := scoreListsConfig(config, "scores")
	if err != nil {
		return "", err
	}
	reduced := make(map[string]float64, len(scoreLists))
	for id, values := range scoreLists {
		var total float64
		for _, value := range values {
			total += value
		}
		if len(values) > 0 {
			reduced[id] = total / float64(len(values))
		}
	}
	winner, _ := selectWinner(reduced, nil)
	return marshalOperationOutput(map[string]interface{}{"scores": reduced, "winner": winner})
}

func executeParseJSONField(config map[string]interface{}) (string, error) {
	text := stringConfig(config, "text", "")
	field := stringConfig(config, "field", "")
	if strings.TrimSpace(text) == "" {
		if fallback, ok := config["fallback"]; ok && fallback != nil {
			return jsonFieldOutput(fallback, config)
		}
		if boolConfig(config, "optional", false) {
			return "", nil
		}
		return "", fmt.Errorf("parse_json_field requires text")
	}
	if strings.TrimSpace(field) == "" {
		return "", fmt.Errorf("parse_json_field requires field")
	}
	if strings.EqualFold(strings.TrimSpace(field), "score") {
		if score, ok := parseScoreValue(text, config); ok {
			value := strconv.FormatFloat(score, 'f', -1, 64)
			if boolConfig(config, "raw", false) {
				return value, nil
			}
			return marshalOperationOutput(map[string]string{"value": value})
		}
	}
	value, ok := extractJSONFieldValue(text, field)
	if !ok || isEmptyJSONFieldValue(value) {
		if fallback, hasFallback := config["fallback"]; hasFallback && fallback != nil {
			return jsonFieldOutput(fallback, config)
		}
		if boolConfig(config, "optional", false) {
			return "", nil
		}
		value = ""
	}
	if isInvalidGeneratedRubricField(field, value) {
		if fallback, hasFallback := config["fallback"]; hasFallback && fallback != nil {
			return jsonFieldOutput(fallback, config)
		}
	}
	return jsonFieldOutput(value, config)
}

func isInvalidGeneratedRubricField(field string, value interface{}) bool {
	if !strings.EqualFold(strings.TrimSpace(field), "rubric") {
		return false
	}
	rubric := parseRubricCriteria(value)
	if len(rubric) < 2 || len(rubric) > 10 {
		return true
	}
	for _, criterion := range rubric {
		if strings.TrimSpace(criterion.Name) == "" || criterion.Weight <= 0 {
			return true
		}
	}
	return false
}

func executeParseSelection(config map[string]interface{}) (string, error) {
	text := stringConfig(config, "text", "")
	labels, err := stringSliceConfig(config, "labels")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("parse_selection requires text")
	}
	value := parseWinner(text, labels)
	if value == "" {
		trimmed := strings.TrimSpace(text)
		for _, label := range labels {
			if strings.EqualFold(trimmed, label) {
				value = label
				break
			}
		}
	}
	if boolConfig(config, "raw", false) {
		return value, nil
	}
	return marshalOperationOutput(map[string]string{"value": value})
}

func extractJSONFieldValue(text, fieldName string) (interface{}, bool) {
	if fieldName == "" {
		fieldName = "answer"
	}
	jsonStr := text
	if idx := strings.Index(text, "{"); idx >= 0 {
		if end := strings.LastIndex(text, "}"); end > idx {
			jsonStr = text[idx : end+1]
		}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, false
	}
	value, ok := data[fieldName]
	return value, ok
}

func jsonFieldOutput(value interface{}, config map[string]interface{}) (string, error) {
	formatted := jsonFieldValueString(value, boolConfig(config, "json_raw", false))
	if boolConfig(config, "format_rubric", false) {
		if rubricText := rubricCriteriaText(parseRubricCriteria(value)); strings.TrimSpace(rubricText) != "" {
			formatted = rubricText
		}
	}
	if boolConfig(config, "raw", false) {
		return formatted, nil
	}
	return marshalOperationOutput(map[string]string{"value": formatted})
}

func rubricCriteriaText(rubric []RubricCriterion) string {
	lines := make([]string, 0, len(rubric))
	for _, criterion := range rubric {
		name := strings.TrimSpace(criterion.Name)
		if name == "" {
			continue
		}
		weight := strconv.FormatFloat(criterion.Weight, 'f', -1, 64)
		description := strings.TrimSpace(criterion.Description)
		if description == "" {
			lines = append(lines, fmt.Sprintf("- %s (%s)", name, weight))
		} else {
			lines = append(lines, fmt.Sprintf("- %s (%s): %s", name, weight, description))
		}
	}
	return strings.Join(lines, "\n")
}

func jsonFieldValueString(value interface{}, jsonRaw bool) string {
	switch typed := value.(type) {
	case string:
		// parse_json_field is generic; answer normalization happens in extract_answer.
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(typed), 'f', -1, 64))
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case nil:
		return ""
	default:
		if jsonRaw {
			if data, err := json.Marshal(typed); err == nil {
				return string(data)
			}
		}
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func isEmptyJSONFieldValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []interface{}:
		return len(typed) == 0
	case map[string]interface{}:
		return len(typed) == 0
	default:
		return false
	}
}

type voteResult struct {
	Counts    map[string]int `json:"counts"`
	Winner    string         `json:"winner"`
	Tie       bool           `json:"tie"`
	Total     int            `json:"total"`
	Unanimous bool           `json:"unanimous"`
}

func tallyVotes(answers []string) voteResult {
	counts := make(map[string]int)
	for _, answer := range answers {
		trimmed := strings.TrimSpace(answer)
		if trimmed == "" {
			continue
		}
		counts[trimmed]++
	}
	winner := ""
	bestCount := 0
	tie := false
	keys := make([]string, 0, len(counts))
	for answer := range counts {
		keys = append(keys, answer)
	}
	sort.Strings(keys)
	for _, answer := range keys {
		count := counts[answer]
		if count > bestCount {
			winner = answer
			bestCount = count
			tie = false
			continue
		}
		if count == bestCount {
			tie = true
		}
	}
	unanimous := winner != "" && len(counts) == 1 && bestCount == len(answers)
	return voteResult{Counts: counts, Winner: winner, Tie: tie, Total: len(answers), Unanimous: unanimous}
}

func operationInputs(config map[string]interface{}, ctx map[string]interface{}) ([]AgentOutput, error) {
	if raw, ok := config["candidates"]; ok {
		return agentOutputsFromValue(raw)
	}
	inputIDs, err := stringSliceConfig(config, "input_ids")
	if err != nil {
		return nil, err
	}
	inputs := make([]AgentOutput, 0, len(inputIDs))
	for _, id := range inputIDs {
		output, ok := ctx[id]
		if !ok || output == nil {
			return nil, fmt.Errorf("input %q not found in workflow context", id)
		}
		model, _ := ctx[id+"__model"].(string)
		inputs = append(inputs, AgentOutput{
			AgentID: id,
			Model:   model,
			Output:  fmt.Sprintf("%v", output),
		})
	}
	return inputs, nil
}

func agentOutputsFromValue(value interface{}) ([]AgentOutput, error) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("candidates must be an array")
	}
	outputs := make([]AgentOutput, 0, len(items))
	for i, item := range items {
		switch typed := item.(type) {
		case string:
			id := fmt.Sprintf("candidate-%d", i+1)
			outputs = append(outputs, AgentOutput{AgentID: id, Output: typed})
		case map[string]interface{}:
			id := stringFromAny(firstPresent(typed, "id", "agent_id"))
			if id == "" {
				id = fmt.Sprintf("candidate-%d", i+1)
			}
			outputs = append(outputs, AgentOutput{
				AgentID: id,
				Model:   stringFromAny(typed["model"]),
				Output:  stringFromAny(typed["output"]),
			})
		default:
			return nil, fmt.Errorf("candidate %d must be a string or object", i)
		}
	}
	return outputs, nil
}

type answerItem struct {
	ID     string
	Answer string
	Output string
}

func answerItems(config map[string]interface{}) ([]answerItem, error) {
	if raw, ok := config["items"]; ok {
		items, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("items must be an array")
		}
		out := make([]answerItem, 0, len(items))
		for i, item := range items {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("item %d must be an object", i)
			}
			id := stringFromAny(firstPresent(obj, "id", "agent_id"))
			if id == "" {
				id = fmt.Sprintf("candidate-%d", i+1)
			}
			out = append(out, answerItem{ID: id, Answer: stringFromAny(obj["answer"]), Output: stringFromAny(obj["output"])})
		}
		return out, nil
	}
	answers, err := stringSliceConfig(config, "answers")
	if err != nil {
		return nil, err
	}
	out := make([]answerItem, 0, len(answers))
	for i, answer := range answers {
		out = append(out, answerItem{ID: fmt.Sprintf("candidate-%d", i+1), Answer: answer})
	}
	return out, nil
}

func voteResultFromValue(value interface{}) (voteResult, error) {
	switch typed := value.(type) {
	case voteResult:
		return typed, nil
	case map[string]interface{}:
		return voteResult{
			Counts:    intMapFromAny(typed["counts"]),
			Winner:    stringFromAny(typed["winner"]),
			Tie:       boolFromAny(typed["tie"]),
			Total:     int(floatFromAny(typed["total"])),
			Unanimous: boolFromAny(typed["unanimous"]),
		}, nil
	case string:
		var parsed voteResult
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return parsed, nil
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(typed), &obj); err == nil {
			return voteResultFromValue(obj)
		}
		return voteResult{}, fmt.Errorf("vote_result must be JSON vote result")
	default:
		return voteResult{}, fmt.Errorf("vote_result must be an object or JSON string")
	}
}

func parseScoreValue(text string, config map[string]interface{}) (float64, bool) {
	if value := extractJSONField(text, "score"); strings.TrimSpace(value) != "" {
		if score, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return score, true
		}
	}

	rubric := parseRubric(config)
	if criterionScores := parseScores(text); len(criterionScores) > 0 {
		if score, matched := calculateWeightedScore(criterionScores, rubric); matched {
			return score, true
		}
	}
	if criterionScores, ok := parseCriterionScores(text); ok {
		if score, matched := calculateWeightedScore(criterionScores, rubric); matched {
			return score, true
		}
	}
	return 0, false
}

func stringSliceConfig(config map[string]interface{}, key string) ([]string, error) {
	raw, ok := config[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, stringFromAny(item))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array", key)
	}
}

func stringSliceConfigOrDefault(config map[string]interface{}, key string, fallback []string) []string {
	values, err := stringSliceConfig(config, key)
	if err != nil {
		return fallback
	}
	return values
}

func scoreMapConfig(config map[string]interface{}, key string) (map[string]float64, error) {
	raw, ok := config[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	out := make(map[string]float64)
	switch typed := raw.(type) {
	case map[string]float64:
		for k, v := range typed {
			out[k] = v
		}
	case map[string]interface{}:
		if nested, ok := typed["scores"]; ok {
			return scoreMapFromValue(nested, key)
		}
		for k, v := range typed {
			out[k] = floatFromAny(v)
		}
	case string:
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return scoreMapFromValue(parsed, key)
		}
		return nil, fmt.Errorf("%s must be an object", key)
	default:
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return out, nil
}

func scoreMapFromValue(raw interface{}, key string) (map[string]float64, error) {
	return scoreMapConfig(map[string]interface{}{key: raw}, key)
}

func scoreListsConfig(config map[string]interface{}, key string) (map[string][]float64, error) {
	raw, ok := config[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	out := make(map[string][]float64)
	switch typed := raw.(type) {
	case map[string]interface{}:
		for id, value := range typed {
			switch list := value.(type) {
			case []interface{}:
				for _, item := range list {
					out[id] = append(out[id], floatFromAny(item))
				}
			case []float64:
				out[id] = append(out[id], list...)
			default:
				out[id] = append(out[id], floatFromAny(value))
			}
		}
	case []interface{}:
		for _, item := range typed {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("%s array entries must be objects", key)
			}
			id := stringFromAny(firstPresent(obj, "id", "agent_id", "candidate"))
			if id == "" {
				return nil, fmt.Errorf("%s entry missing id", key)
			}
			out[id] = append(out[id], floatFromAny(firstPresent(obj, "score", "value")))
		}
	default:
		return nil, fmt.Errorf("%s must be an object or array", key)
	}
	return out, nil
}

func stringMapConfig(config map[string]interface{}, key string) map[string]string {
	raw, ok := config[key]
	if !ok {
		return nil
	}
	out := make(map[string]string)
	switch typed := raw.(type) {
	case map[string]string:
		for k, v := range typed {
			out[k] = v
		}
	case map[string]interface{}:
		for k, v := range typed {
			out[k] = stringFromAny(v)
		}
	}
	return out
}

func stringConfig(config map[string]interface{}, key, fallback string) string {
	if value, ok := config[key]; ok {
		return stringFromAny(value)
	}
	return fallback
}

func boolConfig(config map[string]interface{}, key string, fallback bool) bool {
	value, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

func boolFromAny(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return boolConfig(map[string]interface{}{"value": typed}, "value", false)
	default:
		return false
	}
}

func intMapFromAny(value interface{}) map[string]int {
	out := make(map[string]int)
	switch typed := value.(type) {
	case map[string]int:
		for k, v := range typed {
			out[k] = v
		}
	case map[string]interface{}:
		for k, v := range typed {
			out[k] = int(floatFromAny(v))
		}
	}
	return out
}

func resolveSelectionToCandidate(value interface{}, labels []string, labelToID map[string]string) string {
	text := strings.TrimSpace(stringFromAny(value))
	if text == "" {
		return ""
	}
	selection := text
	if len(labels) > 0 {
		if parsed := parseWinner(text, labels); parsed != "" {
			selection = parsed
		}
	}
	if id := labelToID[selection]; id != "" {
		return id
	}
	for label, id := range labelToID {
		if strings.EqualFold(label, selection) {
			return id
		}
	}
	return selection
}

func stringFromAny(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func floatFromAny(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		f, _ := typed.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return f
	default:
		return 0
	}
}

func firstPresent(values map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func candidateLabels(count int) []string {
	return CandidateLabels(count)
}

func marshalOperationOutput(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
