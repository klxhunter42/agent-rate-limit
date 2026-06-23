package proxy

import "testing"

// TestIsFairUseLimited confirms the z.ai 1313 body patterns are detected so the
// request is never retried or model-switched (which deepens the fair-use
// violation toward a permanent ban).
func TestIsFairUseLimited(t *testing.T) {
	cases := []string{
		`{"type":"error","error":{"type":"api_error","code":"1313","message":"[1313][Your account's current usage pattern does not comply with the Fair Usage Policy, and your request frequency has been limited. For details, please refer to the Subscription Service Agreement. To restore access, please submit a request.][2026062402174354991a3336e84f4a]"}}`,
		`{"error":{"code":"1313"}}`,
		`request rate has been restricted`,
		`Your request frequency has been limited`,
	}
	for i, c := range cases {
		if !isFairUseLimited(c) {
			t.Errorf("case %d: expected fair-use detection, body=%s", i, c)
		}
	}
	// Normal transient 429 / overload must NOT be flagged (those still retry).
	for i, c := range []string{
		`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`,
		`{"type":"error","error":{"type":"overloaded_error","code":"1305"}}`,
		`{"error":{"code":"1302"}}`, // concurrency, different code
	} {
		if isFairUseLimited(c) {
			t.Errorf("case %d: false positive (would suppress a legit retry), body=%s", i, c)
		}
	}
}
