package proxy

import (
	"net/http"

	"github.com/klxhunter/agent-rate-limit/api-gateway/capture"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

// reportUnmask records the response-unmask outcome onto the PrivacyStatus that
// the handler attached to the request context, so the traffic-capture record
// can include it. No-op when capture is disabled or no status is present.
func reportUnmask(resp *http.Response, applied, success bool) {
	if resp == nil || resp.Request == nil {
		return
	}
	if ps := capture.PrivacyStatusFrom(resp.Request.Context()); ps != nil {
		ps.SetUnmask(applied, success)
	}
}

// reportUnmaskStream reports unmask outcome for streaming paths from the
// stream unmasker's accumulated leftover state. unmasker may be nil.
func reportUnmaskStream(resp *http.Response, unmasker *masking.StreamUnmasker) {
	applied := unmasker != nil && unmasker.HasContexts()
	success := unmasker == nil || unmasker.UnmaskSuccess()
	reportUnmask(resp, applied, success)
}
