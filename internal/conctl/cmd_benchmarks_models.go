package conctl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
	conmodels "github.com/alhasaniq/consortium/internal/conctl/models"
	"github.com/alhasaniq/consortium/pkg/providers"
)

const (
	// The V2 Free endpoint is the supported public model-data API. It returns
	// model-level data (not the retired website host-model rows) and requires a
	// valid API key, including for the Free tier.
	defaultBenchmarkModelsSourceURL = "https://artificialanalysis.ai/api/v2/language/models/free"
	defaultBenchmarkModelsRepoFile  = "benchmarks/models_repo/models_flat.json"
	defaultBenchmarkModelsRawFile   = "benchmarks/models_repo/source_raw.json"
	defaultBenchmarkModelsAPIKeyEnv = "ARTIFICIAL_ANALYSIS_API_KEY"
	defaultOpenRouterAPIKeyEnv      = "OPENROUTER_API_KEY"
	// Default filters applied by both `list` and `suggest`.
	defaultMinIntel = 30.0
	defaultMaxCost  = 1000.0
	// Higher-priority sort keys should dominate lower-priority keys.
	// Geometric decay with base 12 gives strong precedence without collapsing
	// completely into lexicographic sorting.
	compositePositionalWeightBase = 12.0
	// Avoid hard-zero geometric collapse for the worst normalized value on a
	// present dimension. Missing dimensions are still treated as missing.
	compositeScoreFloor = 0.05
)

type benchmarkModelsRepo struct {
	SourceURL    string                 `json:"source_url"`
	SourceRows   int                    `json:"source_rows"`
	UniqueModels int                    `json:"unique_models"`
	SnapshotAt   string                 `json:"snapshot_at"`
	Models       []benchmarkModelRecord `json:"models"`
}

type benchmarkModelRecord struct {
	ModelID                          string   `json:"model_id"`
	Slug                             string   `json:"slug"`
	Name                             string   `json:"name"`
	ShortName                        string   `json:"short_name"`
	CreatorName                      string   `json:"creator_name,omitempty"`
	ReasoningModel                   bool     `json:"reasoning_model"`
	ReasoningModelKnown              bool     `json:"reasoning_model_known"`
	Deprecated                       bool     `json:"deprecated"`
	DeprecatedKnown                  bool     `json:"deprecated_known"`
	ReleaseDate                      string   `json:"release_date,omitempty"`
	ContextWindowTokens              *float64 `json:"context_window_tokens,omitempty"`
	IntelligenceIndex                *float64 `json:"intelligence_index,omitempty"`
	Price1MInputTokens               *float64 `json:"price_1m_input_tokens,omitempty"`
	Price1MOutputTokens              *float64 `json:"price_1m_output_tokens,omitempty"`
	Price1MBlended3To1               *float64 `json:"price_1m_blended_3_to_1,omitempty"`
	IntelligenceIndexInputTokens     *float64 `json:"intelligence_index_input_tokens,omitempty"`
	IntelligenceIndexOutputTokens    *float64 `json:"intelligence_index_output_tokens,omitempty"`
	IntelligenceIndexReasoningTokens *float64 `json:"intelligence_index_reasoning_tokens,omitempty"`
	IntelligenceIndexAnswerTokens    *float64 `json:"intelligence_index_answer_tokens,omitempty"`
	CostToRunIndexInputUSD           *float64 `json:"cost_to_run_index_input_usd,omitempty"`
	CostToRunIndexOutputUSD          *float64 `json:"cost_to_run_index_output_usd,omitempty"`
	CostToRunIndexTotalUSD           *float64 `json:"cost_to_run_index_total_usd,omitempty"`
	ValueScore                       *float64 `json:"value_score,omitempty"`
	E2ETotalTimeSec                  *float64 `json:"e2e_total_time_sec,omitempty"`
	E2EP95TimeSec                    *float64 `json:"e2e_p95_time_sec,omitempty"`
	TTFATotalTimeSec                 *float64 `json:"ttfa_total_time_sec,omitempty"`
	OutputSpeedTokSec                *float64 `json:"output_speed_tok_sec,omitempty"`
	ModelURL                         string   `json:"model_url,omitempty"`
	HostsURL                         string   `json:"hosts_url,omitempty"`
	SourceHostModelID                string   `json:"source_host_model_id,omitempty"`
	SourceHostModelSlug              string   `json:"source_host_model_slug,omitempty"`
	SourceHostID                     string   `json:"source_host_id,omitempty"`
	SourceHostName                   string   `json:"source_host_name,omitempty"`
	ComputedPerformanceHostModelID   string   `json:"computed_performance_host_model_id,omitempty"`
	OpenRouterModelID                string   `json:"openrouter_model_id,omitempty"`
	OpenRouterModelName              string   `json:"openrouter_model_name,omitempty"`
	OpenRouterProvider               string   `json:"openrouter_provider,omitempty"`
	OpenRouterMatchMethod            string   `json:"openrouter_match_method,omitempty"`
	Source                           string   `json:"source"`
	SnapshotAt                       string   `json:"snapshot_at"`
}

type benchmarkModelsSyncOutput struct {
	SourceURL         string `json:"source_url"`
	RawOut            string `json:"raw_out"`
	RepoOut           string `json:"repo_out"`
	SourceRows        int    `json:"source_rows"`
	UniqueModels      int    `json:"unique_models"`
	OpenRouterModels  int    `json:"openrouter_models"`
	OpenRouterMapped  int    `json:"openrouter_mapped"`
	OpenRouterCatalog string `json:"openrouter_catalog"`
	SnapshotAt        string `json:"snapshot_at"`
}

type benchmarkModelsListOutput struct {
	SnapshotAt string                 `json:"snapshot_at"`
	Sort       string                 `json:"sort"`
	Total      int                    `json:"total"`
	Models     []benchmarkModelRecord `json:"models"`
}

type benchmarkModelsShowOutput struct {
	SnapshotAt string               `json:"snapshot_at"`
	Model      benchmarkModelRecord `json:"model"`
}

type benchmarkModelsSuggestOutput struct {
	SnapshotAt      string                 `json:"snapshot_at"`
	Top             int                    `json:"top"`
	BestCheap       []benchmarkModelRecord `json:"best_cheap"`
	BestExpensive   []benchmarkModelRecord `json:"best_expensive"`
	MostIntelligent []benchmarkModelRecord `json:"most_intelligent"`
}

type openRouterCatalogResult struct {
	Models []providers.Model
	Source string
}

type openRouterModelCandidate struct {
	model     providers.Model
	idLower   string
	provider  string
	suffixKey string
	nameKey   string
	tokenSet  map[string]struct{}
}

func benchmarksModelsCmd() *app.Command {
	return &app.Command{
		Name: "models",
		Desc: "Model hints repository for benchmark iteration (sync/list/show/suggest).\n\n" +
			"Subcommands:\n" +
			"  sync    Fetch and build local models repo snapshot\n" +
			"  list    List models with intelligence/cost/value hints\n" +
			"  show    Show one model in detail\n" +
			"  suggest Show top hints (value, intelligence, cheapest)\n\n" +
			"Default output format is markdown; use --format json when machine parsing is needed.",
		UsageLine: "conctl benchmarks models <sync|list|show|suggest> [flags]",
		Flags: func() *flag.FlagSet {
			return flag.NewFlagSet("benchmarks models", flag.ContinueOnError)
		},
		Run: func(gf app.GlobalFlags, args []string) int {
			if len(args) == 0 {
				fmt.Fprintln(os.Stderr, "Error: subcommand is required (sync|list|show|suggest)")
				return app.ExitUsage
			}
			subcommand := strings.ToLower(strings.TrimSpace(args[0]))
			subArgs := args[1:]
			switch subcommand {
			case "sync":
				return benchmarksModelsSyncRun(gf, subArgs)
			case "list":
				return benchmarksModelsListRun(gf, subArgs)
			case "show":
				return benchmarksModelsShowRun(gf, subArgs)
			case "suggest":
				return benchmarksModelsSuggestRun(gf, subArgs)
			default:
				fmt.Fprintf(os.Stderr, "Error: unknown models subcommand %q (expected sync|list|show|suggest)\n", subcommand)
				return app.ExitUsage
			}
		},
	}
}

