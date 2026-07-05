package admin

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	page := 1
	limit := 25
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// Parse show filter (parents, children, all)
	showFilter := r.URL.Query().Get("show")
	if showFilter != "children" && showFilter != "all" {
		showFilter = "parents" // default: hide child jobs
	}

	// Parse status filter
	statusFilter := r.URL.Query().Get("status")
	validStatuses := map[string]bool{
		"pending":   true,
		"running":   true,
		"paused":    true,
		"completed": true,
		"failed":    true,
		"cancelled": true,
		"archived":  true,
	}
	if !validStatuses[statusFilter] {
		statusFilter = "" // no status filter
	}

	// Build WHERE clause combining show + status filters
	var conditions []string
	var args []interface{}
	switch showFilter {
	case "children":
		conditions = append(conditions, "COALESCE(parent_execution_id, '') != ''")
	case "all":
		// no condition
	default: // "parents"
		conditions = append(conditions, "COALESCE(parent_execution_id, '') = ''")
	}
	if statusFilter != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, statusFilter)
	}
	// Filter by specific parent job ID
	parentFilter := r.URL.Query().Get("parent")
	if parentFilter != "" {
		conditions = append(conditions, "parent_execution_id = ?")
		args = append(args, parentFilter)
	}
	var whereClause string
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	countSnapshot, err := s.loadJobCountSnapshot(r.Context())
	if err != nil {
		log.Printf("Error loading job counts: %v", err)
		writeJSONError(w, "Failed to count jobs", http.StatusInternalServerError)
		return
	}

	// Parse sort parameters (allowlist prevents SQL injection)
	allowedSortColumns := map[string]string{
		"status":     "status",
		"tokens":     "tokens_total",
		"cost":       "cost",
		"created_at": "created_at",
	}
	sortParam := r.URL.Query().Get("sort")
	sqlSortColumn, ok := allowedSortColumns[sortParam]
	if !ok {
		sqlSortColumn = "created_at"
	}
	sqlSortDir := "DESC"
	if r.URL.Query().Get("dir") == "asc" {
		sqlSortDir = "ASC"
	}
	orderClause := fmt.Sprintf(" ORDER BY %s %s", sqlSortColumn, sqlSortDir)

	// Get total count
	totalItems := countSnapshot.totalItems(showFilter, statusFilter)
	if parentFilter != "" {
		if err := s.db.QueryRow("SELECT COUNT(*) FROM jobs"+whereClause, args...).Scan(&totalItems); err != nil {
			log.Printf("Error counting jobs for parent filter: %v", err)
			writeJSONError(w, "Failed to count jobs", http.StatusInternalServerError)
			return
		}
	}

	// Calculate pagination
	totalPages := (totalItems + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * limit

	// Build page numbers to show (max 5 pages centered around current)
	var pages []int
	startPage := page - 2
	endPage := page + 2
	if startPage < 1 {
		startPage = 1
		endPage = min(5, totalPages)
	}
	if endPage > totalPages {
		endPage = totalPages
		startPage = max(1, totalPages-4)
	}
	for i := startPage; i <= endPage; i++ {
		pages = append(pages, i)
	}

	pagination := Pagination{
		Page:       page,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		StartItem:  offset + 1,
		EndItem:    min(offset+limit, totalItems),
		Pages:      pages,
	}

	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := s.db.Query(`
		SELECT id, query, model, status, created_at, tokens_total, cost,
		       COALESCE(request_data, ''), COALESCE(parent_execution_id, '')
		FROM jobs`+whereClause+orderClause+`
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var jobsList []JobWithPrompt
	for rows.Next() {
		var j storage.Job
		var requestData, parentExecID string
		if err := rows.Scan(&j.ID, &j.Description, &j.Model, &j.Status, &j.CreatedAt, &j.TokensTotal, &j.Cost, &requestData, &parentExecID); err != nil {
			log.Printf("Error scanning job row: %v", err)
			continue
		}
		jobsList = append(jobsList, JobWithPrompt{
			Job:               j,
			InputPrompt:       extractInputPrompt(requestData),
			IsChild:           parentExecID != "",
			ParentExecutionID: parentExecID,
			DirectTokens:      j.TokensTotal,
			DisplayTokens:     j.TokensTotal,
			DirectCost:        j.Cost,
			DisplayCost:       j.Cost,
			DescendantCount:   0,
		})
	}

	if len(jobsList) > 0 {
		ids := make([]string, 0, len(jobsList))
		for _, row := range jobsList {
			ids = append(ids, row.ID)
		}
		if costSummaries, err := s.loadExecutionCostSummaries(ids); err == nil {
			for i := range jobsList {
				if summary, ok := costSummaries[jobsList[i].ID]; ok {
					jobsList[i].DirectTokens = summary.DirectTokens
					jobsList[i].ChildTokens = summary.ChildTokens
					jobsList[i].DisplayTokens = summary.TotalTokens
					jobsList[i].DirectCost = summary.DirectCost
					jobsList[i].ChildCost = summary.ChildCost
					jobsList[i].DisplayCost = summary.TotalCost
					jobsList[i].DescendantCount = summary.DescendantCount
				}
			}
		} else {
			log.Printf("Warning: Failed to compute execution cost summaries for jobs list: %v", err)
		}
	}

	// Worker pool stats: worker count from config, active from one count snapshot.
	workerCount := s.jobManager.Config().WorkerCount
	activeWorkflows := countSnapshot.activeRunningAll()

	// Status counts respecting scope filter (for stats grid + status filter buttons).
	statusCounts := countSnapshot.statusCountsForShow(showFilter)
	var scopedTotal int
	for _, c := range statusCounts {
		scopedTotal += c
	}

	// Scope counts respecting status filter (for scope filter buttons)
	parentCount, childCount, allCount := countSnapshot.scopeCounts(statusFilter)

	data := map[string]interface{}{
		"Jobs":            jobsList,
		"Pagination":      pagination,
		"ShowFilter":      showFilter,
		"StatusFilter":    statusFilter,
		"ActiveWorkflows": activeWorkflows,
		"PoolCapacity":    workerCount,
		"PendingJobs":     statusCounts["pending"],
		"RunningJobs":     statusCounts["running"],
		"PausedJobs":      statusCounts["paused"],
		"CompletedJobs":   statusCounts["completed"],
		"FailedJobs":      statusCounts["failed"],
		"CancelledJobs":   statusCounts["cancelled"],
		"ScopedTotal":     scopedTotal,
		"ParentCount":     parentCount,
		"ChildCount":      childCount,
		"AllCount":        allCount,
		"HasActiveJobs":   statusCounts["pending"] > 0 || statusCounts["running"] > 0,
	}

	writeJSONResponse(w, data)
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	data, err := s.buildJobDetailData(r.Context(), jobID)
	if err != nil {
		writeJSONError(w, "Job not found", http.StatusNotFound)
		return
	}
	writeJSONResponse(w, data)
}
