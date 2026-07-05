package admin

import (
	"context"
	"fmt"
)

const (
	jobScopeParents  = "parents"
	jobScopeChildren = "children"
	jobScopeAll      = "all"
)

type jobCountSnapshot struct {
	counts map[string]map[string]int
}

func (s *Server) loadJobCountSnapshot(ctx context.Context) (jobCountSnapshot, error) {
	snapshot := jobCountSnapshot{
		counts: map[string]map[string]int{
			jobScopeParents:  {},
			jobScopeChildren: {},
		},
	}

	rows, err := s.db.QueryContext(ctx, jobCountSnapshotSQL())
	if err != nil {
		return snapshot, fmt.Errorf("load job count snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var scope, status string
		var count int
		if err := rows.Scan(&scope, &status, &count); err != nil {
			return snapshot, fmt.Errorf("scan job count snapshot: %w", err)
		}
		if _, ok := snapshot.counts[scope]; !ok {
			snapshot.counts[scope] = map[string]int{}
		}
		snapshot.counts[scope][status] = count
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate job count snapshot: %w", err)
	}
	return snapshot, nil
}

func jobCountSnapshotSQL() string {
	return `
		SELECT 'parents' AS scope, status, COUNT(*)
		FROM jobs INDEXED BY idx_jobs_root_status
		WHERE parent_execution_id IS NULL OR parent_execution_id = ''
		GROUP BY status
		UNION ALL
		SELECT 'children' AS scope, status, COUNT(*)
		FROM jobs INDEXED BY idx_jobs_child_status
		WHERE parent_execution_id IS NOT NULL AND parent_execution_id != ''
		GROUP BY status
	`
}

func (s jobCountSnapshot) statusCountsForShow(showFilter string) map[string]int {
	out := map[string]int{}
	switch showFilter {
	case jobScopeChildren:
		copyStatusCounts(out, s.counts[jobScopeChildren])
	case jobScopeAll:
		copyStatusCounts(out, s.counts[jobScopeParents])
		addStatusCounts(out, s.counts[jobScopeChildren])
	default:
		copyStatusCounts(out, s.counts[jobScopeParents])
	}
	return out
}

func (s jobCountSnapshot) scopeCounts(statusFilter string) (parentCount, childCount, allCount int) {
	parentCount = s.scopeTotal(jobScopeParents, statusFilter)
	childCount = s.scopeTotal(jobScopeChildren, statusFilter)
	return parentCount, childCount, parentCount + childCount
}

func (s jobCountSnapshot) totalItems(showFilter, statusFilter string) int {
	switch showFilter {
	case jobScopeChildren:
		return s.scopeTotal(jobScopeChildren, statusFilter)
	case jobScopeAll:
		_, _, all := s.scopeCounts(statusFilter)
		return all
	default:
		return s.scopeTotal(jobScopeParents, statusFilter)
	}
}

func (s jobCountSnapshot) activeRunningAll() int {
	return s.counts[jobScopeParents]["running"] + s.counts[jobScopeChildren]["running"]
}

func (s jobCountSnapshot) scopeTotal(scope, statusFilter string) int {
	if statusFilter != "" {
		return s.counts[scope][statusFilter]
	}
	total := 0
	for _, count := range s.counts[scope] {
		total += count
	}
	return total
}

func copyStatusCounts(dst, src map[string]int) {
	for status, count := range src {
		dst[status] = count
	}
}

func addStatusCounts(dst, src map[string]int) {
	for status, count := range src {
		dst[status] += count
	}
}
