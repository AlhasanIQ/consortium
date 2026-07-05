package optimize

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPPopulationStore struct {
	Client *HTTPClient
}

func NewHTTPPopulationStore(baseURL string, timeout time.Duration) *HTTPPopulationStore {
	return &HTTPPopulationStore{Client: NewHTTPClient(baseURL, timeout)}
}

func (s *HTTPPopulationStore) CreateRun(ctx context.Context, run *OptimizationRun) error {
	var response map[string]interface{}
	return s.Client.PostJSON(ctx, "/api/admin/optimize/runs", run, &response)
}

func (s *HTTPPopulationStore) UpdateRunProgress(ctx context.Context, runID string, generation int, bestOrgID string, bestFitness *Fitness, spentUSD float64, totalOrganisms int, dspyMetricCallsUsed int) error {
	body := map[string]interface{}{
		"generation":             generation,
		"best_organism_id":       bestOrgID,
		"best_fitness":           bestFitness,
		"spent_usd":              spentUSD,
		"total_organisms":        totalOrganisms,
		"dspy_metric_calls_used": dspyMetricCallsUsed,
	}
	var response map[string]interface{}
	return s.Client.PatchJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/progress", body, &response)
}

func (s *HTTPPopulationStore) UpdateRunStatus(ctx context.Context, runID string, status string) error {
	path := "/api/admin/optimize/runs/" + url.PathEscape(runID)
	switch status {
	case "paused":
		path += "/pause"
	case "running":
		path += "/resume"
	case "cancelled":
		path += "/cancel"
	default:
		path += "/status"
	}
	var response map[string]interface{}
	if strings.HasSuffix(path, "/status") {
		body := map[string]interface{}{"status": status}
		return s.Client.PatchJSON(ctx, path, body, &response)
	}
	return s.Client.PostJSON(ctx, path, map[string]interface{}{}, &response)
}

func (s *HTTPPopulationStore) UpdateRunLease(ctx context.Context, runID string, ownerPID int, ownerHostname string) error {
	body := map[string]interface{}{"owner_pid": ownerPID, "owner_hostname": ownerHostname}
	var response map[string]interface{}
	return s.Client.PatchJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/heartbeat", body, &response)
}

func (s *HTTPPopulationStore) ClearRunLease(ctx context.Context, runID string) error {
	body := map[string]interface{}{"clear": true}
	var response map[string]interface{}
	return s.Client.PatchJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/heartbeat", body, &response)
}

func (s *HTTPPopulationStore) GetRun(ctx context.Context, runID string) (*OptimizationRun, error) {
	var run OptimizationRun
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID), nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *HTTPPopulationStore) ListRuns(ctx context.Context, status string, workflowID string, limit int) ([]OptimizationRun, error) {
	query := url.Values{}
	if strings.TrimSpace(status) != "" {
		query.Set("status", status)
	}
	if strings.TrimSpace(workflowID) != "" {
		query.Set("workflow", workflowID)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response struct {
		Runs []OptimizationRun `json:"runs"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/runs", query, &response); err != nil {
		return nil, err
	}
	return response.Runs, nil
}

func (s *HTTPPopulationStore) CreateOrganism(ctx context.Context, organism *Organism) error {
	var response map[string]interface{}
	return s.Client.PostJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(organism.OptRunID)+"/organisms", organism, &response)
}

func (s *HTTPPopulationStore) UpdateOrganismFitness(ctx context.Context, orgID, benchRunID string, fitness *Fitness) error {
	body := map[string]interface{}{"bench_run_id": benchRunID, "fitness": fitness}
	var response map[string]interface{}
	return s.Client.PatchJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(orgID)+"/fitness", body, &response)
}

func (s *HTTPPopulationStore) GetOrganism(ctx context.Context, orgID string) (*Organism, error) {
	var response struct {
		Organism Organism `json:"organism"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(orgID), nil, &response); err != nil {
		return nil, err
	}
	return &response.Organism, nil
}

func (s *HTTPPopulationStore) GetOrganismsByGeneration(ctx context.Context, runID string, generation int, limit int) ([]*Organism, error) {
	query := url.Values{}
	if generation >= 0 {
		query.Set("generation", strconv.Itoa(generation))
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response struct {
		Organisms []*Organism `json:"organisms"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/organisms", query, &response); err != nil {
		return nil, err
	}
	return response.Organisms, nil
}

func (s *HTTPPopulationStore) GetAllOrganisms(ctx context.Context, runID string) ([]*Organism, error) {
	return s.GetOrganismsByGeneration(ctx, runID, -1, 10000)
}

func (s *HTTPPopulationStore) GetBestOrganisms(ctx context.Context, runID string, limit int) ([]*Organism, error) {
	query := url.Values{}
	query.Set("best", "1")
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response struct {
		Organisms []*Organism `json:"organisms"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/organisms", query, &response); err != nil {
		return nil, err
	}
	return response.Organisms, nil
}

func (s *HTTPPopulationStore) GetOrganismLineage(ctx context.Context, orgID string) ([]*Organism, error) {
	var response struct {
		Organisms []*Organism `json:"organisms"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(orgID)+"/lineage", nil, &response); err != nil {
		return nil, err
	}
	return response.Organisms, nil
}

func (s *HTTPPopulationStore) AppendLearningEntry(ctx context.Context, runID string, entry *LearningEntry) error {
	var response map[string]interface{}
	return s.Client.PostJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/learning-log", entry, &response)
}

func (s *HTTPPopulationStore) GetLearningLog(ctx context.Context, runID string, limit int) ([]LearningEntry, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response struct {
		Entries []LearningEntry `json:"entries"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/runs/"+url.PathEscape(runID)+"/learning-log", query, &response); err != nil {
		return nil, err
	}
	return response.Entries, nil
}

func (s *HTTPPopulationStore) CreateParamChanges(ctx context.Context, organismID string, changes []ParamChange) error {
	body := map[string]interface{}{"changes": changes}
	var response map[string]interface{}
	return s.Client.PostJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(organismID)+"/param-changes", body, &response)
}

func (s *HTTPPopulationStore) GetParamChanges(ctx context.Context, organismID string) ([]ParamChange, error) {
	var response struct {
		Changes []ParamChange `json:"changes"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(organismID)+"/param-changes", nil, &response); err != nil {
		return nil, err
	}
	return response.Changes, nil
}

func (s *HTTPPopulationStore) CreateMutationArtifacts(ctx context.Context, organismID string, artifacts []MutationArtifact, compact bool) error {
	body := map[string]interface{}{
		"artifacts": artifacts,
		"compact":   compact,
	}
	var response map[string]interface{}
	return s.Client.PostJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(organismID)+"/mutation-artifacts", body, &response)
}

func (s *HTTPPopulationStore) GetMutationArtifacts(ctx context.Context, organismID string) ([]MutationArtifact, error) {
	var response struct {
		Artifacts []MutationArtifact `json:"artifacts"`
	}
	if err := s.Client.GetJSON(ctx, "/api/admin/optimize/organisms/"+url.PathEscape(organismID)+"/mutation-artifacts", nil, &response); err != nil {
		return nil, err
	}
	return response.Artifacts, nil
}

// Ensure interface implementation stays in sync.
var _ PopulationStore = (*HTTPPopulationStore)(nil)
