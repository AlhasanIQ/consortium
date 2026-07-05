package storage

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Storage) runStartupMigrations() error {
	if err := s.ensureWorkflowVersionColumn(); err != nil {
		return err
	}
	if err := s.ensureOptimizationRunWorkflowVersionColumn(); err != nil {
		return err
	}
	if err := s.ensureOptimizationRunConfigColumns(); err != nil {
		return err
	}
	if err := s.ensureBenchmarkRunMetadataColumns(); err != nil {
		return err
	}
	if err := s.ensureNovomoAgentRunSchema(); err != nil {
		return err
	}
	if err := s.ensureOpenAIObjectTables(); err != nil {
		return err
	}
	if err := s.backfillBenchmarkRunSourceSemantics(); err != nil {
		return err
	}
	if err := s.cleanupStaleOptimizerBenchmarkRuns(); err != nil {
		return err
	}
	if err := s.backfillBenchmarkRunOptimizationLinks(); err != nil {
		return err
	}
	if err := s.backfillBenchmarkRunItemLimitFromDataset(); err != nil {
		return err
	}
	if err := s.backfillWorkflowDefinitionsWithExplicitDefaults(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) ensureOpenAIObjectTables() error {
	for _, prefix := range []string{
		"CREATE TABLE IF NOT EXISTS api_openai_objects",
		"CREATE TABLE IF NOT EXISTS api_openai_items",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_type_created",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_status_created",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_model_created",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_objects_job_id",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_items_object_kind_order",
		"CREATE INDEX IF NOT EXISTS idx_api_openai_items_object_kind_openai_id",
	} {
		stmt, ok := schemaStatementWithPrefix(prefix)
		if !ok {
			return fmt.Errorf("schema statement %q not found", prefix)
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure OpenAI object schema %q: %w", prefix, err)
		}
	}
	return nil
}

func schemaStatementWithPrefix(prefix string) (string, bool) {
	for _, statement := range strings.Split(schema, ";") {
		statement = strings.TrimSpace(statement)
		if strings.Contains(statement, prefix) {
			return statement + ";", true
		}
	}
	return "", false
}

func (s *Storage) ensureNovomoAgentRunSchema() error {
	hasExternalRunID, err := tableHasColumn(s.db, "agent_runs", "external_run_id")
	if err != nil {
		return fmt.Errorf("check agent_runs.external_run_id column: %w", err)
	}
	hasPlannerModel, err := tableHasColumn(s.db, "agent_runs", "planner_model")
	if err != nil {
		return fmt.Errorf("check agent_runs.planner_model column: %w", err)
	}
	hasRunKind, err := tableHasColumn(s.db, "agent_runs", "run_kind")
	if err != nil {
		return fmt.Errorf("check agent_runs.run_kind column: %w", err)
	}
	hasExternalTaskID, err := tableHasColumn(s.db, "agent_runs", "external_task_id")
	if err != nil {
		return fmt.Errorf("check agent_runs.external_task_id column: %w", err)
	}
	hasExternalJobRunID, err := tableHasColumn(s.db, "agent_runs", "external_job_run_id")
	if err != nil {
		return fmt.Errorf("check agent_runs.external_job_run_id column: %w", err)
	}
	hasInheritFromJSON, err := tableHasColumn(s.db, "agent_runs", "inherit_from_json")
	if err != nil {
		return fmt.Errorf("check agent_runs.inherit_from_json column: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin agent run schema migration tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, stmt := range []string{
		`DROP TABLE IF EXISTS agent_tool_calls`,
		`DROP TABLE IF EXISTS agent_events`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("drop legacy agent detail table: %w", err)
		}
	}

	if !hasExternalRunID || hasPlannerModel {
		if _, err := tx.Exec(`DROP TABLE IF EXISTS agent_runs`); err != nil {
			return fmt.Errorf("drop legacy agent_runs table: %w", err)
		}
		if _, err := tx.Exec(agentRunsSchemaSQL); err != nil {
			return fmt.Errorf("create novomo agent_runs table: %w", err)
		}
		hasRunKind = true
		hasExternalTaskID = true
		hasExternalJobRunID = true
		hasInheritFromJSON = true
	}
	if !hasRunKind {
		if _, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN run_kind TEXT NOT NULL DEFAULT 'agent_run' CHECK(run_kind IN ('agent_run', 'novo_run'))`); err != nil {
			return fmt.Errorf("add agent_runs.run_kind column: %w", err)
		}
	}
	if !hasExternalTaskID {
		if _, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN external_task_id TEXT`); err != nil {
			return fmt.Errorf("add agent_runs.external_task_id column: %w", err)
		}
	}
	if !hasExternalJobRunID {
		if _, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN external_job_run_id TEXT`); err != nil {
			return fmt.Errorf("add agent_runs.external_job_run_id column: %w", err)
		}
	}
	if !hasInheritFromJSON {
		if _, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN inherit_from_json TEXT`); err != nil {
			return fmt.Errorf("add agent_runs.inherit_from_json column: %w", err)
		}
	}

	for _, stmt := range agentRunsIndexSQL {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create novomo agent_runs index: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent run schema migration: %w", err)
	}
	committed = true
	return nil
}

