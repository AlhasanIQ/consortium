package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite"
)

const diagnosticSQLiteDriverName = "sqlite-diagnostics"

var registerDiagnosticSQLiteDriverOnce sync.Once

type dbQueryDiagnosticsConfig struct {
	Enabled       bool
	LogAll        bool
	SlowThreshold time.Duration
	StatsInterval time.Duration
}

type dbQueryDiagnosticsState struct {
	mu     sync.Mutex
	config dbQueryDiagnosticsConfig

	QueryCount       uint64
	ExecCount        uint64
	SlowQueryCount   uint64
	ErrorCount       uint64
	TotalDuration    time.Duration
	MaxDuration      time.Duration
	RecentSlowEvents []DBQueryEvent
}

var globalDBQueryDiagnostics = &dbQueryDiagnosticsState{}

// DBQueryTraceDiagnostics summarizes opt-in SQL timing logs captured through the
// diagnostic SQLite driver. It intentionally excludes query arguments.
type DBQueryTraceDiagnostics struct {
	Enabled             bool
	LogAll              bool
	SlowThresholdMs     float64
	QueryCount          uint64
	ExecCount           uint64
	SlowQueryCount      uint64
	ErrorCount          uint64
	TotalDurationMs     float64
	MaxDurationMs       float64
	RecentSlowOrErrored []DBQueryEvent
}

// DBQueryEvent is a bounded, argument-free record of a slow or failed query.
type DBQueryEvent struct {
	Operation  string
	DurationMs float64
	Slow       bool
	Error      string
	SQL        string
	At         time.Time
}

// DBPoolDiagnostics exposes database/sql pool counters in millisecond units.
type DBPoolDiagnostics struct {
	MaxOpenConnections int
	OpenConnections    int
	InUse              int
	Idle               int
	WaitCount          int64
	WaitDurationMs     float64
	MaxIdleClosed      int64
	MaxIdleTimeClosed  int64
	MaxLifetimeClosed  int64
}

// DBSQLiteDiagnostics captures SQLite connection-level settings useful when
// debugging lock contention.
type DBSQLiteDiagnostics struct {
	JournalMode   string
	BusyTimeoutMs int
	Synchronous   int
}

// DBQueueDiagnostics captures durable-job queue state from the jobs table.
type DBQueueDiagnostics struct {
	PendingDurableJobs int
	RunningDurableJobs int
	PendingRootJobs    int
	PendingChildJobs   int
	RunningRootJobs    int
	RunningChildJobs   int
}

// DBTableDiagnostics is a cheap row-count snapshot for hot tables.
type DBTableDiagnostics struct {
	Name  string
	Rows  int64
	Error string `json:",omitempty"`
}

// DBDiagnostics is the storage-layer diagnostics payload returned by the admin
// diagnostics API.
type DBDiagnostics struct {
	Path       string
	Pool       DBPoolDiagnostics
	QueryTrace DBQueryTraceDiagnostics
	SQLite     DBSQLiteDiagnostics
	Queue      DBQueueDiagnostics
	Tables     []DBTableDiagnostics `json:",omitempty"`
}

// DBDiagnosticsOptions controls optional diagnostic work. Table counts can be
// expensive on large local databases, so callers opt in explicitly.
type DBDiagnosticsOptions struct {
	IncludeTables bool
}

func dbQueryDiagnosticsConfigFromEnv() dbQueryDiagnosticsConfig {
	thresholdMs := parsePositiveEnvInt("DB_SLOW_QUERY_THRESHOLD_MS", 250)
	statsIntervalSeconds := parsePositiveEnvInt("DB_STATS_LOG_INTERVAL_SECONDS", 0)
	return dbQueryDiagnosticsConfig{
		Enabled:       parseBoolEnv("DB_QUERY_LOG_ENABLED", false),
		LogAll:        parseBoolEnv("DB_QUERY_LOG_ALL", false),
		SlowThreshold: time.Duration(thresholdMs) * time.Millisecond,
		StatsInterval: time.Duration(statsIntervalSeconds) * time.Second,
	}
}

func parseBoolEnv(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(getenv(name)))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err == nil {
		return parsed
	}
	switch raw {
	case "yes", "y", "on":
		return true
	case "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getenv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func configureDBQueryDiagnostics(cfg dbQueryDiagnosticsConfig) {
	if cfg.SlowThreshold <= 0 {
		cfg.SlowThreshold = 250 * time.Millisecond
	}
	globalDBQueryDiagnostics.mu.Lock()
	globalDBQueryDiagnostics.config = cfg
	globalDBQueryDiagnostics.mu.Unlock()
}

