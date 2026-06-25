package capture

import (
	"io"
	"net/http"
	"time"
)

// RoundTripper wraps next so that every upstream request and its provider
// response are captured. When rec is nil the original RoundTripper is returned
// unchanged (zero overhead when capture is disabled).
func RoundTripper(next http.RoundTripper, rec *Recorder) http.RoundTripper {
	if rec == nil {
		return next
	}
	return &capturingRT{next: next, rec: rec}
}

type capturingRT struct {
	next http.RoundTripper
	rec  *Recorder
}

func (c *capturingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Snapshot the request body via GetBody so req.Body is never consumed.
	// http.NewRequestWithContext sets GetBody for bytes/strings readers, which
	// is how every upstream request in this gateway is built.
	var reqBody []byte
	var reqTrunc bool
	if req.GetBody != nil {
		if rc, err := req.GetBody(); err == nil {
			reqBody, reqTrunc = readCapped(rc, c.rec.maxBody)
			rc.Close()
		}
	}

	base := &record{
		TS:       start.UTC().Format(time.RFC3339Nano),
		TraceID:  req.Header.Get("x-b3-traceid"),
		Provider: req.URL.Hostname(),
		Method:   req.Method,
		URL:      req.URL.String(),
		Request:  newSide(req.Header, reqBody, reqTrunc, ""),
	}
	ps := PrivacyStatusFrom(req.Context())

	resp, err := c.next.RoundTrip(req)
	if err != nil {
		base.DurationMs = time.Since(start).Milliseconds()
		base.Err = err.Error()
		if ps != nil {
			snap := ps.snapshot()
			base.Privacy = &snap
			base.Optimizers = ps.optimizerList()
			base.Client = ps.clientSnapshot()
			if tid := ps.TraceID(); tid != "" {
				base.TraceID = tid
			}
		}
		c.rec.enqueue(base)
		return resp, err
	}

	// Guarantee resp.Request carries the request context so proxy unmask sites
	// can reach the same PrivacyStatus via resp.Request.Context().
	if resp.Request == nil {
		resp.Request = req
	}

	base.Status = resp.StatusCode
	resp.Body = &teeBody{
		src:   resp.Body,
		rec:   c.rec,
		base:  base,
		start: start,
		hdr:   resp.Header,
		enc:   resp.Header.Get("Content-Encoding"),
		ps:    ps,
	}
	return resp, nil
}

// teeBody copies response bytes into a capped buffer as the client reads them,
// then emits the completed record on Close. This captures streaming (SSE) and
// non-streaming responses identically without altering what the client sees.
type teeBody struct {
	src   io.ReadCloser
	rec   *Recorder
	base  *record
	start time.Time
	hdr   http.Header
	enc   string
	ps    *PrivacyStatus

	buf   []byte
	trunc bool
	done  bool
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 && !t.trunc {
		remain := int(t.rec.maxBody) - len(t.buf)
		if remain > 0 {
			if n <= remain {
				t.buf = append(t.buf, p[:n]...)
			} else {
				t.buf = append(t.buf, p[:remain]...)
				t.trunc = true
			}
		} else {
			t.trunc = true
		}
	}
	return n, err
}

func (t *teeBody) Close() error {
	err := t.src.Close()
	if t.done {
		return err
	}
	t.done = true

	body, b64 := encodeBody(t.buf)
	t.base.DurationMs = time.Since(t.start).Milliseconds()
	t.base.Response = side{
		Headers:   redactHeaders(t.hdr),
		Body:      body,
		BodyB64:   b64,
		BodyBytes: len(t.buf),
		Truncated: t.trunc,
	}
	if t.ps != nil {
		snap := t.ps.snapshot()
		t.base.Privacy = &snap
		t.base.Optimizers = t.ps.optimizerList()
		t.base.Client = t.ps.clientSnapshot()
		if tid := t.ps.TraceID(); tid != "" {
			t.base.TraceID = tid
		}
	}
	t.rec.enqueue(t.base)
	return err
}

func newSide(h http.Header, body []byte, trunc bool, _ string) side {
	enc, b64 := encodeBody(body)
	return side{
		Headers:   redactHeaders(h),
		Body:      enc,
		BodyB64:   b64,
		BodyBytes: len(body),
		Truncated: trunc,
	}
}

// readCapped reads up to max bytes; the bool reports whether more remained.
func readCapped(r io.Reader, max int64) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r, max))
	if err != nil {
		return b, false
	}
	// One extra byte tells us whether the source was longer than the cap.
	var extra [1]byte
	n, _ := r.Read(extra[:])
	return b, n > 0
}
