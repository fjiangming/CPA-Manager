// Package inspection implements scheduled Codex account inspection.
//
// The decision logic in this package mirrors the TypeScript implementation in
// src/features/monitoring/codexInspection.ts. When the frontend logic changes,
// the corresponding functions here (resolveWindowAwareProbeAction,
// resolveLegacyProbeAction, etc.) must be updated accordingly.
package inspection

// Schedule holds the scheduled inspection configuration.
type Schedule struct {
	Enabled              bool    `json:"enabled"`
	IntervalHours        int     `json:"intervalHours"`        // 2,4,6,8,12,24
	TargetType           string  `json:"targetType"`           // default "codex"
	Workers              int     `json:"workers"`              // probe concurrency
	DeleteWorkers        int     `json:"deleteWorkers"`        // execute concurrency
	TimeoutMS            int     `json:"timeoutMs"`            // probe timeout in ms
	Retries              int     `json:"retries"`              // probe retry count
	UsedPercentThreshold float64 `json:"usedPercentThreshold"` // 0-100
	SampleSize           int     `json:"sampleSize"`           // 0 = all
	UserAgent            string  `json:"userAgent"`
	AutoExecute          bool    `json:"autoExecute"` // false = record only
	UpdatedAtMS          int64   `json:"updatedAtMs"`
}

// DefaultSchedule returns a schedule with sensible defaults.
func DefaultSchedule() Schedule {
	return Schedule{
		Enabled:              false,
		IntervalHours:        6,
		TargetType:           "codex",
		Workers:              4,
		DeleteWorkers:        4,
		TimeoutMS:            15000,
		Retries:              0,
		UsedPercentThreshold: 100,
		SampleSize:           0,
		UserAgent:            "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
		AutoExecute:          false,
	}
}

// AccountResult holds the probe result for a single auth file account.
// Mirrors CodexInspectionResultItem in codexInspection.ts.
type AccountResult struct {
	Key            string   `json:"key"`
	FileName       string   `json:"fileName"`
	DisplayAccount string   `json:"displayAccount"`
	AuthIndex      string   `json:"authIndex"`
	AccountID      string   `json:"accountId"`
	Provider       string   `json:"provider"`
	Disabled       bool     `json:"disabled"`
	StatusCode     *int     `json:"statusCode"`
	UsedPercent    *float64 `json:"usedPercent"`
	IsQuota        bool     `json:"isQuota"`
	Action         string   `json:"action"`       // keep|delete|disable|enable
	ActionReason   string   `json:"actionReason"`
	Error          string   `json:"error,omitempty"`
	// Execution results (populated after executing suggested actions).
	Executed       bool   `json:"executed"`
	ExecuteSuccess bool   `json:"executeSuccess"`
	ExecuteError   string `json:"executeError,omitempty"`
}

// HistorySummary is the lightweight view returned in list queries.
type HistorySummary struct {
	ID             int64  `json:"id"`
	Trigger        string `json:"trigger"` // "scheduled" | "manual_backend"
	StartedAtMS    int64  `json:"startedAtMs"`
	FinishedAtMS   int64  `json:"finishedAtMs"`
	TotalAccounts  int    `json:"totalAccounts"`
	ProbedAccounts int    `json:"probedAccounts"`
	DeleteCount    int    `json:"deleteCount"`
	DisableCount   int    `json:"disableCount"`
	EnableCount    int    `json:"enableCount"`
	KeepCount      int    `json:"keepCount"`
	Executed       bool   `json:"executed"`
	ExecuteSuccess int    `json:"executeSuccess"`
	ExecuteFailed  int    `json:"executeFailed"`
	Error          string `json:"error,omitempty"`
}

// HistoryRecord is the full detail including per-account results.
type HistoryRecord struct {
	HistorySummary
	AccountResults []AccountResult `json:"accountResults"`
	Schedule       Schedule        `json:"schedule"` // snapshot of settings used
}

// SchedulerStatus reports the scheduler runtime state.
type SchedulerStatus struct {
	Running   bool  `json:"running"`
	LastRunAt int64 `json:"lastRunAtMs"`
	NextRunAt int64 `json:"nextRunAtMs"`
}

// --- Internal types used by inspector.go ---

// authFileEntry represents a single auth file from the CPA management API.
type authFileEntry = map[string]interface{}

// apiCallRequest is the payload sent to POST /v0/management/api-call.
type apiCallRequest struct {
	AuthIndex string            `json:"authIndex,omitempty"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header,omitempty"`
	Data      string            `json:"data,omitempty"`
}

// apiCallResponse is the response from POST /v0/management/api-call.
type apiCallResponse struct {
	StatusCode    interface{}                `json:"status_code"`
	HasStatusCode bool                       `json:"-"` // derived
	Header        map[string][]string        `json:"header"`
	Body          interface{}                `json:"body"`
	BodyText      string                     `json:"bodyText"`
}

// rateLimitInfo mirrors CodexRateLimitInfo.
type rateLimitInfo struct {
	Allowed        *bool        `json:"allowed"`
	LimitReached   *bool        `json:"limit_reached"`
	PrimaryWindow  *usageWindow `json:"primary_window"`
	SecondaryWindow *usageWindow `json:"secondary_window"`
}

// usageWindow mirrors CodexUsageWindow.
type usageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *float64 `json:"limit_window_seconds"`
}

// decision is the internal decision struct.
type decision struct {
	Action       string
	ActionReason string
	UsedPercent  *float64
	IsQuota      bool
}
