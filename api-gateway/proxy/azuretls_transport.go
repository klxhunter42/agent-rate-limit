package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"

	azuretls "github.com/Noooste/azuretls-client"
)

// azuretlsRT adapts azuretls-client's Session.Do to net/http.RoundTripper.
// azuretls impersonates Chrome TLS (JA3/JA4) + HTTP/2 (SETTINGS/WINDOW_UPDATE/
// header-order) fingerprints, replacing the utls http/1.1 path.
//
// IMPORTANT: a single azuretls Session is NOT safe for concurrent Do() -- it
// mutates shared Session state (CookieJar, callback/prehook slices, session
// context). Calling Do() concurrently on one Session races and can wedge the
// connection under load, which was the root cause of "/v1/messages hangs with
// no log after high concurrency". We therefore keep a POOL of sessions sized to
// the Z.AI concurrency cap: each in-flight request checks out its OWN session,
// so no two goroutines ever share one Session. This restores concurrency
// (multiple sessions dispatch in parallel) without the race.
type azuretlsRT struct {
	pool     chan *azuretls.Session
	sessions []*azuretls.Session
}

func newAzureTLSSession(skipTLS bool, proxyURL string, headerTimeout time.Duration) *azuretls.Session {
	session := azuretls.NewSession()
	session.Browser = azuretls.Chrome
	session.InsecureSkipVerify = skipTLS
	session.DisableAutoDecompression = true
	// azuretls defaults TimeOut=30s, which it wires to BOTH TLSHandshakeTimeout
	// and ResponseHeaderTimeout. 30s is too short for glm-5.2 thinking + long
	// Claude Code contexts (header arrives after >30s -> "http2: timeout
	// awaiting response headers"). Raise to StreamTimeout (default 300s).
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

	// Check out an exclusive session for this call. Blocks (with ctx cancel)
	// until one is free; bounded by pool size == ZAI concurrency cap, so the
	// zai pacer semaphore upstream already throttles demand to <= poolSize.
	var s *azuretls.Session
	select {
	case s = <-a.pool:
		defer func() { a.pool <- s }()
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}

	resp, err := s.Do(areq)
	if err != nil {
		return nil, err
	}

	// Map fhttp.Header -> stdlib http.Header (distinct named types).
	hdr := make(http.Header, len(resp.Header))
	for k, vv := range resp.Header {
		hdr[k] = vv
	}

	body := resp.RawBody
	if body == nil {
		body = io.NopCloser(bytes.NewReader(resp.Body))
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
