package proxy

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

// TestSessionBodyReleasesOnce proves the pooled Session is returned exactly
// once when the response body is closed -- including under double-Close, which
// must NOT trigger a second release (a double put would over-fill the pool
// channel and corrupt the pool invariant).
func TestSessionBodyReleasesOnce(t *testing.T) {
	var mu sync.Mutex
	count := 0
	b := &sessionBody{
		ReadCloser: io.NopCloser(bytes.NewReader([]byte("ok"))),
		release: func() {
			mu.Lock()
			count++
			mu.Unlock()
		},
	}
	if err := b.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := b.Close(); err != nil { // double close must be safe + idempotent
		t.Fatalf("second close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("release called %d times, want exactly 1 (double-close must not over-release)", count)
	}
}

// TestSessionBodyReadsThenReleases proves the body stays readable until Close
// (the Session is held for the whole stream), and release only fires on Close.
func TestSessionBodyReadsThenReleases(t *testing.T) {
	var mu sync.Mutex
	count := 0
	b := &sessionBody{
		ReadCloser: io.NopCloser(bytes.NewReader([]byte("payload"))),
		release: func() {
			mu.Lock()
			count++
			mu.Unlock()
		},
	}
	buf, err := io.ReadAll(b)
	if err != nil || string(buf) != "payload" {
		t.Fatalf("read body: %q err=%v", string(buf), err)
	}
	mu.Lock()
	if count != 0 {
		t.Fatalf("release fired during read (%d); Session must be held until Close", count)
	}
	mu.Unlock()
	_ = b.Close()
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("release count=%d after close, want 1", count)
	}
}
