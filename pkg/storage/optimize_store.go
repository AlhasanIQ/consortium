package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
)

func (s *Storage) CreateOptimizationRun(run *optimize.OptimizationRun) error {
	if run == nil {
		return fmt.Errorf("optimization run is nil")
	}
	specJSON, err := json.Marshal(run.Spec)
	if err != nil {
		return fmt.Errorf("marshal optimize spec: %w", err)
	}
	bestFitnessJSON := ""
	if run.BestFitness != nil {
		encoded, err := json.Marshal(run.BestFitness)
		if err != nil {
			return fmt.Errorf("marshal best fitness: %w", err)
		}
		bestFitnessJSON = string(encoded)
	}
	learningJSON, err := json.Marshal(run.LearningLog)
	if err != nil {
		return fmt.Errorf("marshal learning log: %w", err)
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	if run.WorkflowVersion < 1 {
		run.WorkflowVersion = 1
	}

	_, err = s.db.Exec(`
			INSERT INTO optimization_runs (
				id, workflow_id, workflow_version, benchmark, split, item_limit, concurrency,
				spec, strategy, population_size, children_per_parent, max_children_per_generation, adaptive_fanout,
				claude_model, mutator_mode, rng_seed, compact_artifacts,
				total_budget_usd, spent_usd, dspy_metric_calls_used,
				status, generation, total_organisms,
				best_organism_id, best_fitness, learning_log,
				owner_pid, owner_hostname, last_heartbeat_at,
				started_at, completed_at, created_at, updated_at
			) VALUES (
				?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?,
				?, ?, ?,
				?, ?, ?,
				?, ?, ?,
				?, ?, ?, ?
			)
	`,
		run.ID,
		run.WorkflowID,
		run.WorkflowVersion,
		run.Benchmark,
		run.Split,
		run.ItemLimit,
		run.Concurrency,
		string(specJSON),
		defaultStringValue(run.Strategy, "evolutionary"),
		run.PopulationSize,
		max(run.ChildrenPerParent, 1),
		max(run.MaxChildrenPerGeneration, 0),
		boolToInt(run.AdaptiveFanout),
		run.ClaudeModel,
		defaultStringValue(run.MutatorMode, "auto"),
		nullableInt64(run.RNGSeed),
		boolToInt(run.CompactArtifacts),
		run.TotalBudgetUSD,
		run.SpentUSD,
		run.DSPYMetricCallsUsed,
		defaultStringValue(run.Status, "pending"),
		run.Generation,
		run.TotalOrganisms,
		nullableString(run.BestOrganismID),
		nullableString(bestFitnessJSON),
		string(learningJSON),
		nullableInt(run.OwnerPID),
		nullableString(run.OwnerHostname),
		sqlTimePtrValue(run.LastHeartbeatAt),
		sqlTimePtrValue(run.StartedAt),
		sqlTimePtrValue(run.CompletedAt),
		run.CreatedAt,
		run.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create optimization run: %w", err)
	}
	return nil
}

func (s *Storage) UpdateOptimizationRunProgress(runID string, generation int, bestOrgID string, bestFitness *optimize.Fitness, spentUSD float64, totalOrganisms int, dspyMetricCallsUsed int) error {
	var bestFitnessJSON interface{}
	if bestFitness != nil {
		encoded, err := json.Marshal(bestFitness)
		if err != nil {
			return fmt.Errorf("marshal best fitness: %w", err)
		}
		bestFitnessJSON = string(encoded)
	}
	_, err := s.db.Exec(`
			UPDATE optimization_runs
			SET generation = ?,
			    best_organism_id = ?,
			    best_fitness = ?,
			    spent_usd = ?,
			    dspy_metric_calls_used = ?,
			    total_organisms = ?,
			    updated_at = ?
			WHERE id = ?
		`, generation, nullableString(bestOrgID), bestFitnessJSON, spentUSD, dspyMetricCallsUsed, totalOrganisms, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("update optimization run progress: %w", err)
	}
	return nil
}

func (s *Storage) UpdateOptimizationRunStatus(runID string, status string) error {
	now := time.Now().UTC()
	var (
		err   error
		query string
		args  []interface{}
	)
	switch status {
	case "running":
		query = `
			UPDATE optimization_runs
			SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
			WHERE id = ?
		`
		args = []interface{}{status, now, now, runID}
	case "completed", "failed", "cancelled":
		query = `
			UPDATE optimization_runs
			SET status = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE id = ?
		`
		args = []interface{}{status, now, now, runID}
	default:
		query = `
			UPDATE optimization_runs
			SET status = ?, updated_at = ?
			WHERE id = ?
		`
		args = []interface{}{status, now, runID}
	}
	_, err = s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update optimization run status: %w", err)
	}
	return nil
}