const agentRunsSchemaSQL = `
CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
	    node_id TEXT NOT NULL,
	    attempt INTEGER NOT NULL DEFAULT 1,
	    run_kind TEXT NOT NULL DEFAULT 'agent_run' CHECK(run_kind IN ('agent_run', 'novo_run')),
	    external_run_id TEXT NOT NULL,
	    external_job_run_id TEXT,
	    external_task_id TEXT,
	    inherit_from_json TEXT,
	    harness TEXT NOT NULL,
    status TEXT NOT NULL,
    output TEXT,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0,
    error_code TEXT,
    error_message TEXT,
    started_at DATETIME,
    finished_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, run_id, node_id, attempt)
)`

var agentRunsIndexSQL = []string{
	`CREATE INDEX IF NOT EXISTS idx_agent_runs_job_node ON agent_runs(job_id, node_id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_runs_job_created_at ON agent_runs(job_id, created_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_runs_external_run_id ON agent_runs(external_run_id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_runs_external_job_run_id ON agent_runs(external_job_run_id) WHERE external_job_run_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_agent_runs_run_kind ON agent_runs(run_kind)`,
}

func (s *Storage) ensureWorkflowVersionColumn() error {
	hasVersion, err := tableHasColumn(s.db, "workflows", "version")
	if err != nil {
		return fmt.Errorf("check workflows.version column: %w", err)
	}
	if hasVersion {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE workflows ADD COLUMN version INTEGER NOT NULL DEFAULT 1`); err != nil {
		return fmt.Errorf("add workflows.version column: %w", err)
	}
	return nil
}

func (s *Storage) ensureOptimizationRunWorkflowVersionColumn() error {
	hasColumn, err := tableHasColumn(s.db, "optimization_runs", "workflow_version")
	if err != nil {
		// Table may not exist yet on first boot before schema additions are applied.
		return nil
	}
	if hasColumn {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE optimization_runs ADD COLUMN workflow_version INTEGER NOT NULL DEFAULT 1`); err != nil {
		return fmt.Errorf("add optimization_runs.workflow_version column: %w", err)
	}
	return nil
}

