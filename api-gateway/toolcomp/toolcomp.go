package toolcomp

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Enabled  bool
	MaxLines int
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
		Enabled:  envBoolOr("TOOLCOMP_ENABLED", true),
		MaxLines: envIntOr("TOOLCOMP_MAX_LINES", 200),
	}
}

type ToolComp struct {
	cfg Config
}

func New(cfg Config) *ToolComp {
	return &ToolComp{cfg: cfg}
}

// Format represents detected tool result format.
type Format int

const (
	FormatUnknown Format = iota
	FormatJSON
	FormatShellLs
	FormatTable
	FormatDiff
	FormatLog
	FormatProse
)

// Compress applies format-aware compression to tool_result content.
// Returns compressed text and chars saved. Returns input unchanged if
// compression would not reduce size.
func (tc *ToolComp) Compress(text string) (string, int) {
	if !tc.cfg.Enabled || text == "" || len(text) < 256 {
		return text, 0
	}

	format := detectFormat(text)
	compressed := compressByFormat(text, format, tc.cfg.MaxLines)

	saved := len(text) - len(compressed)
	if saved <= 0 {
		return text, 0
	}
	return compressed, saved
}

func detectFormat(text string) Format {
	trimmed := strings.TrimSpace(text)

	// JSON: starts with { or [
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		if json.Valid([]byte(trimmed)) {
			return FormatJSON
		}
	}

	lines := strings.Split(text, "\n")

	// Diff: starts with @@ or diff --git or --- a/ or +++ b/
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "diff --git") || strings.HasPrefix(first, "@@") ||
			strings.HasPrefix(first, "--- ") || strings.HasPrefix(first, "+++ ") {
			return FormatDiff
		}
	}

	// Log: patterns like [INFO], [ERROR], timestamps, level= prefix
	if len(lines) > 3 {
		logCount := 0
		for _, l := range lines[:min(10, len(lines))] {
			if isLogLine(l) {
				logCount++
			}
		}
		if float64(logCount)/float64(min(10, len(lines))) > 0.5 {
			return FormatLog
		}
	}

	// Table: 3+ lines with pipe separators and 2+ columns
	if len(lines) > 3 {
		pipeCount := 0
		for _, l := range lines[:min(10, len(lines))] {
			cols := strings.Count(l, "|")
			if cols >= 2 {
				pipeCount++
			}
		}
		if float64(pipeCount)/float64(min(10, len(lines))) > 0.5 {
			return FormatTable
		}
	}

	// Shell ls/dir: lines that look like file listings
	if len(lines) > 3 {
		fileCount := 0
		for _, l := range lines[:min(10, len(lines))] {
			if isFileListingLine(l) {
				fileCount++
			}
		}
		if float64(fileCount)/float64(min(10, len(lines))) > 0.5 {
			return FormatShellLs
		}
	}

	return FormatProse
}

var (
	logLineRe   = regexp.MustCompile(`^(\d{4}[-/]\d{2}[-/]\d{2}|\d{2}:\d{2}|\[\w+\]|level=|ERROR|WARN|INFO|DEBUG|TRACE|FATAL)`)
	fileLineRe  = regexp.MustCompile(`^[-drlsbcmpSwD]{1,}\s+\S+\s+\S+\s+\d+`)              // ls -l format
	fileLineRe2 = regexp.MustCompile(`\.(go|py|ts|js|json|yaml|yml|md|txt|toml|cfg|mod)$`) // file extensions
	separatorRe = regexp.MustCompile(`^\|?[-:| ]+\|?$`)                                    // table separator lines
)

func isLogLine(line string) bool {
	return logLineRe.MatchString(strings.TrimSpace(line))
}

func isFileListingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if fileLineRe.MatchString(trimmed) {
		return true
	}
	// File with extension, no code indicators
	if fileLineRe2.MatchString(trimmed) && len(trimmed) < 200 {
		return true
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func compressByFormat(text string, format Format, maxLines int) string {
	switch format {
	case FormatJSON:
		return compressJSON(text)
	case FormatShellLs:
		return compressShellLs(text, maxLines)
	case FormatTable:
		return compressTable(text, maxLines)
	case FormatDiff:
		return compressDiff(text, maxLines)
	case FormatLog:
		return compressLog(text, maxLines)
	default:
		return text
	}
}

func compressJSON(text string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(text)); err != nil {
		return text
	}
	result := buf.String()
	if len(result) >= len(text) {
		return text
	}
	return result
}

func compressShellLs(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	var result []string
	// Keep first N lines
	head := min(maxLines-5, len(lines))
	if head < 1 {
		head = 1
	}
	result = append(result, lines[:head]...)

	// Summary line
	remaining := len(lines) - head - 2
	if remaining > 0 {
		result = append(result, "")
		result = append(result, "... "+strconv.Itoa(remaining)+" more files/directories ...")
		result = append(result, "")
	}

	// Keep last 2 lines
	if len(lines) > head {
		result = append(result, lines[len(lines)-2:]...)
	}

	return strings.Join(result, "\n")
}

func compressTable(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		// Skip separator lines (----, ====, etc)
		if separatorRe.MatchString(trimmed) && len(result) > 1 {
			continue
		}
		result = append(result, l)
		if len(result) >= maxLines {
			break
		}
	}

	if len(result) < len(lines) {
		result = append(result, "... "+strconv.Itoa(len(lines)-len(result))+" more rows ...")
	}

	return strings.Join(result, "\n")
}

func compressDiff(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	var result []string
	changes := 0
	for _, l := range lines {
		trimmed := l
		if len(trimmed) > 1 {
			trimmed = l[1:]
		}
		// Keep header lines, changed lines (+/-), and 1 context line after changes
		if strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "@@") ||
			strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") ||
			strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			result = append(result, l)
			if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
				changes++
			}
		} else if len(result) > 0 && (strings.HasPrefix(result[len(result)-1], "+") ||
			strings.HasPrefix(result[len(result)-1], "-") ||
			strings.HasPrefix(result[len(result)-1], "@@")) {
			// Keep 1 context line after changes/hunks
			result = append(result, l)
		}
		if len(result) >= maxLines {
			break
		}
	}

	if len(result) < len(lines) {
		removed := len(lines) - len(result)
		result = append(result, "\n... "+strconv.Itoa(removed)+" unchanged lines omitted ...")
	}

	return strings.Join(result, "\n")
}

func compressLog(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	var result []string
	prevLine := ""

	for _, l := range lines {
		// Dedup consecutive identical lines (ignore leading timestamp)
		normalized := dedupKeyLog(l)
		if normalized == prevLine {
			continue
		}
		prevLine = normalized
		result = append(result, l)
		if len(result) >= maxLines {
			break
		}
	}

	if len(result) < len(lines) {
		result = append(result, "... "+strconv.Itoa(len(lines)-len(result))+" more log lines ...")
	}

	return strings.Join(result, "\n")
}

func dedupKeyLog(line string) string {
	// Strip leading timestamp and level for dedup comparison
	trimmed := strings.TrimSpace(line)
	// Remove common timestamp patterns
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}[\sT]\d{2}:\d{2}:\d{2}`),
		regexp.MustCompile(`^\d{2}:\d{2}:\d{2}`),
		regexp.MustCompile(`^\[\w+\]\s*`),
		regexp.MustCompile(`^level=\w+\s*`),
	} {
		trimmed = re.ReplaceAllString(trimmed, "")
	}
	return strings.TrimSpace(trimmed)
}