func diagnosticDriverName(cfg dbQueryDiagnosticsConfig) string {
	if !cfg.Enabled {
		return "sqlite"
	}
	registerDiagnosticSQLiteDriverOnce.Do(func() {
		sql.Register(diagnosticSQLiteDriverName, &diagnosticDriver{inner: &sqlite.Driver{}})
	})
	return diagnosticSQLiteDriverName
}

func recordDBQuery(operation, query string, duration time.Duration, err error) {
	globalDBQueryDiagnostics.mu.Lock()
	defer globalDBQueryDiagnostics.mu.Unlock()

	cfg := globalDBQueryDiagnostics.config
	if !cfg.Enabled {
		return
	}

	switch operation {
	case "query":
		globalDBQueryDiagnostics.QueryCount++
	case "exec":
		globalDBQueryDiagnostics.ExecCount++
	}

	// Only the primary query/exec samples feed the latency aggregates and the
	// slow-query counter. The "rows" sample (emitted on diagnosticRows.Close)
	// measures full result-set lifetime, which temporally contains its "query"
	// sample, so summing it would double-count TotalDuration and make
	// MaxDuration reflect iteration time rather than query latency. The "rows"
	// sample is still surfaced as an individual event below for visibility.
	primary := operation == "query" || operation == "exec"

	slow := duration >= cfg.SlowThreshold
	if primary {
		globalDBQueryDiagnostics.TotalDuration += duration
		if duration > globalDBQueryDiagnostics.MaxDuration {
			globalDBQueryDiagnostics.MaxDuration = duration
		}
		if slow {
			globalDBQueryDiagnostics.SlowQueryCount++
		}
	}
	errText := ""
	if err != nil && err != driver.ErrSkip {
		globalDBQueryDiagnostics.ErrorCount++
		errText = err.Error()
	}

	shouldLog := cfg.LogAll || slow || errText != ""
	if !shouldLog {
		return
	}

	event := DBQueryEvent{
		Operation:  operation,
		DurationMs: durationMs(duration),
		Slow:       slow,
		Error:      errText,
		SQL:        normalizeDiagnosticSQL(query),
		At:         time.Now(),
	}
	globalDBQueryDiagnostics.RecentSlowEvents = append(globalDBQueryDiagnostics.RecentSlowEvents, event)
	if len(globalDBQueryDiagnostics.RecentSlowEvents) > 25 {
		copy(globalDBQueryDiagnostics.RecentSlowEvents, globalDBQueryDiagnostics.RecentSlowEvents[len(globalDBQueryDiagnostics.RecentSlowEvents)-25:])
		globalDBQueryDiagnostics.RecentSlowEvents = globalDBQueryDiagnostics.RecentSlowEvents[:25]
	}

	log.Printf("[DB query] operation=%s duration=%s slow=%t error=%q sql=%q",
		operation, duration.Round(time.Millisecond), slow, errText, event.SQL)
}

func dbQueryTraceSnapshot() DBQueryTraceDiagnostics {
	globalDBQueryDiagnostics.mu.Lock()
	defer globalDBQueryDiagnostics.mu.Unlock()

	events := make([]DBQueryEvent, len(globalDBQueryDiagnostics.RecentSlowEvents))
	copy(events, globalDBQueryDiagnostics.RecentSlowEvents)
	cfg := globalDBQueryDiagnostics.config
	return DBQueryTraceDiagnostics{
		Enabled:             cfg.Enabled,
		LogAll:              cfg.LogAll,
		SlowThresholdMs:     durationMs(cfg.SlowThreshold),
		QueryCount:          globalDBQueryDiagnostics.QueryCount,
		ExecCount:           globalDBQueryDiagnostics.ExecCount,
		SlowQueryCount:      globalDBQueryDiagnostics.SlowQueryCount,
		ErrorCount:          globalDBQueryDiagnostics.ErrorCount,
		TotalDurationMs:     durationMs(globalDBQueryDiagnostics.TotalDuration),
		MaxDurationMs:       durationMs(globalDBQueryDiagnostics.MaxDuration),
		RecentSlowOrErrored: events,
	}
}

func normalizeDiagnosticSQL(query string) string {
	clean := strings.Join(strings.Fields(query), " ")
	if len(clean) > 240 {
		return clean[:237] + "..."
	}
	return clean
}

func durationMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

type diagnosticDriver struct {
	inner driver.Driver
}

func (d *diagnosticDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &diagnosticConn{inner: conn}, nil
}

type diagnosticConn struct {
	inner driver.Conn
}

func (c *diagnosticConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &diagnosticStmt{inner: stmt, query: query}, nil
}

