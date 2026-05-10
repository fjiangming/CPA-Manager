package inspection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	codexUsageURL          = "https://chatgpt.com/backend-api/wham/usage"
	fiveHourWindowSeconds  = 18000
	weekWindowSeconds      = 604800
	defaultUserAgent       = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"
)

var quotaBodyPatterns = []string{"quota exhausted", "limit reached", "payment_required"}

func resolveProvider(file authFileEntry) string {
	p := readStr(file, "provider", "type")
	return strings.ToLower(strings.TrimSpace(p))
}

// RunInspection executes a full inspection run against the CPA management API.
func RunInspection(ctx context.Context, cpaURL, managementKey string, sch Schedule) (*HistoryRecord, error) {
	startedAt := time.Now().UnixMilli()

	files, err := fetchAuthFiles(ctx, cpaURL, managementKey)
	if err != nil {
		return &HistoryRecord{
			HistorySummary: HistorySummary{StartedAtMS: startedAt, FinishedAtMS: time.Now().UnixMilli(), Error: err.Error()},
			Schedule:       sch,
		}, err
	}

	targetType := strings.ToLower(strings.TrimSpace(sch.TargetType))
	if targetType == "" {
		targetType = "codex"
	}

	var probeSet []authFileEntry
	for _, f := range files {
		if resolveProvider(f) == targetType {
			probeSet = append(probeSet, f)
		}
	}

	sampled := pickSample(probeSet, sch.SampleSize)

	workers := sch.Workers
	if workers <= 0 {
		workers = 4
	}
	timeoutMS := sch.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 15000
	}
	threshold := sch.UsedPercentThreshold
	if threshold <= 0 {
		threshold = 100
	}
	userAgent := strings.TrimSpace(sch.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	retries := sch.Retries
	if retries < 0 {
		retries = 0
	}

	results := probeAccounts(ctx, cpaURL, managementKey, sampled, workers, timeoutMS, retries, threshold, userAgent)

	deleteCount, disableCount, enableCount, keepCount := countActions(results)

	record := &HistoryRecord{
		HistorySummary: HistorySummary{
			StartedAtMS:    startedAt,
			FinishedAtMS:   time.Now().UnixMilli(),
			TotalAccounts:  len(probeSet),
			ProbedAccounts: len(sampled),
			DeleteCount:    deleteCount,
			DisableCount:   disableCount,
			EnableCount:    enableCount,
			KeepCount:      keepCount,
		},
		AccountResults: results,
		Schedule:       sch,
	}

	if sch.AutoExecute {
		success, failed := executeActions(ctx, cpaURL, managementKey, results, sch.DeleteWorkers)
		record.Executed = true
		record.ExecuteSuccess = success
		record.ExecuteFailed = failed
	}

	return record, nil
}

// ExecuteRecordActions executes suggested actions for a stored history record.
func ExecuteRecordActions(ctx context.Context, cpaURL, managementKey string, results []AccountResult, deleteWorkers int) ([]AccountResult, int, int) {
	if deleteWorkers <= 0 {
		deleteWorkers = 4
	}
	success, failed := executeActions(ctx, cpaURL, managementKey, results, deleteWorkers)
	return results, success, failed
}

// --- CPA Management API calls ---