func benchmarksModelsSyncRun(gf app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("benchmarks models sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	sourceURL := fs.String("source-url", defaultBenchmarkModelsSourceURL, "Artificial Analysis V2 model-list URL (Free default; Pro supported)")
	apiKey := fs.String("api-key", "", "Artificial Analysis API key value (fallback: env var; required)")
	apiKeyEnv := fs.String("api-key-env", defaultBenchmarkModelsAPIKeyEnv, "Env var name for API key fallback")
	openRouterAPIKey := fs.String("openrouter-api-key", "", "Optional OpenRouter API key for model ID/name mapping")
	openRouterAPIKeyEnv := fs.String("openrouter-api-key-env", defaultOpenRouterAPIKeyEnv, "Env var name for OpenRouter API key fallback")
	rawOut := fs.String("raw-out", defaultBenchmarkModelsRawFile, "Path to write raw source JSON")
	repoOut := fs.String("repo-out", defaultBenchmarkModelsRepoFile, "Path to write flattened models repo JSON")
	if err := fs.Parse(args); err != nil {
		return app.ExitUsage
	}

	key := strings.TrimSpace(*apiKey)
	if key == "" && *apiKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(*apiKeyEnv))
	}
	if key == "" {
		fmt.Fprintf(os.Stderr, "Error: Artificial Analysis API key is required (set --api-key or %s)\n", *apiKeyEnv)
		return app.ExitUsage
	}
	orKey := strings.TrimSpace(*openRouterAPIKey)
	if orKey == "" && *openRouterAPIKeyEnv != "" {
		orKey = strings.TrimSpace(os.Getenv(*openRouterAPIKeyEnv))
	}

	timeout, err := time.ParseDuration(gf.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid timeout %q: %v\n", gf.Timeout, err)
		return app.ExitUsage
	}

	body, rows, err := fetchBenchmarkModelRows(*sourceURL, key, timeout)
	if err != nil {
		return HandleError(err)
	}

	snapshotAt := time.Now().UTC().Format(time.RFC3339)
	repo := buildBenchmarkModelsRepo(rows, *sourceURL, snapshotAt)
	orCatalog := loadOpenRouterCatalog(orKey, timeout)
	mappedCount := applyOpenRouterMappings(&repo, orCatalog.Models)
	deduplicateOpenRouterMappings(&repo)
	repoChanged := true

	if info, statErr := os.Stat(*repoOut); statErr == nil && !info.IsDir() {
		existingRepo, loadErr := loadBenchmarkModelsRepo(*repoOut)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "Error: read existing repo output: %v\n", loadErr)
			return app.ExitInternal
		}
		if benchmarkModelsRepoEqualIgnoringSnapshot(*existingRepo, repo) {
			setBenchmarkModelsRepoSnapshot(&repo, strings.TrimSpace(existingRepo.SnapshotAt))
			if strings.TrimSpace(repo.SnapshotAt) == "" {
				setBenchmarkModelsRepoSnapshot(&repo, snapshotAt)
			}
			repoChanged = false
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Error: stat repo output %s: %v\n", *repoOut, statErr)
		return app.ExitInternal
	}

	if err := writeJSONFile(*rawOut, body); err != nil {
		fmt.Fprintf(os.Stderr, "Error: write raw output: %v\n", err)
		return app.ExitInternal
	}
	if repoChanged {
		if err := writeJSONFile(*repoOut, repo); err != nil {
			fmt.Fprintf(os.Stderr, "Error: write repo output: %v\n", err)
			return app.ExitInternal
		}
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	out := &benchmarkModelsSyncOutput{
		SourceURL:         *sourceURL,
		RawOut:            *rawOut,
		RepoOut:           *repoOut,
		SourceRows:        repo.SourceRows,
		UniqueModels:      repo.UniqueModels,
		OpenRouterModels:  len(orCatalog.Models),
		OpenRouterMapped:  mappedCount,
		OpenRouterCatalog: orCatalog.Source,
		SnapshotAt:        repo.SnapshotAt,
	}
	return rc.Output(out, benchmarkModelsSyncTable)
}

// modelFilterOpts holds the shared filtering parameters used by both list and suggest.
type modelFilterOpts struct {
	Query             string
	IncludeDeprecated bool
	Reasoning         string
	Frontier          bool
	MinIntel          float64
	MaxCost           float64
}

// defaultModelFilterOpts returns the standard filter pipeline defaults.
func defaultModelFilterOpts() modelFilterOpts {
	return modelFilterOpts{
		// The V2 Free response deliberately omits reasoning_model, so filtering
		// it as false would silently remove every model.
		Reasoning: "any",
		Frontier:  true,
		MinIntel:  defaultMinIntel,
		MaxCost:   defaultMaxCost,
	}
}

// applyModelFilters runs the shared filter pipeline: reasoning/deprecated/query then optional frontier.
func applyModelFilters(models []benchmarkModelRecord, opts modelFilterOpts) ([]benchmarkModelRecord, error) {
	filtered, err := filterBenchmarkModels(models, opts.Query, opts.IncludeDeprecated, opts.Reasoning)
	if err != nil {
		return nil, err
	}
	if opts.Frontier {
		filtered = paretoFrontier(filtered, opts.MinIntel, opts.MaxCost)
	}
	return filtered, nil
}

