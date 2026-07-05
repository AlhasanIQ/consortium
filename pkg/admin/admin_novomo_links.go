package admin

import (
	"net/url"
	"strings"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// NovomoURL describes a frontend-v2 deep link for a persisted Novomo entity.
type NovomoURL struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	ID    string `json:"id"`
	URL   string `json:"url"`
}

// AgentRunSummary is the admin transport shape for Novomo-backed node runs.
type AgentRunSummary struct {
	storage.AgentRun
	NovomoURLs []NovomoURL `json:"novomo_urls,omitempty"`
}

func novomoFrontendV2URL() string {
	return appenv.NovomoFrontendV2URL()
}

func enrichAgentRunsWithNovomoURLs(runs []storage.AgentRun, baseURL string) []AgentRunSummary {
	if len(runs) == 0 {
		return nil
	}
	out := make([]AgentRunSummary, 0, len(runs))
	for _, run := range runs {
		out = append(out, AgentRunSummary{
			AgentRun:   run,
			NovomoURLs: buildNovomoURLs(run, baseURL),
		})
	}
	return out
}

func buildNovomoURLs(run storage.AgentRun, baseURL string) []NovomoURL {
	runKind := strings.TrimSpace(strings.ToLower(run.RunKind))
	if runKind == "" {
		runKind = "agent_run"
	}

	var links []NovomoURL
	add := func(kind, label, route, id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		links = append(links, NovomoURL{
			Kind:  kind,
			Label: label,
			ID:    id,
			URL:   novomoURL(baseURL, route, id),
		})
	}

	if runKind == "novo_run" {
		add("novo_run", "NovoRun", "/novo-runs/", run.ExternalRunID)
		add("task", "Task", "/tasks/", run.ExternalTaskID)
		add("job_run", "Novomo JobRun", "/job-runs/", run.ExternalJobRunID)
		return links
	}

	add("job", "Novomo Job", "/jobs/", run.ExternalRunID)
	add("job_run", "Novomo JobRun", "/job-runs/", run.ExternalJobRunID)
	add("task", "Task", "/tasks/", run.ExternalTaskID)
	return links
}

func novomoURL(baseURL, route, id string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	id = strings.TrimSpace(id)
	if baseURL == "" || id == "" {
		return ""
	}
	return baseURL + route + url.PathEscape(id)
}
