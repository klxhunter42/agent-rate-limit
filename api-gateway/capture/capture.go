// Package capture persists the exact request the gateway sends upstream and the
// raw response the provider returns, to an S3-compatible object store (Tencent
// COS). It hooks at the http.RoundTripper layer so a single chokepoint covers
// every provider and proxy path. Capture is fully async and never blocks the
// request path: records are dropped (and counted) when the worker queue is full.
package capture

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prometheus/client_golang/prometheus"
)

// headersToRedact are stripped to "REDACTED" before a record is stored.
// Lower-cased; matched case-insensitively.
var headersToRedact = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
	"x-goog-api-key":      true,
	"cookie":              true,
	"set-cookie":          true,
}

// Recorder owns the upload worker pool and COS client.
type Recorder struct {
	client  *minio.Client
	bucket  string
	prefix  string
	maxBody int64
	queue   chan *record
	seq     atomic.Uint64

	enqueued atomic.Uint64
	dropped  atomic.Uint64
	uploaded atomic.Uint64
	failed   atomic.Uint64
}

type side struct {
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body,omitempty"`
	BodyB64   bool              `json:"body_b64,omitempty"`
	BodyBytes int               `json:"body_bytes"`
	Truncated bool              `json:"body_truncated,omitempty"`
}

type record struct {
	TS         string       `json:"ts"`
	TraceID    string       `json:"trace_id,omitempty"`
	DurationMs int64        `json:"duration_ms"`
	Provider   string       `json:"provider"`
	Method     string       `json:"method"`
	URL        string       `json:"url"`
	Status     int          `json:"status,omitempty"`
	Err        string       `json:"error,omitempty"`
	Request    side         `json:"request"`
	Response   side         `json:"response"`
	Privacy    *privacyInfo `json:"privacy,omitempty"`
	Optimizers []string     `json:"optimizers,omitempty"`
	Client     *clientInfo  `json:"client,omitempty"`
}

// clientInfo identifies who made the request. key_fp is a sha256 prefix of the
// arl_ API token (never the secret); search by hashing the token the same way.
type clientInfo struct {
	Profile         string `json:"profile,omitempty"`
	KeyFP           string `json:"key_fp,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
}

// privacyInfo reports mask/unmask outcomes (booleans only, no values).
type privacyInfo struct {
	MaskApplied   bool `json:"mask_applied"`
	MaskSuccess   bool `json:"mask_success"`
	UnmaskApplied bool `json:"unmask_applied"`
	UnmaskSuccess bool `json:"unmask_success"`
}

var (
	global     *Recorder
	globalOnce sync.Once
)

// Global lazily builds the singleton recorder from environment variables.
// Returns nil when TRAFFIC_CAPTURE_ENABLED is not true or required COS config
// is missing.
func Global() *Recorder {
	globalOnce.Do(func() {
		if os.Getenv("TRAFFIC_CAPTURE_ENABLED") != "true" {
			return
		}
		endpoint := os.Getenv("COS_ENDPOINT")
		bucket := os.Getenv("COS_BUCKET")
		accessKey := os.Getenv("COS_ACCESS_KEY_ID")
		secretKey := os.Getenv("COS_SECRET_ACCESS_KEY")
		if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
			slog.Warn("traffic capture enabled but COS config incomplete; disabled",
				"endpoint_set", endpoint != "", "bucket_set", bucket != "",
				"access_key_set", accessKey != "", "secret_key_set", secretKey != "")
			return
		}

		secure := os.Getenv("COS_INSECURE") != "true"
		client, err := minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: secure,
			Region: os.Getenv("COS_REGION"),
			// Tencent COS rejects path-style ("must be addressed using COS
			// virtual-styled domain"); force virtual-host addressing.
			BucketLookup: minio.BucketLookupDNS,
		})
		if err != nil {
			slog.Error("traffic capture: COS client init failed; disabled", "error", err)
			return
		}

		r := &Recorder{
			client:  client,
			bucket:  bucket,
			prefix:  envOr("TRAFFIC_CAPTURE_PREFIX", "traffic"),
			maxBody: int64(envIntOr("TRAFFIC_CAPTURE_MAX_BODY_BYTES", 5*1024*1024)),
			queue:   make(chan *record, envIntOr("TRAFFIC_CAPTURE_QUEUE_SIZE", 1024)),
		}
		workers := envIntOr("TRAFFIC_CAPTURE_WORKERS", 4)
		for i := 0; i < workers; i++ {
			go r.worker()
		}
		global = r
		slog.Info("traffic capture enabled", "bucket", bucket, "endpoint", endpoint,
			"prefix", r.prefix, "workers", workers, "max_body_bytes", r.maxBody)
	})
	return global
}

func (r *Recorder) worker() {
	for rec := range r.queue {
		r.upload(rec)
	}
}

func (r *Recorder) upload(rec *record) {
	body, err := json.Marshal(rec)
	if err != nil {
		r.failed.Add(1)
		return
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body); err != nil {
		r.failed.Add(1)
		return
	}
	if err := gz.Close(); err != nil {
		r.failed.Add(1)
		return
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("%s/%s/%s/%s-%d.json.gz",
		r.prefix, rec.Provider, now.Format("2006/01/02/15"),
		safeID(rec.TraceID), r.seq.Add(1))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = r.client.PutObject(ctx, r.bucket, key, &buf, int64(buf.Len()), minio.PutObjectOptions{
		ContentType:     "application/json",
		ContentEncoding: "gzip",
	})
	if err != nil {
		r.failed.Add(1)
		slog.Warn("traffic capture: upload failed", "key", key, "error", err)
		return
	}
	r.uploaded.Add(1)
}

// enqueue submits a record without blocking; drops and counts when full.
func (r *Recorder) enqueue(rec *record) {
	select {
	case r.queue <- rec:
		r.enqueued.Add(1)
	default:
		r.dropped.Add(1)
	}
}

func redactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if headersToRedact[lower(k)] {
			out[k] = "REDACTED"
			continue
		}
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// encodeBody returns a string field plus whether it was base64-encoded.
// Valid UTF-8 is stored verbatim for readability; binary (e.g. gzip) is base64.
func encodeBody(b []byte) (string, bool) {
	if utf8.Valid(b) {
		return string(b), false
	}
	return base64.StdEncoding.EncodeToString(b), true
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// safeID replaces empty / unsafe trace ids with a placeholder for the object key.
func safeID(id string) string {
	if id == "" {
		return "notrace"
	}
	b := []byte(id)
	for i, c := range b {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			b[i] = '_'
		}
	}
	return string(b)
}

// Stats returns counters for metrics/observability.
func (r *Recorder) Stats() (enqueued, dropped, uploaded, failed uint64) {
	return r.enqueued.Load(), r.dropped.Load(), r.uploaded.Load(), r.failed.Load()
}

// RegisterMetrics exposes the recorder counters on the given registry as a
// single labelled CounterFunc. No-op when the recorder is nil (capture off).
func (r *Recorder) RegisterMetrics(reg prometheus.Registerer) {
	if r == nil || reg == nil {
		return
	}
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "traffic_capture_enqueued_total", Help: "Records queued for upload to COS.",
	}, func() float64 { return float64(r.enqueued.Load()) }))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "traffic_capture_dropped_total", Help: "Records dropped because the upload queue was full.",
	}, func() float64 { return float64(r.dropped.Load()) }))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "traffic_capture_uploaded_total", Help: "Records successfully written to COS.",
	}, func() float64 { return float64(r.uploaded.Load()) }))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "traffic_capture_failed_total", Help: "Records that failed to upload to COS.",
	}, func() float64 { return float64(r.failed.Load()) }))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
