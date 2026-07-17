package storage

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestMigrationsApplyCleanly(t *testing.T) {
	// NewStorage(":memory:") applies the full schema + startup migrations.
	// This test verifies the migration chain runs without error on a fresh DB.
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage(:memory:) failed: %v", err)
	}
	defer store.Close()

	// Verify the migrations ran by checking for columns they add.
	db := store.DB()

	t.Run("workflows.version column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "workflows", "version")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("workflows.version column not found after migration")
		}
	})

	t.Run("benchmark_runs.item_limit column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "benchmark_runs", "item_limit")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("benchmark_runs.item_limit column not found after migration")
		}
	})

	t.Run("benchmark_runs.opt_run_id column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "benchmark_runs", "opt_run_id")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("benchmark_runs.opt_run_id column not found after migration")
		}
	})

	t.Run("benchmark_runs.opt_organism_id column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "benchmark_runs", "opt_organism_id")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("benchmark_runs.opt_organism_id column not found after migration")
		}
	})

	t.Run("optimization_runs.workflow_version column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "optimization_runs", "workflow_version")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("optimization_runs.workflow_version column not found after migration")
		}
	})

	t.Run("optimization_runs.mutator_mode column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "optimization_runs", "mutator_mode")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("optimization_runs.mutator_mode column not found after migration")
		}
	})

	t.Run("optimization_runs.rng_seed column exists", func(t *testing.T) {
		has, err := tableHasColumn(db, "optimization_runs", "rng_seed")
		if err != nil {
			t.Fatalf("tableHasColumn error: %v", err)
		}
		if !has {
			t.Error("optimization_runs.rng_seed column not found after migration")
		}
	})

	for _, table := range []string{"api_keys", "api_model_routes", "api_usage", "api_idempotency", "api_openai_objects", "api_openai_items"} {
		t.Run(table+" table exists", func(t *testing.T) {
			var name string
			err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("%s table missing after migration: %v", table, err)
			}
		})
	}
	for _, index := range []string{"idx_api_openai_objects_key_type_created", "idx_api_openai_items_object_kind_order"} {
		t.Run(index+" index exists", func(t *testing.T) {
			if !indexExists(t, db, index) {
				t.Fatalf("%s missing after migration", index)
			}
		})
	}

	t.Run("default api routes seeded", func(t *testing.T) {
		route, err := store.GetDefaultAPIModelRoute()
		if err != nil {
			t.Fatalf("GetDefaultAPIModelRoute: %v", err)
		}
		if route.APIModel != "consortium-default" {
			t.Fatalf("default API route = %q, want consortium-default", route.APIModel)
		}
	})
}

