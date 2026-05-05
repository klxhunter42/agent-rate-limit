package toolcomp

import (
	"strings"
	"testing"
)

func TestCompressJSON(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 50})
	input := `{
  "name": "test-package-with-a-long-name",
  "version": "1.0.0",
  "description": "A test package for verifying JSON compression in the tool result compressor",
  "main": "index.js",
  "scripts": {
    "test": "jest --coverage",
    "build": "tsc && webpack",
    "lint": "eslint src/"
  },
  "dependencies": {
    "foo": "^1.0.0",
    "bar": "^2.0.0",
    "baz": "^3.0.0",
    "qux": "^4.0.0"
  },
  "devDependencies": {
    "typescript": "^5.0.0",
    "jest": "^29.0.0"
  }
}`
	result, saved := tc.Compress(input)
	if saved <= 0 {
		t.Errorf("expected JSON compression to save chars, saved=%d", saved)
	}
	if strings.Contains(result, "  ") {
		t.Error("compact JSON should not have double spaces")
	}
	if len(result) >= len(input) {
		t.Error("result should be shorter than input")
	}
}

func TestCompressShellLs(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 10})
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "-rw-r--r--  1 user  staff  1234 May  6 file_"+strings.Repeat("x", 10)+".go")
	}
	input := strings.Join(lines, "\n")

	result, saved := tc.Compress(input)
	if saved <= 0 {
		t.Error("expected savings on long ls output")
	}
	if strings.Contains(result, "more files") == false {
		t.Error("should contain summary line")
	}
}

func TestCompressShellLsShort(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 50})
	input := "-rw-r--r--  1 user  staff  1234 May  6 a.go\n-rw-r--r--  1 user  staff  5678 May  6 b.go"
	_, saved := tc.Compress(input)
	if saved != 0 {
		t.Error("short ls should not be compressed")
	}
}

func TestCompressTable(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 10})
	var lines []string
	lines = append(lines, "| Name | Age | City |")
	lines = append(lines, "|------|-----|------|")
	for i := 0; i < 100; i++ {
		lines = append(lines, "| user_"+strings.Repeat("x", 10)+" | 25 | Bangkok |")
	}
	input := strings.Join(lines, "\n")

	result, saved := tc.Compress(input)
	if saved <= 0 {
		t.Error("expected savings on long table")
	}
	// Separator line should be stripped
	tableLines := strings.Split(result, "\n")
	for _, l := range tableLines[2:] {
		if strings.HasPrefix(strings.TrimSpace(l), "|-") {
			t.Error("separator lines should be stripped after header")
		}
	}
}

func TestCompressDiff(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 20})
	var lines []string
	lines = append(lines, "diff --git a/file.go b/file.go")
	lines = append(lines, "--- a/file.go")
	lines = append(lines, "+++ b/file.go")
	lines = append(lines, "@@ -1,5 +1,5 @@")
	lines = append(lines, " unchanged line 1")
	for i := 0; i < 50; i++ {
		lines = append(lines, " unchanged line "+strings.Repeat("x", 20))
	}
	lines = append(lines, "-old code here")
	lines = append(lines, "+new code here")
	lines = append(lines, " context after change")

	input := strings.Join(lines, "\n")
	result, saved := tc.Compress(input)
	if saved <= 0 {
		t.Error("expected savings on long diff")
	}
	if !strings.Contains(result, "old code here") || !strings.Contains(result, "new code here") {
		t.Error("changed lines should be preserved")
	}
}

func TestCompressLog(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 20})
	var lines []string
	for i := 0; i < 100; i++ {
		if i%10 == 0 {
			lines = append(lines, "2026-05-06 10:00:01 [INFO] Server started on port 8080")
		}
		lines = append(lines, "2026-05-06 10:00:"+formatSeconds(i%60)+" [INFO] Request processed successfully")
	}
	input := strings.Join(lines, "\n")

	_, saved := tc.Compress(input)
	if saved <= 0 {
		t.Error("expected savings on log with duplicates")
	}
}

func TestCompressDisabled(t *testing.T) {
	tc := New(Config{Enabled: false, MaxLines: 50})
	input := strings.Repeat("line\n", 100)
	result, saved := tc.Compress(input)
	if saved != 0 {
		t.Error("disabled should not compress")
	}
	if result != input {
		t.Error("disabled should return input unchanged")
	}
}

func TestCompressTooShort(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 50})
	input := "short text"
	_, saved := tc.Compress(input)
	if saved != 0 {
		t.Error("short text should not be compressed")
	}
}

func TestCompressEmpty(t *testing.T) {
	tc := New(Config{Enabled: true, MaxLines: 50})
	result, saved := tc.Compress("")
	if saved != 0 {
		t.Error("empty should not compress")
	}
	if result != "" {
		t.Error("empty should return empty")
	}
}

func TestDetectFormatJSON(t *testing.T) {
	f := detectFormat(`{"key": "value", "num": 42}`)
	if f != FormatJSON {
		t.Errorf("expected FormatJSON, got %d", f)
	}
}

func TestDetectFormatDiff(t *testing.T) {
	f := detectFormat("diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new")
	if f != FormatDiff {
		t.Errorf("expected FormatDiff, got %d", f)
	}
}

func TestDetectFormatLog(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "[INFO] Processing request "+strings.Repeat("x", 20))
	}
	f := detectFormat(strings.Join(lines, "\n"))
	if f != FormatLog {
		t.Errorf("expected FormatLog, got %d", f)
	}
}

func TestDetectFormatTable(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, "| col1 | col2 | col3 |")
	}
	f := detectFormat(strings.Join(lines, "\n"))
	if f != FormatTable {
		t.Errorf("expected FormatTable, got %d", f)
	}
}

func TestDetectFormatUnknown(t *testing.T) {
	f := detectFormat("Just some regular text without any special format.")
	if f != FormatProse {
		t.Errorf("expected FormatProse, got %d", f)
	}
}

func formatSeconds(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n))
}