func (s *Storage) ensureOptimizationRunConfigColumns() error {
	type migration struct {
		column string
		sql    string
	}
	migrations := []migration{
		{column: "children_per_parent", sql: `ALTER TABLE optimization_runs ADD COLUMN children_per_parent INTEGER DEFAULT 1`},
		{column: "max_children_per_generation", sql: `ALTER TABLE optimization_runs ADD COLUMN max_children_per_generation INTEGER DEFAULT 0`},
		{column: "adaptive_fanout", sql: `ALTER TABLE optimization_runs ADD COLUMN adaptive_fanout INTEGER DEFAULT 0`},
		{column: "mutator_mode", sql: `ALTER TABLE optimization_runs ADD COLUMN mutator_mode TEXT NOT NULL DEFAULT 'auto'`},
		{column: "rng_seed", sql: `ALTER TABLE optimization_runs ADD COLUMN rng_seed INTEGER`},
		{column: "compact_artifacts", sql: `ALTER TABLE optimization_runs ADD COLUMN compact_artifacts INTEGER DEFAULT 0`},
		{column: "dspy_metric_calls_used", sql: `ALTER TABLE optimization_runs ADD COLUMN dspy_metric_calls_used INTEGER DEFAULT 0`},
	}
	for _, m := range migrations {
		hasColumn, err := tableHasColumn(s.db, "optimization_runs", m.column)
		if err != nil {
			// Table may not exist yet on first boot before schema additions are applied.
			return nil
		}
		if hasColumn {
			continue
		}
		if _, err := s.db.Exec(m.sql); err != nil {
			return fmt.Errorf("add optimization_runs.%s column: %w", m.column, err)
		}
	}
	if _, err := s.db.Exec(`
		UPDATE optimization_runs
		SET mutator_mode = 'auto'
		WHERE TRIM(LOWER(COALESCE(mutator_mode, ''))) IN ('', 'hybrid', 'hybrid_comb_llm_adaptive', 'multi_operator')
		`); err != nil {
		return fmt.Errorf("normalize optimization_runs.mutator_mode values: %w", err)
	}
	return nil
}

func (s *Storage) ensureBenchmarkRunMetadataColumns() error {
	type migration struct {
		column string
		sql    string
	}
	migrations := []migration{
		{column: "item_limit", sql: `ALTER TABLE benchmark_runs ADD COLUMN item_limit INTEGER DEFAULT 0`},
		{column: "opt_run_id", sql: `ALTER TABLE benchmark_runs ADD COLUMN opt_run_id TEXT`},
		{column: "opt_organism_id", sql: `ALTER TABLE benchmark_runs ADD COLUMN opt_organism_id TEXT`},
	}
	for _, m := range migrations {
		hasColumn, err := tableHasColumn(s.db, "benchmark_runs", m.column)
		if err != nil {
			// Table may not exist yet before schema additions are applied.
			return nil
		}
		if hasColumn {
			continue
		}
		if _, err := s.db.Exec(m.sql); err != nil {
			return fmt.Errorf("add benchmark_runs.%s column: %w", m.column, err)
		}
	}
	indexStatements := []string{
		`CREATE INDEX IF NOT EXISTS idx_benchmark_runs_source ON benchmark_runs(source, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_benchmark_runs_opt_run_id ON benchmark_runs(opt_run_id) WHERE opt_run_id IS NOT NULL`,
	}
	for _, stmt := range indexStatements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure benchmark metadata indexes: %w", err)
		}
	}
	return nil
}

func (s *Storage) backfillBenchmarkRunSourceSemantics() error {
	statements := []string{
		`UPDATE benchmark_runs
		 SET source = 'optimizer'
		 WHERE workflow_id LIKE 'opt-%'`,
		`UPDATE benchmark_runs
		 SET source = 'replay'
		 WHERE source != 'optimizer'
		   AND (id LIKE 'replay-%' OR workflow_id LIKE 'replay-%')`,
		`UPDATE benchmark_runs
		 SET source = 'manual'
		 WHERE source IN ('backend', 'api', 'completed', 'failed', 'cancelled', 'running', 'queued', '')`,
		`UPDATE benchmark_runs
		 SET source = 'manual'
		 WHERE source NOT IN ('manual', 'benchloop', 'optimizer', 'imported', 'replay')`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("backfill benchmark source semantics: %w", err)
		}
	}
	return nil
}

