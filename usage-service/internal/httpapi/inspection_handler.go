package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager/usage-service/internal/inspection"
)

func (s *Server) handleInspection(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v0/management/inspection")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "schedule" && r.Method == http.MethodGet:
		s.handleGetSchedule(w, r)
	case path == "schedule" && r.Method == http.MethodPut:
		s.handleUpdateSchedule(w, r)
	case path == "history" && r.Method == http.MethodGet:
		s.handleListHistory(w, r)
	case strings.HasSuffix(path, "/execute") && r.Method == http.MethodPost:
		s.handleExecuteHistory(w, r, path)
	case strings.HasPrefix(path, "history/") && r.Method == http.MethodGet:
		s.handleGetHistory(w, r, path)
	case path == "run" && r.Method == http.MethodPost:
		s.handleRunNow(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, _ *http.Request) {
	sch := s.scheduler.GetSchedule()
	status := s.scheduler.GetStatus()
	writeJSON(w, http.StatusOK, map[string]any{"schedule": sch, "status": status})
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	var sch inspection.Schedule
	if err := json.NewDecoder(r.Body).Decode(&sch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body: " + err.Error()})
		return
	}
	if err := s.scheduler.UpdateSchedule(r.Context(), sch); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update schedule failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schedule": s.scheduler.GetSchedule(), "status": s.scheduler.GetStatus()})
}

func (s *Server) handleListHistory(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	rows, err := s.store.ListInspectionHistory(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list history failed: " + err.Error()})
		return
	}
	records := make([]inspection.HistorySummary, 0, len(rows))
	for _, row := range rows {
		records = append(records, inspection.HistorySummary{
			ID:             row.ID,
			Trigger:        row.Trigger,
			StartedAtMS:    row.StartedAtMS,
			FinishedAtMS:   row.FinishedAtMS,
			TotalAccounts:  row.TotalAccounts,
			ProbedAccounts: row.ProbedAccounts,
			DeleteCount:    row.DeleteCount,
			DisableCount:   row.DisableCount,
			EnableCount:    row.EnableCount,
			KeepCount:      row.KeepCount,
			Executed:       row.Executed,
			ExecuteSuccess: row.ExecuteSuccess,
			ExecuteFailed:  row.ExecuteFailed,
			Error:          row.Error,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request, path string) {
	idStr := strings.TrimPrefix(path, "history/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid history id"})
		return
	}
	row, err := s.store.GetInspectionHistory(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get history failed: " + err.Error()})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "history record not found"})
		return
	}

	var accountResults []inspection.AccountResult
	_ = json.Unmarshal([]byte(row.DetailsJSON), &accountResults)
	var schedule inspection.Schedule
	_ = json.Unmarshal([]byte(row.ScheduleJSON), &schedule)

	record := inspection.HistoryRecord{
		HistorySummary: inspection.HistorySummary{
			ID:             row.ID,
			Trigger:        row.Trigger,
			StartedAtMS:    row.StartedAtMS,
			FinishedAtMS:   row.FinishedAtMS,
			TotalAccounts:  row.TotalAccounts,
			ProbedAccounts: row.ProbedAccounts,
			DeleteCount:    row.DeleteCount,
			DisableCount:   row.DisableCount,
			EnableCount:    row.EnableCount,
			KeepCount:      row.KeepCount,
			Executed:       row.Executed,
			ExecuteSuccess: row.ExecuteSuccess,
			ExecuteFailed:  row.ExecuteFailed,
			Error:          row.Error,
		},
		AccountResults: accountResults,
		Schedule:       schedule,
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": record})
}

func (s *Server) handleExecuteHistory(w http.ResponseWriter, r *http.Request, path string) {
	idStr := strings.TrimPrefix(path, "history/")
	idStr = strings.TrimSuffix(idStr, "/execute")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid history id"})
		return
	}
	row, err := s.store.GetInspectionHistory(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get history failed: " + err.Error()})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "history record not found"})
		return
	}
	if row.Executed {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "actions already executed"})
		return
	}

	setup, ok, sErr := s.store.LoadSetup(r.Context())
	if sErr != nil || !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "setup not found"})
		return
	}

	var results []inspection.AccountResult
	_ = json.Unmarshal([]byte(row.DetailsJSON), &results)

	var schedule inspection.Schedule
	_ = json.Unmarshal([]byte(row.ScheduleJSON), &schedule)
	dw := schedule.DeleteWorkers
	if dw <= 0 {
		dw = 4
	}

	results, success, failed := inspection.ExecuteRecordActions(r.Context(), setup.CPAUpstreamURL, setup.ManagementKey, results, dw)

	detailsData, _ := json.Marshal(results)
	if dbErr := s.store.UpdateInspectionHistoryExecution(r.Context(), id, string(detailsData), success, failed); dbErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "save execution result failed: " + dbErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": success, "failed": failed})
}

func (s *Server) handleRunNow(w http.ResponseWriter, r *http.Request) {
	record, err := s.scheduler.RunNow(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": fmt.Sprintf("run inspection failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": record})
}
