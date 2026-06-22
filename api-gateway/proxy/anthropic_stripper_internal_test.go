package proxy

import (
	"testing"
)

// TestStripperWithholdsUnclosedTag proves the latency mechanism: when a streamed
// chunk opens a tag like <thinking> with no matching close, Feed() returns ""
// (the text is withheld from the client) instead of passing it through. The
// relay then writes an empty content_block_delta, so the client sees no text
// until the stripper later releases the held span. With STRIP_GLM_TOOL_XML=false
// (the new default) the stripper is never created, so this never happens.
func TestStripperWithholdsUnclosedTag(t *testing.T) {
	s := &toolUseStripper{}

	// Open tag with no close -> text withheld (not delivered to client).
	out := s.Feed("<thinking>let me reason about this")
	if out != "" {
		t.Fatalf("open tag should be withheld (empty), got %q", out)
	}
	// Trailing span with an unclosed tag is held until Flush at stream end.
	tail := s.Feed("final <div> unclosed")
	_ = tail
	flushed := s.Flush()
	t.Logf("withheld on open tag (chunk returned empty); held span released at flush: %q", flushed)
}

// TestStripperPassesThroughWhenNoTag confirms the fast path: plain text with no
// '<' is returned immediately (not buffered).
func TestStripperPassesThroughWhenNoTag(t *testing.T) {
	s := &toolUseStripper{}
	out := s.Feed("plain text no tags here")
	if out != "plain text no tags here" {
		t.Errorf("expected pass-through, got %q", out)
	}
}