func (s *Storage) cleanupStaleOptimizerBenchmarkRuns() error {
	rows, err := s.db.Query(`
		SELECT br.id
		FROM benchmark_runs br
		LEFT JOIN optimization_organisms oo ON oo.bench_run_id = br.id
		WHERE br.workflow_id LIKE 'opt-%'
		  AND br.status IN ('completed', 'failed', 'cancelled')
		  AND oo.id IS NULL
	`)
	if err != nil {
		return fmt.Errorf("query stale optimizer benchmark runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stale := make([]string, 0)
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return fmt.Errorf("scan stale optimizer run id: %w", err)
		}
		stale = append(stale, strings.TrimSpace(runID))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stale optimizer run ids: %w", err)
	}
	stale = normalizeNonEmptyIDs(stale)
	if len(stale) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin stale optimizer cleanup tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	inClause := inClausePlaceholders(len(stale))
	args := make([]interface{}, len(stale))
	for i, id := range stale {
		args[i] = id
	}

	deleteQueries := []string{
		`DELETE FROM benchmark_run_item_attempts WHERE run_id IN (` + inClause + `)`,
		`DELETE FROM benchmark_run_items WHERE run_id IN (` + inClause + `)`,
		`DELETE FROM benchmark_run_failure_counts WHERE run_id IN (` + inClause + `)`,
		`DELETE FROM benchmark_runs WHERE id IN (` + inClause + `)`,
	}
	for _, query := range deleteQueries {
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("delete stale optimizer benchmark rows: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stale optimizer cleanup: %w", err)
	}
	committed = true
	log.Printf("Removed %d stale optimizer benchmark runs", len(stale))
	return nil
}

func (s *Storage) backfillBenchmarkRunOptimizationLinks() error {
	if _, err := s.db.Exec(`
		UPDATE benchmark_runs
		SET opt_run_id = (
				SELECT oo.opt_run_id
				FROM optimization_organisms oo
				WHERE oo.bench_run_id = benchmark_runs.id
				ORDER BY oo.created_at DESC
				LIMIT 1
			),
			opt_organism_id = (
				SELECT oo.id
				FROM optimization_organisms oo
				WHERE oo.bench_run_id = benchmark_runs.id
				ORDER BY oo.created_at DESC
				LIMIT 1
			)
		WHERE EXISTS (
			SELECT 1
			FROM optimization_organisms oo
			WHERE oo.bench_run_id = benchmark_runs.id
		)
	`); err != nil {
		return fmt.Errorf("backfill benchmark optimizer links: %w", err)
	}
	if _, err := s.db.Exec(`
		UPDATE benchmark_runs
		SET opt_run_id = NULL,
			opt_organism_id = NULL
		WHERE source != 'optimizer'
	`); err != nil {
		return fmt.Errorf("clear non-optimizer benchmark optimizer links: %w", err)
	}
	return nil
}

