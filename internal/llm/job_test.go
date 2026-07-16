// internal/llm/job_test.go
package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// DECIDE(new in v2, row #148): Submit returns immediately with a job id;
// the function runs asynchronously.
func TestJobManagerSubmitReturnsImmediately(t *testing.T) {
	jm := NewJobManager[string]()
	started := make(chan struct{})
	release := make(chan struct{})
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		close(started)
		<-release
		return "done", nil
	})
	if id == "" {
		t.Fatal("expected a non-empty job id")
	}
	<-started // proves the function actually started running
	job, ok := jm.Get(id)
	if !ok {
		t.Fatal("expected the job to be trackable immediately after Submit")
	}
	if job.Status != StatusPending {
		t.Errorf("Status = %v, want StatusPending", job.Status)
	}
	close(release)
}

// DECIDE(new in v2, row #148): once the function returns, status flips to
// Done and Result is populated — "submit -> job id; status polled; result
// swapped in".
func TestJobManagerPollUntilDone(t *testing.T) {
	jm := NewJobManager[string]()
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		return "the-result", nil
	})
	deadline := time.Now().Add(2 * time.Second)
	var job Job[string]
	for time.Now().Before(deadline) {
		j, ok := jm.Get(id)
		if !ok {
			t.Fatal("job disappeared")
		}
		job = j
		if job.Status != StatusPending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.Status != StatusDone {
		t.Fatalf("Status = %v, want StatusDone", job.Status)
	}
	if job.Result != "the-result" {
		t.Errorf("Result = %q", job.Result)
	}
	if job.Err != nil {
		t.Errorf("Err = %v, want nil", job.Err)
	}
}

// A failed job function surfaces StatusFailed and the error, never panics
// or leaves the job stuck pending.
func TestJobManagerPollUntilFailed(t *testing.T) {
	jm := NewJobManager[string]()
	wantErr := errors.New("boom")
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		return "", wantErr
	})
	deadline := time.Now().Add(2 * time.Second)
	var job Job[string]
	for time.Now().Before(deadline) {
		j, _ := jm.Get(id)
		job = j
		if job.Status != StatusPending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.Status != StatusFailed {
		t.Fatalf("Status = %v, want StatusFailed", job.Status)
	}
	if !errors.Is(job.Err, wantErr) {
		t.Errorf("Err = %v, want %v", job.Err, wantErr)
	}
}

// Get on an unknown id reports ok=false, never a zero-value success.
func TestJobManagerGetUnknownID(t *testing.T) {
	jm := NewJobManager[string]()
	if _, ok := jm.Get("nonexistent"); ok {
		t.Error("expected ok=false for an unknown job id")
	}
}

// Two concurrent jobs get distinct ids and don't clobber each other's
// state (basic concurrency-safety smoke test; run with -race).
func TestJobManagerConcurrentJobsIndependent(t *testing.T) {
	jm := NewJobManager[int]()
	id1 := jm.Submit(context.Background(), func(ctx context.Context) (int, error) { return 1, nil })
	id2 := jm.Submit(context.Background(), func(ctx context.Context) (int, error) { return 2, nil })
	if id1 == id2 {
		t.Fatal("expected distinct job ids")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j1, _ := jm.Get(id1)
		j2, _ := jm.Get(id2)
		if j1.Status == StatusDone && j2.Status == StatusDone {
			if j1.Result != 1 || j2.Result != 2 {
				t.Errorf("results crossed: j1=%d j2=%d", j1.Result, j2.Result)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("jobs did not both complete in time")
}
