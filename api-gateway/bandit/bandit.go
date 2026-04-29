package bandit

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

const dim = 10

type Arm struct {
	ID          string
	Description string
}

type Prediction struct {
	Tool       string
	Confidence float64
}

type Config struct {
	Enabled bool
	Alpha   float64
	Decay   float64
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
		Enabled: envBoolOr("BANDIT_ENABLED", true),
		Alpha:   envFloatOr("BANDIT_ALPHA", 1.0),
		Decay:   envFloatOr("BANDIT_DECAY", 0.99),
	}
}

type armState struct {
	A [dim][dim]float64
	B [dim]float64
}

type banditMetrics struct {
	selections *prometheus.CounterVec
	reward     *prometheus.CounterVec
	duration   prometheus.Histogram
}

type Bandit struct {
	cfg  Config
	rdb  *redis.Client
	arms []Arm
	m    *banditMetrics
	rng  *rand.Rand
}

func New(reg prometheus.Registerer, rdb *redis.Client, arms []Arm) *Bandit {
	cfg := LoadConfig()
	m := &banditMetrics{
		selections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "bandit_selections_total", Help: "Bandit arm selections",
		}, []string{"arm", "exploratory"}),
		reward: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "bandit_reward_total", Help: "Bandit reward by arm",
		}, []string{"arm"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "bandit_selection_duration_seconds", Help: "Selection duration",
		}),
	}
	reg.MustRegister(m.selections, m.reward, m.duration)
	return &Bandit{cfg: cfg, rdb: rdb, arms: arms, m: m, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Select chooses an arm given the current context features.
func (b *Bandit) Select(ctx context.Context, features [dim]float64) (string, bool) {
	start := time.Now()
	states := b.loadStates(ctx)

	bestArm := ""
	bestScore := -1.0
	exploratory := false

	for _, arm := range b.arms {
		s, ok := states[arm.ID]
		if !ok {
			s = newArmState()
			states[arm.ID] = s
		}

		theta := matVecMul(invert(s.A), s.B)
		phi := features
		mean := dot(theta, phi)
		variance := dot(matVecMul(invert(s.A), phi), phi)
		score := mean + b.cfg.Alpha*math.Sqrt(math.Abs(variance))

		if score > bestScore || bestArm == "" {
			bestScore = score
			bestArm = arm.ID
			// Consider exploratory if mean is low (uncertain)
			exploratory = math.Abs(variance) > 1.0
		}
	}

	b.m.selections.WithLabelValues(bestArm, fmt.Sprintf("%v", exploratory)).Inc()
	b.m.duration.Observe(time.Since(start).Seconds())
	return bestArm, exploratory
}

// Update records the reward for a previously selected arm.
func (b *Bandit) Update(ctx context.Context, armID string, features [dim]float64, reward float64) {
	states := b.loadStates(ctx)
	s, ok := states[armID]
	if !ok {
		s = newArmState()
	}
	// A += phi * phi^T
	for i := 0; i < dim; i++ {
		for j := 0; j < dim; j++ {
			s.A[i][j] += features[i] * features[j]
		}
	}
	// b += reward * phi
	for i := 0; i < dim; i++ {
		s.B[i] += reward * features[i]
	}
	states[armID] = s

	b.m.reward.WithLabelValues(armID).Add(reward)
	b.saveState(ctx, armID, s)
}

// LoadState restores bandit parameters from Redis.
func (b *Bandit) LoadState(ctx context.Context) error {
	return nil // States loaded lazily
}

func (b *Bandit) loadStates(ctx context.Context) map[string]*armState {
	states := make(map[string]*armState, len(b.arms))
	for _, arm := range b.arms {
		key := fmt.Sprintf("bandit:state:%s", arm.ID)
		data, err := b.rdb.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}
		var s armState
		if json.Unmarshal(data, &s) == nil {
			states[arm.ID] = &s
		}
	}
	return states
}

func (b *Bandit) saveState(ctx context.Context, armID string, s *armState) {
	key := fmt.Sprintf("bandit:state:%s", armID)
	data, _ := json.Marshal(s)
	b.rdb.Set(ctx, key, data, 24*time.Hour)
}

func (b *Bandit) Close() {}

func newArmState() *armState {
	s := &armState{}
	for i := 0; i < dim; i++ {
		s.A[i][i] = 1.0 // Identity matrix
	}
	return s
}

func invert(m [dim][dim]float64) [dim][dim]float64 {
	// Gauss-Jordan elimination for 10x10 matrix
	var aug [dim][2 * dim]float64
	for i := 0; i < dim; i++ {
		for j := 0; j < dim; j++ {
			aug[i][j] = m[i][j]
		}
		aug[i][dim+i] = 1.0
	}
	for col := 0; col < dim; col++ {
		pivot := aug[col][col]
		if math.Abs(pivot) < 1e-12 {
			aug[col][col] = 1e-12
			pivot = 1e-12
		}
		for j := 0; j < 2*dim; j++ {
			aug[col][j] /= pivot
		}
		for row := 0; row < dim; row++ {
			if row == col {
				continue
			}
			factor := aug[row][col]
			for j := 0; j < 2*dim; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}
	var result [dim][dim]float64
	for i := 0; i < dim; i++ {
		for j := 0; j < dim; j++ {
			result[i][j] = aug[i][dim+j]
		}
	}
	return result
}

func matVecMul(m [dim][dim]float64, v [dim]float64) [dim]float64 {
	var result [dim]float64
	for i := 0; i < dim; i++ {
		for j := 0; j < dim; j++ {
			result[i] += m[i][j] * v[j]
		}
	}
	return result
}

func dot(a, b [dim]float64) float64 {
	var s float64
	for i := 0; i < dim; i++ {
		s += a[i] * b[i]
	}
	return s
}
