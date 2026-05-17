package desctrim

import "testing"

func TestTrimDescriptions_Disabled(t *testing.T) {
	cfg := Config{Enabled: false, MaxLen: 200, AlwaysSkip: "Read,Edit,Write,Bash"}
	dt := New(cfg)
	tools := []ToolDesc{
		{Name: "Agent", Description: longDesc()},
	}
	out, saved := dt.TrimDescriptions(tools)
	if saved != 0 {
		t.Fatalf("disabled: expected 0 saved, got %d", saved)
	}
	if out[0].Description != longDesc() {
		t.Fatal("disabled: description should be unchanged")
	}
}

func TestTrimDescriptions_ShortUnchanged(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 200, AlwaysSkip: ""}
	dt := New(cfg)
	tools := []ToolDesc{
		{Name: "Read", Description: "Reads a file from the local filesystem."},
	}
	out, saved := dt.TrimDescriptions(tools)
	if saved != 0 {
		t.Fatalf("short desc: expected 0 saved, got %d", saved)
	}
	if out[0].Description != "Reads a file from the local filesystem." {
		t.Fatal("short desc should be unchanged")
	}
}

func TestTrimDescriptions_AlwaysSkip(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 50, AlwaysSkip: "Read"}
	dt := New(cfg)
	tools := []ToolDesc{
		{Name: "Read", Description: longDesc()},
	}
	out, saved := dt.TrimDescriptions(tools)
	if saved != 0 {
		t.Fatalf("skip: expected 0 saved, got %d", saved)
	}
	if out[0].Description != longDesc() {
		t.Fatal("skipped tool should be unchanged")
	}
}

func TestTrimDescriptions_ParagraphSplit(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 200, AlwaysSkip: ""}
	dt := New(cfg)
	desc := "First paragraph is short enough.\n\nSecond paragraph with lots of extra text that should be removed entirely from the description. Plus even more padding text to push total length well beyond the two hundred character max length threshold so the trim logic actually triggers."
	tools := []ToolDesc{{Name: "Agent", Description: desc}}
	out, saved := dt.TrimDescriptions(tools)
	if saved == 0 {
		t.Fatal("expected savings from paragraph split")
	}
	if out[0].Description != "First paragraph is short enough." {
		t.Fatalf("unexpected trimmed result: %q", out[0].Description)
	}
}

func TestTrimDescriptions_SentenceSplit(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 80, AlwaysSkip: ""}
	dt := New(cfg)
	desc := "Launch a new agent to handle complex multi-step tasks. Each agent type has specific capabilities. This is a third sentence that should be cut."
	tools := []ToolDesc{{Name: "Agent", Description: desc}}
	out, saved := dt.TrimDescriptions(tools)
	if saved == 0 {
		t.Fatal("expected savings from sentence split")
	}
	if out[0].Description != "Launch a new agent to handle complex multi-step tasks." {
		t.Fatalf("unexpected trimmed result: %q", out[0].Description)
	}
}

func TestTrimDescriptions_HardTruncate(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 30, AlwaysSkip: ""}
	dt := New(cfg)
	desc := "A very long description without any sentence breaks or paragraph breaks just a continuous string that goes on and on"
	tools := []ToolDesc{{Name: "X", Description: desc}}
	out, saved := dt.TrimDescriptions(tools)
	if saved == 0 {
		t.Fatal("expected savings from hard truncate")
	}
	if len(out[0].Description) > 35 { // maxLen + len("...")
		t.Fatalf("hard truncate result too long: %q", out[0].Description)
	}
	if out[0].Description != "A very long description withou..." {
		t.Fatalf("unexpected hard truncate: %q", out[0].Description)
	}
}

func TestTrimDescriptions_MultipleTools(t *testing.T) {
	cfg := Config{Enabled: true, MaxLen: 40, AlwaysSkip: "Bash"}
	dt := New(cfg)
	tools := []ToolDesc{
		{Name: "Bash", Description: longDesc()},      // skipped
		{Name: "Agent", Description: longDesc()},     // trimmed
		{Name: "Read", Description: "Short desc."},   // unchanged (short)
		{Name: "WebSearch", Description: longDesc()}, // trimmed
	}
	out, saved := dt.TrimDescriptions(tools)
	if saved == 0 {
		t.Fatal("expected savings")
	}
	if out[0].Description != longDesc() {
		t.Fatal("Bash should be unchanged (skip list)")
	}
	if out[1].Description == longDesc() {
		t.Fatal("Agent should be trimmed")
	}
	if out[2].Description != "Short desc." {
		t.Fatal("Read short desc should be unchanged")
	}
	if out[3].Description == longDesc() {
		t.Fatal("WebSearch should be trimmed")
	}
}

func longDesc() string {
	return "Launch a new agent to handle complex, multi-step tasks. Each agent type has specific capabilities and tools available to it.\n\nAvailable agent types and the tools they have access to:\n- claude: Catch-all for any task that doesn't fit a more specific agent.\n- Explore: Fast read-only search agent for locating code.\n\nWhen using the Agent tool, specify a subagent_type parameter to select which agent type to use."
}
