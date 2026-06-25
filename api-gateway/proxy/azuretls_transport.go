package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	azuretls "github.com/Noooste/azuretls-client"
)

// azuretlsRT adapts azuretls-client's Session.Do to net/http.RoundTripper.
// azuretls impersonates Chrome TLS (JA3/JA4) + HTTP/2 fingerprints.
//
// A single azuretls Session is NOT safe for concurrent use: Do() mutates shared
// Session state (CookieJar, callback/prehook slices, session context), and with
// IgnoreBody=true the response body (RawBody) keeps streaming from the Session's
// connection AFTER Do() returns. So a Session can be in use by an in-flight
// stream while another Do() starts -- that concurrent use raced and wedged the
// connection, which was the root cause of "/v1/messages hangs with no log under
// high concurrency".
//
// We therefore keep a POOL of sessions sized to the Z.AI concurrency cap and
// hold each checked-out Session for the ENTIRE request lifetime (until the
// response body is Closed), so no two goroutines ever touch one Session -- not
// during Do(), and not during the streamed body read. This makes a concurrent-
// use race structurally impossible. Pool size == zai concurrency cap, so the
// zai pacer upstream already bounds demand to <= poolSize and checkout never
// starves.
type azuretlsRT struct {
	pool     chan *azuretls.Session
	sessions []*azuretls.Session
}

func newAzureTLSSession(skipTLS bool, proxyURL string, headerTimeout time.Duration) *azuretls.Session {
	session := azuretls.NewSession()
	session.Browser = azuretls.Chrome
	session.InsecureSkipVerify = skipTLS
	session.DisableAutoDecompression = true
	// azuretls defaults TimeOut=30s, wired to BOTH TLSHandshakeTimeout and
	// ResponseHeaderTimeout. 30s is too short for glm-5.2 thinking + long
	// Claude Code contexts (header arrives after >30s -> "http2: timeout
	// awaiting response headers"). Raise to StreamTimeout (default 300s).
	// This bounds the header wait; the streamed body is bounded by the request
	// context (client disconnect cancels the underlying http.Request ctx).
	if headerTimeout <= 0 {
		headerTimeout = 300 * time.Second
	}
	session.SetTimeout(headerTimeout)
	if proxyURL != "" {
		if err := session.SetProxy(proxyURL); err != nil {
			slog.Warn("zai-azuretls: set proxy failed", "error", err, "proxy", proxyURL)
		}
	}
	return session
}

func newAzureTLSRoundTripper(skipTLS bool, proxyURL string, headerTimeout time.Duration, poolSize int) (http.RoundTripper, func()) {
	if poolSize < 1 {
		poolSize = 1
	}
	rt := &azuretlsRT{
		pool:     make(chan *azuretls.Session, poolSize),
		sessions: make([]*azuretls.Session, 0, poolSize),
	}
	for i := 0; i < poolSize; i++ {
		s := newAzureTLSSession(skipTLS, proxyURL, headerTimeout)
		rt.sessions = append(rt.sessions, s)
		rt.pool <- s
	}
	slog.Info("zai-azuretls: session pool created", "size", poolSize)
	return rt, func() {
		for _, s := range rt.sessions {
			s.Close()
		}
	}
}

func (a *azuretlsRT) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read body to bytes: azuretls Request.Body is `any`; anthropic.go rebuilds
	// a fresh httpReq per retry so consuming here is safe.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	// Build OrderedHeaders to preserve header-order fingerprint (Akamai).
	oh := azuretls.OrderedHeaders{}
	for k, vv := range req.Header {
		oh = append(oh, append([]string{k}, vv...))
	}

	areq := &azuretls.Request{
		Method:         req.Method,
		Url:            req.URL.String(),
		Body:           bodyBytes,
		OrderedHeaders: oh,
		IgnoreBody:     true, // stream via RawBody instead of eager []byte read
		ForceHTTP1:     false,
		ContentLength:  req.ContentLength,
	}
	areq.SetContext(req.Context())

	// Check out an exclusive session for this entire request (Do + body read).
	var s *azuretls.Session
	select {
	case s = <-a.pool:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}

	resp, err := s.Do(areq)
	if err != nil {
		a.put(s) // no body to stream -> release immediately
		return nil, err
	}

	// Map fhttp.Header -> stdlib http.Header (distinct named types).
	hdr := make(http.Header, len(resp.Header))
	for k, vv := range resp.Header {
		hdr[k] = vv
	}

	// Hold the Session until the body is fully read/closed: RawBody streams from
	// the Session's connection, so releasing earlier would let another request
	// reuse this Session mid-stream (the concurrent-use race). Release on Close.
	var body io.ReadCloser
	if resp.RawBody != nil {
		body = &sessionBody{ReadCloser: resp.RawBody, release: func() { a.put(s) }}
	} else {
		body = io.NopCloser(bytes.NewReader(resp.Body))
		a.put(s) // eager body already materialized -> safe to release now
	}

	return &http.Response{
		Status:        resp.Status,
		StatusCode:    resp.StatusCode,
		Proto:         "HTTP/2.0",
		ProtoMajor:    2,
		ProtoMinor:    0,
		Header:        hdr,
		Body:          body,
		ContentLength: resp.ContentLength,
		Request:       req,
	}, nil
}

// put returns a session to the pool. Non-blocking: the pool is buffered to its
// size and each checkout pairs with exactly one release (guarded by sync.Once
// on the body path), so there is always room -- the default guards against any
// future double-release leaking a deadlock rather than a session.
func (a *azuretlsRT) put(s *azuretls.Session) {
	select {
	case a.pool <- s:
	default:
	}
}

// sessionBody releases the pooled azuretls Session back exactly once when the
// response body is closed, so a Session is never reused while its stream is
// still being read.
type sessionBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *sessionBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// selectiveRoundTripper routes Z.AI hosts to the azuretls fingerprint transport
// and all other hosts to the standard net/http transport.
type selectiveRoundTripper struct {
	std http.RoundTripper
	zai http.RoundTripper // nil when TLS_FINGERPRINT_ENABLED=false
}

func (s *selectiveRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.zai != nil && isZAIHost(req.URL.Hostname()) {
		return s.zai.RoundTrip(req)
	}
	return s.std.RoundTrip(req)
}

// Compile-time interface checks.
var (
	_ http.RoundTripper = (*azuretlsRT)(nil)
	_ http.RoundTripper = (*selectiveRoundTripper)(nil)
)