func benchmarksModelsListRun(gf app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("benchmarks models list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("repo", defaultBenchmarkModelsRepoFile, "Path to flattened models repo JSON")
	limit := fs.Int("limit", 25, "Max models to display (0 = all)")
	sortBy := fs.String("sort", "value", "Sort: value|intelligence|cost|rank|name (comma-separated for multi-key, e.g. cost,intelligence)")
	defaults := defaultModelFilterOpts()
	query := fs.String("query", "", "Case-insensitive filter against name/slug")
	includeDeprecated := fs.Bool("include-deprecated", false, "Include deprecated models")
	reasoning := fs.String("reasoning", defaults.Reasoning, "Filter reasoning models: any|true|false")
	noFrontier := fs.Bool("no-frontier", false, "Disable Pareto frontier filtering (frontier is on by default)")
	minIntel := fs.Float64("min-intel", defaults.MinIntel, "Minimum intelligence index for frontier filtering")
	maxCost := fs.Float64("max-cost", defaults.MaxCost, "Maximum cost-to-run USD for frontier filtering (0 = no cap)")
	if err := fs.Parse(args); err != nil {
		return app.ExitUsage
	}

	repo, err := loadBenchmarkModelsRepo(*repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	filtered, err := applyModelFilters(repo.Models, modelFilterOpts{
		Query:             *query,
		IncludeDeprecated: *includeDeprecated,
		Reasoning:         *reasoning,
		Frontier:          !*noFrontier,
		MinIntel:          *minIntel,
		MaxCost:           *maxCost,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}
	sortKeys, err := parseSortKeys(*sortBy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}
	sortBenchmarkModels(filtered, strings.Join(sortKeys, ","))
	filtered = applyModelLimit(filtered, *limit)

	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	sortLabel := strings.ToLower(strings.TrimSpace(*sortBy))
	if !*noFrontier {
		sortLabel = "frontier+" + sortLabel
	}
	out := &benchmarkModelsListOutput{
		SnapshotAt: repo.SnapshotAt,
		Sort:       sortLabel,
		Total:      len(filtered),
		Models:     filtered,
	}
	return rc.Output(out, benchmarkModelsListTable)
}

func benchmarksModelsShowRun(gf app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("benchmarks models show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("repo", defaultBenchmarkModelsRepoFile, "Path to flattened models repo JSON")
	modelRef := fs.String("model", "", "Model slug/id/name fragment (required)")
	if err := fs.Parse(args); err != nil {
		return app.ExitUsage
	}
	if strings.TrimSpace(*modelRef) == "" {
		fmt.Fprintln(os.Stderr, "Error: --model is required")
		return app.ExitUsage
	}

	repo, err := loadBenchmarkModelsRepo(*repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	model, err := findBenchmarkModel(repo.Models, *modelRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	out := &benchmarkModelsShowOutput{
		SnapshotAt: repo.SnapshotAt,
		Model:      model,
	}
	return rc.Output(out, benchmarkModelsShowTable)
}

func benchmarksModelsSuggestRun(gf app.GlobalFlags, args []string) int {
	fs := flag.NewFlagSet("benchmarks models suggest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoPath := fs.String("repo", defaultBenchmarkModelsRepoFile, "Path to flattened models repo JSON")
	top := fs.Int("top", 8, "Number of hints per section")
	query := fs.String("query", "", "Case-insensitive filter against name/slug")
	includeDeprecated := fs.Bool("include-deprecated", false, "Include deprecated models")
	reasoning := fs.String("reasoning", "any", "Filter reasoning models: any|true|false")
	if err := fs.Parse(args); err != nil {
		return app.ExitUsage
	}
	if *top < 1 {
		fmt.Fprintln(os.Stderr, "Error: --top must be >= 1")
		return app.ExitUsage
	}

	repo, err := conmodels.LoadModelsRepo(*repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	suggestions, err := conmodels.Suggest(repo, conmodels.SuggestOptions{
		Top:               *top,
		Track:             "balanced",
		Query:             *query,
		IncludeDeprecated: *includeDeprecated,
		Reasoning:         *reasoning,
		MinIntel:          defaultMinIntel,
		MaxCost:           defaultMaxCost,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return app.ExitUsage
	}

	rc, err := NewRunContext(gf)
	if err != nil {
		return HandleError(err)
	}
	defer rc.Cancel()

	out := &benchmarkModelsSuggestOutput{
		SnapshotAt:      suggestions.SnapshotAt,
		Top:             *top,
		BestCheap:       convertSuggestedModels(suggestions.Cheap),
		BestExpensive:   convertSuggestedModels(suggestions.Expensive),
		MostIntelligent: convertSuggestedModels(suggestions.MostIntelligent),
	}
	return rc.Output(out, benchmarkModelsSuggestTable)
}

func convertSuggestedModels(items []conmodels.Model) []benchmarkModelRecord {
	if len(items) == 0 {
		return nil
	}
	out := make([]benchmarkModelRecord, 0, len(items))
	for _, model := range items {
		out = append(out, benchmarkModelRecord{
			ModelID:                model.ModelID,
			Slug:                   model.Slug,
			Name:                   model.Name,
			ShortName:              model.ShortName,
			ReasoningModel:         model.ReasoningModel,
			Deprecated:             model.Deprecated,
			IntelligenceIndex:      model.IntelligenceIndex,
			CostToRunIndexTotalUSD: model.CostToRunIndexTotalUS,
			OpenRouterModelID:      model.OpenRouterModelID,
			OpenRouterModelName:    model.OpenRouterModelName,
			SnapshotAt:             "",
		})
	}
	return out
}

func fetchBenchmarkModelRows(sourceURL, apiKey string, timeout time.Duration) (map[string]interface{}, []map[string]interface{}, error) {
	return fetchBenchmarkModelRowsWithClient(sourceURL, apiKey, &http.Client{Timeout: timeout})
}

func fetchBenchmarkModelRowsWithClient(sourceURL, apiKey string, client *http.Client) (map[string]interface{}, []map[string]interface{}, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, nil, errors.New("Artificial Analysis API key is required")
	}
	if client == nil {
		return nil, nil, errors.New("source HTTP client is required")
	}

	parsedURL, err := url.Parse(sourceURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse source URL: %w", err)
	}
	if parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return nil, nil, errors.New("source URL must be an absolute HTTPS URL")
	}

	rows := make([]map[string]interface{}, 0)
	pages := make([]map[string]interface{}, 0)
	for page := 1; ; page++ {
		pageURL := *parsedURL
		query := pageURL.Query()
		query.Set("page", strconv.Itoa(page))
		pageURL.RawQuery = query.Encode()

		req, err := http.NewRequest(http.MethodGet, pageURL.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("accept", "application/json")
		req.Header.Set("x-api-key", strings.TrimSpace(apiKey))

		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch source page %d: %w", page, err)
		}
		var raw map[string]interface{}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			err = json.NewDecoder(resp.Body).Decode(&raw)
		} else {
			var snippet map[string]interface{}
			_ = json.NewDecoder(resp.Body).Decode(&snippet)
			err = fmt.Errorf("source request failed: status=%d body=%v", resp.StatusCode, snippet)
		}
		closeErr := resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close source response: %w", closeErr)
		}

		pageRows, hasMore, err := validateV2LanguageModelsPage(raw, page)
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, raw)
		rows = append(rows, pageRows...)
		if !hasMore {
			break
		}
	}

	// Keep every original page in source_raw.json. Combining all data in the
	// first response would make its pagination metadata untrue.
	return map[string]interface{}{
		"source_url": sourceURL,
		"pages":      pages,
	}, rows, nil
}

// validateV2LanguageModelsPage validates the stable V2 list envelope before
// mapping it. This prevents a successful but differently shaped response from
// silently producing an empty model repository.
func validateV2LanguageModelsPage(raw map[string]interface{}, requestedPage int) ([]map[string]interface{}, bool, error) {
	if raw == nil {
		return nil, false, errors.New("source payload is empty")
	}
	tier, ok := raw["tier"].(string)
	if !ok || (tier != "free" && tier != "pro" && tier != "commercial") {
		return nil, false, errors.New("source payload does not include tier")
	}
	version, ok := raw["intelligence_index_version"].(float64)
	if !ok || version <= 0 {
		return nil, false, errors.New("source payload does not include numeric intelligence_index_version")
	}
	pagination, ok := raw["pagination"].(map[string]interface{})
	if !ok {
		return nil, false, errors.New("source payload does not include pagination")
	}
	page, pageOK := integerFrom(pagination, "page")
	totalPages, totalPagesOK := integerFrom(pagination, "total_pages")
	pageSize, pageSizeOK := integerFrom(pagination, "page_size")
	hasMore, hasMoreOK := pagination["has_more"].(bool)
	if !pageOK || !totalPagesOK || !pageSizeOK || !hasMoreOK || page != requestedPage || pageSize < 1 || totalPages < page {
		return nil, false, fmt.Errorf("source payload has invalid pagination for page %d", requestedPage)
	}
	if hasMore != (page < totalPages) {
		return nil, false, fmt.Errorf("source payload pagination has inconsistent has_more on page %d", requestedPage)
	}

	data, ok := raw["data"].([]interface{})
	if !ok {
		return nil, false, errors.New("source payload does not include data[]")
	}
	rows := make([]map[string]interface{}, 0, len(data))
	for i, item := range data {
		row, ok := item.(map[string]interface{})
		if !ok {
			return nil, false, fmt.Errorf("source payload data[%d] is not an object", i)
		}
		if err := validateV2LanguageModelRow(row); err != nil {
			return nil, false, fmt.Errorf("source payload data[%d]: %w", i, err)
		}
		rows = append(rows, row)
	}
	return rows, hasMore, nil
}

func validateV2LanguageModelRow(row map[string]interface{}) error {
	for _, key := range []string{"id", "name", "slug"} {
		if value, ok := row[key].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a non-empty string", key)
		}
	}
	if value, ok := row["release_date"]; !ok || (value != nil && !isString(value)) {
		return errors.New("release_date must be a string or null")
	}
	if err := validateNullableObject(row, "model_creator", []string{"id", "name"}, true); err != nil {
		return err
	}
	if err := validateObjectWithNullableNumbers(row, "evaluations", []string{
		"artificial_analysis_intelligence_index",
		"artificial_analysis_coding_index",
		"artificial_analysis_agentic_index",
	}); err != nil {
		return err
	}
	if value, ok := row["artificial_analysis_intelligence_index_cost"]; !ok || (value != nil && !isObject(value)) {
		return errors.New("artificial_analysis_intelligence_index_cost must be an object or null")
	}
	if value := row["artificial_analysis_intelligence_index_cost"]; value != nil {
		cost := value.(map[string]interface{})
		if !isNumber(cost["total_cost"]) {
			return errors.New("artificial_analysis_intelligence_index_cost.total_cost must be a number")
		}
	}
	if err := validateObjectWithNullableNumbers(row, "pricing", []string{
		"price_1m_input_tokens",
		"price_1m_output_tokens",
		"price_1m_cache_hit_tokens",
		"price_1m_cache_write_tokens",
	}); err != nil {
		return err
	}
	if err := validateObjectWithNullableNumbers(row, "performance", []string{
		"median_output_tokens_per_second",
		"median_time_to_first_token_seconds",
		"median_time_to_first_answer_token_seconds",
		"median_end_to_end_response_time_seconds",
	}); err != nil {
		return err
	}
	return nil
}

func validateNullableObject(row map[string]interface{}, key string, requiredStrings []string, allowNil bool) error {
	value, ok := row[key]
	if !ok || (value == nil && !allowNil) {
		return fmt.Errorf("%s must be an object", key)
	}
	if value == nil {
		return nil
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s must be an object or null", key)
	}
	for _, field := range requiredStrings {
		if value, ok := object[field].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s.%s must be a non-empty string", key, field)
		}
	}
	return nil
}

func validateObjectWithNullableNumbers(row map[string]interface{}, key string, fields []string) error {
	value, ok := row[key]
	if !ok || !isObject(value) {
		return fmt.Errorf("%s must be an object", key)
	}
	object := value.(map[string]interface{})
	for _, field := range fields {
		value, exists := object[field]
		if !exists || (value != nil && !isNumber(value)) {
			return fmt.Errorf("%s.%s must be a number or null", key, field)
		}
	}
	return nil
}

func isObject(value interface{}) bool {
	_, ok := value.(map[string]interface{})
	return ok
}

func isString(value interface{}) bool {
	_, ok := value.(string)
	return ok
}

func isNumber(value interface{}) bool {
	switch value.(type) {
	case float64, float32, int, int32, int64, json.Number:
		return true
	default:
		return false
	}
}

func loadOpenRouterCatalog(apiKey string, timeout time.Duration) openRouterCatalogResult {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	key := strings.TrimSpace(apiKey)
	provider := providers.NewOpenRouterProvider(providers.ProviderConfig{
		Name:    "openrouter",
		APIKey:  key,
		Timeout: timeout,
	})
	if key == "" {
		return openRouterCatalogResult{
			Models: provider.Models(),
			Source: "cache_static",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := provider.RefreshModels(ctx); err != nil {
		return openRouterCatalogResult{
			Models: provider.Models(),
			Source: "cache_static",
		}
	}
	return openRouterCatalogResult{
		Models: provider.Models(),
		Source: "cache_hydrated",
	}
}

func applyOpenRouterMappings(repo *benchmarkModelsRepo, openRouterModels []providers.Model) int {
	if repo == nil || len(repo.Models) == 0 || len(openRouterModels) == 0 {
		return 0
	}

	candidates := buildOpenRouterCandidates(openRouterModels)
	if len(candidates) == 0 {
		return 0
	}

	mapped := 0
	for i := range repo.Models {
		if strings.TrimSpace(repo.Models[i].OpenRouterModelID) != "" {
			// Pro V2 responses can provide the canonical OpenRouter ID directly.
			// Treat it as an established mapping rather than replacing it with a
			// heuristic catalog match.
			if repo.Models[i].OpenRouterProvider == "" {
				repo.Models[i].OpenRouterProvider = openRouterProviderFromID(repo.Models[i].OpenRouterModelID)
			}
			if repo.Models[i].OpenRouterMatchMethod == "" {
				repo.Models[i].OpenRouterMatchMethod = "artificial_analysis"
			}
			mapped++
			continue
		}
		match, ok := findOpenRouterMatch(repo.Models[i], candidates)
		if !ok {
			continue
		}
		repo.Models[i].OpenRouterModelID = match.model.ID
		repo.Models[i].OpenRouterModelName = match.model.Name
		repo.Models[i].OpenRouterProvider = openRouterProviderFromID(match.model.ID)
		repo.Models[i].OpenRouterMatchMethod = match.idLowerMethod()
		mapped++
	}
	return mapped
}

// deduplicateOpenRouterMappings resolves conflicts when multiple AA models
// map to the same OpenRouter ID. Keeps the mapping on the model with the
// highest intelligence index and clears it from the others.
func deduplicateOpenRouterMappings(repo *benchmarkModelsRepo) {
	if repo == nil || len(repo.Models) == 0 {
		return
	}

	// Build map of openRouterModelID → model indices.
	byOR := make(map[string][]int)
	for i, m := range repo.Models {
		orID := strings.TrimSpace(m.OpenRouterModelID)
		if orID == "" {
			continue
		}
		byOR[orID] = append(byOR[orID], i)
	}

	for _, indices := range byOR {
		if len(indices) <= 1 {
			continue
		}
		// Find the index with highest intelligence.
		bestIdx := indices[0]
		bestIntel := ptrFloatOr(repo.Models[bestIdx].IntelligenceIndex, -1)
		for _, idx := range indices[1:] {
			intel := ptrFloatOr(repo.Models[idx].IntelligenceIndex, -1)
			if intel > bestIntel {
				bestIntel = intel
				bestIdx = idx
			}
		}
		// Clear mapping from all but the best.
		for _, idx := range indices {
			if idx == bestIdx {
				continue
			}
			repo.Models[idx].OpenRouterModelID = ""
			repo.Models[idx].OpenRouterModelName = ""
			repo.Models[idx].OpenRouterProvider = ""
			repo.Models[idx].OpenRouterMatchMethod = ""
		}
	}
}

func buildOpenRouterCandidates(models []providers.Model) []openRouterModelCandidate {
	seen := make(map[string]struct{}, len(models))
	out := make([]openRouterModelCandidate, 0, len(models))
	for _, model := range models {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if id == "" || !strings.Contains(id, "/") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		provider := openRouterProviderFromID(model.ID)
		suffix := openRouterSuffixFromID(model.ID)
		combined := strings.TrimSpace(strings.Join([]string{suffix, model.Name}, " "))
		out = append(out, openRouterModelCandidate{
			model:     model,
			idLower:   id,
			provider:  provider,
			suffixKey: normalizeModelKey(suffix),
			nameKey:   normalizeModelKey(model.Name),
			tokenSet:  tokenizeModelKey(combined),
		})
	}
	return out
}

type openRouterMatch struct {
	model  providers.Model
	method string
}

func (m openRouterMatch) idLowerMethod() string {
	return strings.ToLower(strings.TrimSpace(m.method))
}

func findOpenRouterMatch(record benchmarkModelRecord, candidates []openRouterModelCandidate) (openRouterMatch, bool) {
	slugKey := normalizeModelKey(record.Slug)
	nameKeys := []string{
		normalizeModelKey(record.ShortName),
		normalizeModelKey(record.Name),
	}
	providerHints := providerHintsForRecord(record)

	if slugKey != "" {
		exactSlug := filterCandidates(candidates, func(c openRouterModelCandidate) bool {
			return c.suffixKey == slugKey
		})
		if match, ok := chooseWithProviderHints(exactSlug, providerHints); ok {
			method := "slug_exact"
			if hasProviderHint(providerHints, openRouterProviderFromID(match.model.ID)) {
				method = "slug_provider_exact"
			}
			return openRouterMatch{model: match.model, method: method}, true
		}
	}

	for _, nk := range nameKeys {
		if nk == "" {
			continue
		}
		exactName := filterCandidates(candidates, func(c openRouterModelCandidate) bool {
			return c.nameKey == nk
		})
		if match, ok := chooseWithProviderHints(exactName, providerHints); ok {
			method := "name_exact"
			if hasProviderHint(providerHints, openRouterProviderFromID(match.model.ID)) {
				method = "name_provider_exact"
			}
			return openRouterMatch{model: match.model, method: method}, true
		}
	}

	aaTokens := tokenizeModelKey(strings.Join([]string{record.Slug, record.ShortName, record.Name}, " "))
	if len(aaTokens) == 0 {
		return openRouterMatch{}, false
	}

	bestScore := -1.0
	secondScore := -1.0
	var best openRouterModelCandidate
	for _, candidate := range candidates {
		score := jaccardSimilarity(aaTokens, candidate.tokenSet)
		if slugKey != "" && candidate.suffixKey == slugKey {
			score += 0.35
		}
		if hasProviderHint(providerHints, candidate.provider) {
			score += 0.10
		}
		if slugKey != "" && (strings.Contains(candidate.suffixKey, slugKey) || strings.Contains(slugKey, candidate.suffixKey)) {
			score += 0.08
		}
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			best = candidate
		} else if score > secondScore {
			secondScore = score
		}
	}

	if bestScore >= 0.72 && bestScore-secondScore >= 0.05 {
		return openRouterMatch{model: best.model, method: "fuzzy"}, true
	}
	return openRouterMatch{}, false
}

func filterCandidates(candidates []openRouterModelCandidate, keep func(openRouterModelCandidate) bool) []openRouterModelCandidate {
	out := make([]openRouterModelCandidate, 0, len(candidates))
	for _, c := range candidates {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func chooseWithProviderHints(candidates []openRouterModelCandidate, providerHints map[string]struct{}) (openRouterModelCandidate, bool) {
	if len(candidates) == 0 {
		return openRouterModelCandidate{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	withProvider := make([]openRouterModelCandidate, 0, len(candidates))
	for _, c := range candidates {
		if hasProviderHint(providerHints, c.provider) {
			withProvider = append(withProvider, c)
		}
	}
	if len(withProvider) == 1 {
		return withProvider[0], true
	}
	if len(withProvider) > 1 {
		candidates = withProvider
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].idLower < candidates[j].idLower
	})
	return candidates[0], true
}

func providerHintsForRecord(record benchmarkModelRecord) map[string]struct{} {
	hints := make(map[string]struct{})
	addProviderHints(hints, record.CreatorName)
	addProviderHints(hints, record.SourceHostName)
	return hints
}

var openRouterProviderAliases = map[string][]string{
	"anthropic":       {"anthropic"},
	"google":          {"google"},
	"openai":          {"openai"},
	"x ai":            {"x-ai", "xai"},
	"xai":             {"x-ai", "xai"},
	"meta":            {"meta-llama", "meta"},
	"meta llama":      {"meta-llama"},
	"microsoft":       {"microsoft", "azure"},
	"microsoft azure": {"microsoft", "azure"},
	"moonshot":        {"moonshotai", "moonshot"},
	"moonshot ai":     {"moonshotai", "moonshot"},
	"minimax":         {"minimax"},
	"mini max":        {"minimax"},
	"deepseek":        {"deepseek"},
	"z ai":            {"z-ai"},
	"ibm":             {"ibm"},
	"xiaomi":          {"xiaomi"},
	"mistral":         {"mistralai", "mistral"},
	"mistralai":       {"mistralai", "mistral"},
	"cohere":          {"cohere"},
	"qwen":            {"qwen"},
	"alibaba":         {"qwen", "alibaba"},
	"nvidia":          {"nvidia"},
	"amazon":          {"amazon", "aws"},
	"aws":             {"amazon", "aws"},
}

func addProviderHints(hints map[string]struct{}, raw string) {
	key := normalizeModelKey(raw)
	if key == "" {
		return
	}
	hints[strings.ReplaceAll(key, " ", "-")] = struct{}{}
	for _, alias := range openRouterProviderAliases[key] {
		hints[alias] = struct{}{}
	}
	for _, token := range strings.Fields(key) {
		for _, alias := range openRouterProviderAliases[token] {
			hints[alias] = struct{}{}
		}
	}
}

func hasProviderHint(hints map[string]struct{}, provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || len(hints) == 0 {
		return false
	}
	_, ok := hints[provider]
	return ok
}

func openRouterProviderFromID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func openRouterSuffixFromID(id string) string {
	id = strings.TrimSpace(id)
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return id
	}
	return parts[1]
}

func normalizeModelKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", " ",
		"_", " ",
		"-", " ",
		".", " ",
		":", " ",
		"(", " ",
		")", " ",
		",", " ",
		"+", " plus ",
		"&", " and ",
	)
	s = replacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func tokenizeModelKey(raw string) map[string]struct{} {
	key := normalizeModelKey(raw)
	out := make(map[string]struct{})
	if key == "" {
		return out
	}
	for _, token := range strings.Fields(key) {
		if modelNoiseTokens[token] {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

var modelNoiseTokens = map[string]bool{
	"model":        true,
	"reasoning":    true,
	"adaptive":     true,
	"thinking":     true,
	"preview":      true,
	"experimental": true,
}

func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]struct{}, len(a)+len(b))
	for key := range a {
		union[key] = struct{}{}
		if _, ok := b[key]; ok {
			intersection++
		}
	}
	for key := range b {
		union[key] = struct{}{}
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

func buildBenchmarkModelsRepo(rows []map[string]interface{}, sourceURL, snapshotAt string) benchmarkModelsRepo {
	// V2 returns one consolidated row per model. Deduplicate defensively by ID
	// so a malformed paginated response cannot create duplicate suggestions.
	byModel := make(map[string]benchmarkModelRecord, len(rows))
	for _, row := range rows {
		record, ok := benchmarkModelRecordFromV2Row(row, snapshotAt)
		if !ok || strings.TrimSpace(record.ModelID) == "" {
			continue
		}
		byModel[record.ModelID] = record
	}

	models := make([]benchmarkModelRecord, 0, len(byModel))
	for _, model := range byModel {
		// Skip models with zero cost — these are self-hosted/free-tier entries
		// that produce misleading rankings (zero cost inflates value/composite scores).
		if model.CostToRunIndexTotalUSD != nil && *model.CostToRunIndexTotalUSD == 0 {
			continue
		}
		models = append(models, model)
	}
	sort.SliceStable(models, func(i, j int) bool {
		li := strings.ToLower(models[i].ShortName)
		lj := strings.ToLower(models[j].ShortName)
		if li == lj {
			return strings.ToLower(models[i].Slug) < strings.ToLower(models[j].Slug)
		}
		return li < lj
	})

	return benchmarkModelsRepo{
		SourceURL:    sourceURL,
		SourceRows:   len(rows),
		UniqueModels: len(models),
		SnapshotAt:   snapshotAt,
		Models:       models,
	}
}

func benchmarkModelRecordFromV2Row(row map[string]interface{}, snapshotAt string) (benchmarkModelRecord, bool) {
	if row == nil {
		return benchmarkModelRecord{}, false
	}
	creator := mapFrom(row, "model_creator")
	evaluations := mapFrom(row, "evaluations")
	pricing := mapFrom(row, "pricing")
	performance := mapFrom(row, "performance")
	cost := mapFrom(row, "artificial_analysis_intelligence_index_cost")
	tokenCounts := mapFrom(row, "artificial_analysis_intelligence_index_token_counts")
	if evaluations == nil || pricing == nil || performance == nil {
		return benchmarkModelRecord{}, false
	}

	record := benchmarkModelRecord{
		ModelID:                          stringFrom(row, "id"),
		Slug:                             stringFrom(row, "slug"),
		Name:                             stringFrom(row, "name"),
		ShortName:                        stringFrom(row, "name"),
		CreatorName:                      stringFrom(creator, "name"),
		ReasoningModel:                   boolFrom(row, "reasoning_model"),
		ReasoningModelKnown:              hasKey(row, "reasoning_model"),
		Deprecated:                       boolFrom(row, "deprecated"),
		DeprecatedKnown:                  hasKey(row, "deprecated"),
		ReleaseDate:                      stringFrom(row, "release_date"),
		ContextWindowTokens:              floatPointerFrom(row, "context_window_tokens"),
		IntelligenceIndex:                floatPointerFrom(evaluations, "artificial_analysis_intelligence_index"),
		Price1MInputTokens:               floatPointerFrom(pricing, "price_1m_input_tokens"),
		Price1MOutputTokens:              floatPointerFrom(pricing, "price_1m_output_tokens"),
		Price1MBlended3To1:               floatPointerFrom(pricing, "price_1m_blended_3_to_1"),
		IntelligenceIndexInputTokens:     floatPointerFrom(tokenCounts, "input_tokens"),
		IntelligenceIndexOutputTokens:    floatPointerFrom(tokenCounts, "output_tokens"),
		IntelligenceIndexReasoningTokens: floatPointerFrom(tokenCounts, "reasoning_tokens"),
		IntelligenceIndexAnswerTokens:    floatPointerFrom(tokenCounts, "answer_tokens"),
		E2ETotalTimeSec:                  floatPointerFrom(performance, "median_end_to_end_response_time_seconds"),
		TTFATotalTimeSec:                 floatPointerFrom(performance, "median_time_to_first_answer_token_seconds"),
		OutputSpeedTokSec:                floatPointerFrom(performance, "median_output_tokens_per_second"),
		CostToRunIndexTotalUSD:           floatPointerFrom(cost, "total_cost"),
		CostToRunIndexInputUSD:           floatPointerFrom(cost, "input_cost"),
		OpenRouterModelID:                stringFrom(row, "openrouter_api_id"),
		OpenRouterProvider:               openRouterProviderFromID(stringFrom(row, "openrouter_api_id")),
		Source:                           "artificialanalysis.v2.language_models",
		SnapshotAt:                       snapshotAt,
	}
	record.CostToRunIndexOutputUSD = sumAllFloatPointers(
		floatPointerFrom(cost, "reasoning_cost"),
		floatPointerFrom(cost, "answer_cost"),
	)
	if record.OpenRouterModelID != "" {
		record.OpenRouterMatchMethod = "artificial_analysis"
	}
	if record.ModelID == "" || record.Slug == "" || record.Name == "" {
		return benchmarkModelRecord{}, false
	}
	if record.IntelligenceIndex != nil && record.CostToRunIndexTotalUSD != nil && *record.CostToRunIndexTotalUSD > 0 {
		value := *record.IntelligenceIndex / *record.CostToRunIndexTotalUSD
		record.ValueScore = &value
	}
	return record, true
}

func sumAllFloatPointers(values ...*float64) *float64 {
	total := 0.0
	for _, value := range values {
		if value == nil {
			return nil
		}
		total += *value
	}
	return &total
}

func loadBenchmarkModelsRepo(path string) (*benchmarkModelsRepo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("models repo file not found at %s (run: conctl benchmarks models sync)", path)
		}
		return nil, fmt.Errorf("read models repo file %s: %w", path, err)
	}
	var repo benchmarkModelsRepo
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("parse models repo file %s: %w", path, err)
	}
	return &repo, nil
}

func filterBenchmarkModels(models []benchmarkModelRecord, query string, includeDeprecated bool, reasoning string) ([]benchmarkModelRecord, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	reasoning = strings.ToLower(strings.TrimSpace(reasoning))
	switch reasoning {
	case "", "any":
		reasoning = "any"
	case "true", "false":
	default:
		return nil, fmt.Errorf("invalid --reasoning value %q (expected any|true|false)", reasoning)
	}

	out := make([]benchmarkModelRecord, 0, len(models))
	for _, model := range models {
		if !includeDeprecated && model.Deprecated {
			continue
		}
		if reasoning == "true" && !model.ReasoningModel {
			continue
		}
		if reasoning == "false" && model.ReasoningModel {
			continue
		}
		if q != "" {
			haystack := strings.ToLower(strings.Join([]string{
				model.ShortName,
				model.Name,
				model.Slug,
				model.ModelID,
				model.OpenRouterModelID,
				model.OpenRouterModelName,
			}, " "))
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		out = append(out, model)
	}
	return out, nil
}

// validSortKeys lists the recognized single-key sort tokens.
var validSortKeys = map[string]bool{
	"value":        true,
	"intelligence": true,
	"cost":         true,
	"rank":         true,
	"name":         true,
}

// parseSortKeys splits a comma-separated sort string into validated keys.
// Returns an error if any key is unrecognized or if multi-key constraints are violated.
func parseSortKeys(raw string) ([]string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return []string{"value"}, nil
	}
	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		k := strings.TrimSpace(p)
		if k == "" {
			continue
		}
		if !validSortKeys[k] {
			return nil, fmt.Errorf("unknown sort key %q (valid: value, intelligence, cost, rank, name)", k)
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return []string{"value"}, nil
	}
	// Multi-key composite requires numerical dimensions only.
	if len(keys) > 1 {
		for _, k := range keys {
			if k == "name" {
				return nil, fmt.Errorf("%q cannot be used in multi-key sort (not a numerical dimension)", k)
			}
			if k == "rank" {
				return nil, fmt.Errorf("%q cannot be combined with other keys (already a composite sort)", k)
			}
		}
	}
	return keys, nil
}

// sortKeyComparator returns a three-way comparator for a single sort key.
// Comparators return 0 on tie (no built-in tiebreaker) so multi-key chains
// can fall through to the next key.
func sortKeyComparator(key string) func(a, b benchmarkModelRecord) int {
	switch key {
	case "intelligence":
		return func(a, b benchmarkModelRecord) int {
			return cmpFloatDesc(a.IntelligenceIndex, b.IntelligenceIndex)
		}
	case "cost":
		return func(a, b benchmarkModelRecord) int {
			return cmpFloatAsc(a.CostToRunIndexTotalUSD, b.CostToRunIndexTotalUSD)
		}
	case "name":
		return func(a, b benchmarkModelRecord) int {
			la := strings.ToLower(a.ShortName)
			lb := strings.ToLower(b.ShortName)
			if la != lb {
				if la < lb {
					return -1
				}
				return 1
			}
			sa := strings.ToLower(a.Slug)
			sb := strings.ToLower(b.Slug)
			if sa < sb {
				return -1
			}
			if sa > sb {
				return 1
			}
			return 0
		}
	default: // "value"
		return func(a, b benchmarkModelRecord) int {
			return cmpFloatDesc(a.ValueScore, b.ValueScore)
		}
	}
}

// cmpFloatDesc compares two optional floats descending (higher first). Nil sorts last.
func cmpFloatDesc(a, b *float64) int {
	av := ptrFloatOr(a, math.Inf(-1))
	bv := ptrFloatOr(b, math.Inf(-1))
	if av == bv {
		return 0
	}
	if av > bv {
		return -1
	}
	return 1
}

// cmpFloatAsc compares two optional floats ascending (lower first). Nil sorts last.
func cmpFloatAsc(a, b *float64) int {
	av := ptrFloatOr(a, math.Inf(1))
	bv := ptrFloatOr(b, math.Inf(1))
	if av == bv {
		return 0
	}
	if av < bv {
		return -1
	}
	return 1
}

func sortBenchmarkModels(models []benchmarkModelRecord, sortBy string) {
	keys, err := parseSortKeys(sortBy)
	if err != nil {
		// Fallback to value on parse error (caller already validated).
		keys = []string{"value"}
	}

	// "rank" → equal-weight composite across intelligence and cost.
	for _, k := range keys {
		if k == "rank" {
			sortByCompositeRank(models)
			return
		}
	}

	// Single key: direct comparison sort.
	if len(keys) == 1 {
		cmp := sortKeyComparator(keys[0])
		sort.SliceStable(models, func(i, j int) bool {
			return cmp(models[i], models[j]) < 0
		})
		return
	}

	// Multi-key: weighted percentile composite. First key gets highest weight.
	sortByWeightedComposite(models, keys, nil)
}

// paretoFrontier returns only Pareto-optimal models on the intelligence vs cost plane.
// A model is dominated if another model has >= intelligence AND <= cost (with at least one strict).
// Optional minIntel and maxCost (0 = no cap) pre-filter candidates before the dominance check.
// The result is sorted by cost ascending, intelligence descending (cheapest non-dominated first).
func paretoFrontier(models []benchmarkModelRecord, minIntel, maxCost float64) []benchmarkModelRecord {
	// Pre-filter: require both intelligence and cost data, apply floors/caps.
	cands := make([]benchmarkModelRecord, 0, len(models))
	for _, m := range models {
		if m.IntelligenceIndex == nil || m.CostToRunIndexTotalUSD == nil {
			continue
		}
		if minIntel > 0 && *m.IntelligenceIndex < minIntel {
			continue
		}
		if maxCost > 0 && *m.CostToRunIndexTotalUSD > maxCost {
			continue
		}
		cands = append(cands, m)
	}

	// Sort by cost ascending, intelligence descending (for the sweep).
	sort.SliceStable(cands, func(i, j int) bool {
		ci, cj := *cands[i].CostToRunIndexTotalUSD, *cands[j].CostToRunIndexTotalUSD
		if ci != cj {
			return ci < cj
		}
		return *cands[i].IntelligenceIndex > *cands[j].IntelligenceIndex
	})

	// Sweep: keep only models that improve the best intelligence seen so far.
	// As cost increases, a model is non-dominated only if it offers higher intelligence
	// than everything cheaper.
	out := make([]benchmarkModelRecord, 0, len(cands))
	bestIntel := math.Inf(-1)
	for _, m := range cands {
		if *m.IntelligenceIndex > bestIntel {
			out = append(out, m)
			bestIntel = *m.IntelligenceIndex
		}
	}
	return out
}

func findBenchmarkModel(models []benchmarkModelRecord, ref string) (benchmarkModelRecord, error) {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return benchmarkModelRecord{}, errors.New("model reference is empty")
	}

	for _, model := range models {
		if strings.EqualFold(model.Slug, needle) || strings.EqualFold(model.ModelID, needle) {
			return model, nil
		}
	}

	matches := make([]benchmarkModelRecord, 0, 8)
	for _, model := range models {
		haystack := strings.ToLower(strings.Join([]string{
			model.ShortName,
			model.Name,
			model.Slug,
			model.ModelID,
			model.OpenRouterModelID,
			model.OpenRouterModelName,
		}, " "))
		if strings.Contains(haystack, needle) {
			matches = append(matches, model)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		slugs := make([]string, 0, len(matches))
		for _, m := range matches {
			slugs = append(slugs, m.Slug)
		}
		sort.Strings(slugs)
		return benchmarkModelRecord{}, fmt.Errorf("model reference %q matched multiple models: %s", ref, strings.Join(slugs, ", "))
	}
	return benchmarkModelRecord{}, fmt.Errorf("model %q not found", ref)
}

func applyModelLimit(models []benchmarkModelRecord, limit int) []benchmarkModelRecord {
	if limit <= 0 || limit >= len(models) {
		return models
	}
	return models[:limit]
}

func benchmarkModelsRepoEqualIgnoringSnapshot(left, right benchmarkModelsRepo) bool {
	return reflect.DeepEqual(normalizeBenchmarkModelsRepoForCompare(left), normalizeBenchmarkModelsRepoForCompare(right))
}

func normalizeBenchmarkModelsRepoForCompare(repo benchmarkModelsRepo) benchmarkModelsRepo {
	repo.SnapshotAt = ""
	for i := range repo.Models {
		repo.Models[i].SnapshotAt = ""
	}
	return repo
}

func setBenchmarkModelsRepoSnapshot(repo *benchmarkModelsRepo, snapshotAt string) {
	if repo == nil {
		return
	}
	repo.SnapshotAt = snapshotAt
	for i := range repo.Models {
		repo.Models[i].SnapshotAt = snapshotAt
	}
}

// sortByCompositeRank sorts by equal-weight percentile composite across
// intelligence (higher=better) and cost (lower=better).
func sortByCompositeRank(models []benchmarkModelRecord) {
	sortByWeightedComposite(models, []string{"intelligence", "cost"}, []float64{1, 1})
}

// weightedDimension describes one scoring dimension for weighted composite sorting.
type weightedDimension struct {
	weight        float64
	lowerIsBetter bool
	valueFunc     func(benchmarkModelRecord) float64
	hasFunc       func(benchmarkModelRecord) bool
}

// dimensionForKey returns the dimension definition for a sort key.
func dimensionForKey(key string) (weightedDimension, bool) {
	switch key {
	case "intelligence":
		return weightedDimension{
			lowerIsBetter: false,
			valueFunc:     func(m benchmarkModelRecord) float64 { return ptrFloatOr(m.IntelligenceIndex, 0) },
			hasFunc:       func(m benchmarkModelRecord) bool { return m.IntelligenceIndex != nil },
		}, true
	case "cost":
		return weightedDimension{
			lowerIsBetter: true,
			valueFunc:     func(m benchmarkModelRecord) float64 { return ptrFloatOr(m.CostToRunIndexTotalUSD, math.Inf(1)) },
			hasFunc:       func(m benchmarkModelRecord) bool { return m.CostToRunIndexTotalUSD != nil },
		}, true
	case "value":
		return weightedDimension{
			lowerIsBetter: false,
			valueFunc:     func(m benchmarkModelRecord) float64 { return ptrFloatOr(m.ValueScore, 0) },
			hasFunc:       func(m benchmarkModelRecord) bool { return m.ValueScore != nil },
		}, true
	default:
		return weightedDimension{}, false
	}
}

// sortByWeightedComposite sorts models using raw min-max normalized geometric mean scoring.
// Keys earlier in the list receive higher weight using geometric decay:
// base^(N-1), base^(N-2), ..., base^0.
// If weights is non-nil, explicit weights are used instead.
func sortByWeightedComposite(models []benchmarkModelRecord, keys []string, weights []float64) {
	n := len(models)
	if n == 0 || len(keys) == 0 {
		return
	}

	nKeys := len(keys)
	dims := make([]weightedDimension, 0, nKeys)
	for i, key := range keys {
		d, ok := dimensionForKey(key)
		if !ok {
			continue
		}
		if weights != nil && i < len(weights) {
			d.weight = weights[i]
		} else {
			power := float64(nKeys - i - 1)
			d.weight = math.Pow(compositePositionalWeightBase, power)
		}
		dims = append(dims, d)
	}
	if len(dims) == 0 {
		return
	}

	// Min-max normalize each dimension to [0, 1] where 1 = best.
	dimNorm := make([]map[int]float64, len(dims))
	for di, d := range dims {
		dimNorm[di] = minMaxNormalize(models, d.valueFunc, d.hasFunc, d.lowerIsBetter)
	}

	// Compute weighted geometric mean of normalized values per model.
	// Geometric mean = (N1^w1 × N2^w2 × ... × Nk^wk)^(1/Σw)
	// This naturally penalizes models extreme in one dimension — both
	// dimensions must be strong to score well (the "green quadrant" effect).
	// Models missing any requested dimension score 0 (pushed to bottom).
	expectedWeight := 0.0
	for _, d := range dims {
		expectedWeight += d.weight
	}
	scores := make([]float64, n)
	for i := range models {
		totalWeight := 0.0
		logSum := 0.0
		for di, d := range dims {
			if v, ok := dimNorm[di][i]; ok {
				v = math.Max(v, compositeScoreFloor)
				logSum += d.weight * math.Log(v)
				totalWeight += d.weight
			}
		}
		if totalWeight == expectedWeight {
			scores[i] = math.Exp(logSum / totalWeight)
		}
		// else: missing dimension → score stays 0
	}

	// Sort via index array — scores are keyed by original position, so we
	// cannot sort models in-place (SliceStable would misalign scores after swaps).
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		si, sj := scores[order[i]], scores[order[j]]
		if si != sj {
			return si > sj
		}
		return strings.ToLower(models[order[i]].Slug) < strings.ToLower(models[order[j]].Slug)
	})
	reordered := make([]benchmarkModelRecord, n)
	for i, idx := range order {
		reordered[i] = models[idx]
	}
	copy(models, reordered)
}

