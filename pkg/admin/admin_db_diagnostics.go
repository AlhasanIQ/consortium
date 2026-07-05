package admin

import (
	"net/http"

	"github.com/alhasaniq/consortium/pkg/storage"
)

func (s *Server) handleDBDiagnostics(w http.ResponseWriter, r *http.Request) {
	includeTables := r.URL.Query().Get("tables") == "true" || r.URL.Query().Get("include") == "tables"
	data := map[string]interface{}{
		"Database": s.storage.DBDiagnosticsWithOptions(r.Context(), storage.DBDiagnosticsOptions{
			IncludeTables: includeTables,
		}),
	}
	if s.jobManager != nil {
		data["Workers"] = s.jobManager.WorkerStats()
	}
	writeJSONResponse(w, data)
}
