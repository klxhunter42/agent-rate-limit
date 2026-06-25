package capture

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeRT returns a fixed response, echoing nothing from the request.
type fakeRT struct {
	resp *http.Response
	err  error
	got  *http.Request
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.got = req
	return f.resp, f.err
}

func newTestRecorder(maxBody int64) *Recorder {
	return &Recorder{maxBody: maxBody, queue: make(chan *record, 8)}
}

func mkResp(status int, body string, hdr http.Header) *http.Response {
	if hdr == nil {
		hdr = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCapture_RequestAndResponse(t *testing.T) {
	rec := newTestRecorder(1 << 20)
	respHdr := http.Header{"Content-Type": {"application/json"}}
	fake := &fakeRT{resp: mkResp(200, `{"ok":true}`, respHdr)}
	rt := RoundTripper(fake, rec)

	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true",
		strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Authorization", "Bearer sk-ant-secret")
	req.Header.Set("X-B3-Traceid", "trace123")

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// Client must still see the full, unmodified response body.
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != `{"ok":true}` {
		t.Fatalf("client body = %q, want full body", got)
	}

	r := <-rec.queue
	if r.Provider != "api.anthropic.com" {
		t.Errorf("provider = %q", r.Provider)
	}
	if r.TraceID != "trace123" {
		t.Errorf("trace = %q", r.TraceID)
	}
	if r.Status != 200 {
		t.Errorf("status = %d", r.Status)
	}
	if r.Request.Body != `{"model":"claude"}` {
		t.Errorf("req body = %q", r.Request.Body)
	}
	if r.Response.Body != `{"ok":true}` {
		t.Errorf("resp body = %q", r.Response.Body)
	}
	if r.Request.Headers["Authorization"] != "REDACTED" {
		t.Errorf("authorization not redacted: %q", r.Request.Headers["Authorization"])
	}
}

func TestCapture_TruncatesLargeResponse(t *testing.T) {
	rec := newTestRecorder(10)
	big := strings.Repeat("a", 100)
	fake := &fakeRT{resp: mkResp(200, big, nil)}
	rt := RoundTripper(fake, rec)

	req, _ := http.NewRequest(http.MethodPost, "https://api.test.com/x", nil)
	resp, _ := rt.RoundTrip(req)

	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(got) != 100 {
		t.Fatalf("client got %d bytes, want full 100", len(got))
	}

	r := <-rec.queue
	if !r.Response.Truncated {
		t.Error("expected response truncated")
	}
	if len(r.Response.Body) != 10 {
		t.Errorf("captured %d bytes, want cap 10", len(r.Response.Body))
	}
}

func TestCapture_BinaryBodyBase64(t *testing.T) {
	rec := newTestRecorder(1 << 20)
	binary := string([]byte{0xff, 0xfe, 0x00, 0x01})
	fake := &fakeRT{resp: mkResp(200, binary, http.Header{"Content-Encoding": {"gzip"}})}
	rt := RoundTripper(fake, rec)

	req, _ := http.NewRequest(http.MethodGet, "https://api.test.com/x", nil)
	resp, _ := rt.RoundTrip(req)
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := <-rec.queue
	if !r.Response.BodyB64 {
		t.Error("expected base64 flag for binary body")
	}
}

func TestCapture_TransportError(t *testing.T) {
	rec := newTestRecorder(1 << 20)
	fake := &fakeRT{err: io.ErrUnexpectedEOF}
	rt := RoundTripper(fake, rec)

	req, _ := http.NewRequest(http.MethodPost, "https://api.test.com/x", bytes.NewReader([]byte("body")))
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error propagated")
	}
	r := <-rec.queue
	if r.Err == "" {
		t.Error("expected error recorded")
	}
	if r.Request.Body != "body" {
		t.Errorf("req body = %q", r.Request.Body)
	}
}

func TestCapture_NilRecorderPassthrough(t *testing.T) {
	fake := &fakeRT{resp: mkResp(200, "x", nil)}
	rt := RoundTripper(fake, nil)
	if rt != fake {
		t.Fatal("nil recorder must return the original RoundTripper unchanged")
	}
}

func TestCapture_EnqueueDropsWhenFull(t *testing.T) {
	rec := &Recorder{maxBody: 10, queue: make(chan *record, 1)}
	rec.enqueue(&record{})
	rec.enqueue(&record{}) // queue full -> dropped
	if _, dropped, _, _ := rec.Stats(); dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

func TestCapture_PrivacyStatusSnapshot(t *testing.T) {
	rec := newTestRecorder(1 << 20)
	fake := &fakeRT{resp: mkResp(200, "ok", nil)}
	rt := RoundTripper(fake, rec)

	ps := NewPrivacyStatus()
	ps.SetMask(true, true)
	ps.SetUnmask(true, false) // unmask ran but a placeholder leaked

	req, _ := http.NewRequest(http.MethodPost, "https://api.test.com/x", strings.NewReader("{}"))
	req = req.WithContext(ContextWithPrivacyStatus(req.Context(), ps))

	resp, _ := rt.RoundTrip(req)
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := <-rec.queue
	if r.Privacy == nil {
		t.Fatal("expected privacy info in record")
	}
	if !r.Privacy.MaskApplied || !r.Privacy.MaskSuccess {
		t.Errorf("mask: %+v", r.Privacy)
	}
	if !r.Privacy.UnmaskApplied || r.Privacy.UnmaskSuccess {
		t.Errorf("unmask: want applied=true success=false, got %+v", r.Privacy)
	}
}

func TestCapture_ClientAndTrace(t *testing.T) {
	rec := newTestRecorder(1 << 20)
	fake := &fakeRT{resp: mkResp(200, "ok", nil)}
	rt := RoundTripper(fake, rec)

	cs := NewRequestStatus()
	cs.SetClient("lotuss-prod", "arl_secrettoken", "creq-1", "sess-9", "trace-abc")
	cs.AddOptimizer("chunker")

	req, _ := http.NewRequest(http.MethodPost, "https://api.test.com/x", strings.NewReader("{}"))
	req = req.WithContext(ContextWithPrivacyStatus(req.Context(), cs))
	resp, _ := rt.RoundTrip(req)
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := <-rec.queue
	if r.TraceID != "trace-abc" {
		t.Errorf("trace id = %q, want inbound trace-abc", r.TraceID)
	}
	if r.Client == nil || r.Client.Profile != "lotuss-prod" {
		t.Fatalf("client: %+v", r.Client)
	}
	if r.Client.KeyFP == "" || r.Client.KeyFP == "arl_secrettoken" {
		t.Errorf("key_fp must be a non-empty hash, not the raw token: %q", r.Client.KeyFP)
	}
	// Deterministic: same token hashes the same way for search.
	cs2 := NewRequestStatus()
	cs2.SetClient("", "arl_secrettoken", "", "", "")
	if cs2.clientSnapshot().KeyFP != r.Client.KeyFP {
		t.Error("key_fp not deterministic")
	}
}
