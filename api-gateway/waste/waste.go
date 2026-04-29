package waste

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

type Finding struct {
	Detector    string   `json:"detector"`
	Severity    Severity `json:"severity"`
	Message     string   `json:"message"`
	TokensWaste int      `json:"tokens_wasted"`
	Suggestion  string   `json:"suggestion"`
}

type RequestRecord struct {
	SessionID string
	Model     string
	Input     int
	Output    int
	Timestamp time.Time
}

type Config struct {
	Enabled      bool
	ScanInterval time.Duration
	MinRequests  int
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
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

func LoadConfig() Config {
	return Config{
		Enabled:      envBoolOr("WASTE_ENABLED", true),
		ScanInterval: 60 * time.Second,
		MinRequests:  envIntOr("WASTE_MIN_REQUESTS", 10),
	}
}

type wasteMetrics struct {
	findings    *prometheus.CounterVec
	tokensWaste *prometheus.CounterVec
	scanDur     prometheus.Histogram
}

type WasteDetector struct {
	cfg Config
	m   *wasteMetrics
	mu  sync.Mutex

	// Per-session accumulation
	sessions map[string][]RequestRecord
}

func New(reg prometheus.Registerer) *WasteDetector {
	cfg := LoadConfig()
	m := &wasteMetrics{
		findings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "waste_findings_total", Help: "Waste findings",
		}, []string{"detector", "severity"}),
		tokensWaste: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "waste_tokens_wasted_total", Help: "Tokens wasted by detector",
		}, []string{"detector"}),
		scanDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "waste_scan_duration_seconds", Help: "Waste scan duration",
		}),
	}
	reg.MustRegister(m.findings, m.tokensWaste, m.scanDur)
	return &WasteDetector{cfg: cfg, m: m, sessions: make(map[string][]RequestRecord)}
}

// RecordRequest accumulates session data for detection.
func (w *WasteDetector) RecordRequest(sessionID, model string, input, output int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sessions[sessionID] = append(w.sessions[sessionID], RequestRecord{
		SessionID: sessionID, Model: model, Input: input, Output: output, Timestamp: time.Now(),
	})
	// Evict old sessions
	for id, recs := range w.sessions {
		if len(recs) > 0 && time.Since(recs[len(recs)-1].Timestamp) > 30*time.Minute {
			delete(w.sessions, id)
		}
	}
}

// Scan runs all detectors against accumulated session data.
func (w *WasteDetector) Scan(ctx context.Context) []Finding {
	start := time.Now()
	w.mu.Lock()
	sessions := make(map[string][]RequestRecord, len(w.sessions))
	for k, v := range w.sessions {
		sessions[k] = v
	}
	w.mu.Unlock()

	var findings []Finding
	for sessionID, records := range sessions {
		if len(records) < w.cfg.MinRequests {
			continue
		}
		dets := []detectorFunc{
			w.detectEmptyResponse,
			w.detectRetryChurn,
			w.detectLoopDetection,
			w.detectOversizedContext,
			w.detectBudgetExceeded,
			w.detectRedundantToolCall,
			w.detectLowValueResponse,
		}
		for _, d := range dets {
			if f := d(sessionID, records); f != nil {
				findings = append(findings, *f)
				w.m.findings.WithLabelValues(f.Detector, string(f.Severity)).Inc()
				w.m.tokensWaste.WithLabelValues(f.Detector).Add(float64(f.TokensWaste))
			}
		}
	}
	w.m.scanDur.Observe(time.Since(start).Seconds())
	return findings
}

// StartBackgroundScanner launches periodic scanning.
func (w *WasteDetector) StartBackgroundScanner(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Scan(ctx)
			}
		}
	}()
}

// GetFindingsJSON returns findings as JSON for API endpoint.
func (w *WasteDetector) GetFindingsJSON() string {
	findings := w.Scan(context.Background())
	data, _ := json.Marshal(findings)
	return string(data)
}

type detectorFunc func(sessionID string, records []RequestRecord) *Finding

func (w *WasteDetector) detectEmptyResponse(sessionID string, records []RequestRecord) *Finding {
	var emptyCount, totalTokens int
	for _, r := range records {
		if r.Output == 0 {
			emptyCount++
		}
		totalTokens += r.Input
	}
	if emptyCount > 0 && float64(emptyCount)/float64(len(records)) > 0.1 {
		return &Finding{
			Detector:    "empty_response",
			Severity:    SeverityHigh,
			Message:     fmtEmptyResponse(emptyCount, len(records)),
			TokensWaste: totalTokens * emptyCount,
			Suggestion:  "Check upstream API health or model availability.",
		}
	}
	return nil
}

