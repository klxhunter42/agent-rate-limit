package summarizer

import (
	"strings"
	"testing"
)

func TestTextRankBasic(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.5, Method: "textrank"}}

	content := `The quick brown fox jumps over the lazy dog. This is a common pangram used for testing. Pangrams contain every letter of the alphabet at least once. Testing is important for software quality. Software quality depends on many factors including testing coverage. Good testing practices lead to fewer bugs in production.`

	result := s.textRankSummarize(content)
	if result == "" {
		t.Fatal("textRankSummarize returned empty")
	}
	if len(result) >= len(content) {
		t.Errorf("result should be shorter than input: got %d >= %d", len(result), len(content))
	}
}

func TestTextRankPreservesOrder(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.6, Method: "textrank"}}

	content := `First sentence about alpha. Second sentence about beta. Third sentence mentions alpha and beta together. Fourth sentence about gamma. Fifth sentence discusses alpha again in detail.`

	result := s.textRankSummarize(content)

	// Sentences should appear in original order
	sentences := strings.Split(content, ". ")
	for i := 0; i < len(sentences)-1; i++ {
		// Check that sentence i appears before sentence i+1 if both are present
		if strings.Contains(result, sentences[i]) && strings.Contains(result, sentences[i+1]) {
			idxI := strings.Index(result, sentences[i])
			idxI1 := strings.Index(result, sentences[i+1])
			if idxI > idxI1 {
				t.Errorf("sentence %d appears after sentence %d in output", i, i+1)
			}
		}
	}
}

func TestTextRankFewerThan3Sentences(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.3, Method: "textrank"}}

	content := `Short text. Only two sentences here.`

	result := s.textRankSummarize(content)
	if result == "" {
		t.Fatal("should not return empty for <3 sentences")
	}
}

func TestTextRankEmpty(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.3, Method: "textrank"}}
	result := s.textRankSummarize("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestTextRankMultiParagraph(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.4, Method: "textrank"}}

	content := `Token optimization reduces the cost of LLM API calls. Several techniques exist for compressing prompts. Semantic deduplication removes duplicate sentences. Delta encoding sends only changes between requests.

Caveman compression instructs the model to write concisely. This reduces output token count significantly. TextComp removes filler words and verbose phrases. The combination of techniques yields substantial savings.

Progressive disclosure shows summaries first. Users can request more detail when needed. This approach saves tokens on most requests. Only interested users pay the full cost.`

	result := s.textRankSummarize(content)
	if result == "" {
		t.Fatal("empty result")
	}
	if len(result) >= len(content) {
		t.Errorf("result should be shorter: got %d >= %d", len(result), len(content))
	}
}

func TestTextRankWithinBudget(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.3, Method: "textrank"}}

	content := strings.Repeat("This is a test sentence about token optimization. ", 50)
	maxLen := int(float64(len(content)) * 0.3)

	result := s.textRankSummarize(content)
	if len(result) > maxLen+100 { // small tolerance
		t.Errorf("result exceeds budget: got %d, max %d", len(result), maxLen)
	}
}

func TestJaccardTR(t *testing.T) {
	a := map[string]bool{"hello": true, "world": true, "foo": true}
	b := map[string]bool{"hello": true, "world": true, "bar": true}

	j := jaccardTR(a, b)
	if j < 0.4 || j > 0.6 {
		t.Errorf("expected ~0.5, got %.3f", j)
	}
}

func TestJaccardTRIdentical(t *testing.T) {
	a := map[string]bool{"hello": true, "world": true}
	j := jaccardTR(a, a)
	if j != 1.0 {
		t.Errorf("identical sets should be 1.0, got %.3f", j)
	}
}

func TestJaccardTRDisjoint(t *testing.T) {
	a := map[string]bool{"hello": true}
	b := map[string]bool{"world": true}
	j := jaccardTR(a, b)
	if j != 0.0 {
		t.Errorf("disjoint sets should be 0.0, got %.3f", j)
	}
}

func TestTokenizeTR(t *testing.T) {
	tokens := tokenizeTR("Hello, World! Testing abc.")
	if !tokens["hello"] {
		t.Error("missing 'hello'")
	}
	if !tokens["world"] {
		t.Error("missing 'world'")
	}
	if !tokens["testing"] {
		t.Error("missing 'testing'")
	}
	if tokens["ab"] {
		t.Error("'ab' should be filtered (len <= 2)")
	}
}

func TestSplitSentencesTR(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence."
	sentences := splitSentencesTR(text)
	if len(sentences) != 3 {
		t.Errorf("expected 3 sentences, got %d", len(sentences))
	}
}

func TestConvergedTR(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	b := []float64{1.00001, 2.00001, 3.00001}
	if !convergedTR(a, b) {
		t.Error("should converge with tiny diff")
	}
	b[0] = 1.1
	if convergedTR(a, b) {
		t.Error("should not converge with large diff")
	}
}

func TestExtractiveSummarize(t *testing.T) {
	s := &Summarizer{cfg: Config{MaxRatio: 0.5, Method: "firstsentence"}}

	content := `First paragraph with first sentence. And more text in the paragraph.

Second paragraph with its first sentence. Plus additional content here.

Third paragraph starting with a sentence. Ending with some more words.`

	result := s.extractiveSummarize(content)
	if result == "" {
		t.Fatal("empty result")
	}
	if len(result) >= len(content) {
		t.Errorf("result should be shorter: got %d >= %d", len(result), len(content))
	}
}
