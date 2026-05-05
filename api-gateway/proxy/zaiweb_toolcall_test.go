package proxy

import (
	"strings"
	"testing"
)

func TestXmlBlockRe_ToolCallAndResponse(t *testing.T) {
	// Actual GLM output format: <tool_call\nJSON\n</tool_call\n
	input := "I'll search for that.<tool_call\n{\"name\": \"Agent\", \"arguments\": {\"prompt\": \"search\"}}\n</tool_call\n<tool_response\nI don't have access to tools.\n</tool_response\nNow I can help."
	got := xmlBlockRe.ReplaceAllString(input, "")
	want := "I'll search for that.Now I can help."
	if got != want {
		t.Errorf("tool_call/response: got %q, want %q", got, want)
	}
}

func TestXmlBlockRe_ToolCallStreaming(t *testing.T) {
	var s toolUseStripper
	chunks := []string{
		"I'll search.\n\n",
		"<tool_call",
		">\n",
		`{"name": "Agent"}`,
		"\n</tool_call",
		">\n",
		"\n<tool_response",
		">\n",
		"Not found.\n",
		"</tool_response",
		">\n\n",
		"Here is what I found.",
	}
	var total string
	for _, c := range chunks {
		total += s.Feed(c)
	}
	total += s.Flush()
	if strings.Contains(total, "<tool_") || strings.Contains(total, "</tool_") {
		t.Errorf("streaming: XML leaked: %q", total)
	}
	if !strings.Contains(total, "I'll search") || !strings.Contains(total, "Here is what") {
		t.Errorf("streaming: real text lost: %q", total)
	}
}

func TestXmlBlockRe_DirectiveTag(t *testing.T) {
	input := "before<directive><!-- some system note --></directive>after"
	got := xmlBlockRe.ReplaceAllString(input, "")
	want := "beforeafter"
	if got != want {
		t.Errorf("directive: got %q, want %q", got, want)
	}
}

func TestXmlBlockRe_MixedTags(t *testing.T) {
	input := "a<tool_call\n{\"name\":\"Bash\"}\n</tool_call\nb<directive>warn</directive>c<tool_use><name>x</name></tool_use>d"
	got := xmlBlockRe.ReplaceAllString(input, "")
	want := "abcd"
	if got != want {
		t.Errorf("mixed: got %q, want %q", got, want)
	}
}
