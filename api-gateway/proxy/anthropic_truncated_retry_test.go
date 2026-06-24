package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

// TestNonStreamRetriesOnTruncatedJSON verifies that a non-streaming upstream
// response of HTTP 200 with a truncated JSON body (starts with '{' but is cut
// mid-content, like GLM returns under overload) is treated as transient and
// retried, instead of being passed to the client as a 502 "malformed response".
func TestNonStreamRetriesOnTruncatedJSON(t *testing.T) {
	var hits int32

	// First call: 200 with truncated JSON (invalid). Second call: 200 valid.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		if n == 1 {
			// Truncated mid-content: starts with valid JSON but never closes.
			_, _ = w.Write([]byte(`{"id":"msg_trunc","type":"message","role":"assistant","model":"glm-5.1","content":[{"type":"thinking","thinking":"Now I'll modify handleConfirm`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"glm-5.1","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`))
	}))
	defer mock.Close()

	cfg := &config.Config{
		UpstreamURL:              mock.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: time.Millisecond,
		TransientRetryMax:        2,
	}
	p := proxy.NewAnthropicProxy(cfg, metrics.New(func() float64 { return 0 }, nil))

	body := []byte(`{"model":"glm-5.1","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	err := p.ProxyTransparent(w, req, "test-key", body, "glm-5.1", false,
		proxy.FeedbackFunc(func(int, time.Duration, http.Header) {}), nil, nil)
	if err != nil {
		t.Fatalf("request returned error: %v body=%s", err, w.Body.String())
	}
	if w.Code != 200 {
		t.Fatalf("expected 200 after retry, got %d body=%s", w.Code, w.Body.String())
	}
	if hits < 2 {
		t.Fatalf("expected >=2 upstream hits (truncated then retry), got %d", hits)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("client response not valid JSON: %v body=%s", err, w.Body.String())
	}
	if resp["id"] != "msg_ok" {
		t.Fatalf("expected recovered msg_ok, got %v", resp["id"])
	}
	fmt.Printf("truncated-JSON retry test PASS: hits=%d -> client got 200 with valid msg_ok\n", hits)
}
