package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	DefaultDataDir = "benchmarks/data"
	RunSizeSmall   = "small"
	RunSizeFull    = "full"
	RunSizeCustom  = "custom"
)

type datasetRow struct {
	ID           string   `json:"id"`
	Benchmark    string   `json:"benchmark"`
	Split        string   `json:"split"`
	Subject      string   `json:"subject"`
	Language     string   `json:"language"`
	Question     string   `json:"question"`
	Choices      []string `json:"choices"`
	AnswerIndex  int      `json:"answer_index"`
	AnswerLabel  string   `json:"answer_label"`
	GoldAnswer   string   `json:"gold_answer"`
	GoldSolution string   `json:"gold_solution"`
}

func NormalizeBenchmark(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "global-mmlu", "global_mmlu", "globalmmlu":
		return BenchmarkGlobalMMLU, nil
	case "global-mmlu-lite", "global_mmlu_lite", "globalmmlulite":
		return BenchmarkGlobalMMLULite, nil
	case "mmlu-pro", "mmlu_pro", "mmlupro":
		return BenchmarkMMLUPro, nil
	case "math-500", "math_500", "math500":
		return BenchmarkMath500, nil
	default:
		return "", fmt.Errorf("unsupported benchmark %q", name)
	}
}

func NormalizeSplit(split string) string {
	s := strings.ToLower(strings.TrimSpace(split))
	if s == "" {
		return "test"
	}
	return s
}

func NormalizeRunSize(runSize string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(runSize))
	if s == "" {
		return RunSizeFull, nil
	}
	switch s {
	case RunSizeSmall:
		return RunSizeSmall, nil
	case RunSizeFull:
		return RunSizeFull, nil
	case RunSizeCustom, "split":
		return RunSizeCustom, nil
	default:
		return "", fmt.Errorf("unsupported run-size %q (use small, full, or custom)", runSize)
	}
}

func BenchmarkDirName(benchmark string) (string, error) {
	norm, err := NormalizeBenchmark(benchmark)
	if err != nil {
		return "", err
	}
	switch norm {
	case BenchmarkGlobalMMLU:
		return "global_mmlu", nil
	case BenchmarkGlobalMMLULite:
		return "global_mmlu_lite", nil
	case BenchmarkMMLUPro:
		return "mmlu_pro", nil
	case BenchmarkMath500:
		return "math_500", nil
	default:
		return "", fmt.Errorf("no directory mapping for benchmark %q", benchmark)
	}
}

func DatasetPath(dataDir, benchmark, split string) (string, error) {
	dirName, err := BenchmarkDirName(benchmark)
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, dirName, NormalizeSplit(split)+".jsonl"), nil
}

func ResolveDatasetPathForRun(dataDir, benchmark, runSize, explicitSplit string) (string, string, error) {
	normBenchmark, err := NormalizeBenchmark(benchmark)
	if err != nil {
		return "", "", err
	}

	normRunSize, err := NormalizeRunSize(runSize)
	if err != nil {
		return "", "", err
	}

	var selectedSplit string
	if normRunSize == RunSizeCustom {
		if strings.TrimSpace(explicitSplit) == "" {
			return "", "", fmt.Errorf("split must be provided when run-size=custom")
		}
		selectedSplit = NormalizeSplit(explicitSplit)
	} else {
		selectedSplit, err = defaultSplitForRunSize(normBenchmark, normRunSize)
		if err != nil {
			return "", "", err
		}
	}

	path, err := DatasetPath(dataDir, normBenchmark, selectedSplit)
	if err != nil {
		return "", "", err
	}

	// Validate that the configured split exists and is non-empty.
	if _, err := countDatasetItems(path); err != nil {
		return "", "", fmt.Errorf("invalid dataset for benchmark=%s run-size=%s split=%s: %w", normBenchmark, normRunSize, selectedSplit, err)
	}
	return selectedSplit, path, nil
}

func defaultSplitForRunSize(benchmark, runSize string) (string, error) {
	switch runSize {
	case RunSizeSmall:
		switch benchmark {
		case BenchmarkGlobalMMLU, BenchmarkGlobalMMLULite:
			return "dev", nil
		case BenchmarkMMLUPro:
			return "validation", nil
		case BenchmarkMath500:
			return "dev", nil
		default:
			return "", fmt.Errorf("unsupported benchmark %q", benchmark)
		}
	case RunSizeFull:
		switch benchmark {
		case BenchmarkGlobalMMLU, BenchmarkGlobalMMLULite, BenchmarkMMLUPro, BenchmarkMath500:
			return "test", nil
		default:
			return "", fmt.Errorf("unsupported benchmark %q", benchmark)
		}
	default:
		return "", fmt.Errorf("unsupported run-size %q (use small, full, or custom)", runSize)
	}
}

