package delta

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

const (
	maxLCSBytes = 50000
	maxOps      = 200
)

type DeltaOp struct {
	Type byte // '+', '-', '='
	Data string
}

type DeltaResult struct {
	Ops        []DeltaOp
	SavedBytes int
}

type Config struct {
	Enabled       bool
	MinSavingsPct float64
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled:       envBoolOr("DELTA_ENABLED", true),
		MinSavingsPct: envFloatOr("DELTA_MIN_SAVINGS_PCT", 10.0),
	}
}

type deltaMetrics struct {
	encodes    *prometheus.CounterVec
	charsSaved prometheus.Counter
	savingsPct prometheus.Histogram
}

type Delta struct {
	cfg Config
	rdb *redis.Client
	m   *deltaMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Delta {
	cfg := LoadConfig()
	m := &deltaMetrics{
		encodes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "delta_encodes_total", Help: "Delta encodes",
		}, []string{"result"}),
		charsSaved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "delta_chars_saved_total", Help: "Chars saved by delta encoding",
		}),
		savingsPct: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "delta_savings_pct", Help: "Delta savings percentage",
			Buckets: []float64{5, 10, 20, 30, 50, 70, 90},
		}),
	}
	reg.MustRegister(m.encodes, m.charsSaved, m.savingsPct)
	return &Delta{cfg: cfg, rdb: rdb, m: m}
}

// Encode computes a diff between current content and a cached version.
func (d *Delta) Encode(ctx context.Context, cacheKey, content string) (string, int, bool) {
	if d.rdb == nil {
		return content, 0, false
	}

	baseline, err := d.rdb.Get(ctx, "delta:baseline:"+cacheKey).Result()
	if err != nil {
		// No baseline, store current
		d.rdb.Set(ctx, "delta:baseline:"+cacheKey, content, 24*time.Hour)
		d.m.encodes.WithLabelValues("passthrough").Inc()
		return content, 0, false
	}

	if len(content) > maxLCSBytes || len(baseline) > maxLCSBytes {
		d.m.encodes.WithLabelValues("passthrough").Inc()
		return content, 0, false
	}

	result := computeDelta([]byte(baseline), []byte(content))
	if result == nil || len(result.Ops) == 0 {
		d.m.encodes.WithLabelValues("passthrough").Inc()
		return content, 0, false
	}

	savingsPct := float64(result.SavedBytes) / float64(len(content)) * 100
	if savingsPct < d.cfg.MinSavingsPct {
		d.m.encodes.WithLabelValues("passthrough").Inc()
		return content, 0, false
	}

	// Serialize ops
	var buf bytes.Buffer
	for _, op := range result.Ops {
		buf.WriteByte(op.Type)
		fmt.Fprintf(&buf, "%d:%s", len(op.Data), op.Data)
	}
	delta := buf.String()

	// Store new baseline
	d.rdb.Set(ctx, "delta:baseline:"+cacheKey, content, 24*time.Hour)

	d.m.encodes.WithLabelValues("delta").Inc()
	d.m.charsSaved.Add(float64(result.SavedBytes))
	d.m.savingsPct.Observe(savingsPct)
	return delta, result.SavedBytes, true
}

// Decode applies a delta patch to reconstruct the original content.
func (d *Delta) Decode(delta string, base string) (string, error) {
	if !strings.ContainsAny(delta, "+-=") {
		return delta, nil
	}
	ops, err := parseDelta(delta)
	if err != nil {
		return delta, err
	}

	var buf bytes.Buffer
	for _, op := range ops {
		switch op.Type {
		case '=':
			// Keep from base (Data is a count string)
			n := 0
			fmt.Sscanf(op.Data, "%d", &n)
			if n <= len(base) {
				buf.WriteString(base[:n])
				base = base[n:]
			}
		case '+':
			buf.WriteString(op.Data)
		case '-':
			// Skip from base
			n := 0
			fmt.Sscanf(op.Data, "%d", &n)
			if n <= len(base) {
				base = base[n:]
			}
		}
	}
	return buf.String(), nil
}

// StoreBaseline caches content for future delta computation.
func (d *Delta) StoreBaseline(ctx context.Context, cacheKey, content string) error {
	if d.rdb == nil {
		return nil
	}
	return d.rdb.Set(ctx, "delta:baseline:"+cacheKey, content, 24*time.Hour).Err()
}

func computeDelta(old, new []byte) *DeltaResult {
	// Simple line-based diff using LCS
	oldLines := bytes.Split(old, []byte("\n"))
	newLines := bytes.Split(new, []byte("\n"))

	if len(oldLines) > maxOps || len(newLines) > maxOps {
		return nil
	}

	// Build LCS table
	m, n := len(oldLines), len(newLines)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if bytes.Equal(oldLines[i-1], newLines[j-1]) {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	// Backtrack to produce ops
	var ops []DeltaOp
	i, j := m, n
	for i > 0 && j > 0 {
		if bytes.Equal(oldLines[i-1], newLines[j-1]) {
			ops = append(ops, DeltaOp{Type: '=', Data: string(oldLines[i-1]) + "\n"})
			i--
			j--
		} else if lcs[i-1][j] > lcs[i][j-1] {
			ops = append(ops, DeltaOp{Type: '-', Data: string(oldLines[i-1]) + "\n"})
			i--
		} else {
			ops = append(ops, DeltaOp{Type: '+', Data: string(newLines[j-1]) + "\n"})
			j--
		}
	}
	for i > 0 {
		ops = append(ops, DeltaOp{Type: '-', Data: string(oldLines[i-1]) + "\n"})
		i--
	}
	for j > 0 {
		ops = append(ops, DeltaOp{Type: '+', Data: string(newLines[j-1]) + "\n"})
		j--
	}

	// Reverse
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}

	// Compact: merge consecutive same-type ops
	var compacted []DeltaOp
	for _, op := range ops {
		if len(compacted) > 0 && compacted[len(compacted)-1].Type == op.Type && op.Type != '=' {
			compacted[len(compacted)-1].Data += op.Data
		} else {
			compacted = append(compacted, op)
		}
	}

	savedBytes := len(new) - lcsSerializedSize(compacted)
	if savedBytes < 0 {
		savedBytes = 0
	}

	return &DeltaResult{Ops: compacted, SavedBytes: savedBytes}
}

func lcsSerializedSize(ops []DeltaOp) int {
	total := 0
	for _, op := range ops {
		total += 1 + len(op.Data) // type byte + data
	}
	return total
}

func parseDelta(s string) ([]DeltaOp, error) {
	var ops []DeltaOp
	i := 0
	for i < len(s) {
		opType := s[i]
		i++
		colonIdx := strings.IndexByte(s[i:], ':')
		if colonIdx < 0 {
			break
		}
		var length int
		fmt.Sscanf(s[i:i+colonIdx], "%d", &length)
		i += colonIdx + 1
		if i+length > len(s) {
			break
		}
		ops = append(ops, DeltaOp{Type: opType, Data: s[i : i+length]})
		i += length
	}
	return ops, nil
}
