package masking

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProcessChunkJSON_UndefinedFallbackKeepsJSONValid proves that when GLM
// emits the literal "undefined" noise token instead of a [[TYPE_N]] placeholder
// inside a tool_use input_json_delta, the fallback replacement must still yield
// valid JSON. The original value (email/secret/path) may contain characters
// that break JSON (" , \ , newline); the fallback path must JSON-escape them.
//
// Regression: the accumulated tool_use input failed to parse, surfacing in
// Claude Code as "The model's tool call could not be parsed".
func TestProcessChunkJSON_UndefinedFallbackKeepsJSONValid(t *testing.T) {
	secrets := NewMaskContext()
	// Original contains a literal double-quote and a backslash: JSON-breaking.
	secrets.Mapping["[[EMAIL_ADDRESS_1]]"] = `a"b\c`

	u := NewStreamUnmasker(nil, secrets)
	// glmNoiseMode is on by default (NewStreamUnmasker), matching GLM relay.

	// GLM emitted literal "undefined" instead of preserving [[EMAIL_ADDRESS_1]].
	chunk := `{"email":"undefined"}`

	got := u.ProcessChunkJSON(chunk)
	t.Logf("output: %q", got)

	// The client concatenates every input_json_delta then JSON.parses it.
	// The single-chunk case must already be valid JSON.
	if !json.Valid([]byte(got)) {
		t.Fatalf("ProcessChunkJSON produced invalid JSON: %q\n(special chars in original must be JSON-escaped)", got)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("unmarshal failed: %v (raw=%q)", err, got)
	}
	if m["email"] != `a"b\c` {
		t.Fatalf("expected email restored to a\"b\\c, got %q", m["email"])
	}
}

// TestProcessChunkJSON_UndefinedFallbackNewlineKeepsJSONValid covers an
// original containing a newline, which must be escaped to \n in JSON mode.
func TestProcessChunkJSON_UndefinedFallbackNewlineKeepsJSONValid(t *testing.T) {
	secrets := NewMaskContext()
	secrets.Mapping["[[PATH_1]]"] = "line1\nline2"

	u := NewStreamUnmasker(nil, secrets)

	got := u.ProcessChunkJSON(`{"p":"undefined"}`)
	t.Logf("output: %q", got)
	if !json.Valid([]byte(got)) {
		t.Fatalf("produced invalid JSON: %q", got)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("unmarshal failed: %v (raw=%q)", err, got)
	}
	if m["p"] != "line1\nline2" {
		t.Fatalf("expected newline preserved, got %q", m["p"])
	}
}

// TestProcessChunkJSON_PlaceholderSplitKeepsJSONValid is a baseline for the
// normal placeholder path: a placeholder split across two chunks with a
// JSON-breaking original must still parse.
func TestProcessChunkJSON_PlaceholderSplitKeepsJSONValid(t *testing.T) {
	secrets := NewMaskContext()
	secrets.Mapping["[[IP_ADDRESS_1]]"] = `1.2.3.4"x`

	u := NewStreamUnmasker(nil, secrets)

	out := u.ProcessChunkJSON(`{"ip":"[[`) +
		u.ProcessChunkJSON(`IP_ADDRESS_1]]"}`)
	out = strings.TrimSpace(out)
	t.Logf("output: %q", out)
	if !json.Valid([]byte(out)) {
		t.Fatalf("split-placeholder path produced invalid JSON: %q", out)
	}
}

// TestProcessChunkJSON_UndefinedSplitAcrossChunksKeepsJSONValid covers the real
// production failure mode: GLM emits "undefined" but the token, the surrounding
// JSON quotes, and a JSON-breaking original value all arrive split across
// separate SSE input_json_delta chunks. The concatenation the client parses
// must still be valid JSON.
func TestProcessChunkJSON_UndefinedSplitAcrossChunksKeepsJSONValid(t *testing.T) {
	secrets := NewMaskContext()
	secrets.Mapping["[[EMAIL_ADDRESS_1]]"] = `a"b\c`

	u := NewStreamUnmasker(nil, secrets)

	// "undefined" split as "unde" | "fined" across the value position.
	out := u.ProcessChunkJSON(`{"email":"unde`) +
		u.ProcessChunkJSON(`fined"}`)
	t.Logf("output: %q", out)
	if !json.Valid([]byte(out)) {
		t.Fatalf("split-undefined produced invalid JSON: %q", out)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal failed: %v (raw=%q)", err, out)
	}
	if m["email"] != `a"b\c` {
		t.Fatalf("expected email a\"b\\c, got %q", m["email"])
	}
}
