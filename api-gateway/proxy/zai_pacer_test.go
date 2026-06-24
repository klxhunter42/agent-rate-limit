package proxy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
)

// TestZaiSpacerSpacesConcurrentSameKey proves that concurrent waiters sharing one
// key dispatch at least `gap` apart, not all at once. The pre-fix bug stored
// last=now, so every waiter computed the same remainder and woke together.
func TestZaiSpacerSpacesConcurrentSameKey(t *testing.T) {
	s := newZaiSpacer()
	gap := 40 * time.Millisecond
	const n = 5

	var mu sync.Mutex
	dispatch := make([]time.Duration, n)
	var wg sync.WaitGroup
	wg.Add(n)
	start := time.Now()

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			s.Wait(context.Background(), "key-A", gap)
			mu.Lock()
			dispatch[idx] = time.Since(start)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Sort dispatch offsets to inspect ordering regardless of goroutine schedule.
	sorted := append([]time.Duration(nil), dispatch...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	for i := 1; i < n; i++ {
		delta := sorted[i] - sorted[i-1]
		if delta < gap-5*time.Millisecond {
			t.Fatalf("waiter %d dispatched only %s after waiter %d (gap=%s); offsets=%v",
				i, delta, i-1, gap, sorted)
		}
	}
	// Total spread must be ~ (n-1)*gap, not ~gap (the burst symptom).
	wantMin := time.Duration(n-1)*gap - gap
	if sorted[n-1] < wantMin {
		t.Fatalf("total spread %s below expected ~%s; offsets=%v", sorted[n-1], wantMin, sorted)
	}
}

// TestZaiSpacerParallelizesAcrossKeys proves different keys are not serialized.
func TestZaiSpacerParallelizesAcrossKeys(t *testing.T) {
	s := newZaiSpacer()
	gap := 80 * time.Millisecond

	var wg sync.WaitGroup
	start := time.Now()
	dA, dB := time.Duration(0), time.Duration(0)
	wg.Add(2)
	go func() { defer wg.Done(); s.Wait(context.Background(), "key-A", gap); dA = time.Since(start) }()
	go func() { defer wg.Done(); s.Wait(context.Background(), "key-B", gap); dB = time.Since(start) }()
	wg.Wait()

	if dA > gap/2 && dB > gap/2 {
		t.Fatalf("distinct keys serialized: dA=%s dB=%s (expected both < %s)", dA, dB, gap/2)
	}
}

// TestNewAnthropicProxyDispatchModeResolvesCap proves ZAI_DISPATCH_MODE is the
// single toggle: "sequential" forces the dispatch semaphore to 1 (no concurrent
// glm-* burst -> no Z.AI 1313), anything else ("parallel") honors ZAIConcurrencyCap.
func TestNewAnthropicProxyDispatchModeResolvesCap(t *testing.T) {
	cases := []struct {
		mode    string
		cfgCap  int
		wantCap int
	}{
		{"sequential", 5, 1},
		{"SEQUENTIAL", 5, 1}, // case-insensitive
		{"seq", 5, 1},        // alias-ish -> still safe (not "parallel")
		{"", 5, 1},           // unset -> safe sequential
		{"para", 5, 1},       // typo -> safe sequential (fail-safe)
		{"parallel", 5, 5},
		{"Parallel", 3, 3}, // case-insensitive parallel honors cap
	}
	for _, c := range cases {
		cfg := &config.Config{ZAIConcurrencyCap: c.cfgCap, ZaiDispatchMode: c.mode}
		p := NewAnthropicProxy(cfg, nil)
		got := cap(p.zai.sem)
		if got != c.wantCap {
			t.Errorf("mode=%q cfgCap=%d: sem cap=%d want %d", c.mode, c.cfgCap, got, c.wantCap)
		}
	}
}
