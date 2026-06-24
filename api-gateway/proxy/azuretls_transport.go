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
// Session.Do delegates to the HTTP/2 transport's RoundTrip, which multiplexes
// streams on a single connection (HTTP/2 native concurrency) -- so it is safe
// for concurrent use WITHOUT an external mutex. The earlier per-call mutex
// serialized all Z.AI traffic onto one stream and was a throughput killer.
type azuretlsRT struct {
	session *azuretls.Session
}

func newAzureTLSRoundTripper(skipTLS bool, proxyURL string, headerTimeout time.Duration) (http.RoundTripper, func()) {
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
	rt := &azuretlsRT{session: session}
	return rt, func() { session.Close() }
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

	resp, err := a.session.Do(areq)
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
