package masking

// TextSpan represents extracted text from a request body.
type TextSpan struct {
	Text         string
	Path         string // JSON path, e.g. "messages[0].content[1].text"
	MessageIndex int
	PartIndex    int
	NestedIndex  int    // for tool_result nested content (-1 = unused)
	Role         string // "system", "user", "assistant", "tool"
}

// MaskedSpan holds a masked version of a TextSpan.
type MaskedSpan struct {
	Path         string
	MaskedText   string
	MessageIndex int
	PartIndex    int
	NestedIndex  int
}

// SecretLocation is a detected secret span in text.
type SecretLocation struct {
	Start int
	End   int
	Type  string
}

// PIIEntity is a detected PII span from Presidio.
type PIIEntity struct {
	EntityType string
	Start      int
	End        int
	Score      float64
}