func ListAvailableSplits(dataDir, benchmark string) ([]string, error) {
	dirName, err := BenchmarkDirName(benchmark)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, dirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read dataset dir %q: %w", root, err)
	}

	splits := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".jsonl" {
			continue
		}
		split := strings.TrimSuffix(name, ".jsonl")
		if split == "" {
			continue
		}
		splits = append(splits, split)
	}
	sort.Strings(splits)
	return splits, nil
}

func ParseCSVValues(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func countDatasetItems(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open dataset %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan dataset %q: %w", path, err)
	}
	if count == 0 {
		return 0, fmt.Errorf("dataset %q has no data rows", path)
	}
	return count, nil
}

func LoadDataset(path string, limit int) ([]DatasetItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dataset %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	// Questions and options can exceed scanner defaults.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	items := make([]DatasetItem, 0, 1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var row datasetRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse dataset line %d: %w", lineNum, err)
		}

		item, err := toDatasetItem(row, lineNum)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan dataset %q: %w", path, err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("dataset %q is empty", path)
	}
	return items, nil
}

func toDatasetItem(row datasetRow, lineNum int) (DatasetItem, error) {
	question := strings.TrimSpace(row.Question)
	if question == "" {
		return DatasetItem{}, fmt.Errorf("line %d: missing question", lineNum)
	}

	id := strings.TrimSpace(row.ID)
	if id == "" {
		id = "line-" + strconv.Itoa(lineNum)
	}

	subject := strings.TrimSpace(row.Subject)
	if subject == "" {
		subject = "unknown"
	}
	benchmark := strings.TrimSpace(row.Benchmark)
	if benchmark == "" {
		benchmark = "unknown"
	}
	normalizedBenchmark, normErr := NormalizeBenchmark(benchmark)
	language := strings.TrimSpace(row.Language)
	if language == "" {
		if normErr == nil &&
			(normalizedBenchmark == BenchmarkMMLUPro || normalizedBenchmark == BenchmarkGlobalMMLU || normalizedBenchmark == BenchmarkGlobalMMLULite || normalizedBenchmark == BenchmarkMath500) {
			language = "en"
		} else {
			language = "unknown"
		}
	}

	format := BenchmarkFormatMCQA
	if normErr == nil {
		format = FormatForBenchmark(normalizedBenchmark)
	}

	answerLabel := strings.ToUpper(strings.TrimSpace(row.AnswerLabel))
	answerIndex := row.AnswerIndex
	goldAnswer := strings.TrimSpace(row.GoldAnswer)
	goldSolution := strings.TrimSpace(row.GoldSolution)

	if format == BenchmarkFormatMCQA {
		if len(row.Choices) < 2 {
			return DatasetItem{}, fmt.Errorf("line %d: expected at least 2 choices", lineNum)
		}

		if answerLabel == "" {
			if answerIndex < 0 || answerIndex >= len(row.Choices) {
				return DatasetItem{}, fmt.Errorf("line %d: invalid answer_index=%d for %d choices", lineNum, answerIndex, len(row.Choices))
			}
			answerLabel = LabelForIndex(answerIndex)
		} else {
			idx, ok := IndexFromLabel(answerLabel)
			if !ok || idx >= len(row.Choices) {
				return DatasetItem{}, fmt.Errorf("line %d: invalid answer_label=%q for %d choices", lineNum, answerLabel, len(row.Choices))
			}
			answerIndex = idx
		}
	} else {
		if goldAnswer == "" {
			goldAnswer = strings.TrimSpace(row.AnswerLabel)
		}
		if goldAnswer == "" {
			return DatasetItem{}, fmt.Errorf("line %d: missing gold_answer for benchmark %q", lineNum, benchmark)
		}
		if answerLabel == "" {
			// Keep compatibility with existing admin payloads that expect answer_label.
			answerLabel = goldAnswer
		}
		answerIndex = 0
	}

	return DatasetItem{
		ID:           id,
		Benchmark:    benchmark,
		Split:        strings.TrimSpace(row.Split),
		Subject:      subject,
		Language:     language,
		Question:     question,
		Choices:      row.Choices,
		AnswerIndex:  answerIndex,
		AnswerLabel:  answerLabel,
		GoldAnswer:   goldAnswer,
		GoldSolution: goldSolution,
	}, nil
}
