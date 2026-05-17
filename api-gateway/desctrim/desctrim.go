package desctrim

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Enabled    bool
	MaxLen     int
	AlwaysSkip string // comma-separated tool names to skip
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled:    envBoolOr("DESCTRIM_ENABLED", true),
		MaxLen:     envIntOr("DESCTRIM_MAX_LEN", 200),
		AlwaysSkip: envOr("DESCTRIM_ALWAYS_SKIP", "Read,Edit,Write,Bash"),
	}
}

type DescTrim struct {
	cfg  Config
	skip map[string]bool
}

type ToolDesc struct {
	Name        string
	Description string
}

func New(cfg Config) *DescTrim {
	return &DescTrim{
		cfg:  cfg,
		skip: parseSkipList(cfg.AlwaysSkip),
	}
}

// TrimDescriptions trims tool descriptions to first paragraph/sentence.
// Returns trimmed tools and total chars saved.
func (dt *DescTrim) TrimDescriptions(tools []ToolDesc) ([]ToolDesc, int) {
	if !dt.cfg.Enabled {
		return tools, 0
	}

	totalSaved := 0
	result := make([]ToolDesc, len(tools))
	for i, t := range tools {
		trimmed, saved := dt.trimOne(t)
		totalSaved += saved
		result[i] = trimmed
	}
	return result, totalSaved
}

func (dt *DescTrim) trimOne(t ToolDesc) (ToolDesc, int) {
	if dt.skip[t.Name] {
		return t, 0
	}
	desc := t.Description
	if len(desc) <= dt.cfg.MaxLen {
		return t, 0
	}

	// Phase 1: keep first paragraph
	if idx := strings.Index(desc, "\n\n"); idx > 0 {
		desc = desc[:idx]
	}
	// Phase 2: keep first sentence if still too long
	if len(desc) > dt.cfg.MaxLen {
		// Find first ". " (sentence end followed by space/newline)
		if idx := sentenceEnd(desc); idx > 0 {
			desc = desc[:idx+1] // include the period
		}
	}
	// Phase 3: hard truncate
	if len(desc) > dt.cfg.MaxLen {
		desc = desc[:dt.cfg.MaxLen] + "..."
	}

	saved := len(t.Description) - len(desc)
	return ToolDesc{Name: t.Name, Description: desc}, saved
}

// sentenceEnd finds the first ". " or ".\n" boundary in s.
func sentenceEnd(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && (s[i+1] == ' ' || s[i+1] == '\n') {
			return i
		}
	}
	return -1
}

func parseSkipList(s string) map[string]bool {
	m := make(map[string]bool)
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			m[name] = true
		}
	}
	return m
}
