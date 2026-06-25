package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

// RequestStatus carries per-request capture metadata: privacy mask/unmask
// outcomes and which optimizer rules fired. It is attached to the request
// context by the handler, populated by the handler (mask, optimizers) and the
// proxy (unmask), then snapshotted into the capture record by the RoundTripper.
// Booleans and rule names only; never the masked content itself.
type RequestStatus struct {
	mu            sync.Mutex
	maskApplied   bool
	maskSuccess   bool
	unmaskApplied bool
	unmaskSuccess bool
	optimizers    map[string]struct{}

	profile   string
	keyFP     string
	clientReq string
	sessionID string
	traceID   string
}

// PrivacyStatus is the former name; kept as an alias for callers.
type PrivacyStatus = RequestStatus

type ctxKey struct{}

// NewRequestStatus returns a zeroed status.
func NewRequestStatus() *RequestStatus { return &RequestStatus{} }

// NewPrivacyStatus is retained for callers; returns a zeroed status.
func NewPrivacyStatus() *RequestStatus { return &RequestStatus{} }

// ContextWithPrivacyStatus attaches ps to ctx.
func ContextWithPrivacyStatus(ctx context.Context, ps *RequestStatus) context.Context {
	return context.WithValue(ctx, ctxKey{}, ps)
}

// PrivacyStatusFrom returns the status attached to ctx, or nil.
func PrivacyStatusFrom(ctx context.Context) *RequestStatus {
	if ctx == nil {
		return nil
	}
	ps, _ := ctx.Value(ctxKey{}).(*RequestStatus)
	return ps
}

// AddOptimizer records that an optimizer rule fired for the request bound to
// ctx. No-op when no status is attached (capture disabled).
func AddOptimizer(ctx context.Context, name string) {
	if ps := PrivacyStatusFrom(ctx); ps != nil {
		ps.AddOptimizer(name)
	}
}

// SetMask records the request-masking outcome. applied=false means no masking
// was needed (trivially successful).
func (p *RequestStatus) SetMask(applied, success bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.maskApplied, p.maskSuccess = applied, success
	p.mu.Unlock()
}

// SetUnmask records the response-unmasking outcome. success=true means no
// masked placeholder survived in the unmasked output.
func (p *RequestStatus) SetUnmask(applied, success bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.unmaskApplied, p.unmaskSuccess = applied, success
	p.mu.Unlock()
}

// AddOptimizer records a fired optimizer rule (deduplicated).
func (p *RequestStatus) AddOptimizer(name string) {
	if p == nil || name == "" {
		return
	}
	p.mu.Lock()
	if p.optimizers == nil {
		p.optimizers = make(map[string]struct{})
	}
	p.optimizers[name] = struct{}{}
	p.mu.Unlock()
}

// optimizerList returns the fired optimizer rules, sorted.
func (p *RequestStatus) optimizerList() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.optimizers) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.optimizers))
	for k := range p.optimizers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetClient records who made the request: profile name, a non-reversible
// fingerprint of the arl_ API token (sha256 prefix; never the secret),
// client-request-id, session-id, and the inbound trace id. Empty fields are
// omitted from the record.
func (p *RequestStatus) SetClient(profile, apiToken, clientReq, sessionID, traceID string) {
	if p == nil {
		return
	}
	var fp string
	if apiToken != "" {
		sum := sha256.Sum256([]byte(apiToken))
		fp = "sha256:" + hex.EncodeToString(sum[:])[:16]
	}
	p.mu.Lock()
	p.profile = profile
	p.keyFP = fp
	p.clientReq = clientReq
	p.sessionID = sessionID
	p.traceID = traceID
	p.mu.Unlock()
}

// TraceID returns the inbound trace id recorded via SetClient.
func (p *RequestStatus) TraceID() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.traceID
}

// clientSnapshot returns the client block, or nil when nothing was recorded.
func (p *RequestStatus) clientSnapshot() *clientInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.profile == "" && p.keyFP == "" && p.clientReq == "" && p.sessionID == "" {
		return nil
	}
	return &clientInfo{
		Profile:         strings.TrimSpace(p.profile),
		KeyFP:           p.keyFP,
		ClientRequestID: p.clientReq,
		SessionID:       p.sessionID,
	}
}

func (p *RequestStatus) snapshot() privacyInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return privacyInfo{
		MaskApplied:   p.maskApplied,
		MaskSuccess:   p.maskSuccess,
		UnmaskApplied: p.unmaskApplied,
		UnmaskSuccess: p.unmaskSuccess,
	}
}
