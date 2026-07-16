// internal/llm/job.go
package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Status is a Job's lifecycle state.
type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Job is a snapshot of one asynchronous unit of work. Get returns a copy,
// safe to read after the call without holding any lock.
type Job[T any] struct {
	ID     string
	Status Status
	Result T
	Err    error
}

// JobManager is a generic, goroutine-based submit/poll/result table
// (design spec §LLM subsystem "Job model", row #148): LLM calls take up
// to 120s (triage) / 180s (moc cleanup, phase 4) — a synchronous HTTP
// handler dies on mobile Safari over Tailscale. Submit runs fn in a new
// goroutine and returns immediately with a job id; Get polls the current
// status. No callbacks, no UI types — generic over the result type so it
// backs both a raw LLM string job and a typed triage.Proposal job without
// internal/llm importing internal/triage (design spec's "stateless verbs"
// invariant).
type JobManager[T any] struct {
	mu   sync.Mutex
	jobs map[string]*Job[T]
}

func NewJobManager[T any]() *JobManager[T] {
	return &JobManager[T]{jobs: make(map[string]*Job[T])}
}

// Submit runs fn(ctx) in a new goroutine under a fresh job id, tracks its
// status, and returns the id immediately. ctx governs fn's own
// cancellation/deadline (e.g. a 120s triage timeout) — it is intentionally
// NOT tied to any HTTP request context, since a job must keep running
// after the request that started it returns (the whole point of the
// job model).
func (jm *JobManager[T]) Submit(ctx context.Context, fn func(context.Context) (T, error)) string {
	id := newJobID()
	job := &Job[T]{ID: id, Status: StatusPending}
	jm.mu.Lock()
	jm.jobs[id] = job
	jm.mu.Unlock()

	go func() {
		result, err := fn(ctx)
		jm.mu.Lock()
		defer jm.mu.Unlock()
		job.Result = result
		job.Err = err
		if err != nil {
			job.Status = StatusFailed
		} else {
			job.Status = StatusDone
		}
	}()

	return id
}

// Get returns a copy of the job's current state, or ok=false if id is
// unknown.
func (jm *JobManager[T]) Get(id string) (Job[T], bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	j, ok := jm.jobs[id]
	if !ok {
		return Job[T]{}, false
	}
	return *j, true
}

func newJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
