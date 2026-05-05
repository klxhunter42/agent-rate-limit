package proxy

import (
	"strings"
	"testing"
)

func TestToolUseStripper_CompleteBlock(t *testing.T) {
	var s toolUseStripper
	got := s.Feed("hello <tool_use><name>x</name></tool_use> world")
	want := "hello world"
	if got != want {
		t.Errorf("Feed complete block: got %q, want %q", got, want)
	}
}

func TestToolUseStripper_SplitAcrossChunks(t *testing.T) {
	var s toolUseStripper
	got1 := s.Feed("hello <too")
	// partialTagEnd detects "<too" as partial "<tool_use", buffers it
	if got1 != "hello " {
		t.Logf("chunk1: got %q, expected 'hello '", got1)
	}
	got2 := s.Feed("l_use><name>x</name></tool_use> world")
	total := got1 + got2 + s.Flush()
	// regex \s* eats space after </tool_use>, so result is "hello world"
	if total != "hello world" {
		t.Errorf("total: got %q, want %q", total, "hello world")
	}
}

func TestToolUseStripper_MultipleBlocks(t *testing.T) {
	var s toolUseStripper
	got := s.Feed("a<tool_use>1</tool_use>b<tool_use>2</tool_use>c")
	want := "abc"
	if got != want {
		t.Errorf("multiple blocks: got %q, want %q", got, want)
	}
}

func TestToolUseStripper_NoBlock(t *testing.T) {
	var s toolUseStripper
	got := s.Feed("just normal text")
	want := "just normal text"
	if got != want {
		t.Errorf("no block: got %q, want %q", got, want)
	}
}

func TestToolUseStripper_FlushIncomplete(t *testing.T) {
	var s toolUseStripper
	got := s.Feed("hello <tool_use>never closed")
	got += s.Flush()
	if got != "hello " {
		t.Errorf("flush incomplete: got %q, want %q", got, "hello ")
	}
}

func TestToolUseBlockRe_NonStream(t *testing.T) {
	input := "before<tool_use>\n<server_name>anthropic</server_name>\n<tool_name>Agent</tool_name>\n</tool_use>after"
	got := toolUseBlockRe.ReplaceAllString(input, "")
	want := "beforeafter"
	if got != want {
		t.Errorf("regex non-stream: got %q, want %q", got, want)
	}
}

func TestToolUseStripper_RealWorldMultiBlock(t *testing.T) {
	var s toolUseStripper
	chunks := []string{
		"I'll search for that.\n\n",
		"<tool_use>\n<server_name>anthropic</server_name>\n",
		"<tool_name>Agent</tool_name>\n<arguments>\n",
		"<agent_name>doc-fixer</agent_name>\n</arguments>\n</tool_use>\n\n",
		"Here is what I found.",
	}
	var total string
	for _, c := range chunks {
		total += s.Feed(c)
	}
	total += s.Flush()
	if strings.Contains(total, "<tool_use>") || strings.Contains(total, "</tool_use>") {
		t.Errorf("real-world: tool_use tags leaked: %q", total)
	}
	if !strings.Contains(total, "I'll search") || !strings.Contains(total, "Here is what") {
		t.Errorf("real-world: real text lost: %q", total)
	}
}
