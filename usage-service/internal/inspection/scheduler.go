package inspection

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

// Scheduler manages timed inspection execution.
type Scheduler struct {
	store         *store.Store
	mu            sync.Mutex
	cancel        context.CancelFunc
	schedule      Schedule
	status        SchedulerStatus
	cpaURL        string
	managementKey string
	running       bool
}

// NewScheduler creates a new scheduler backed by the given store.
func NewScheduler(s *store.Store) *Scheduler {
	return &Scheduler{
		store:    s,
		schedule: DefaultSchedule(),
	}
}

// Start loads the schedule from the database and begins the timer loop.
func (s *Scheduler) Start(ctx context.Context, cpaURL, managementKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
	s.cpaURL = cpaURL
	s.managementKey = managementKey

	if raw, ok, err := s.store.LoadInspectionSchedule(ctx); err == nil && ok {
		var sch Schedule
		if json.Unmarshal([]byte(raw), &sch) == nil {
			s.schedule = sch
		}
	}

	if !s.schedule.Enabled {
		s.status.Running = false
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	s.status.Running = true
	s.status.NextRunAt = time.Now().Add(s.interval()).UnixMilli()
	go s.loop(runCtx)
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
}

func (s *Scheduler) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = false
	s.status.Running = false
	s.status.NextRunAt = 0
}

// UpdateSchedule saves and applies a new schedule.
func (s *Scheduler) UpdateSchedule(ctx context.Context, sch Schedule) error {
	sch.UpdatedAtMS = time.Now().UnixMilli()
	data, err := json.Marshal(sch)
	if err != nil {
		return err
	}
	if err := s.store.SaveInspectionSchedule(ctx, string(data), sch.UpdatedAtMS); err != nil {
		return err
	}
	s.mu.Lock()
	s.schedule = sch
	wasRunning := s.running
	cpaURL := s.cpaURL
	key := s.managementKey
	s.mu.Unlock()

	if wasRunning {
		s.Stop()
	}
	if sch.Enabled && cpaURL != "" && key != "" {
		s.Start(ctx, cpaURL, key)
	}
	return nil
}

// UpdateCPAConfig updates the CPA connection settings used by the scheduler.
func (s *Scheduler) UpdateCPAConfig(ctx context.Context, cpaURL, managementKey string) {
	s.mu.Lock()
	oldURL := s.cpaURL
	oldKey := s.managementKey
	s.cpaURL = cpaURL
	s.managementKey = managementKey
	isEnabled := s.schedule.Enabled
	isRunning := s.running
	s.mu.Unlock()

	if isEnabled && (oldURL != cpaURL || oldKey != managementKey) {
		if isRunning {
			s.Stop()
		}
		if cpaURL != "" && managementKey != "" {
			s.Start(ctx, cpaURL, managementKey)
		}
	}
}

// RunNow triggers one inspection immediately.
func (s *Scheduler) RunNow(ctx context.Context) (*HistoryRecord, error) {
	s.mu.Lock()
	sch := s.schedule
	cpaURL := s.cpaURL
	key := s.managementKey
	s.mu.Unlock()

	if cpaURL == "" || key == "" {
		return nil, ErrNotConfigured
	}
	return s.runOnce(ctx, "manual_backend", cpaURL, key, sch)
}

// GetSchedule returns the current schedule.
func (s *Scheduler) GetSchedule() Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.schedule
}

// GetStatus returns the current scheduler status.
func (s *Scheduler) GetStatus() SchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Scheduler) interval() time.Duration {
	hours := s.schedule.IntervalHours
	if hours <= 0 {
		hours = 6
	}
	return time.Duration(hours) * time.Hour
}

func (s *Scheduler) loop(ctx context.Context) {
	interval := s.interval()
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.mu.Lock()
			cpaURL := s.cpaURL
			key := s.managementKey
			sch := s.schedule
			s.mu.Unlock()

			if cpaURL == "" || key == "" {
				log.Printf("[inspection] scheduler: skipping, CPA not configured")
				timer.Reset(interval)
				continue
			}

			log.Printf("[inspection] scheduler: starting scheduled inspection")
			record, err := s.runOnce(ctx, "scheduled", cpaURL, key, sch)
			if err != nil {
				log.Printf("[inspection] scheduler: failed: %v", err)
			} else if record != nil {
				log.Printf("[inspection] scheduler: done probed=%d del=%d dis=%d en=%d",
					record.ProbedAccounts, record.DeleteCount, record.DisableCount, record.EnableCount)
			}

			s.mu.Lock()
			interval = s.interval()
			s.status.NextRunAt = time.Now().Add(interval).UnixMilli()
			s.mu.Unlock()
			timer.Reset(interval)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context, trigger, cpaURL, key string, sch Schedule) (*HistoryRecord, error) {
	record, err := RunInspection(ctx, cpaURL, key, sch)
	if record == nil {
		record = &HistoryRecord{}
	}
	record.Trigger = trigger

	s.mu.Lock()
	s.status.LastRunAt = time.Now().UnixMilli()
	s.mu.Unlock()

	detailsData, _ := json.Marshal(record.AccountResults)
	scheduleData, _ := json.Marshal(record.Schedule)
	if _, dbErr := s.store.InsertInspectionHistory(ctx, store.InspectionHistoryRow{
		Trigger:        record.Trigger,
		StartedAtMS:    record.StartedAtMS,
		FinishedAtMS:   record.FinishedAtMS,
		TotalAccounts:  record.TotalAccounts,
		ProbedAccounts: record.ProbedAccounts,
		DeleteCount:    record.DeleteCount,
		DisableCount:   record.DisableCount,
		EnableCount:    record.EnableCount,
		KeepCount:      record.KeepCount,
		Executed:       record.Executed,
		ExecuteSuccess: record.ExecuteSuccess,
		ExecuteFailed:  record.ExecuteFailed,
		ScheduleJSON:   string(scheduleData),
		DetailsJSON:    string(detailsData),
		Error:          record.Error,
	}); dbErr != nil {
		log.Printf("[inspection] scheduler: save history failed: %v", dbErr)
	}

	if pruneErr := s.store.PruneInspectionHistory(ctx, 100); pruneErr != nil {
		log.Printf("[inspection] scheduler: prune failed: %v", pruneErr)
	}

	return record, err
}

// ErrNotConfigured is returned when CPA URL or management key is missing.
var ErrNotConfigured = errNotConfigured("inspection: CPA not configured, run setup first")

type errNotConfigured string

func (e errNotConfigured) Error() string { return string(e) }
