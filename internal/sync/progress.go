package sync

import (
	"fmt"
	"sync/atomic"
	"time"
)

// SyncJob represents an in-progress or completed sync operation.
type SyncJob struct {
	ID          string `json:"id"`
	Completed   int64  `json:"completed"`
	Status      string `json:"status"` // "running", "done", "error"
	Tab         string `json:"tab"`
	Region      string `json:"region"`
	CurrentStep string `json:"currentStep,omitempty"`
	Error       string `json:"error,omitempty"`
}

// activeSyncJob holds the current sync job in memory (no need for SQLite).
var activeSyncJob atomic.Pointer[SyncJob]

// StartSync creates a new sync job and returns its ID.
func StartSync(tab, region string) string {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	job := &SyncJob{
		ID:     id,
		Status: "running",
		Tab:    tab,
		Region: region,
	}
	activeSyncJob.Store(job)
	return id
}

// IncrSync atomically increments the completed count and sets the current step label.
func IncrSync(jobID string, label string) {
	job := activeSyncJob.Load()
	if job == nil || job.ID != jobID {
		return
	}
	atomic.AddInt64(&job.Completed, 1)
	job.CurrentStep = label
}

// FinishSync marks the active job as done.
func FinishSync(jobID string) {
	job := activeSyncJob.Load()
	if job == nil || job.ID != jobID {
		return
	}
	job.Status = "done"
}

// ErrorSync marks the active job as errored.
func ErrorSync(jobID string, errMsg string) {
	job := activeSyncJob.Load()
	if job == nil || job.ID != jobID {
		return
	}
	job.Status = "error"
	job.Error = errMsg
}

// GetSyncProgress returns the current sync job (or nil if none).
func GetSyncProgress() *SyncJob {
	return activeSyncJob.Load()
}

// IsSyncing returns true if a sync is currently running.
func IsSyncing() bool {
	job := activeSyncJob.Load()
	return job != nil && job.Status == "running"
}

// ClearSync removes the active sync job.
func ClearSync() {
	activeSyncJob.Store(nil)
}
