package proxy

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"strings"
	"time"

	tls_lib "github.com/refraction-networking/utls"
)

// zaiHosts are domains that should use utls fingerprint impersonation.
var zaiHosts = []string{
	"api.z.ai",
}

// isZAIHost checks if the target host belongs to Z.AI and needs TLS fingerprint masking.
func isZAIHost(host string) bool {
	h := strings.TrimSuffix(host, ":443")
	for _, d := range zaiHosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// utlsConn wraps a *utls.UConn to expose ConnectionState() as crypto/tls.ConnectionState.
// Required because Go's net/http.Transport does a hard type assertion pconn.conn.(*tls.Conn)
// at dialConn() to extract tlsState. utls connections are not *tls.Conn, so this wrapper
// provides the same interface. Note: this alone is NOT sufficient for HTTP/2 detection at
// the net/http layer - see dialUTLS for the ALPN workaround.
type utlsConn struct {
	net.Conn
	uConn *tls_lib.UConn
}

func (w *utlsConn) ConnectionState() tls.ConnectionState {
	us := w.uConn.ConnectionState()
	return tls.ConnectionState{
		Version:                    us.Version,
		HandshakeComplete:          us.HandshakeComplete,
		CipherSuite:                us.CipherSuite,
		NegotiatedProtocol:         us.NegotiatedProtocol,
		NegotiatedProtocolIsMutual: us.NegotiatedProtocolIsMutual,
		ServerName:                 us.ServerName,
		PeerCertificates:           us.PeerCertificates,
	}
}

// dialUTLS creates a TLS connection using utls with Chrome TLS fingerprint impersonation.
//
// ALPN is forced to http/1.1 only (not h2) to work around a fundamental incompatibility
// between utls and Go's net/http.Transport: dialConn() does a hard type assertion
// `pconn.conn.(*tls.Conn)` (transport.go:1795) to detect HTTP/2 via tlsState, which
// always fails for utls connections regardless of any interface wrappers. By advertising
// only http/1.1 in ALPN, the server responds with HTTP/1.1 text frames, which Go's
// HTTP/1.x handler processes without needing tlsState. This costs HTTP/2 multiplexing
// but SSE streams are inherently single-request, so there is zero practical impact.
func dialUTLS(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(addr)

	// Build Chrome fingerprint spec with http/1.1 ALPN only.
	spec, err := tls_lib.UTLSIdToSpec(tls_lib.HelloChrome_Auto)
	if err != nil {
		tcpConn.Close()
		return nil, err
	}
	// Replace ALPN extension: Chrome default advertises h2+http/1.1, we want only http/1.1.
	for i, ext := range spec.Extensions {
		if _, ok := ext.(*tls_lib.ALPNExtension); ok {
			spec.Extensions[i] = &tls_lib.ALPNExtension{AlpnProtocols: []string{"http/1.1"}}
			break
		}
	}

	uConn := tls_lib.UClient(tcpConn, &tls_lib.Config{
		ServerName:         host,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}, tls_lib.HelloCustom)
	if err := uConn.ApplyPreset(&spec); err != nil {
		tcpConn.Close()
		return nil, err
	}
	if err := uConn.HandshakeContext(ctx); err != nil {
		tcpConn.Close()
		return nil, err
	}

	slog.Debug("zai-utls: handshake complete", "addr", addr, "alpn", uConn.ConnectionState().NegotiatedProtocol)
	return &utlsConn{Conn: uConn, uConn: uConn}, nil
}

// newZAITLSdialer returns a DialTLSContext function that uses utls for Z.AI hosts
// and falls back to standard Go TLS for all other hosts.
func newZAITLSdialer(skipTLS bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	stdTLS := &tls.Dialer{
		Config: &tls.Config{InsecureSkipVerify: skipTLS},
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if isZAIHost(host) {
			slog.Debug("zai-utls: using Chrome fingerprint", "addr", addr)
			return dialUTLS(ctx, network, addr, stdTLS.Config)
		}
		return stdTLS.DialContext(ctx, network, addr)
	}
}