func (w *WasteDetector) detectRetryChurn(sessionID string, records []RequestRecord) *Finding {
	var wastedTokens int
	for i := 1; i < len(records); i++ {
		if records[i].Input == records[i-1].Input && records[i].Output == 0 {
			wastedTokens += records[i].Input
		}
	}
	if wastedTokens > 5000 {
		return &Finding{
			Detector:    "retry_churn",
			Severity:    SeverityMedium,
			Message:     "Repeated identical requests with no output - retry churn detected.",
			TokensWaste: wastedTokens,
			Suggestion:  "Investigate error handling and retry logic.",
		}
	}
	return nil
}

func (w *WasteDetector) detectLoopDetection(sessionID string, records []RequestRecord) *Finding {
	if len(records) < 4 {
		return nil
	}
	var loopSize int
	for size := 2; size <= len(records)/2; size++ {
		isLoop := true
		for i := size; i < len(records); i++ {
			if records[i].Input != records[i-size].Input {
				isLoop = false
				break
			}
		}
		if isLoop {
			loopSize = size
			break
		}
	}
	if loopSize > 0 {
		var wasted int
		for _, r := range records[loopSize:] {
			wasted += r.Input
		}
		return &Finding{
			Detector:    "loop_detection",
			Severity:    SeverityHigh,
			Message:     fmt.Sprintf("Loop detected: %d-cycle repeating for %d requests", loopSize, len(records)-loopSize),
			TokensWaste: wasted,
			Suggestion:  "Add loop detection guard in agent logic.",
		}
	}
	return nil
}

func (w *WasteDetector) detectOversizedContext(sessionID string, records []RequestRecord) *Finding {
	var wasted int
	for _, r := range records {
		if r.Input > 100000 {
			excess := r.Input - 100000
			wasted += excess
		}
	}
	if wasted > 100000 {
		return &Finding{
			Detector:    "oversized_context",
			Severity:    SeverityMedium,
			Message:     "Multiple requests with context > 100K tokens.",
			TokensWaste: wasted,
			Suggestion:  "Enable context truncation or progressive disclosure.",
		}
	}
	return nil
}

func (w *WasteDetector) detectBudgetExceeded(sessionID string, records []RequestRecord) *Finding {
	modelSet := make(map[string]int)
	for _, r := range records {
		modelSet[r.Model]++
	}
	if len(modelSet) > 3 {
		var wasted int
		for _, r := range records {
			wasted += r.Input / 2 // estimate
		}
		return &Finding{
			Detector:    "budget_exceeded",
			Severity:    SeverityMedium,
			Message:     fmt.Sprintf("Session used %d different models - possible budget sprawl.", len(modelSet)),
			TokensWaste: wasted / 2,
			Suggestion:  "Consolidate model usage for cost efficiency.",
		}
	}
	return nil
}

func (w *WasteDetector) detectRedundantToolCall(sessionID string, records []RequestRecord) *Finding {
	var redundantCount, wasted int
	for i := 1; i < len(records); i++ {
		if records[i].Input == records[i-1].Input && records[i].Output == records[i-1].Output {
			redundantCount++
			wasted += records[i].Input + records[i].Output
		}
	}
	if redundantCount > 0 {
		return &Finding{
			Detector:    "redundant_tool_call",
			Severity:    SeverityLow,
			Message:     fmt.Sprintf("%d identical request-response pairs detected.", redundantCount),
			TokensWaste: wasted,
			Suggestion:  "Add caching or dedup for repeated tool calls.",
		}
	}
	return nil
}

func (w *WasteDetector) detectLowValueResponse(sessionID string, records []RequestRecord) *Finding {
	var lowValueCount, wasted int
	for _, r := range records {
		if r.Input > 5000 && r.Output < 50 {
			lowValueCount++
			wasted += r.Input
		}
	}
	if lowValueCount >= 3 {
		return &Finding{
			Detector:    "low_value_response",
			Severity:    SeverityLow,
			Message:     fmt.Sprintf("%d requests with >5K input but <50 output tokens.", lowValueCount),
			TokensWaste: wasted,
			Suggestion:  "Review prompts for unnecessary context injection.",
		}
	}
	return nil
}

func fmtEmptyResponse(count, total int) string {
	return fmt.Sprintf("%d of %d requests returned empty responses (%.0f%%).", count, total, float64(count)/float64(total)*100)
}