func fetchAuthFiles(ctx context.Context, cpaURL, managementKey string) ([]authFileEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cpaURL+"/v0/management/auth-files", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch auth-files failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Response can be {"files": [...]} or just [...]
	var wrapper struct {
		Files []authFileEntry `json:"files"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Files != nil {
		return wrapper.Files, nil
	}
	var files []authFileEntry
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, fmt.Errorf("parse auth-files response: %w", err)
	}
	return files, nil
}

func probeAccount(ctx context.Context, cpaURL, managementKey string, file authFileEntry, timeoutMS, retries int, threshold float64, userAgent string) AccountResult {
	account := toAccountResult(file)
	if account.AuthIndex == "" {
		account.Action = "keep"
		account.ActionReason = "缺少 auth_index，保留账号"
		account.Error = "缺少 auth_index"
		return account
	}

	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    userAgent,
	}
	if account.AccountID != "" {
		headers["Chatgpt-Account-Id"] = account.AccountID
	}

	payload := apiCallRequest{
		AuthIndex: account.AuthIndex,
		Method:    "GET",
		URL:       codexUsageURL,
		Header:    headers,
	}

	var result apiCallResponse
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		result, lastErr = doAPICall(ctx, cpaURL, managementKey, payload, timeoutMS)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		account.Action = "keep"
		account.ActionReason = "探测异常，保留账号"
		account.Error = lastErr.Error()
		return account
	}

	statusCode, hasStatus := parseStatusCode(result.StatusCode)
	if !hasStatus {
		account.Action = "keep"
		account.ActionReason = "探测响应缺少 status_code，保留账号"
		account.Error = "响应缺少 status_code"
		return account
	}

	sc := statusCode
	account.StatusCode = &sc

	rl := parseRateLimit(result.Body)
	usedPct := deriveUsedPercent(rl)
	bodyText := strings.ToLower(fmt.Sprintf("%v", result.Body))
	isQuota := statusCode == 402 ||
		matchesQuotaPattern(bodyText) ||
		isRateLimitReached(rl) ||
		(usedPct != nil && *usedPct >= threshold)

	d := resolveProbeAction(account, statusCode, rl, usedPct, isQuota, threshold)
	account.Action = d.Action
	account.ActionReason = d.ActionReason
	account.UsedPercent = d.UsedPercent
	account.IsQuota = d.IsQuota
	return account
}

func doAPICall(ctx context.Context, cpaURL, managementKey string, payload apiCallRequest, timeoutMS int) (apiCallResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return apiCallResponse{}, err
	}

	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, cpaURL+"/v0/management/api-call", strings.NewReader(string(data)))
	if err != nil {
		return apiCallResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout + 5*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return apiCallResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiCallResponse{}, fmt.Errorf("api-call failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiCallResponse{}, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return apiCallResponse{}, fmt.Errorf("parse api-call response: %w", err)
	}

	return apiCallResponse{
		StatusCode: raw["status_code"],
		Body:       raw["body"],
	}, nil
}

func deleteAuthFile(ctx context.Context, cpaURL, managementKey, fileName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cpaURL+"/v0/management/auth-files?name="+fileName, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delete failed: %s", resp.Status)
	}
	return nil
}

func patchAuthFileDisabled(ctx context.Context, cpaURL, managementKey, fileName string, disabled bool) error {
	payload, _ := json.Marshal(map[string]interface{}{"name": fileName, "disabled": disabled})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, cpaURL+"/v0/management/auth-files", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("patch failed: %s", resp.Status)
	}
	return nil
}

// --- Decision logic (mirrors codexInspection.ts) ---

// resolveProbeAction decides what action to take for an account.
// See: codexInspection.ts resolveProbeAction (line 607-618)
func resolveProbeAction(account AccountResult, statusCode int, rl *rateLimitInfo, usedPercent *float64, isQuota bool, threshold float64) decision {
	if d := resolveWindowAwareProbeAction(account, statusCode, rl, threshold); d != nil {
		return *d
	}
	return resolveLegacyProbeAction(account, statusCode, usedPercent, isQuota, threshold)
}

// resolveWindowAwareProbeAction uses 5h/week window info for decisions.
// See: codexInspection.ts resolveWindowAwareProbeAction (line 537-605)
func resolveWindowAwareProbeAction(account AccountResult, statusCode int, rl *rateLimitInfo, threshold float64) *decision {
	if rl == nil {
		return nil
	}
	fiveH, weekly := pickClassifiedWindows(rl)
	weeklyPct := getWindowUsedPercent(weekly)
	if weekly == nil || weeklyPct == nil {
		return nil
	}
	fiveHPct := getWindowUsedPercent(fiveH)
	weeklyOver := *weeklyPct >= threshold
	fiveHOver := fiveHPct != nil && *fiveHPct >= threshold

	if statusCode == 401 {
		return &decision{Action: "delete", ActionReason: "接口返回 401，建议删除失效账号", UsedPercent: weeklyPct, IsQuota: false}
	}
	if weeklyOver {
		if account.Disabled {
			return &decision{Action: "keep", ActionReason: "周额度达到阈值，但账号已禁用", UsedPercent: weeklyPct, IsQuota: true}
		}
		return &decision{Action: "disable", ActionReason: "周额度达到阈值，建议禁用账号", UsedPercent: weeklyPct, IsQuota: true}
	}
	if account.Disabled {
		reason := "周额度仍可用，建议立即启用账号"
		if fiveHOver {
			reason = "5 小时额度达到阈值，但周额度仍可用，建议立即启用账号"
		}
		return &decision{Action: "enable", ActionReason: reason, UsedPercent: weeklyPct, IsQuota: false}
	}
	if fiveHOver {
		return &decision{Action: "keep", ActionReason: "5 小时额度达到阈值，但周额度仍可用，暂不禁用账号", UsedPercent: weeklyPct, IsQuota: false}
	}
	return &decision{Action: "keep", ActionReason: "周额度仍可用，无需处理", UsedPercent: weeklyPct, IsQuota: false}
}

// resolveLegacyProbeAction is the fallback when no window info is available.
// See: codexInspection.ts resolveLegacyProbeAction (line 489-535)
func resolveLegacyProbeAction(account AccountResult, statusCode int, usedPercent *float64, isQuota bool, threshold float64) decision {
	overThreshold := usedPercent != nil && *usedPercent >= threshold
	if statusCode == 401 {
		return decision{Action: "delete", ActionReason: "接口返回 401，建议删除失效账号", UsedPercent: usedPercent, IsQuota: false}
	}
	if isQuota || overThreshold {
		if account.Disabled {
			reason := "额度已耗尽，但账号已禁用"
			if overThreshold {
				reason = "额度超阈值，但账号已禁用"
			}
			return decision{Action: "keep", ActionReason: reason, UsedPercent: usedPercent, IsQuota: isQuota}
		}
		reason := "额度已耗尽，建议禁用账号"
		if overThreshold {
			reason = "额度超阈值，建议禁用账号"
		}
		return decision{Action: "disable", ActionReason: reason, UsedPercent: usedPercent, IsQuota: isQuota}
	}
	if statusCode == 200 && account.Disabled {
		return decision{Action: "enable", ActionReason: "账号恢复健康，建议重新启用", UsedPercent: usedPercent, IsQuota: false}
	}
	return decision{Action: "keep", ActionReason: "无需处理", UsedPercent: usedPercent, IsQuota: false}
}

// --- Helper functions ---

func probeAccounts(ctx context.Context, cpaURL, managementKey string, files []authFileEntry, workers, timeoutMS, retries int, threshold float64, userAgent string) []AccountResult {
	if len(files) == 0 {
		return nil
	}
	results := make([]AccountResult, len(files))
	var mu sync.Mutex
	cursor := 0

	var wg sync.WaitGroup
	sem := workers
	if sem <= 0 {
		sem = 4
	}
	if sem > len(files) {
		sem = len(files)
	}
	for i := 0; i < sem; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				idx := cursor
				cursor++
				mu.Unlock()
				if idx >= len(files) {
					return
				}
				if ctx.Err() != nil {
					results[idx] = AccountResult{Action: "keep", ActionReason: "巡检已取消", Error: ctx.Err().Error()}
					continue
				}
				results[idx] = probeAccount(ctx, cpaURL, managementKey, files[idx], timeoutMS, retries, threshold, userAgent)
			}
		}()
	}
	wg.Wait()
	return results
}

func executeActions(ctx context.Context, cpaURL, managementKey string, results []AccountResult, deleteWorkers int) (int, int) {
	if deleteWorkers <= 0 {
		deleteWorkers = 4
	}
	var actionable []int
	for i, r := range results {
		if r.Action == "delete" || r.Action == "disable" || r.Action == "enable" {
			actionable = append(actionable, i)
		}
	}
	if len(actionable) == 0 {
		return 0, 0
	}

	var mu sync.Mutex
	success, failed := 0, 0
	cursor := 0

	var wg sync.WaitGroup
	sem := deleteWorkers
	if sem > len(actionable) {
		sem = len(actionable)
	}
	for i := 0; i < sem; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				idx := cursor
				cursor++
				mu.Unlock()
				if idx >= len(actionable) {
					return
				}
				ri := actionable[idx]
				r := &results[ri]
				var err error
				switch r.Action {
				case "delete":
					err = deleteAuthFile(ctx, cpaURL, managementKey, r.FileName)
				case "disable":
					err = patchAuthFileDisabled(ctx, cpaURL, managementKey, r.FileName, true)
				case "enable":
					err = patchAuthFileDisabled(ctx, cpaURL, managementKey, r.FileName, false)
				}
				r.Executed = true
				if err != nil {
					r.ExecuteError = err.Error()
					mu.Lock()
					failed++
					mu.Unlock()
				} else {
					r.ExecuteSuccess = true
					mu.Lock()
					success++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return success, failed
}

func toAccountResult(file authFileEntry) AccountResult {
	name := readStr(file, "name")
	if name == "" {
		name = readStr(file, "id")
	}
	if name == "" {
		name = readStr(file, "auth_index")
	}
	if name == "" {
		name = "unknown-auth-file"
	}

	display := readStr(file, "account")
	if display == "" {
		display = readStr(file, "email")
	}
	if display == "" {
		display = readStr(file, "label")
	}
	if display == "" {
		display = readStr(file, "name")
	}
	if display == "" {
		display = readStr(file, "id")
	}
	if display == "" {
		display = "-"
	}

	authIndex := readStr(file, "auth_index")
	if authIndex == "" {
		authIndex = readStr(file, "authIndex")
	}

	provider := readStr(file, "provider")
	if provider == "" {
		provider = readStr(file, "type")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))

	disabled := isDisabled(file)
	accountID := resolveAccountID(file)

	return AccountResult{
		Key:            fmt.Sprintf("%s::%s", name, coalesce(authIndex, "-")),
		FileName:       name,
		DisplayAccount: display,
		AuthIndex:      authIndex,
		AccountID:      accountID,
		Provider:       provider,
		Disabled:       disabled,
		Action:         "keep",
	}
}

func readStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func isDisabled(file authFileEntry) bool {
	if v, ok := file["disabled"]; ok {
		switch d := v.(type) {
		case bool:
			return d
		case float64:
			return d != 0
		case string:
			l := strings.ToLower(strings.TrimSpace(d))
			return l == "true" || l == "1"
		}
	}
	status := strings.ToLower(readStr(file, "status", "state"))
	return status == "disabled" || status == "inactive"
}

func resolveAccountID(file authFileEntry) string {
	for _, key := range []string{"chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"} {
		if v := readStr(file, key); v != "" {
			return v
		}
	}
	if meta, ok := file["metadata"].(map[string]interface{}); ok {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"} {
			if v, ok := meta[key]; ok && v != nil {
				s := strings.TrimSpace(fmt.Sprintf("%v", v))
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func pickClassifiedWindows(rl *rateLimitInfo) (*usageWindow, *usageWindow) {
	if rl == nil {
		return nil, nil
	}
	primary := rl.PrimaryWindow
	secondary := rl.SecondaryWindow
	var fiveH, weekly *usageWindow
	for _, w := range []*usageWindow{primary, secondary} {
		if w == nil {
			continue
		}
		sec := getWindowSeconds(w)
		if sec == fiveHourWindowSeconds && fiveH == nil {
			fiveH = w
		} else if sec == weekWindowSeconds && weekly == nil {
			weekly = w
		}
	}
	if fiveH == nil && primary != nil && primary != weekly {
		fiveH = primary
	}
	if weekly == nil && secondary != nil && secondary != fiveH {
		weekly = secondary
	}
	return fiveH, weekly
}

func getWindowUsedPercent(w *usageWindow) *float64 {
	if w == nil || w.UsedPercent == nil {
		return nil
	}
	v := *w.UsedPercent
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func getWindowSeconds(w *usageWindow) float64 {
	if w == nil || w.LimitWindowSeconds == nil {
		return 0
	}
	return *w.LimitWindowSeconds
}

func deriveUsedPercent(rl *rateLimitInfo) *float64 {
	if rl == nil {
		return nil
	}
	var maxVal *float64
	for _, w := range []*usageWindow{rl.PrimaryWindow, rl.SecondaryWindow} {
		v := getWindowUsedPercent(w)
		if v != nil && (maxVal == nil || *v > *maxVal) {
			maxVal = v
		}
	}
	return maxVal
}

func isRateLimitReached(rl *rateLimitInfo) bool {
	if rl == nil {
		return false
	}
	if rl.Allowed != nil && !*rl.Allowed {
		return true
	}
	if rl.LimitReached != nil && *rl.LimitReached {
		return true
	}
	for _, w := range []*usageWindow{rl.PrimaryWindow, rl.SecondaryWindow} {
		v := getWindowUsedPercent(w)
		if v != nil && *v >= 100 {
			return true
		}
	}
	return false
}

func matchesQuotaPattern(body string) bool {
	for _, p := range quotaBodyPatterns {
		if strings.Contains(body, p) {
			return true
		}
	}
	return false
}

func parseStatusCode(raw interface{}) (int, bool) {
	if raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n, true
		}
		return 0, false
	}
	return 0, false
}

func parseRateLimit(body interface{}) *rateLimitInfo {
	if body == nil {
		return nil
	}
	var m map[string]interface{}
	switch b := body.(type) {
	case map[string]interface{}:
		m = b
	case string:
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			return nil
		}
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil
		}
	}
	rlRaw, ok := m["rate_limit"]
	if !ok {
		rlRaw, ok = m["rateLimit"]
	}
	if !ok || rlRaw == nil {
		return nil
	}
	data, err := json.Marshal(rlRaw)
	if err != nil {
		return nil
	}
	var rl rateLimitInfo
	if err := json.Unmarshal(data, &rl); err != nil {
		return nil
	}
	return &rl
}

func countActions(results []AccountResult) (del, dis, en, keep int) {
	for _, r := range results {
		switch r.Action {
		case "delete":
			del++
		case "disable":
			dis++
		case "enable":
			en++
		default:
			keep++
		}
	}
	return
}

func pickSample(items []authFileEntry, sampleSize int) []authFileEntry {
	if sampleSize <= 0 || sampleSize >= len(items) {
		cp := make([]authFileEntry, len(items))
		copy(cp, items)
		return cp
	}
	shuffled := make([]authFileEntry, len(items))
	copy(shuffled, items)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled[:sampleSize]
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Ensure errors package is used.
var _ = errors.New
