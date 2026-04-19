package proxy

import (
	"log/slog"
	"sync"
	"time"
)

type strategy string

const (
	strategyRoundRobin strategy = "round-robin"
	strategyFillFirst  strategy = "fill-first"
)

var currentStrategy strategy = strategyRoundRobin

func SetStrategy(s string) {
	switch s {
	case "fill-first":
		currentStrategy = strategyFillFirst
	case "round-robin":
		currentStrategy = strategyRoundRobin
	default:
		slog.Warn("unknown strategy, keeping current", "requested", s, "current", currentStrategy)
		return
	}
	slog.Info("routing strategy changed", "strategy", currentStrategy)
}

func GetStrategy() string {
	return string(currentStrategy)
}

// KeyPool manages a pool of upstream API keys with per-key RPM tracking
// and automatic cooldown on 429/overloaded errors.
//
// Selection strategy:
//  1. Weighted round-robin favoring keys with most remaining RPM budget
//  2. Skip keys in cooldown (recently received 429)
//  3. If all keys are in cooldown, pick the one whose cooldown expires soonest
type KeyPool struct {
	keys     []*keyEntry
	rpmLimit int64
	mu       sync.Mutex
	idx      int // round-robin cursor
}

type keyEntry struct {
	apiKey        string
	timestamps    []int64 // unix-millis of recent requests (sliding window)
	cooldownUntil int64   // unix-millis; 0 = not in cooldown
}

// NewKeyPool creates a key pool. keys may be empty (passthrough mode — client
// key is used directly). rpmLimit is the per-key requests-per-minute budget.
func NewKeyPool(keys []string, rpmLimit int) *KeyPool {
	if len(keys) == 0 {
		return &KeyPool{rpmLimit: int64(rpmLimit)}
	}

	entries := make([]*keyEntry, len(keys))
	for i, k := range keys {
		entries[i] = &keyEntry{apiKey: k}
	}

	slog.Info("key pool initialized",
		"keys", len(keys),
		"rpm_limit", rpmLimit,
	)
	return &KeyPool{keys: entries, rpmLimit: int64(rpmLimit)}
}

// Passthrough returns true when no keys are configured — the pool is a no-op.
func (kp *KeyPool) Passthrough() bool {
	return len(kp.keys) == 0
}

// Acquire selects the best available key and returns it.
// Returns ("", false) only if every key is exhausted and in active cooldown.
func (kp *KeyPool) Acquire() (apiKey string, ok bool) {
	if kp.Passthrough() {
		return "", true // caller uses client key
	}

	kp.mu.Lock()
	defer kp.mu.Unlock()

	now := time.Now().UnixMilli()
	windowStart := now - 60_000 // 1-minute sliding window

	// Clean up old timestamps for all keys and find the best candidate.
	var best *keyEntry
	bestBudget := int64(-1)

	for _, k := range kp.keys {
		k.trimBefore(windowStart)

		// Skip keys in cooldown.
		if k.cooldownUntil > 0 && now < k.cooldownUntil {
			continue
		}

		budget := kp.rpmLimit - int64(len(k.timestamps))
		if currentStrategy == strategyFillFirst {
			if budget > bestBudget {
				bestBudget = budget
				best = k
			}
		} else {
			if budget > 0 && budget > bestBudget {
				bestBudget = budget
				best = k
			}
		}
	}

	// round-robin: if no key has budget > 0 but some have budget == 0, allow them.
	if currentStrategy == strategyRoundRobin && best == nil {
		for _, k := range kp.keys {
			k.trimBefore(windowStart)
			if k.cooldownUntil > 0 && now < k.cooldownUntil {
				continue
			}
			budget := kp.rpmLimit - int64(len(k.timestamps))
			if budget > bestBudget {
				bestBudget = budget
				best = k
			}
		}
	}

	// All in cooldown - release mutex, wait, then retry once.
	if best == nil {
		soonest := int64(0)
		for _, k := range kp.keys {
			if k.cooldownUntil > 0 && (soonest == 0 || k.cooldownUntil < soonest) {
				soonest = k.cooldownUntil
				best = k
			}
		}
		if best == nil {
			return "", false
		}
		wait := time.Until(time.UnixMilli(best.cooldownUntil))
		kp.mu.Unlock()
		if wait > 0 {
			slog.Warn("all keys in cooldown, waiting", "wait_ms", wait.Milliseconds())
			time.Sleep(wait)
		}
		kp.mu.Lock()
		best.cooldownUntil = 0
	}

	best.timestamps = append(best.timestamps, now)
	return best.apiKey, true
}

