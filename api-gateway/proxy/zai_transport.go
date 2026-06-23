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
// Go's http2 transport type-asserts connections to connectionStater interface which requires
// ConnectionState() crypto/tls.ConnectionState. Without this wrapper, Go cannot detect that
// ALPN negotiated h2 and falls back to HTTP/1.x parser, which chokes on HTTP/2 binary frames.
type utlsConn struct {
	net.Conn
	uConn *tls_lib.UConn
}

// ConnectionState returns crypto/tls.ConnectionState mapped from utls's own ConnectionState.
// Only fields needed by Go's http2 transport and http.Response.TLS are populated.
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

// dialUTLS creates a TLS connection using utls with Chrome fingerprint impersonation,
// wrapped to expose crypto/tls.ConnectionState for Go's HTTP/2 detection.
func dialUTLS(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	host, _, _ := net.SplitHostPort(addr)
	uConn := tls_lib.UClient(tcpConn, &tls_lib.Config{
		ServerName:         host,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}, tls_lib.HelloChrome_Auto)

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
	// Standard Go TLS dialer for non-Z.AI hosts.
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
