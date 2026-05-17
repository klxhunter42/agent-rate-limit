package proxy

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// dnsCache caches DNS lookup results to avoid repeated resolution.
type dnsCache struct {
	mu    sync.RWMutex
	cache map[string]cachedEntry
	ttl   time.Duration
}

type cachedEntry struct {
	addrs  []string
	expiry time.Time
}

func newDNSCache(ttl time.Duration) *dnsCache {
	return &dnsCache{cache: make(map[string]cachedEntry), ttl: ttl}
}

func (d *dnsCache) lookup(ctx context.Context, host string) ([]string, error) {
	now := time.Now()

	d.mu.RLock()
	if entry, ok := d.cache[host]; ok && now.Before(entry.expiry) {
		addrs := entry.addrs
		d.mu.RUnlock()
		return addrs, nil
	}
	d.mu.RUnlock()

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	addrs := make([]string, len(ips))
	for i, ip := range ips {
		addrs[i] = ip.IP.String()
	}

	d.mu.Lock()
	d.cache[host] = cachedEntry{addrs: addrs, expiry: now.Add(d.ttl)}
	d.mu.Unlock()

	return addrs, nil
}

var (
	sharedTransport     *http.Transport
	sharedTransportOnce sync.Once
	dnsResolver         *dnsCache

	mitmEnabled  atomic.Bool
	mitmProxyURL *url.URL
)

func init() {
	dnsResolver = newDNSCache(30 * time.Second)
	proxyStr := os.Getenv("MITM_PROXY_URL")
	if proxyStr == "" {
		proxyStr = os.Getenv("HTTPS_PROXY")
	}
	if proxyStr != "" {
		if u, err := url.Parse(proxyStr); err == nil {
			mitmProxyURL = u
		}
	}
}

// SetMITM enables or disables mitmproxy routing at runtime.
func SetMITM(enabled bool) {
	mitmEnabled.Store(enabled)
	if sharedTransport != nil {
		sharedTransport.CloseIdleConnections()
	}
}

// GetMITM returns whether mitmproxy is currently enabled.
func GetMITM() bool { return mitmEnabled.Load() }

// GetMITMProxyURL returns the configured proxy URL.
func GetMITMProxyURL() string {
	if mitmProxyURL == nil {
		return ""
	}
	return mitmProxyURL.String()
}

// mitmProxyFunc returns the proxy URL when mitm is enabled, nil otherwise.
func mitmProxyFunc(req *http.Request) (*url.URL, error) {
	if mitmEnabled.Load() && mitmProxyURL != nil {
		return mitmProxyURL, nil
	}
	return nil, nil
}

// ensureProxyURL reads env vars if mitmProxyURL is nil (handles init ordering).
func ensureProxyURL() {
	if mitmProxyURL != nil {
		return
	}
	proxyStr := os.Getenv("MITM_PROXY_URL")
	if proxyStr == "" {
		proxyStr = os.Getenv("HTTPS_PROXY")
	}
	if proxyStr != "" {
		if u, err := url.Parse(proxyStr); err == nil {
			mitmProxyURL = u
		}
	}
}

// SharedTransport returns a singleton Transport with DNS caching, connection
// pooling, and explicit timeouts. All proxies should use this.
func SharedTransport() *http.Transport {
	sharedTransportOnce.Do(func() {
		ensureProxyURL()

		slog.Info("shared transport creating", "skipTLS", mitmProxyURL != nil, "proxy_url", GetMITMProxyURL())

		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		sharedTransport = &http.Transport{
			Proxy: mitmProxyFunc,
			DisableCompression: true, // prevent gzip decompression buffering on SSE streams
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return dialer.DialContext(ctx, network, addr)
				}
				// Warm DNS cache for TCP connections.
				if network == "tcp" {
					_, _ = dnsResolver.lookup(ctx, host)
				}
				return dialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 4 * time.Hour,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       120 * time.Second,
			MaxConnsPerHost:       0,
			ForceAttemptHTTP2:     true,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: mitmProxyURL != nil},
		}
	})
	return sharedTransport
}

// SharedClient returns an http.Client using the shared transport.
// timeout=0 means no global timeout (controlled per-request for streaming).
func SharedClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: SharedTransport(),
	}
}

// imageClient is a shared HTTP client for image downloads.
var imageClient = SharedClient(15 * time.Second)
