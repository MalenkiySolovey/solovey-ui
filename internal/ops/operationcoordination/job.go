package operationcoordination

import "strings"

type Progress struct {
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Unit      string `json:"unit"`
}

// Job is a finite owner-operation read envelope, not another durable job or
// scheduler. Nil progress/deadline/cancel request means no such owner truth.
type Job struct {
	Reference
	JobID            string    `json:"jobId"`
	Revision         uint64    `json:"revision"`
	Phase            string    `json:"phase"`
	ExpectedRevision string    `json:"expectedRevision,omitempty"`
	Progress         *Progress `json:"progress,omitempty"`
	Deadline         int64     `json:"deadline,omitempty"`
	CancelRequested  *bool     `json:"cancelRequested,omitempty"`
	CancelSupported  bool      `json:"cancelSupported"`
	CriticalSection  bool      `json:"criticalSection"`
	Outcome          Outcome   `json:"outcome"`
	RecoveryRequired bool      `json:"recoveryRequired"`
	Problem          *Problem  `json:"problem,omitempty"`
	ResultRef        string    `json:"resultRef,omitempty"`
}

func ProjectJob(change Change, progress *Progress, criticalSection bool) Job {
	result := Job{Reference: change.Reference, JobID: "job:" + strings.TrimPrefix(change.CorrelationID, "operation:"),
		Revision: change.Revision, Phase: change.OwnerPhase, ExpectedRevision: change.ExpectedRevision,
		CriticalSection: criticalSection, Outcome: change.Outcome, RecoveryRequired: change.RecoveryRequired, Problem: change.Problem}
	if progress != nil && progress.Unit == "bytes" && progress.Total > 0 && progress.Completed >= 0 && progress.Completed <= progress.Total {
		copy := *progress
		result.Progress = &copy
	}
	if change.Outcome != OutcomePending {
		result.ResultRef = change.OperationID
	}
	return result
}
