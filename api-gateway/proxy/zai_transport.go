package proxy

import "strings"

// zaiHosts are domains that should use TLS/HTTP2 fingerprint impersonation.
var zaiHosts = []string{
	"api.z.ai",
}

// isZAIHost checks if the target host belongs to Z.AI and needs fingerprint masking.
func isZAIHost(host string) bool {
	h := strings.TrimSuffix(host, ":443")
	for _, d := range zaiHosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