func (s *Storage) UpdateOptimizationRunLease(runID string, ownerPID int, ownerHostname string) error {
	_, err := s.db.Exec(`
		UPDATE optimization_runs
		SET owner_pid = ?, owner_hostname = ?, last_heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, ownerPID, ownerHostname, time.Now().UTC(), time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("update optimization run lease: %w", err)
	}
	return nil
}

func (s *Storage) ClearOptimizationRunLease(runID string) error {
	_, err := s.db.Exec(`
		UPDATE optimization_runs
		SET owner_pid = NULL, owner_hostname = NULL, last_heartbeat_at = NULL, updated_at = ?
		WHERE id = ?
	`, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("clear optimization run lease: %w", err)
	}
	return nil
}

func (s *Storage) GetOptimizationRun(runID string) (*optimize.OptimizationRun, error) {
	row := s.db.QueryRow(`
			SELECT id, workflow_id, workflow_version, benchmark, split, item_limit, concurrency,
			       spec, strategy, population_size, children_per_parent, max_children_per_generation, adaptive_fanout,
			       claude_model, mutator_mode, rng_seed, compact_artifacts,
			       total_budget_usd, spent_usd, dspy_metric_calls_used,
			       status, generation, total_organisms,
			       COALESCE(best_organism_id, ''), COALESCE(best_fitness, ''), COALESCE(learning_log, '[]'),
			       COALESCE(owner_pid, 0), COALESCE(owner_hostname, ''), last_heartbeat_at,
		       started_at, completed_at, created_at, updated_at
		FROM optimization_runs
		WHERE id = ?
	`, runID)

	var (
		run              optimize.OptimizationRun
		specJSON         string
		bestFitness      string
		learningJSON     string
		adaptiveFanout   int
		compactArtifacts int
		rngSeed          sql.NullInt64
		ownerPID         int
		ownerHostname    string
		heartbeatAt      sql.NullTime
		startedAt        sql.NullTime
		completedAt      sql.NullTime
	)
	if err := row.Scan(
		&run.ID,
		&run.WorkflowID,
		&run.WorkflowVersion,
		&run.Benchmark,
		&run.Split,
		&run.ItemLimit,
		&run.Concurrency,
		&specJSON,
		&run.Strategy,
		&run.PopulationSize,
		&run.ChildrenPerParent,
		&run.MaxChildrenPerGeneration,
		&adaptiveFanout,
		&run.ClaudeModel,
		&run.MutatorMode,
		&rngSeed,
		&compactArtifacts,
		&run.TotalBudgetUSD,
		&run.SpentUSD,
		&run.DSPYMetricCallsUsed,
		&run.Status,
		&run.Generation,
		&run.TotalOrganisms,
		&run.BestOrganismID,
		&bestFitness,
		&learningJSON,
		&ownerPID,
		&ownerHostname,
		&heartbeatAt,
		&startedAt,
		&completedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get optimization run: %w", err)
	}

	run.Spec = &optimize.OptimizeSpec{}
	if err := json.Unmarshal([]byte(specJSON), run.Spec); err != nil {
		return nil, fmt.Errorf("decode run spec: %w", err)
	}
	if strings.TrimSpace(bestFitness) != "" {
		parsed := &optimize.Fitness{}
		if err := json.Unmarshal([]byte(bestFitness), parsed); err != nil {
			return nil, fmt.Errorf("decode best fitness: %w", err)
		}
		run.BestFitness = parsed
	}
	if strings.TrimSpace(learningJSON) != "" {
		if err := json.Unmarshal([]byte(learningJSON), &run.LearningLog); err != nil {
			return nil, fmt.Errorf("decode learning log: %w", err)
		}
	}
	if ownerPID > 0 {
		run.OwnerPID = ownerPID
	}
	run.OwnerHostname = ownerHostname
	run.AdaptiveFanout = adaptiveFanout != 0
	run.CompactArtifacts = compactArtifacts != 0
	if rngSeed.Valid {
		value := rngSeed.Int64
		run.RNGSeed = &value
	}
	if heartbeatAt.Valid {
		t := heartbeatAt.Time.UTC()
		run.LastHeartbeatAt = &t
	}
	if startedAt.Valid {
		t := startedAt.Time.UTC()
		run.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time.UTC()
		run.CompletedAt = &t
	}
	return &run, nil
}

func (s *Storage) ListOptimizationRuns(status string, workflowID string, limit int) ([]optimize.OptimizationRun, error) {
	if limit < 1 {
		limit = 20
	}
	query := `
			SELECT id, workflow_id, workflow_version, benchmark, split, item_limit, concurrency,
			       spec, strategy, population_size, children_per_parent, max_children_per_generation, adaptive_fanout,
			       claude_model, mutator_mode, rng_seed, compact_artifacts,
			       total_budget_usd, spent_usd, dspy_metric_calls_used,
			       status, generation, total_organisms,
			       COALESCE(best_organism_id, ''), COALESCE(best_fitness, ''), COALESCE(learning_log, '[]'),
			       COALESCE(owner_pid, 0), COALESCE(owner_hostname, ''), last_heartbeat_at,
		       started_at, completed_at, created_at, updated_at
		FROM optimization_runs
		WHERE 1=1`
	args := make([]interface{}, 0)
	if strings.TrimSpace(status) != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if strings.TrimSpace(workflowID) != "" {
		query += ` AND workflow_id = ?`
		args = append(args, workflowID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list optimization runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	runs := make([]optimize.OptimizationRun, 0)
	for rows.Next() {
		var (
			run              optimize.OptimizationRun
			specJSON         string
			bestFitness      string
			learningJSON     string
			adaptiveFanout   int
			compactArtifacts int
			rngSeed          sql.NullInt64
			ownerPID         int
			ownerHostname    string
			heartbeatAt      sql.NullTime
			startedAt        sql.NullTime
			completedAt      sql.NullTime
		)
		if err := rows.Scan(
			&run.ID,
			&run.WorkflowID,
			&run.WorkflowVersion,
			&run.Benchmark,
			&run.Split,
			&run.ItemLimit,
			&run.Concurrency,
			&specJSON,
			&run.Strategy,
			&run.PopulationSize,
			&run.ChildrenPerParent,
			&run.MaxChildrenPerGeneration,
			&adaptiveFanout,
			&run.ClaudeModel,
			&run.MutatorMode,
			&rngSeed,
			&compactArtifacts,
			&run.TotalBudgetUSD,
			&run.SpentUSD,
			&run.DSPYMetricCallsUsed,
			&run.Status,
			&run.Generation,
			&run.TotalOrganisms,
			&run.BestOrganismID,
			&bestFitness,
			&learningJSON,
			&ownerPID,
			&ownerHostname,
			&heartbeatAt,
			&startedAt,
			&completedAt,
			&run.CreatedAt,
			&run.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan optimization run row: %w", err)
		}
		run.Spec = &optimize.OptimizeSpec{}
		if err := json.Unmarshal([]byte(specJSON), run.Spec); err != nil {
			return nil, fmt.Errorf("decode run spec: %w", err)
		}
		if strings.TrimSpace(bestFitness) != "" {
			parsed := &optimize.Fitness{}
			if err := json.Unmarshal([]byte(bestFitness), parsed); err != nil {
				return nil, fmt.Errorf("decode best fitness: %w", err)
			}
			run.BestFitness = parsed
		}
		if strings.TrimSpace(learningJSON) != "" {
			if err := json.Unmarshal([]byte(learningJSON), &run.LearningLog); err != nil {
				return nil, fmt.Errorf("decode learning log: %w", err)
			}
		}
		run.AdaptiveFanout = adaptiveFanout != 0
		run.CompactArtifacts = compactArtifacts != 0
		if rngSeed.Valid {
			value := rngSeed.Int64
			run.RNGSeed = &value
		}
		if ownerPID > 0 {
			run.OwnerPID = ownerPID
		}
		run.OwnerHostname = ownerHostname
		if heartbeatAt.Valid {
			t := heartbeatAt.Time.UTC()
			run.LastHeartbeatAt = &t
		}
		if startedAt.Valid {
			t := startedAt.Time.UTC()
			run.StartedAt = &t
		}
		if completedAt.Valid {
			t := completedAt.Time.UTC()
			run.CompletedAt = &t
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate optimization runs: %w", err)
	}
	return runs, nil
}

func (s *Storage) CreateOptimizationOrganism(org *optimize.Organism) error {
	if org == nil {
		return fmt.Errorf("organism is nil")
	}
	paramValuesJSON, err := json.Marshal(org.ParamValues)
	if err != nil {
		return fmt.Errorf("marshal organism param values: %w", err)
	}
	parentIDsJSON, err := json.Marshal(org.ParentIDs)
	if err != nil {
		return fmt.Errorf("marshal organism parent ids: %w", err)
	}
	if org.CreatedAt.IsZero() {
		org.CreatedAt = time.Now().UTC()
	}

	_, err = s.db.Exec(`
		INSERT INTO optimization_organisms (
			id, opt_run_id, generation, parent_ids,
			param_values, workflow_json,
			mutation_type, mutation_log,
			bench_run_id,
			accuracy, adjusted_accuracy, parse_rate, cost_per_item,
			avg_latency_ms, p95_latency_ms,
			failed_items, total_items, flagged_items,
			composite_score, feasible, constraint_violations,
			evaluated_at, created_at
		) VALUES (
			?, ?, ?, ?,
			?, ?,
			?, ?,
			?,
			?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?
		)
	`,
		org.ID,
		org.OptRunID,
		org.Generation,
		string(parentIDsJSON),
		string(paramValuesJSON),
		string(org.WorkflowJSON),
		org.MutationType,
		org.MutationLog,
		nullableString(org.BenchRunID),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.Accuracy }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.AdjustedAccuracy }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.ParseRate }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.CostPerItem }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.AvgLatencyMS }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.P95LatencyMS }),
		nullableFitnessInt(org.Fitness, func(f *optimize.Fitness) int { return f.FailedItems }),
		nullableFitnessInt(org.Fitness, func(f *optimize.Fitness) int { return f.TotalItems }),
		nullableFitnessInt(org.Fitness, func(f *optimize.Fitness) int { return f.FlaggedItems }),
		nullableFitnessFloat(org.Fitness, func(f *optimize.Fitness) float64 { return f.CompositeScore }),
		nullableFitnessBool(org.Fitness, func(f *optimize.Fitness) bool { return f.Feasible }),
		nullableFitnessViolations(org.Fitness),
		sqlTimePtrValue(org.EvaluatedAt),
		org.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create optimization organism: %w", err)
	}
	return nil
}

func (s *Storage) UpdateOptimizationOrganismFitness(orgID string, benchRunID string, fitness *optimize.Fitness) error {
	if fitness == nil {
		return fmt.Errorf("fitness is nil")
	}
	violationsJSON, err := json.Marshal(fitness.ConstraintViolations)
	if err != nil {
		return fmt.Errorf("marshal constraint violations: %w", err)
	}
	_, err = s.db.Exec(`
		UPDATE optimization_organisms
		SET bench_run_id = ?,
		    accuracy = ?, adjusted_accuracy = ?, parse_rate = ?, cost_per_item = ?,
		    avg_latency_ms = ?, p95_latency_ms = ?,
		    failed_items = ?, total_items = ?, flagged_items = ?,
		    composite_score = ?, feasible = ?, constraint_violations = ?,
		    evaluated_at = ?
		WHERE id = ?
	`, benchRunID,
		fitness.Accuracy,
		fitness.AdjustedAccuracy,
		fitness.ParseRate,
		fitness.CostPerItem,
		fitness.AvgLatencyMS,
		fitness.P95LatencyMS,
		fitness.FailedItems,
		fitness.TotalItems,
		fitness.FlaggedItems,
		fitness.CompositeScore,
		boolToIntOpt(fitness.Feasible),
		string(violationsJSON),
		time.Now().UTC(),
		orgID,
	)
	if err != nil {
		return fmt.Errorf("update optimization organism fitness: %w", err)
	}
	return nil
}

func (s *Storage) GetOptimizationOrganism(orgID string) (*optimize.Organism, error) {
	rows, err := s.queryOptimizationOrganisms(`WHERE o.id = ?`, []interface{}{orgID}, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}

func (s *Storage) GetOptimizationLinkByBenchmarkRunID(runID string) (string, string, error) {
	row := s.db.QueryRow(`
		SELECT opt_run_id, id
		FROM optimization_organisms
		WHERE bench_run_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, strings.TrimSpace(runID))
	var optRunID string
	var orgID string
	if err := row.Scan(&optRunID, &orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", fmt.Errorf("query optimization link by benchmark run id: %w", err)
	}
	return optRunID, orgID, nil
}

