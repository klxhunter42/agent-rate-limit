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

// dialUTLS creates a TLS connection using utls with Chrome fingerprint impersonation.
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

	return uConn, nil
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
