package proxy

import (
	"context"
	"sync"
	"time"
)

// zaiPacer bounds Z.AI dispatch timing to reduce the automation signal from a
// single shared key+IP: a concurrency semaphore + per-key minimum spacing.
type zaiPacer struct {
	sem    chan struct{}
	spacer *zaiSpacer
}

// zaiSpacer enforces a minimum gap between dispatches keyed by api-key, so
// multiple keys parallelize while a single key is paced.
type zaiSpacer struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newZaiSpacer() *zaiSpacer {
	return &zaiSpacer{last: make(map[string]time.Time)}
}

// Wait blocks until at least gap has elapsed since the last Wait for this key.
func (s *zaiSpacer) Wait(ctx context.Context, key string, gap time.Duration) {
	if gap <= 0 || key == "" {
		return
	}
	s.mu.Lock()
	// Project this waiter's dispatch time as last[key]+gap, clamped to now.
	// Storing the projected (not actual) time makes concurrent same-key waiters
	// queue behind each other: waiter N+1 sees last = waiter N's projected slot
	// and computes its own slot one gap further out. Storing bare now would let
	// them all compute the same small remainder and wake together.
	now := time.Now()
	dispatchAt := s.last[key].Add(gap)
	if !dispatchAt.After(now) {
		dispatchAt = now
	}
	s.last[key] = dispatchAt
	s.mu.Unlock()
	remaining := time.Until(dispatchAt)
	if remaining <= 0 {
		return
	}
	select {
	case <-time.After(remaining):
	case <-ctx.Done():
	}
}

func newZaiPacer(cap int) *zaiPacer {
	if cap <= 0 {
		cap = 1
	}
	return &zaiPacer{
		sem:    make(chan struct{}, cap),
		spacer: newZaiSpacer(),
	}
}

// Acquire blocks until a concurrency slot is available or ctx is cancelled.
func (p *zaiPacer) Acquire(ctx context.Context) error {
	select {
	case p.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a concurrency slot.
func (p *zaiPacer) Release() {
	select {
	case <-p.sem:
	default:
	}
}
