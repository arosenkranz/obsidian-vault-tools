// internal/web/jobs.go
package web

import (
	"context"
	"sync"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

// triageJobs tracks in-flight and completed triage proposal jobs, keyed by
// the note's vault-relative filename (design spec's "in-process map keyed
// by note path" — Core interaction contract, row #150). Regenerated on
// restart; fine for one user (design spec).
//
// approveMu serializes the read-check-apply-clear sequence in
// handleTriageApprove end to end. Without it, two concurrent approve
// requests for the same note (e.g. a browser double-click) can both
// observe the job as StatusDone before either clears it and both call
// triage.Apply for the same note — not corrupting (the loser's
// vault.WriteNoteAtomic create-new-target refusal fails cleanly) but
// confusing and unnecessary. A single coarse mutex is sufficient here:
// per the design spec this is a single-user local tool at a scale (10^3-
// 10^4 notes) where serializing all approvals introduces no meaningful
// contention, so per-note lock striping would be over-engineering.
type triageJobs struct {
	mgr       *llm.JobManager[triage.Proposal]
	mu        sync.Mutex
	byNote    map[string]string // note filename -> current job id
	approveMu sync.Mutex        // serializes handleTriageApprove's check-apply-clear sequence
}

func newTriageJobs() *triageJobs {
	return &triageJobs{mgr: llm.NewJobManager[triage.Proposal](), byNote: make(map[string]string)}
}

// submit starts a new proposal job for note and records it as the
// current job for that note (overwriting any prior job id — e.g. a
// re-propose after a skip).
func (j *triageJobs) submit(ctx context.Context, note string, fn func(context.Context) (triage.Proposal, error)) string {
	id := j.mgr.Submit(ctx, fn)
	j.mu.Lock()
	j.byNote[note] = id
	j.mu.Unlock()
	return id
}

// current returns the current job (and whether one exists) for note.
func (j *triageJobs) current(note string) (llm.Job[triage.Proposal], bool) {
	j.mu.Lock()
	id, ok := j.byNote[note]
	j.mu.Unlock()
	if !ok {
		return llm.Job[triage.Proposal]{}, false
	}
	return j.mgr.Get(id)
}

// clear discards the tracked job for note (after approve/skip).
func (j *triageJobs) clear(note string) {
	j.mu.Lock()
	delete(j.byNote, note)
	j.mu.Unlock()
}