// minMaxNormalize returns normalized values in [0, 1] for each model index.
// 1 = best, 0 = worst. For lowerIsBetter dimensions the scale is inverted.
func minMaxNormalize(models []benchmarkModelRecord, valueFunc func(benchmarkModelRecord) float64, hasFunc func(benchmarkModelRecord) bool, lowerIsBetter bool) map[int]float64 {
	// Collect values for models that have data.
	type entry struct {
		idx int
		val float64
	}
	entries := make([]entry, 0, len(models))
	for i, m := range models {
		if hasFunc(m) {
			entries = append(entries, entry{i, valueFunc(m)})
		}
	}
	if len(entries) == 0 {
		return nil
	}

	minVal, maxVal := entries[0].val, entries[0].val
	for _, e := range entries[1:] {
		if e.val < minVal {
			minVal = e.val
		}
		if e.val > maxVal {
			maxVal = e.val
		}
	}

	norm := make(map[int]float64, len(entries))
	span := maxVal - minVal
	for _, e := range entries {
		if span == 0 {
			norm[e.idx] = 1.0
		} else if lowerIsBetter {
			norm[e.idx] = (maxVal - e.val) / span
		} else {
			norm[e.idx] = (e.val - minVal) / span
		}
	}
	return norm
}

func benchmarkModelsSyncTable(w io.Writer, data interface{}, format string) {
	out, ok := data.(*benchmarkModelsSyncOutput)
	if !ok || out == nil {
		return
	}
	headers := []string{"SOURCE_ROWS", "UNIQUE_MODELS", "OR_MODELS", "OR_MAPPED", "OR_CATALOG", "SNAPSHOT", "REPO_FILE", "RAW_FILE"}
	rows := [][]string{{
		strconv.Itoa(out.SourceRows),
		strconv.Itoa(out.UniqueModels),
		strconv.Itoa(out.OpenRouterModels),
		strconv.Itoa(out.OpenRouterMapped),
		out.OpenRouterCatalog,
		out.SnapshotAt,
		out.RepoOut,
		out.RawOut,
	}}
	fmt.Fprintf(w, "Source: %s\n\n", out.SourceURL)
	printTableAuto(w, headers, rows, format)
}