// Report429 marks a key as rate-limited, putting it in cooldown.
func (kp *KeyPool) Report429(apiKey string) {
	if kp.Passthrough() {
		return
	}

	kp.mu.Lock()
	defer kp.mu.Unlock()

	for _, k := range kp.keys {
		if k.apiKey == apiKey {
			k.cooldownUntil = time.Now().Add(cooldownDuration).UnixMilli()
			slog.Warn("key cooldown after 429",
				"key_suffix", suffix(apiKey),
				"cooldown", cooldownDuration,
			)
			return
		}
	}
}

// ReportSuccess clears cooldown for a key (positive signal).
func (kp *KeyPool) ReportSuccess(apiKey string) {
	if kp.Passthrough() {
		return
	}

	kp.mu.Lock()
	defer kp.mu.Unlock()

	for _, k := range kp.keys {
		if k.apiKey == apiKey {
			k.cooldownUntil = 0
			return
		}
	}
}

// Status returns a snapshot for monitoring.
type KeyPoolStatus struct {
	TotalKeys int              `json:"total_keys"`
	Keys      []KeyStatusEntry `json:"keys"`
}

type KeyStatusEntry struct {
	Suffix     string `json:"suffix"`
	RPMUsed    int    `json:"rpm_used"`
	RPMLimit   int    `json:"rpm_limit"`
	InCooldown bool   `json:"in_cooldown"`
}

func (kp *KeyPool) Status() KeyPoolStatus {
	if kp.Passthrough() {
		return KeyPoolStatus{TotalKeys: 0}
	}

	kp.mu.Lock()
	defer kp.mu.Unlock()

	now := time.Now().UnixMilli()
	windowStart := now - 60_000

	status := KeyPoolStatus{TotalKeys: len(kp.keys)}
	for _, k := range kp.keys {
		k.trimBefore(windowStart)
		status.Keys = append(status.Keys, KeyStatusEntry{
			Suffix:     suffix(k.apiKey),
			RPMUsed:    len(k.timestamps),
			RPMLimit:   int(kp.rpmLimit),
			InCooldown: k.cooldownUntil > 0 && now < k.cooldownUntil,
		})
	}
	return status
}

const cooldownDuration = 10 * time.Second

// SyncFromStore replaces the key pool entries with the provided keys.
// Preserves RPM state for keys that still exist, adds new ones, removes stale ones.
func (kp *KeyPool) SyncFromStore(keys []string) {
	if len(keys) == 0 {
		return
	}

	kp.mu.Lock()
	defer kp.mu.Unlock()

	existing := make(map[string]*keyEntry, len(kp.keys))
	for _, e := range kp.keys {
		existing[e.apiKey] = e
	}

	var entries []*keyEntry
	for _, k := range keys {
		if e, ok := existing[k]; ok {
			entries = append(entries, e)
		} else {
			entries = append(entries, &keyEntry{apiKey: k})
		}
	}

	if len(entries) > 0 {
		kp.keys = entries
		slog.Info("key pool synced from token store", "keys", len(entries))
	}
}

// IsValidKey checks if the given key matches any key in the pool.
// In passthrough mode (no keys configured), accepts any non-empty key.
func (kp *KeyPool) IsValidKey(key string) bool {
	if key == "" {
		return false
	}
	if kp.Passthrough() {
		return true
	}
	kp.mu.Lock()
	defer kp.mu.Unlock()
	for _, k := range kp.keys {
		if k.apiKey == key {
			return true
		}
	}
	return false
}

func (k *keyEntry) trimBefore(windowStart int64) {
	i := 0
	for i < len(k.timestamps) && k.timestamps[i] < windowStart {
		i++
	}
	if i > 0 {
		k.timestamps = k.timestamps[i:]
	}
}

func suffix(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}
