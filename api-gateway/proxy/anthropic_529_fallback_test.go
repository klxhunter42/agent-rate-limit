package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

// Test529OverloadModelFallbackParallel fires N concurrent requests for glm-5.2
// at a mock upstream that returns 529 overloaded ONLY for glm-5.2 and 200 for any
// sibling. Each request wires the OnRateLimitError hook the handler uses in GLM/
// BYOK mode (limiter.SuggestFallbackModel). Every request must succeed via model
// fallback instead of stalling on the overloaded model.
func Test529OverloadModelFallbackParallel(t *testing.T) {
	const parallel = 8

	var glm52Hits, glm51Hits int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		model, _ := m["model"].(string)
		if model == "glm-5.2" {
			atomic.AddInt32(&glm52Hits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`))
			return
		}
		atomic.AddInt32(&glm51Hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"` + model + `","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`))
	}))
	defer mock.Close()

	cfg := &config.Config{
		UpstreamURL:              mock.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: time.Millisecond,
		TransientRetryMax:        2,
	}
	p := proxy.NewAnthropicProxy(cfg, metrics.New(func() float64 { return 0 }, nil))

	limiter := middleware.NewAdaptiveLimiter(
		map[string]int{"glm-5.2": 4, "glm-5.1": 8},
		nil, 2, 64, 1,
	)

	body := []byte(`{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)

	var wg sync.WaitGroup
	var succ int32
	start := make(chan struct{})
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // fire all at once

			tried := map[string]bool{"glm-5.2": true}
			opts := &proxy.ProxyOptions{
				OnRateLimitError: func(oldKey string) (proxy.FallbackResult, bool) {
					next := limiter.SuggestFallbackModel("glm-5.2", tried)
					if next == "" {
						return proxy.FallbackResult{}, false
					}
					tried[next] = true
					return proxy.FallbackResult{APIKey: oldKey, ModelOverride: next}, true
				},
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			w := httptest.NewRecorder()
			err := p.ProxyTransparent(w, req, "test-key", body, "glm-5.2", false,
				proxy.FeedbackFunc(func(int, time.Duration, http.Header) {}), nil, opts)
			if err != nil {
				t.Errorf("request failed: %v", w.Body.String())
				return
			}
			if w.Code != 200 {
				t.Errorf("expected 200 via fallback, got %d body=%s", w.Code, w.Body.String())
				return
			}
			atomic.AddInt32(&succ, 1)
		}()
	}
	close(start)
	wg.Wait()

	if succ != parallel {
		t.Fatalf("expected %d successes, got %d", parallel, succ)
	}
	if glm51Hits != parallel {
		t.Errorf("expected %d fallback hits on glm-5.1, got %d", parallel, glm51Hits)
	}
	if glm52Hits != parallel {
		t.Errorf("expected %d initial hits on glm-5.2 (one 529 then fall back), got %d", parallel, glm52Hits)
	}
	fmt.Printf("529 fallback load test PASS: %d parallel reqs all succeeded. glm-5.2=%d (529) -> glm-5.1=%d (200)\n",
		parallel, glm52Hits, glm51Hits)
}