func (s *Storage) ListOptimizationOrganisms(runID string, generation *int, limit int) ([]*optimize.Organism, error) {
	if limit < 1 {
		limit = 100
	}
	where := `WHERE o.opt_run_id = ?`
	args := []interface{}{runID}
	if generation != nil {
		where += ` AND o.generation = ?`
		args = append(args, *generation)
	}
	return s.queryOptimizationOrganisms(where, args, limit)
}

func (s *Storage) GetBestOptimizationOrganisms(runID string, limit int) ([]*optimize.Organism, error) {
	if limit < 1 {
		limit = 5
	}
	query := `
		SELECT
			o.id, o.opt_run_id, o.generation,
			COALESCE(o.parent_ids, '[]'), o.param_values, o.workflow_json,
			COALESCE(o.mutation_type, ''), COALESCE(o.mutation_log, ''), COALESCE(o.bench_run_id, ''),
			o.accuracy, o.adjusted_accuracy, o.parse_rate, o.cost_per_item,
			o.avg_latency_ms, o.p95_latency_ms,
			COALESCE(o.failed_items, 0), COALESCE(o.total_items, 0), COALESCE(o.flagged_items, 0),
			o.composite_score, COALESCE(o.feasible, 1), COALESCE(o.constraint_violations, '[]'),
			o.created_at, o.evaluated_at
		FROM optimization_organisms o
		WHERE o.opt_run_id = ?
		  AND o.composite_score IS NOT NULL
		ORDER BY
			CASE WHEN COALESCE(o.feasible, 0) = 1 THEN 1 ELSE 0 END DESC,
			o.composite_score DESC,
			o.created_at ASC
		LIMIT ?
	`
	rows, err := s.db.Query(query, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("query best optimization organisms: %w", err)
	}
	defer func() { _ = rows.Close() }()
	orgs, err := scanOptimizationOrganisms(rows)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

func (s *Storage) GetOptimizationLineage(orgID string) ([]*optimize.Organism, error) {
	queue := []string{orgID}
	seen := map[string]struct{}{}
	lineage := make([]*optimize.Organism, 0)

	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		if _, ok := seen[currentID]; ok {
			continue
		}
		seen[currentID] = struct{}{}

		org, err := s.GetOptimizationOrganism(currentID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		lineage = append(lineage, org)
		for _, parentID := range org.ParentIDs {
			if strings.TrimSpace(parentID) == "" {
				continue
			}
			queue = append(queue, parentID)
		}
	}
	return lineage, nil
}

func (s *Storage) CreateOptimizationParamChanges(organismID string, changes []optimize.ParamChange) error {
	if len(changes) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin param changes tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`
		INSERT INTO optimization_param_changes (organism_id, param_path, old_value, new_value, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare param change insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, change := range changes {
		if _, err := stmt.Exec(organismID, change.Path, nullableString(change.OldValue), nullableString(change.NewValue), nullableString(change.Reason), now); err != nil {
			return fmt.Errorf("insert param change: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit param changes tx: %w", err)
	}
	committed = true
	return nil
}

func (s *Storage) GetOptimizationParamChanges(organismID string) ([]optimize.ParamChange, error) {
	rows, err := s.db.Query(`
		SELECT param_path, COALESCE(old_value, ''), COALESCE(new_value, ''), COALESCE(reason, '')
		FROM optimization_param_changes
		WHERE organism_id = ?
		ORDER BY id ASC
	`, organismID)
	if err != nil {
		return nil, fmt.Errorf("query optimization param changes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	changes := make([]optimize.ParamChange, 0)
	for rows.Next() {
		var change optimize.ParamChange
		if err := rows.Scan(&change.Path, &change.OldValue, &change.NewValue, &change.Reason); err != nil {
			return nil, fmt.Errorf("scan param change row: %w", err)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate param change rows: %w", err)
	}
	return changes, nil
}

func (s *Storage) CreateOptimizationMutationArtifacts(organismID string, artifacts []optimize.MutationArtifact, compact bool) error {
	if len(artifacts) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin mutation artifacts tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`
		INSERT INTO optimization_mutation_artifacts (
			organism_id, input_prompt_hash, input_prompt, raw_output_hash, raw_output, claude_model, claude_cli_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare mutation artifact insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.InputPromptHash) == "" || strings.TrimSpace(artifact.RawOutputHash) == "" {
			return fmt.Errorf("mutation artifact hashes are required")
		}
		inputPrompt := artifact.InputPrompt
		rawOutput := artifact.RawOutput
		if compact {
			inputPrompt = ""
			rawOutput = ""
		}
		if _, err := stmt.Exec(
			organismID,
			artifact.InputPromptHash,
			nullableString(inputPrompt),
			artifact.RawOutputHash,
			nullableString(rawOutput),
			nullableString(artifact.ClaudeModel),
			nullableString(artifact.ClaudeCLIVersion),
			now,
		); err != nil {
			return fmt.Errorf("insert mutation artifact: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mutation artifacts tx: %w", err)
	}
	committed = true
	return nil
}

func (s *Storage) GetOptimizationMutationArtifacts(organismID string) ([]optimize.MutationArtifact, error) {
	rows, err := s.db.Query(`
		SELECT
			COALESCE(input_prompt_hash, ''),
			COALESCE(input_prompt, ''),
			COALESCE(raw_output_hash, ''),
			COALESCE(raw_output, ''),
			COALESCE(claude_model, ''),
			COALESCE(claude_cli_version, '')
		FROM optimization_mutation_artifacts
		WHERE organism_id = ?
		ORDER BY id ASC
	`, organismID)
	if err != nil {
		return nil, fmt.Errorf("query mutation artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	artifacts := make([]optimize.MutationArtifact, 0)
	for rows.Next() {
		var artifact optimize.MutationArtifact
		if err := rows.Scan(
			&artifact.InputPromptHash,
			&artifact.InputPrompt,
			&artifact.RawOutputHash,
			&artifact.RawOutput,
			&artifact.ClaudeModel,
			&artifact.ClaudeCLIVersion,
		); err != nil {
			return nil, fmt.Errorf("scan mutation artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mutation artifacts: %w", err)
	}
	return artifacts, nil
}

func (s *Storage) AppendOptimizationLearningEntry(runID string, entry *optimize.LearningEntry) error {
	if entry == nil {
		return fmt.Errorf("learning entry is nil")
	}
	run, err := s.GetOptimizationRun(runID)
	if err != nil {
		return err
	}
	logEntries := append([]optimize.LearningEntry(nil), run.LearningLog...)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	logEntries = append(logEntries, *entry)
	encoded, err := json.Marshal(logEntries)
	if err != nil {
		return fmt.Errorf("marshal learning log: %w", err)
	}
	_, err = s.db.Exec(`
		UPDATE optimization_runs
		SET learning_log = ?, updated_at = ?
		WHERE id = ?
	`, string(encoded), time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("append learning entry: %w", err)
	}
	return nil
}

func (s *Storage) GetOptimizationLearningLog(runID string, limit int) ([]optimize.LearningEntry, error) {
	run, err := s.GetOptimizationRun(runID)
	if err != nil {
		return nil, err
	}
	entries := run.LearningLog
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

func (s *Storage) queryOptimizationOrganisms(whereClause string, args []interface{}, limit int) ([]*optimize.Organism, error) {
	if limit < 1 {
		limit = 100
	}
	query := `
		SELECT
			o.id, o.opt_run_id, o.generation,
			COALESCE(o.parent_ids, '[]'), o.param_values, o.workflow_json,
			COALESCE(o.mutation_type, ''), COALESCE(o.mutation_log, ''), COALESCE(o.bench_run_id, ''),
			o.accuracy, o.adjusted_accuracy, o.parse_rate, o.cost_per_item,
			o.avg_latency_ms, o.p95_latency_ms,
			COALESCE(o.failed_items, 0), COALESCE(o.total_items, 0), COALESCE(o.flagged_items, 0),
			o.composite_score, COALESCE(o.feasible, 1), COALESCE(o.constraint_violations, '[]'),
			o.created_at, o.evaluated_at
		FROM optimization_organisms o
		` + whereClause + `
		ORDER BY o.generation ASC, o.created_at ASC
		LIMIT ?
	`
	rows, err := s.db.Query(query, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("query optimization organisms: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOptimizationOrganisms(rows)
}

func scanOptimizationOrganisms(rows *sql.Rows) ([]*optimize.Organism, error) {
	organisms := make([]*optimize.Organism, 0)
	for rows.Next() {
		var (
			org              optimize.Organism
			parentIDsJSON    string
			paramValuesJSON  string
			workflowJSON     string
			benchRunID       string
			accuracy         sql.NullFloat64
			adjustedAccuracy sql.NullFloat64
			parseRate        sql.NullFloat64
			costPerItem      sql.NullFloat64
			avgLatencyMS     sql.NullFloat64
			p95LatencyMS     sql.NullFloat64
			failedItems      int
			totalItems       int
			flaggedItems     int
			compositeScore   sql.NullFloat64
			feasibleInt      int
			violationsJSON   string
			evaluatedAt      sql.NullTime
		)
		if err := rows.Scan(
			&org.ID,
			&org.OptRunID,
			&org.Generation,
			&parentIDsJSON,
			&paramValuesJSON,
			&workflowJSON,
			&org.MutationType,
			&org.MutationLog,
			&benchRunID,
			&accuracy,
			&adjustedAccuracy,
			&parseRate,
			&costPerItem,
			&avgLatencyMS,
			&p95LatencyMS,
			&failedItems,
			&totalItems,
			&flaggedItems,
			&compositeScore,
			&feasibleInt,
			&violationsJSON,
			&org.CreatedAt,
			&evaluatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan optimization organism row: %w", err)
		}
		org.WorkflowJSON = json.RawMessage(workflowJSON)
		if strings.TrimSpace(parentIDsJSON) != "" {
			if err := json.Unmarshal([]byte(parentIDsJSON), &org.ParentIDs); err != nil {
				return nil, fmt.Errorf("decode parent_ids: %w", err)
			}
		}
		if err := json.Unmarshal([]byte(paramValuesJSON), &org.ParamValues); err != nil {
			return nil, fmt.Errorf("decode param_values: %w", err)
		}
		org.BenchRunID = benchRunID

		if accuracy.Valid || adjustedAccuracy.Valid || parseRate.Valid || costPerItem.Valid || avgLatencyMS.Valid || p95LatencyMS.Valid || compositeScore.Valid {
			fitness := &optimize.Fitness{
				FailedItems:  failedItems,
				TotalItems:   totalItems,
				FlaggedItems: flaggedItems,
				Feasible:     feasibleInt != 0,
			}
			if accuracy.Valid {
				fitness.Accuracy = accuracy.Float64
			}
			if adjustedAccuracy.Valid {
				fitness.AdjustedAccuracy = adjustedAccuracy.Float64
			}
			if parseRate.Valid {
				fitness.ParseRate = parseRate.Float64
			}
			if costPerItem.Valid {
				fitness.CostPerItem = costPerItem.Float64
			}
			if avgLatencyMS.Valid {
				fitness.AvgLatencyMS = avgLatencyMS.Float64
			}
			if p95LatencyMS.Valid {
				fitness.P95LatencyMS = p95LatencyMS.Float64
			}
			if compositeScore.Valid {
				fitness.CompositeScore = compositeScore.Float64
			}
			if strings.TrimSpace(violationsJSON) != "" {
				if err := json.Unmarshal([]byte(violationsJSON), &fitness.ConstraintViolations); err != nil {
					return nil, fmt.Errorf("decode constraint_violations: %w", err)
				}
			}
			org.Fitness = fitness
		}
		if evaluatedAt.Valid {
			t := evaluatedAt.Time.UTC()
			org.EvaluatedAt = &t
		}
		organisms = append(organisms, &org)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate optimization organism rows: %w", err)
	}
	return organisms, nil
}

func nullableString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func boolToIntOpt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sqlTimePtrValue(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

func nullableFitnessFloat(f *optimize.Fitness, getter func(*optimize.Fitness) float64) interface{} {
	if f == nil {
		return nil
	}
	return getter(f)
}

func nullableFitnessInt(f *optimize.Fitness, getter func(*optimize.Fitness) int) interface{} {
	if f == nil {
		return nil
	}
	return getter(f)
}

func nullableFitnessBool(f *optimize.Fitness, getter func(*optimize.Fitness) bool) interface{} {
	if f == nil {
		return nil
	}
	return boolToIntOpt(getter(f))
}

func nullableFitnessViolations(f *optimize.Fitness) interface{} {
	if f == nil {
		return nil
	}
	encoded, err := json.Marshal(f.ConstraintViolations)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func defaultStringValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