func (c *diagnosticConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.inner.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &diagnosticStmt{inner: stmt, query: query}, nil
	}
	return c.Prepare(query)
}

func (c *diagnosticConn) Close() error {
	return c.inner.Close()
}

func (c *diagnosticConn) Begin() (driver.Tx, error) {
	return c.inner.Begin() //nolint:staticcheck // required by legacy driver.Conn; inner BeginTx used when available
}

func (c *diagnosticConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.inner.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Begin()
}

func (c *diagnosticConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	result, err := execer.ExecContext(ctx, query, args)
	recordDBQuery("exec", query, time.Since(start), err)
	return result, err
}

func (c *diagnosticConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := queryer.QueryContext(ctx, query, args)
	recordDBQuery("query", query, time.Since(start), err)
	if err != nil {
		return rows, err
	}
	return &diagnosticRows{inner: rows, query: query, start: start}, nil
}

func (c *diagnosticConn) Ping(ctx context.Context) error {
	if pinger, ok := c.inner.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *diagnosticConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.inner.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *diagnosticConn) IsValid() bool {
	if validator, ok := c.inner.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

func (c *diagnosticConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.inner.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

type diagnosticStmt struct {
	inner driver.Stmt
	query string
}

func (s *diagnosticStmt) Close() error {
	return s.inner.Close()
}

func (s *diagnosticStmt) NumInput() int {
	return s.inner.NumInput()
}

func (s *diagnosticStmt) Exec(args []driver.Value) (driver.Result, error) {
	start := time.Now()
	result, err := s.inner.Exec(args) //nolint:staticcheck // required by legacy driver.Stmt; ExecContext used when available
	recordDBQuery("exec", s.query, time.Since(start), err)
	return result, err
}

func (s *diagnosticStmt) Query(args []driver.Value) (driver.Rows, error) {
	start := time.Now()
	rows, err := s.inner.Query(args) //nolint:staticcheck // required by legacy driver.Stmt; QueryContext used when available
	recordDBQuery("query", s.query, time.Since(start), err)
	if err != nil {
		return rows, err
	}
	return &diagnosticRows{inner: rows, query: s.query, start: start}, nil
}

func (s *diagnosticStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	result, err := execer.ExecContext(ctx, args)
	recordDBQuery("exec", s.query, time.Since(start), err)
	return result, err
}

func (s *diagnosticStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := queryer.QueryContext(ctx, args)
	recordDBQuery("query", s.query, time.Since(start), err)
	if err != nil {
		return rows, err
	}
	return &diagnosticRows{inner: rows, query: s.query, start: start}, nil
}

type diagnosticRows struct {
	inner driver.Rows
	query string
	start time.Time
	once  sync.Once
}

func (r *diagnosticRows) Columns() []string {
	return r.inner.Columns()
}

func (r *diagnosticRows) Close() error {
	err := r.inner.Close()
	r.once.Do(func() {
		recordDBQuery("rows", r.query, time.Since(r.start), err)
	})
	return err
}

func (r *diagnosticRows) Next(dest []driver.Value) error {
	return r.inner.Next(dest)
}

func (r *diagnosticRows) HasNextResultSet() bool {
	if rows, ok := r.inner.(driver.RowsNextResultSet); ok {
		return rows.HasNextResultSet()
	}
	return false
}

func (r *diagnosticRows) NextResultSet() error {
	if rows, ok := r.inner.(driver.RowsNextResultSet); ok {
		return rows.NextResultSet()
	}
	return driver.ErrSkip
}

func (r *diagnosticRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.inner.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return nil
}

func (r *diagnosticRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.inner.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *diagnosticRows) ColumnTypeLength(index int) (length int64, ok bool) {
	if rows, ok := r.inner.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *diagnosticRows) ColumnTypeNullable(index int) (nullable, ok bool) {
	if rows, ok := r.inner.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *diagnosticRows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	if rows, ok := r.inner.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (s *diagnosticStmt) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := s.inner.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

func (s *Storage) startDBStatsLogger(interval time.Duration) {
	if interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.dbStatsCancel = cancel
	go s.dbStatsLoggerLoop(ctx, interval)
}

func (s *Storage) dbStatsLoggerLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastWaitCount int64
	var lastWaitDuration time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		stats := s.db.Stats()
		trace := dbQueryTraceSnapshot()
		waitDelta := stats.WaitCount - lastWaitCount
		waitDurationDelta := stats.WaitDuration - lastWaitDuration
		lastWaitCount = stats.WaitCount
		lastWaitDuration = stats.WaitDuration
		log.Printf("[DB stats] open=%d in_use=%d idle=%d wait_count=%d wait_delta=%d wait_ms=%.1f wait_delta_ms=%.1f queries=%d execs=%d slow=%d errors=%d",
			stats.OpenConnections,
			stats.InUse,
			stats.Idle,
			stats.WaitCount,
			waitDelta,
			durationMs(stats.WaitDuration),
			durationMs(waitDurationDelta),
			trace.QueryCount,
			trace.ExecCount,
			trace.SlowQueryCount,
			trace.ErrorCount,
		)
	}
}

// DBDiagnostics returns current pool, query-trace, SQLite, queue, and hot-table
// diagnostics for admin and conctl callers.
func (s *Storage) DBDiagnostics(ctx context.Context) DBDiagnostics {
	return s.DBDiagnosticsWithOptions(ctx, DBDiagnosticsOptions{})
}

// DBDiagnosticsWithOptions returns current pool, query-trace, SQLite, queue,
// and optionally hot-table diagnostics for admin and conctl callers.
func (s *Storage) DBDiagnosticsWithOptions(ctx context.Context, opts DBDiagnosticsOptions) DBDiagnostics {
	diag := DBDiagnostics{
		Path:       s.dbPath,
		Pool:       dbPoolDiagnostics(s.db.Stats()),
		QueryTrace: dbQueryTraceSnapshot(),
		SQLite:     s.sqliteDiagnostics(ctx),
		Queue:      s.queueDiagnostics(ctx),
	}
	if opts.IncludeTables {
		diag.Tables = s.tableDiagnostics(ctx)
	}
	return diag
}

func dbPoolDiagnostics(stats sql.DBStats) DBPoolDiagnostics {
	return DBPoolDiagnostics{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDurationMs:     durationMs(stats.WaitDuration),
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxIdleTimeClosed:  stats.MaxIdleTimeClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}
}

func (s *Storage) sqliteDiagnostics(ctx context.Context) DBSQLiteDiagnostics {
	var diag DBSQLiteDiagnostics
	_ = s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&diag.JournalMode)
	_ = s.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&diag.BusyTimeoutMs)
	_ = s.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&diag.Synchronous)
	return diag
}

