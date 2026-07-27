package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func (a *App) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	if a.reports == nil {
		writeError(w, http.StatusServiceUnavailable, "Reports are not configured")
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/reports"), "/")
	if path == "" {
		reports, err := a.reports.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, friendlyError(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"reports":       reports,
			"timezone":      a.reports.Timezone(),
			"retentionDays": a.reports.RetentionDays(),
		})
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		report, err := a.reports.Read(parts[0])
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Report not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, friendlyError(err))
			return
		}
		writeJSON(w, http.StatusOK, report)
		return
	}
	if len(parts) == 2 && parts[1] == "download" {
		a.downloadReport(w, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "Report not found")
}

func (a *App) downloadReport(w http.ResponseWriter, date string) {
	file, info, err := a.reports.Open(date)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Report not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, friendlyError(err))
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="palctrl-report-%s.csv"`, date))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}
