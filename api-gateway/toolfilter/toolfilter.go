package toolfilter

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	Enabled    bool
	MaxTools   int
	AlwaysKeep string // comma-separated tool names
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
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled:    envBoolOr("TOOLFILTER_ENABLED", true),
		MaxTools:   envIntOr("TOOLFILTER_MAX_TOOLS", 15),
		AlwaysKeep: envOr("TOOLFILTER_ALWAYS_KEEP", "Read,Edit,Write,Bash"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type ToolFilter struct {
	cfg Config
}

func New(cfg Config) *ToolFilter {
	return &ToolFilter{cfg: cfg}
}

// Tool represents a tool definition from the request.
type Tool struct {
	Name        string
	Description string
}

// FilterTools selects the most relevant tools from the manifest based on intent.
// Returns filtered tool list. If tool count <= MaxTools, returns input unchanged.
func (tf *ToolFilter) FilterTools(tools []Tool, recentMessages string) []Tool {
	if !tf.cfg.Enabled || len(tools) <= tf.cfg.MaxTools {
		return tools
	}

	alwaysKeep := parseAlwaysKeep(tf.cfg.AlwaysKeep)
	intent := ClassifyIntent(recentMessages)
	keywords := extractKeywords(recentMessages)

	type scoredTool struct {
		tool  Tool
		score float64
	}

	scored := make([]scoredTool, 0, len(tools))
	kept := make(map[string]bool)

	// Phase 1: Always-keep tools
	for _, t := range tools {
		if alwaysKeep[t.Name] {
			kept[t.Name] = true
			scored = append(scored, scoredTool{tool: t, score: 100.0})
		}
	}

	// Phase 2: Score remaining tools
	for _, t := range tools {
		if kept[t.Name] {
			continue
		}
		score := tf.scoreTool(t, intent, keywords)
		scored = append(scored, scoredTool{tool: t, score: score})
	}

	// Phase 3: Sort by score, keep top MaxTools
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	maxTools := tf.cfg.MaxTools
	if maxTools > len(scored) {
		maxTools = len(scored)
	}

	result := make([]Tool, 0, maxTools)
	for i := 0; i < maxTools; i++ {
		result = append(result, scored[i].tool)
	}

	return result
}

func (tf *ToolFilter) scoreTool(t Tool, intent string, keywords map[string]bool) float64 {
	score := 0.0

	// Intent-based scoring
	nameLower := strings.ToLower(t.Name)
	descLower := strings.ToLower(t.Description)

	switch intent {
	case "code":
		if containsAny(nameLower, "edit", "write", "replace", "insert", "add", "symbol", "refactor") {
			score += 5.0
		}
	case "search":
		if containsAny(nameLower, "find", "search", "grep", "glob", "symbol", "query", "list") {
			score += 5.0
		}
	case "analysis":
		if containsAny(nameLower, "graph", "impact", "flow", "review", "detect", "analyze") {
			score += 5.0
		}
	case "action":
		if containsAny(nameLower, "run", "exec", "deploy", "build", "test") {
			score += 5.0
		}
	}

	// Keyword overlap with description
	for kw := range keywords {
		if strings.Contains(descLower, kw) || strings.Contains(nameLower, kw) {
			score += 2.0
		}
	}

	// Base score from description length (longer = more specific = more useful)
	score += float64(len(t.Description)) / 1000.0

	return score
}

func ClassifyIntent(text string) string {
	textLower := strings.ToLower(text)

	codeIndicators := 0
	searchIndicators := 0
	analysisIndicators := 0
	actionIndicators := 0

	for _, kw := range []string{"fix", "implement", "add", "create", "write", "edit", "modify", "update", "refactor",
		"สร้าง", "เขียน", "แก้", "เพิ่ม", "ลบ", "แก้ไข", "ทำ", "ติดตั้ง", "ปรับ", "ย้าย", "เปลี่ยน", "ตั้งค่า"} {
		if strings.Contains(textLower, kw) {
			codeIndicators++
		}
	}
	for _, kw := range []string{"find", "search", "where", "locate", "grep", "list", "show",
		"หา", "ค้นหา", "ดู", "เช็ค", "ตรวจสอบ", "แสดง", "เปิด", "อ่าน"} {
		if strings.Contains(textLower, kw) {
			searchIndicators++
		}
	}
	for _, kw := range []string{"analyze", "review", "explain", "understand", "how", "why", "what",
		"วิเคราะห์", "อธิบาย", "สรุป", "review", "ทำไม", "อะไร", "ยังไง", "เข้าใจ"} {
		if strings.Contains(textLower, kw) {
			analysisIndicators++
		}
	}
	for _, kw := range []string{"run", "execute", "deploy", "build", "test", "start", "restart",
		"รัน", "ทดสอบ", "สั่ง", "deploy", "build", "start", "หยุด", "เริ่ม", "ต่อ"} {
		if strings.Contains(textLower, kw) {
			actionIndicators++
		}
	}

	max := codeIndicators
	intent := "code"
	if searchIndicators > max {
		max = searchIndicators
		intent = "search"
	}
	if analysisIndicators > max {
		max = analysisIndicators
		intent = "analysis"
	}
	if actionIndicators > max {
		intent = "action"
	}

	return intent
}

func extractKeywords(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	keywords := make(map[string]bool)
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?'\"()[]{}")
		if len(w) > 3 {
			keywords[w] = true
		}
	}
	return keywords
}

func parseAlwaysKeep(s string) map[string]bool {
	result := make(map[string]bool)
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			result[name] = true
		}
	}
	return result
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
