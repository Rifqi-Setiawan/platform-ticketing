package expiry

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type queue struct{ remaining, calls, purges int }

func (q *queue) ExpireNext(ctx context.Context) (bool, error) {
	q.calls++
	if q.remaining == 0 {
		return false, nil
	}
	q.remaining--
	return true, nil
}
func (q *queue) PurgeExpiredSession(ctx context.Context) (bool, error) { q.purges++; return false, nil }

func TestBacklogIsBoundedAndResumed(t *testing.T) {
	q := &queue{remaining: 56}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	drain(t.Context(), q, log)
	if q.remaining != 6 || q.calls != 50 || q.purges != 1 {
		t.Fatalf("first pass %+v", q)
	}
	drain(t.Context(), q, log)
	if q.remaining != 0 || q.calls != 57 || q.purges != 2 {
		t.Fatalf("second pass %+v", q)
	}
}

type startupProbe struct{ arrived chan struct{} }

func (p startupProbe) ExpireNext(ctx context.Context) (bool, error) {
	close(p.arrived)
	<-ctx.Done()
	return false, ctx.Err()
}
func (p startupProbe) PurgeExpiredSession(context.Context) (bool, error) { return false, nil }
func TestStartupScansAndShutdownCancelsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := startupProbe{arrived: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); Run(ctx, p, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	select {
	case <-p.arrived:
	case <-time.After(800 * time.Millisecond):
		t.Fatal("startup did not scan")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
