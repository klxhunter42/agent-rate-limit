package filter

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
	"github.com/prometheus/client_golang/prometheus"
)

type Intent int

const (
	IntentCode Intent = iota
	IntentAnalysis
	IntentSearch
	IntentAction
	IntentChat
)

func (i Intent) String() string {
	return [...]string{"code", "analysis", "search", "action", "chat"}[i]
}

var intentPatterns = map[Intent][]*regexp.Regexp{
	IntentCode: {
		regexp.MustCompile(`(?i)\b(write|implement|fix|refactor|create file|add function|define class|build)\b`),
		regexp.MustCompile(`(?i)\b(cod(?:e|ing)|function|method|struct|interface|class)\b`),
	},
	IntentAnalysis: {
		regexp.MustCompile(`(?i)\b(explain|analyze|why does|how does|compare|review|evaluate|assess)\b`),
		regexp.MustCompile(`(?i)\b(meaning|purpose|reason|difference between|trade-off)\b`),
	},
	IntentSearch: {
		regexp.MustCompile(`(?i)\b(find|search|where is|locate|list all|show me|grep|which file)\b`),
		regexp.MustCompile(`(?i)\b(how many|count| occurrences of|references to)\b`),
	},
	IntentAction: {
		regexp.MustCompile(`(?i)\b(run|execute|deploy|test|build|compile|start|stop|restart|apply)\b`),
		regexp.MustCompile(`(?i)\b(install|uninstall|migrate|rollback|upgrade|configure)\b`),
	},
}

type Config struct {
	Enabled bool
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled: envBoolOr("FILTER_ENABLED", false),
	}
}

type filterMetrics struct {
	intents    *prometheus.CounterVec
	charsSaved *prometheus.CounterVec
}

type Filter struct {
	cfg Config
	m   *filterMetrics
}

func New(reg prometheus.Registerer) *Filter {
	cfg := LoadConfig()
	m := &filterMetrics{
		intents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "filter_intents_total", Help: "Intent classification counts",
		}, []string{"intent"}),
		charsSaved: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "filter_chars_saved_total", Help: "Chars saved by intent filtering",
		}, []string{"intent"}),
	}
	reg.MustRegister(m.intents, m.charsSaved)
	return &Filter{cfg: cfg, m: m}
}

// ClassifyIntent detects the primary intent from request messages.
func (f *Filter) ClassifyIntent(messages []map[string]any) Intent {
	scores := map[Intent]int{}
	lastUserMsg := ""

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		if lastUserMsg == "" {
			lastUserMsg = content
		}
		for intent, patterns := range intentPatterns {
			for _, pat := range patterns {
				if pat.MatchString(content) {
					scores[intent]++
				}
			}
		}
		if lastUserMsg != "" {
			break
		}
	}

	bestIntent := IntentChat
	bestScore := 0
	for intent, score := range scores {
		if score > bestScore {
			bestScore = score
			bestIntent = intent
		}
	}

	f.m.intents.WithLabelValues(bestIntent.String()).Inc()
	return bestIntent
}

// FilterResponse filters response content based on intent.
// Returns filtered content and chars saved.
func (f *Filter) FilterResponse(content string, intent Intent) (string, int) {
	origLen := len(content)
	var result string

	switch intent {
	case IntentCode:
		result = f.extractCodeBlocks(content)
	case IntentSearch:
		result = f.extractKeyLines(content)
	default:
		return content, 0
	}

	saved := origLen - len(result)
	if saved <= 0 {
		return content, 0
	}
	f.m.charsSaved.WithLabelValues(intent.String()).Add(float64(saved))
	return result, saved
}

func (f *Filter) extractCodeBlocks(content string) string {
	segments := tokenizer.SplitCodeBlocks(content)
	var code []string
	for _, seg := range segments {
		if seg.IsCode {
			code = append(code, seg.Text)
		}
	}
	if len(code) == 0 {
		return content
	}
	return strings.Join(code, "\n\n")
}

func (f *Filter) extractKeyLines(content string) string {
	lines := strings.Split(content, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Keep lines that start with bullets, numbers, or contain file paths
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "•") || strings.Contains(trimmed, ".go") ||
			strings.Contains(trimmed, ".ts") || strings.Contains(trimmed, ".py") ||
			strings.Contains(trimmed, ":") && len(trimmed) < 120 {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return content
	}
	return fmt.Sprintf("%s\n\n[%d lines filtered for relevance]\n", strings.Join(kept, "\n"), len(lines)-len(kept))
}
