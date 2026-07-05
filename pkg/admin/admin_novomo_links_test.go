package admin

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestEnrichAgentRunsWithNovomoURLs(t *testing.T) {
	runs := []storage.AgentRun{
		{
			ID:               "row-agent",
			RunKind:          "agent_run",
			ExternalRunID:    "job-123",
			ExternalJobRunID: "jobrun-123",
		},
		{
			ID:             "row-superagent",
			RunKind:        "novo_run",
			ExternalRunID:  "nr-123",
			ExternalTaskID: "task-123",
		},
	}

	got := enrichAgentRunsWithNovomoURLs(runs, "http://127.0.0.1:5174/")

	if len(got) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(got))
	}
	assertNovomoLink(t, got[0].NovomoURLs, "job", "Novomo Job", "job-123", "http://127.0.0.1:5174/jobs/job-123")
	assertNovomoLink(t, got[0].NovomoURLs, "job_run", "Novomo JobRun", "jobrun-123", "http://127.0.0.1:5174/job-runs/jobrun-123")
	assertNovomoLink(t, got[1].NovomoURLs, "novo_run", "NovoRun", "nr-123", "http://127.0.0.1:5174/novo-runs/nr-123")
	assertNovomoLink(t, got[1].NovomoURLs, "task", "Task", "task-123", "http://127.0.0.1:5174/tasks/task-123")
}

func assertNovomoLink(t *testing.T, links []NovomoURL, kind, label, id, url string) {
	t.Helper()
	for _, link := range links {
		if link.Kind == kind {
			if link.Label != label || link.ID != id || link.URL != url {
				t.Fatalf("unexpected %s link: %+v", kind, link)
			}
			return
		}
	}
	t.Fatalf("missing %s link in %+v", kind, links)
}
