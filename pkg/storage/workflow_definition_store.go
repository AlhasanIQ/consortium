package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/alhasaniq/consortium/pkg/seeds"
)

// CreateWorkflow creates a new workflow definition
func (s *Storage) CreateWorkflow(wf *WorkflowDefinition) error {
	query := `
		INSERT INTO workflows (id, name, description, definition, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	version := wf.Version
	if version < 1 {
		version = 1
	}
	_, err := s.db.Exec(query, wf.ID, wf.Name, wf.Description, wf.Definition, version, now, now)
	if err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}
	wf.Version = version
	return nil
}

// GetWorkflow retrieves a workflow by ID
func (s *Storage) GetWorkflow(id string) (*WorkflowDefinition, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), definition, COALESCE(version, 1), created_at, updated_at
		FROM workflows WHERE id = ?
	`
	wf := &WorkflowDefinition{}
	err := s.db.QueryRow(query, id).Scan(
		&wf.ID, &wf.Name, &wf.Description, &wf.Definition, &wf.Version,
		&wf.CreatedAt, &wf.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("workflow not found: %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}
	return wf, nil
}

// UpdateWorkflow updates an existing workflow definition
func (s *Storage) UpdateWorkflow(wf *WorkflowDefinition) error {
	var (
		query  string
		args   []interface{}
		result sql.Result
		err    error
	)
	now := time.Now()
	if wf.Version > 0 {
		query = `
			UPDATE workflows
			SET name = ?, description = ?, definition = ?, version = version + 1, updated_at = ?
			WHERE id = ? AND version = ?
		`
		args = []interface{}{wf.Name, wf.Description, wf.Definition, now, wf.ID, wf.Version}
	} else {
		query = `
			UPDATE workflows
			SET name = ?, description = ?, definition = ?, version = COALESCE(version, 1) + 1, updated_at = ?
			WHERE id = ?
		`
		args = []interface{}{wf.Name, wf.Description, wf.Definition, now, wf.ID}
	}
	result, err = s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		if wf.Version > 0 {
			return ErrConflict
		}
		return ErrNotFound
	}
	wf.Version++
	return nil
}

// DeleteWorkflow deletes a workflow definition
func (s *Storage) DeleteWorkflow(id string) error {
	query := `DELETE FROM workflows WHERE id = ?`
	_, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}
	return nil
}

// ListWorkflows retrieves all workflow definitions
func (s *Storage) ListWorkflows(limit int) ([]WorkflowDefinition, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), definition, COALESCE(version, 1), created_at, updated_at
		FROM workflows
		ORDER BY updated_at DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var workflows []WorkflowDefinition
	for rows.Next() {
		var wf WorkflowDefinition
		err := rows.Scan(&wf.ID, &wf.Name, &wf.Description, &wf.Definition, &wf.Version, &wf.CreatedAt, &wf.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow: %w", err)
		}
		workflows = append(workflows, wf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflows: %w", err)
	}
	return workflows, nil
}

// GetSeedWorkflows returns metadata about all available seed workflows.
// Delegates to the seeds package which owns the embedded definitions.
func (s *Storage) GetSeedWorkflows() ([]SeedWorkflowInfo, error) {
	return seeds.GetAllInfo(), nil
}

// GetSeedWorkflowJSONByID returns the embedded seed workflow JSON by workflow ID.
// Delegates to the seeds package which owns the embedded definitions.
func (s *Storage) GetSeedWorkflowJSONByID(workflowID string) (string, error) {
	result, err := seeds.GetJSONByID(workflowID)
	if err != nil {
		return "", ErrNotFound
	}
	return result, nil
}

// seedWorkflows seeds all workflow definitions if they don't exist
func (s *Storage) seedWorkflows() error {
	// List of all seed workflows to load
	allSeeds := seeds.GetAllDefinitions()

	for _, seed := range allSeeds {
		if err := s.seedWorkflow(seed); err != nil {
			log.Printf("Warning: failed to seed workflow: %v", err)
			// Continue with other seeds even if one fails
		}
	}

	return nil
}

// seedWorkflow seeds a single workflow from JSON if it doesn't exist
func (s *Storage) seedWorkflow(seed seeds.Definition) error {
	// Check if workflow already exists
	existing, err := s.GetWorkflow(seed.ID)
	if err == nil {
		// Workflow exists — update if definition changed
		if existing.Definition != seed.JSON {
			existing.Name = seed.Name
			existing.Description = seed.Description
			existing.Definition = seed.JSON
			if err := s.UpdateWorkflow(existing); err != nil {
				return fmt.Errorf("failed to update seed workflow %s: %w", seed.ID, err)
			}
			log.Printf("Updated seed workflow: %s (id: %s)", seed.Name, seed.ID)
		}
		return nil
	}

	// Create the workflow definition
	wf := &WorkflowDefinition{
		ID:          seed.ID,
		Name:        seed.Name,
		Description: seed.Description,
		Definition:  seed.JSON,
	}

	if err := s.CreateWorkflow(wf); err != nil {
		return fmt.Errorf("failed to seed workflow %s: %w", seed.ID, err)
	}

	log.Printf("Seeded workflow: %s (id: %s)", seed.Name, seed.ID)
	return nil
}
