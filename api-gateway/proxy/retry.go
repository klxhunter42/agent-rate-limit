package proxy

import (
	"math/rand"
	"time"
)

// jitteredBackoff computes exponential backoff (base * attempt^2, capped at 5m),
// optionally multiplied by a random 0.5x..1.5x factor to avoid the mechanical
// deterministic pattern that flags traffic as automated.
func jitteredBackoff(base time.Duration, attempt int, jitter bool) time.Duration {
	b := base * time.Duration(attempt*attempt)
	if b > 5*time.Minute {
		b = 5 * time.Minute
	}
	if !jitter {
		return b
	}
	return time.Duration(float64(b) * (0.5 + rand.Float64()))
}