func benchmarkModelsListTable(w io.Writer, data interface{}, format string) {
	out, ok := data.(*benchmarkModelsListOutput)
	if !ok || out == nil {
		return
	}
	fmt.Fprintf(w, "Snapshot: %s\n", out.SnapshotAt)
	fmt.Fprintf(w, "Sort: %s\n\n", out.Sort)
	headers := []string{"MODEL", "SLUG", "INTEL", "COST_TO_RUN_USD", "VALUE", "SPEED", "OPENROUTER", "REASONING", "DEPRECATED"}
	rows := make([][]string, 0, len(out.Models))
	for _, model := range out.Models {
		rows = append(rows, []string{
			model.ShortName,
			model.Slug,
			formatFloat(model.IntelligenceIndex, 2),
			formatFloat(model.CostToRunIndexTotalUSD, 4),
			formatFloat(model.ValueScore, 4),
			formatSpeed(model.E2ETotalTimeSec),
			truncate(model.OpenRouterModelID, 34),
			formatBool(model.ReasoningModel),
			formatBool(model.Deprecated),
		})
	}
	printTableAuto(w, headers, rows, format)
}

func benchmarkModelsShowTable(w io.Writer, data interface{}, format string) {
	out, ok := data.(*benchmarkModelsShowOutput)
	if !ok || out == nil {
		return
	}
	model := out.Model
	fmt.Fprintf(w, "Snapshot: %s\n\n", out.SnapshotAt)

	headers := []string{"FIELD", "VALUE"}
	rows := [][]string{
		{"short_name", model.ShortName},
		{"slug", model.Slug},
		{"model_id", model.ModelID},
		{"creator_name", model.CreatorName},
		{"reasoning_model", formatBool(model.ReasoningModel)},
		{"deprecated", formatBool(model.Deprecated)},
		{"release_date", model.ReleaseDate},
		{"intelligence_index", formatFloat(model.IntelligenceIndex, 2)},
		{"cost_to_run_index_total_usd", formatFloat(model.CostToRunIndexTotalUSD, 4)},
		{"value_score", formatFloat(model.ValueScore, 4)},
		{"context_window_tokens", formatFloat(model.ContextWindowTokens, 0)},
		{"price_1m_input_tokens", formatFloat(model.Price1MInputTokens, 4)},
		{"price_1m_output_tokens", formatFloat(model.Price1MOutputTokens, 4)},
		{"price_1m_blended_3_to_1", formatFloat(model.Price1MBlended3To1, 4)},
		{"intelligence_index_input_tokens", formatFloat(model.IntelligenceIndexInputTokens, 0)},
		{"intelligence_index_output_tokens", formatFloat(model.IntelligenceIndexOutputTokens, 0)},
		{"intelligence_index_reasoning_tokens", formatFloat(model.IntelligenceIndexReasoningTokens, 0)},
		{"intelligence_index_answer_tokens", formatFloat(model.IntelligenceIndexAnswerTokens, 0)},
		{"cost_to_run_index_input_usd", formatFloat(model.CostToRunIndexInputUSD, 4)},
		{"cost_to_run_index_output_usd", formatFloat(model.CostToRunIndexOutputUSD, 4)},
		{"e2e_total_time_sec", formatFloat(model.E2ETotalTimeSec, 2)},
		{"e2e_p95_time_sec", formatFloat(model.E2EP95TimeSec, 2)},
		{"ttfa_total_time_sec", formatFloat(model.TTFATotalTimeSec, 2)},
		{"output_speed_tok_sec", formatFloat(model.OutputSpeedTokSec, 2)},
		{"model_url", model.ModelURL},
		{"hosts_url", model.HostsURL},
		{"source_host_name", model.SourceHostName},
		{"source_host_model_slug", model.SourceHostModelSlug},
		{"openrouter_model_id", model.OpenRouterModelID},
		{"openrouter_model_name", model.OpenRouterModelName},
		{"openrouter_provider", model.OpenRouterProvider},
		{"openrouter_match_method", model.OpenRouterMatchMethod},
		{"source", model.Source},
	}
	printTableAuto(w, headers, rows, format)
}