func TestMigrationsIdempotent(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage(:memory:) failed: %v", err)
	}
	defer store.Close()

	if err := store.CreateWorkflow(&WorkflowDefinition{
		ID:         "migration-idempotent",
		Name:       "Migration Idempotent",
		Definition: `{"nodes":[{"data":{"type":"prompt","config":{}}}]}`,
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := store.runStartupMigrations(); err != nil {
		t.Fatalf("first runStartupMigrations() failed: %v", err)
	}

	first, err := store.GetWorkflow("migration-idempotent")
	if err != nil {
		t.Fatalf("GetWorkflow after first migration: %v", err)
	}
	if first.Version != 2 {
		t.Fatalf("workflow version after legacy-default backfill = %d, want 2", first.Version)
	}

	if err := store.runStartupMigrations(); err != nil {
		t.Fatalf("second runStartupMigrations() failed: %v", err)
	}
	second, err := store.GetWorkflow("migration-idempotent")
	if err != nil {
		t.Fatalf("GetWorkflow after second migration: %v", err)
	}
	if second.Version != first.Version || second.Definition != first.Definition {
		t.Fatalf("second migration changed an already migrated workflow: first version/definition=(%d,%s), second=(%d,%s)", first.Version, first.Definition, second.Version, second.Definition)
	}
}

func TestStartupMigrationsUpgradeLegacyTablesAndBackfillRows(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	legacySchema := []string{
		`CREATE TABLE workflows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			definition TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE optimization_runs (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			benchmark TEXT NOT NULL,
			split TEXT NOT NULL,
			status TEXT NOT NULL,
			last_heartbeat_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE benchmark_runs (
			id TEXT PRIMARY KEY,
			benchmark TEXT NOT NULL,
			split TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			workflow_name TEXT NOT NULL DEFAULT '',
			dataset_path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			total_items INTEGER DEFAULT 0,
			started_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			source TEXT NOT NULL DEFAULT 'manual'
		)`,
		`CREATE TABLE agent_runs (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1,
			run_kind TEXT NOT NULL DEFAULT 'agent_run',
			external_run_id TEXT NOT NULL,
			external_task_id TEXT,
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
			UNIQUE(job_id, run_id, node_id, attempt)
		)`,
	}
	for _, statement := range legacySchema {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema should coexist with legacy tables: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO workflows (id, name, definition) VALUES (?, ?, ?)
	`, "legacy-workflow", "Legacy workflow", `{"nodes":[{"data":{"type":"prompt","config":{}}}]}`); err != nil {
		t.Fatalf("insert legacy workflow: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO optimization_runs (id, workflow_id, benchmark, split, status)
		VALUES ('legacy-opt', 'legacy-workflow', 'mmlu', 'dev', 'pending')
	`); err != nil {
		t.Fatalf("insert legacy optimization run: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO benchmark_runs (id, benchmark, split, workflow_id, status, total_items, source)
		VALUES ('legacy-bench', 'mmlu', 'dev', 'legacy-workflow', 'completed', 2, 'backend')
	`); err != nil {
		t.Fatalf("insert legacy benchmark run: %v", err)
	}

	store := &Storage{db: db}
	if err := store.runStartupMigrations(); err != nil {
		t.Fatalf("runStartupMigrations: %v", err)
	}

	for _, column := range []struct {
		table  string
		column string
	}{
		{table: "workflows", column: "version"},
		{table: "optimization_runs", column: "workflow_version"},
		{table: "optimization_runs", column: "children_per_parent"},
		{table: "optimization_runs", column: "max_children_per_generation"},
		{table: "optimization_runs", column: "adaptive_fanout"},
		{table: "optimization_runs", column: "mutator_mode"},
		{table: "optimization_runs", column: "rng_seed"},
		{table: "optimization_runs", column: "compact_artifacts"},
		{table: "optimization_runs", column: "dspy_metric_calls_used"},
		{table: "benchmark_runs", column: "item_limit"},
		{table: "benchmark_runs", column: "opt_run_id"},
		{table: "benchmark_runs", column: "opt_organism_id"},
		{table: "agent_runs", column: "external_job_run_id"},
		{table: "agent_runs", column: "inherit_from_json"},
	} {
		has, err := tableHasColumn(db, column.table, column.column)
		if err != nil {
			t.Fatalf("tableHasColumn(%s.%s): %v", column.table, column.column, err)
		}
		if !has {
			t.Fatalf("legacy migration did not add %s.%s", column.table, column.column)
		}
	}

	workflow, err := store.GetWorkflow("legacy-workflow")
	if err != nil {
		t.Fatalf("GetWorkflow after migration: %v", err)
	}
	var definition map[string]interface{}
	if err := json.Unmarshal([]byte(workflow.Definition), &definition); err != nil {
		t.Fatalf("migrated workflow definition is invalid JSON: %v", err)
	}
	nodes, ok := definition["nodes"].([]interface{})
	if !ok || len(nodes) != 1 {
		t.Fatalf("migrated workflow nodes = %#v, want one node", definition["nodes"])
	}
	node, ok := nodes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("migrated workflow node = %#v, want object", nodes[0])
	}
	data, ok := node["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("migrated workflow data = %#v, want object", node["data"])
	}
	config, ok := data["config"].(map[string]interface{})
	if !ok || config["retryPolicy"] == nil || config["maxTokens"] == nil {
		t.Fatalf("legacy workflow defaults were not backfilled: %#v", config)
	}

	var source string
	var itemLimit int
	if err := db.QueryRow(`SELECT source, item_limit FROM benchmark_runs WHERE id = 'legacy-bench'`).Scan(&source, &itemLimit); err != nil {
		t.Fatalf("read migrated benchmark run: %v", err)
	}
	if source != "manual" || itemLimit != 2 {
		t.Fatalf("migrated benchmark metadata = source %q, item_limit %d; want manual, 2", source, itemLimit)
	}
}

func TestStartupMigrationsRecreateOpenAIObjectTables(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage(:memory:) failed: %v", err)
	}
	defer store.Close()
	db := store.DB()

	if _, err := db.Exec(`DROP TABLE api_openai_items`); err != nil {
		t.Fatalf("drop api_openai_items: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE api_openai_objects`); err != nil {
		t.Fatalf("drop api_openai_objects: %v", err)
	}
	if err := store.runStartupMigrations(); err != nil {
		t.Fatalf("runStartupMigrations failed: %v", err)
	}
	if !indexExists(t, db, "idx_api_openai_items_object_kind_order") {
		t.Fatal("idx_api_openai_items_object_kind_order missing after migration")
	}

	now := time.Now().UTC()
	if err := store.CreateOpenAIObjectWithItems(&OpenAIObjectRecord{
		ID:           "resp-migrated",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-test",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		Status:       OpenAIObjectStatusCompleted,
		Store:        true,
		MetadataJSON: `{}`,
		RequestJSON:  `{}`,
		ResponseJSON: `{"id":"resp-migrated"}`,
		UsageJSON:    `{}`,
		CreatedAt:    now,
		UpdatedAt:    now,
		CompletedAt:  &now,
	}, []OpenAIObjectItem{
		{
			ID:           "item-migrated-input",
			ObjectID:     "resp-migrated",
			ItemKind:     OpenAIItemKindInput,
			ItemIndex:    0,
			OpenAIItemID: "msg-migrated",
			Role:         "user",
			ContentJSON:  `{"text":"hello"}`,
			RawJSON:      `{"id":"msg-migrated"}`,
			CreatedAt:    now,
		},
	}); err != nil {
		t.Fatalf("CreateOpenAIObjectWithItems after migration: %v", err)
	}
}

func TestSchemaInitBeforeMigrationsWithExistingAgentRunsMissingHandoffColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`
		CREATE TABLE agent_runs (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1,
			external_run_id TEXT NOT NULL,
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
			run_kind TEXT NOT NULL DEFAULT 'agent_run',
			external_task_id TEXT,
			UNIQUE(job_id, run_id, node_id, attempt)
		)
	`); err != nil {
		t.Fatalf("create old agent_runs table: %v", err)
	}

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema init should not require migrated agent_runs columns: %v", err)
	}

	store := &Storage{db: db}
	if err := store.runStartupMigrations(); err != nil {
		t.Fatalf("runStartupMigrations failed: %v", err)
	}
	for _, column := range []string{"external_job_run_id", "inherit_from_json"} {
		has, err := tableHasColumn(db, "agent_runs", column)
		if err != nil {
			t.Fatalf("tableHasColumn(%s): %v", column, err)
		}
		if !has {
			t.Fatalf("agent_runs.%s missing after migration", column)
		}
	}
	if !indexExists(t, db, "idx_agent_runs_external_job_run_id") {
		t.Fatal("idx_agent_runs_external_job_run_id missing after migration")
	}
}

