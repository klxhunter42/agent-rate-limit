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

// TestStreamWithTagsNotBuffered reproduces the latency bug: a GLM streamed
// response containing '<' that look like tag prefixes (<thinking>, <system>,
// <div> in code/prose) used to make toolUseStripper hold the whole stream until
// a close tag or stream end. With StripGLMToolXML defaulting to false, the
// stream must flush incrementally.
func TestStreamWithTagsNotBuffered(t *testing.T) {
	const gap = 40 * time.Millisecond

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
		// Open tags with NO close yet + incidental '<' in code. Previously these
		// forced the stripper to buffer everything that followed.
		emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"<thinking>let me reason\"}}\n\n")
		time.Sleep(gap)
		emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" about <system> and <div>foo\"}}\n\n")
		time.Sleep(gap)
		emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" done\"}}\n\n")
		time.Sleep(gap)
		emit("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer mock.Close()

	cfg := &config.Config{
		UpstreamURL:        mock.URL,
		StripGLMToolXML:    false, // default: stripper disabled
		UpstreamMaxRetries: 0,
	}
	p := proxy.NewAnthropicProxy(cfg, metrics.New(func() float64 { return 0 }, nil))

	body := []byte(`{"model":"glm-5.2","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rw := &timingRW{start: time.Now()}

	err := p.ProxyTransparent(rw, req, "test-key", body, "glm-5.2", true,
		proxy.FeedbackFunc(func(int, time.Duration, http.Header) {}), nil, nil)
	if err != nil {
		t.Fatalf("ProxyTransparent error: %v", err)
	}

	total := 4 * gap
	fmt.Printf("tagged stream (stripper off): firstWrite=%v writes=%d flushes=%d totalStream=%v\n",
		rw.firstWrite, rw.writeN, rw.flushN, total)

	if !rw.wroteFirst {
		t.Fatalf("no bytes written to client")
	}
	if rw.firstWrite > total/2 {
		t.Errorf("STREAM HELD BACK: firstWrite=%v >= half stream (%v) - tag prefixes are still buffering the stream", rw.firstWrite, total/2)
	}
	if rw.writeN < 3 {
		t.Errorf("expected incremental writes, got %d", rw.writeN)
	}
}