func benchmarkModelsSuggestTable(w io.Writer, data interface{}, format string) {
	out, ok := data.(*benchmarkModelsSuggestOutput)
	if !ok || out == nil {
		return
	}
	fmt.Fprintf(w, "Snapshot: %s\n", out.SnapshotAt)
	fmt.Fprintf(w, "Top: %d\n\n", out.Top)

	base := fmt.Sprintf("conctl benchmarks models list --limit %d", out.Top)
	frontierFlags := fmt.Sprintf(" --min-intel %.0f --max-cost %.0f", defaultMinIntel, defaultMaxCost)

	writeSuggestionSection(w, "Best Cheap Models (cost, intelligence)", base+" --sort cost,intelligence"+frontierFlags, out.BestCheap, format)
	fmt.Fprintln(w)
	writeSuggestionSection(w, "Best Expensive Models (intelligence, cost)", base+" --sort intelligence,cost"+frontierFlags, out.BestExpensive, format)
	fmt.Fprintln(w)
	writeSuggestionSection(w, "Most Intelligent Models", base+" --sort intelligence --no-frontier", out.MostIntelligent, format)
}

func writeSuggestionSection(w io.Writer, title string, cmd string, models []benchmarkModelRecord, format string) {
	fmt.Fprintf(w, "%s\nie. `%s`\n", title, cmd)
	headers := []string{"MODEL", "SLUG", "INTEL", "COST_TO_RUN_USD", "VALUE", "SPEED", "OPENROUTER"}
	rows := make([][]string, 0, len(models))
	for _, model := range models {
		rows = append(rows, []string{
			model.ShortName,
			model.Slug,
			formatFloat(model.IntelligenceIndex, 2),
			formatFloat(model.CostToRunIndexTotalUSD, 4),
			formatFloat(model.ValueScore, 4),
			formatSpeed(model.E2ETotalTimeSec),
			truncate(model.OpenRouterModelID, 34),
		})
	}
	printTableAuto(w, headers, rows, format)
}

