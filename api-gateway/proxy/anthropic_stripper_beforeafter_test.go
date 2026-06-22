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

// TestStreamerStripperBeforeAfter reproduces the exact slow flow (a GLM stream
// whose text contains open tags like <thinking>/<system>/<div>) and compares the
// OLD default (stripper on) vs the NEW default (stripper off), measuring when the
// actual body content token "reason" reaches the client.
func TestStreamerStripperBeforeAfter(t *testing.T) {
	const gap = 40 * time.Millisecond
	const totalStream = 4 * gap

	run := func(strip bool) (firstContent time.Duration, delivered bool) {
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
			emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"<thinking>let me reason\"}}\n\n")
			time.Sleep(gap)
			emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" about <system> config and <div> in code\"}}\n\n")
			time.Sleep(gap)
			emit("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" more output here\"}}\n\n")
			time.Sleep(gap)
			emit("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		}))
		defer mock.Close()

		cfg := &config.Config{
			UpstreamURL:        mock.URL,
			StripGLMToolXML:    strip,
			UpstreamMaxRetries: 0,
		}
		p := proxy.NewAnthropicProxy(cfg, metrics.New(func() float64 { return 0 }, nil))
		body := []byte(`{"model":"glm-5.2","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		rw := &timingRW{start: time.Now(), contentMark: "reason"}
		if err := p.ProxyTransparent(rw, req, "k", body, "glm-5.2", true,
			proxy.FeedbackFunc(func(int, time.Duration, http.Header) {}), nil, nil); err != nil {
			t.Fatalf("err: %v", err)
		}
		return rw.firstContent, rw.gotContent
	}

	onFC, onDelivered := run(true)    // OLD default (stripper on)
	offFC, offDelivered := run(false) // NEW default (stripper off)

	onStr := "NEVER (held/strip)"
	if onDelivered {
		onStr = fmtMS(onFC)
	}
	offStr := "NEVER"
	if offDelivered {
		offStr = fmtMS(offFC)
	}

	fmt.Printf("\n================ SLOW-FLOW BEFORE/AFTER ================\n")
	fmt.Printf("stream with <thinking>/<system>/<div>, total upstream = %v\n", totalStream)
	fmt.Printf("time-to-content-token ('reason' reaching the client):\n")
	fmt.Printf("  OLD (STRIP on, ex-default):  %s\n", onStr)
	fmt.Printf("  NEW (STRIP off, default):    %s\n", offStr)
	fmt.Printf("=======================================================\n")

	if !offDelivered {
		t.Fatalf("NEW default never delivered the content token")
	}
	if offFC > totalStream/2 {
		t.Errorf("NEW default delivered content late: %v >= %v", offFC, totalStream/2)
	}
}

func fmtMS(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000) }