func TestMigrationsTableHasColumn(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage(:memory:) failed: %v", err)
	}
	defer store.Close()
	db := store.DB()

	tests := []struct {
		name   string
		table  string
		column string
		want   bool
	}{
		{
			name:   "existing column",
			table:  "jobs",
			column: "status",
			want:   true,
		},
		{
			name:   "non-existing column",
			table:  "jobs",
			column: "nonexistent_column_xyz",
			want:   false,
		},
		{
			name:   "non-existing table returns false without error",
			table:  "nonexistent_table_xyz",
			column: "any",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			has, err := tableHasColumn(db, tt.table, tt.column)
			if err != nil {
				t.Fatalf("tableHasColumn(%s, %s) error: %v", tt.table, tt.column, err)
			}
			if has != tt.want {
				t.Errorf("tableHasColumn(%s, %s) = %v, want %v", tt.table, tt.column, has, tt.want)
			}
		})
	}
}

func indexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query index %s: %v", name, err)
	}
	return found == name
}

func TestMigrationsDefaultBenchmarkDatasetPath(t *testing.T) {
	tests := []struct {
		benchmark string
		split     string
		want      string
	}{
		{"global-mmlu-lite", "dev", "benchmarks/data/global-mmlu-lite/dev.jsonl"},
		{"", "dev", ""},
		{"global-mmlu-lite", "", ""},
		{"  ", "  ", ""},
		{"MMLU", "TEST", "benchmarks/data/mmlu/test.jsonl"},
	}

	for _, tt := range tests {
		t.Run(tt.benchmark+"_"+tt.split, func(t *testing.T) {
			got := defaultBenchmarkDatasetPath(tt.benchmark, tt.split)
			if got != tt.want {
				t.Errorf("defaultBenchmarkDatasetPath(%q, %q) = %q, want %q", tt.benchmark, tt.split, got, tt.want)
			}
		})
	}
}

func TestMigrationsCountJSONLLines(t *testing.T) {
	t.Run("empty path returns error", func(t *testing.T) {
		_, err := countJSONLLines("")
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := countJSONLLines("/tmp/definitely_does_not_exist_12345.jsonl")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}