func (s *Storage) backfillBenchmarkRunItemLimitFromDataset() error {
	type runRecord struct {
		ID         string
		Benchmark  string
		Split      string
		Dataset    string
		TotalItems int
		ItemLimit  int
	}

	observedByRun := make(map[string]int)
	obsRows, err := s.db.Query(`
		SELECT run_id, COUNT(DISTINCT item_id)
		FROM benchmark_run_items
		GROUP BY run_id
	`)
	if err != nil {
		return fmt.Errorf("query benchmark observed item counts: %w", err)
	}
	defer func() { _ = obsRows.Close() }()
	for obsRows.Next() {
		var runID string
		var count int
		if err := obsRows.Scan(&runID, &count); err != nil {
			return fmt.Errorf("scan benchmark observed item count: %w", err)
		}
		observedByRun[runID] = count
	}
	if err := obsRows.Err(); err != nil {
		return fmt.Errorf("iterate benchmark observed item counts: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, benchmark, split, COALESCE(dataset_path, ''), COALESCE(total_items, 0), COALESCE(item_limit, 0)
		FROM benchmark_runs
	`)
	if err != nil {
		return fmt.Errorf("query benchmark runs for item_limit backfill: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]runRecord, 0)
	for rows.Next() {
		var rec runRecord
		if err := rows.Scan(&rec.ID, &rec.Benchmark, &rec.Split, &rec.Dataset, &rec.TotalItems, &rec.ItemLimit); err != nil {
			return fmt.Errorf("scan benchmark run for item_limit backfill: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate benchmark runs for item_limit backfill: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	datasetSizeCache := make(map[string]int)
	datasetLoadAttempted := make(map[string]bool)
	resolveDatasetSize := func(path string) int {
		path = strings.TrimSpace(path)
		if path == "" {
			return 0
		}
		if size, ok := datasetSizeCache[path]; ok {
			return size
		}
		if datasetLoadAttempted[path] {
			return 0
		}
		datasetLoadAttempted[path] = true
		size, err := countJSONLLines(path)
		if err != nil {
			return 0
		}
		datasetSizeCache[path] = size
		return size
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin item_limit backfill tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	updated := 0
	for _, rec := range records {
		observed := observedByRun[rec.ID]
		if observed <= 0 && rec.TotalItems > 0 {
			observed = rec.TotalItems
		}

		datasetSize := resolveDatasetSize(rec.Dataset)
		if datasetSize == 0 {
			datasetSize = resolveDatasetSize(defaultBenchmarkDatasetPath(rec.Benchmark, rec.Split))
		}

		derived := 0
		switch {
		case observed <= 0:
			derived = 0
		case datasetSize > 0 && observed >= datasetSize:
			derived = 0
		default:
			derived = observed
		}

		if rec.ItemLimit == derived {
			continue
		}
		if _, err := tx.Exec(`UPDATE benchmark_runs SET item_limit = ?, updated_at = ? WHERE id = ?`, derived, time.Now().UTC(), rec.ID); err != nil {
			return fmt.Errorf("update benchmark run item_limit for %s: %w", rec.ID, err)
		}
		updated++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit item_limit backfill: %w", err)
	}
	committed = true
	if updated > 0 {
		log.Printf("Backfilled item_limit for %d benchmark runs", updated)
	}
	return nil
}

func defaultBenchmarkDatasetPath(benchmark, split string) string {
	benchmark = strings.TrimSpace(strings.ToLower(benchmark))
	split = strings.TrimSpace(strings.ToLower(split))
	if benchmark == "" || split == "" {
		return ""
	}
	return filepath.Join("benchmarks", "data", benchmark, split+".jsonl")
}

func countJSONLLines(path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("dataset path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 10*1024*1024)
	lines := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return lines, nil
}

func (s *Storage) backfillWorkflowDefinitionsWithExplicitDefaults() error {
	rows, err := s.db.Query(`SELECT id, definition FROM workflows`)
	if err != nil {
		return fmt.Errorf("query workflow definitions for backfill: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("warning: close workflow backfill rows: %v", err)
		}
	}()

	type update struct {
		id         string
		definition string
	}
	updates := make([]update, 0)
	for rows.Next() {
		var workflowID string
		var definition string
		if err := rows.Scan(&workflowID, &definition); err != nil {
			return fmt.Errorf("scan workflow row for backfill: %w", err)
		}
		updated, changed, err := ApplyLegacyWorkflowDefaults(definition)
		if err != nil {
			return fmt.Errorf("backfill workflow %s: %w", workflowID, err)
		}
		if changed {
			updates = append(updates, update{id: workflowID, definition: updated})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate workflow rows for backfill: %w", err)
	}

	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin workflow backfill tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()
	for _, item := range updates {
		if _, err := tx.Exec(`
			UPDATE workflows
			SET definition = ?, version = COALESCE(version, 1) + 1, updated_at = ?
			WHERE id = ?
		`, item.definition, now, item.id); err != nil {
			return fmt.Errorf("update workflow %s during backfill: %w", item.id, err)
		}
		log.Printf("Backfilled explicit defaults for workflow %s", item.id)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow backfill tx: %w", err)
	}
	committed = true
	return nil
}

func tableHasColumn(db *sql.DB, tableName string, columnName string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