func writeJSONFile(path string, value interface{}) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return os.WriteFile(path, encoded, 0o644)
}

func mapFrom(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	out, _ := v.(map[string]interface{})
	return out
}

func hasKey(m map[string]interface{}, key string) bool {
	_, ok := m[key]
	return ok
}

func stringFrom(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", s)
	}
}

func boolFrom(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(b))
		return err == nil && parsed
	default:
		return false
	}
}

func integerFrom(m map[string]interface{}, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	value, ok := m[key]
	if !ok || value == nil {
		return 0, false
	}
	var number float64
	switch n := value.(type) {
	case float64:
		number = n
	case float32:
		number = float64(n)
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		if n > int64(math.MaxInt) || n < int64(math.MinInt) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil || parsed > int64(math.MaxInt) || parsed < int64(math.MinInt) {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
	if math.Trunc(number) != number || number > float64(math.MaxInt) || number < float64(math.MinInt) {
		return 0, false
	}
	return int(number), true
}

func floatPointerFrom(m map[string]interface{}, key string) *float64 {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case float32:
		f := float64(n)
		return &f
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	case int32:
		f := float64(n)
		return &f
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

func ptrFloatOr(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func formatFloat(value *float64, decimals int) string {
	if value == nil {
		return "-"
	}
	format := "%." + strconv.Itoa(decimals) + "f"
	return fmt.Sprintf(format, *value)
}

func formatSpeed(seconds *float64) string {
	if seconds == nil {
		return "-"
	}
	return fmt.Sprintf("%.1fs", *seconds)
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
