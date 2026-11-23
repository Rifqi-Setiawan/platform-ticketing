package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
	"tribunpass/internal/booking"
	"tribunpass/internal/postgres"
)

type observedStore struct {
	*postgres.Store
	mu     sync.Mutex
	counts map[string]uint64
}

func newObservedStore(s *postgres.Store) *observedStore {
	return &observedStore{Store: s, counts: make(map[string]uint64)}
}

func (s *observedStore) inc(name string) { s.mu.Lock(); s.counts[name]++; s.mu.Unlock() }

func (s *observedStore) CreateHold(ctx context.Context, cmd booking.CreateHoldCommand) (booking.CreateHoldResult, error) {
	r, err := s.Store.CreateHold(ctx, cmd)
	if err == nil {
		s.inc(fmt.Sprintf("tribunpass_hold_requests_total{outcome=%q}", r.Code))
	}
	return r, err
}
func (s *observedStore) ApplyPayment(ctx context.Context, event booking.PaymentEvent, id string) (booking.PaymentResult, error) {
	r, err := s.Store.ApplyPayment(ctx, event, id)
	if err == nil {
		s.inc(fmt.Sprintf("tribunpass_callbacks_total{outcome=%q}", r.Outcome))
		if r.Replayed {
			s.inc("tribunpass_callback_replays_total")
		}
	}
	return r, err
}
func (s *observedStore) metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	p, err := s.Stats(ctx)
	if err != nil {
		http.Error(w, "metrics unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "tribunpass_overdue_holds %d\ntribunpass_oldest_overdue_seconds %g\ntribunpass_pool_open %d\ntribunpass_pool_in_use %d\ntribunpass_pool_wait_total %d\ntribunpass_pool_wait_seconds_total %g\n", p.PendingExpired, p.OldestOverdueSeconds, p.DB.OpenConnections, p.DB.InUse, p.DB.WaitCount, p.DB.WaitDuration.Seconds())
	s.mu.Lock()
	lines := make([]string, 0, len(s.counts))
	for key, v := range s.counts {
		lines = append(lines, fmt.Sprintf("%s %d", key, v))
	}
	s.mu.Unlock()
	sort.Strings(lines)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}
