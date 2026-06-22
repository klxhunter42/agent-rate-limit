package proxy_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

// timingRW is an http.ResponseWriter + http.Flusher that records WHEN the first
// byte is written and how many times Write/Flush fire, so we can tell whether the
// gateway relays a stream incrementally or buffers it.
type timingRW struct {
	h          http.Header
	status     int
	buf        bytes.Buffer
	start      time.Time
	firstWrite time.Duration
	wroteFirst bool
	writeN     int
	flushN     int
}

func (t *timingRW) Header() http.Header {
	if t.h == nil {
		t.h = http.Header{}
	}
	return t.h
}
func (t *timingRW) Write(b []byte) (int, error) {
	if !t.wroteFirst {
		t.firstWrite = time.Since(t.start)
		t.wroteFirst = true
	}
	t.writeN++
	return t.buf.Write(b)
}
func (t *timingRW) WriteHeader(s int) { t.status = s }
func (t *timingRW) Flush()            { t.flushN++ }

// TestStreamRelayFlushesImmediately verifies the gateway does NOT buffer the SSE
// stream: when the upstream emits chunks 50ms apart, the first byte must reach the
// client within a few ms (not after the whole stream). A large firstWrite means the
// relay buffers -> clients perceive streaming as frozen/slow.
func TestStreamRelayFlushesImmediately(t *testing.T) {
	const gap = 50 * time.Millisecond

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		emit := func(s string) {
			w.Write([]byte(s))
			if f != nil {
				f.Flush()
			}
		}
		emit("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"glm-5.2\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n")
		time.Sleep(gap)
		emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"a\"}}\n\n")
		time.Sleep(gap)
		emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"b\"}}\n\n")
		time.Sleep(gap)
		emit("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer mock.Close()

	cfg := &config.Config{
		UpstreamURL:              mock.URL,
		UpstreamMaxRetries:       0,
		UpstreamRetryBaseBackoff: time.Millisecond,
		TransientRetryMax:        0,
	}
	p := proxy.NewAnthropicProxy(cfg, metrics.New(func() float64 { return 0 }, nil))

	body := []byte(`{"model":"glm-5.2","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	rw := &timingRW{start: time.Now()}
	err := p.ProxyTransparent(rw, req, "test-key", body, "glm-5.2", true,
		proxy.FeedbackFunc(func(int, time.Duration, http.Header) {}), nil, nil)
	if err != nil {
		t.Fatalf("ProxyTransparent error: %v", err)
	}

	totalStream := 3 * gap // ~150ms of upstream sleeps
	fmt.Printf("stream relay: firstWrite=%v writes=%d flushes=%d status=%d totalStream=%v\n",
		rw.firstWrite, rw.writeN, rw.flushN, rw.status, totalStream)

	// Decisive assertion: first byte must arrive well before the whole stream finished.
	if !rw.wroteFirst {
		t.Fatalf("no bytes were written to the client at all")
	}
	if rw.firstWrite > totalStream/2 {
		t.Errorf("RELAY BUFFERS: firstWrite=%v is >= half the stream (%v) -> gateway is buffering the SSE stream before flushing the client", rw.firstWrite, totalStream/2)
	}
	if rw.writeN < 2 {
		t.Errorf("expected multiple incremental writes, got %d -> stream not relayed chunk-by-chunk", rw.writeN)
	}
}
