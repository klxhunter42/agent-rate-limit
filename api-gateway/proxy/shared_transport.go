package proxy

import (
	"context"
	"net"
	"net/http"
	"sync"
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
)

func init() {
	dnsResolver = newDNSCache(30 * time.Second)
}

// SharedTransport returns a singleton Transport with DNS caching, connection
// pooling, and explicit timeouts. All proxies should use this.
func SharedTransport() *http.Transport {
	sharedTransportOnce.Do(func() {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		sharedTransport = &http.Transport{
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
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       120 * time.Second,
			MaxConnsPerHost:       0,
			ForceAttemptHTTP2:     true,
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
