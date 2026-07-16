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
type triageJobs struct {
	mgr    *llm.JobManager[triage.Proposal]
	mu     sync.Mutex
	byNote map[string]string // note filename -> current job id
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