func (s *Storage) queueDiagnostics(ctx context.Context) DBQueueDiagnostics {
	var diag DBQueueDiagnostics
	diag.PendingRootJobs = s.countJobsForDiagnostics(ctx, "idx_jobs_pending_durable_root_created", "status = 'pending' AND dag_hash IS NOT NULL AND dag_hash != '' AND (parent_execution_id IS NULL OR parent_execution_id = '')")
	diag.PendingChildJobs = s.countJobsForDiagnostics(ctx, "idx_jobs_pending_durable_child_created", "status = 'pending' AND dag_hash IS NOT NULL AND dag_hash != '' AND parent_execution_id IS NOT NULL AND parent_execution_id != ''")
	diag.RunningRootJobs = s.countJobsForDiagnostics(ctx, "idx_jobs_running_durable_created", "status = 'running' AND dag_hash IS NOT NULL AND dag_hash != '' AND (parent_execution_id IS NULL OR parent_execution_id = '')")
	diag.RunningChildJobs = s.countJobsForDiagnostics(ctx, "idx_jobs_running_durable_created", "status = 'running' AND dag_hash IS NOT NULL AND dag_hash != '' AND parent_execution_id IS NOT NULL AND parent_execution_id != ''")
	diag.PendingDurableJobs = diag.PendingRootJobs + diag.PendingChildJobs
	diag.RunningDurableJobs = diag.RunningRootJobs + diag.RunningChildJobs
	return diag
}

func (s *Storage) countJobsForDiagnostics(ctx context.Context, indexName, where string) int {
	var count int
	from := "jobs"
	if indexName != "" {
		from += " INDEXED BY " + indexName
	}
	query := "SELECT COUNT(*) FROM " + from + " WHERE " + where
	if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		log.Printf("Warning: failed to load DB queue diagnostic count (%s): %v", where, err)
	}
	return count
}

func (s *Storage) tableDiagnostics(ctx context.Context) []DBTableDiagnostics {
	tableNames := []string{
		"jobs",
		"workflow_node_executions",
		"workflow_node_execution_attempts",
		"execution_history",
		"activity_results",
		"agent_runs",
		"benchmark_runs",
		"benchmark_run_items",
		"benchmark_runner_sessions",
	}
	out := make([]DBTableDiagnostics, 0, len(tableNames))
	for _, name := range tableNames {
		row := DBTableDiagnostics{Name: name}
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", name)
		if err := s.db.QueryRowContext(ctx, query).Scan(&row.Rows); err != nil {
			row.Error = err.Error()
		}
		out = append(out, row)
	}
	return out
}
