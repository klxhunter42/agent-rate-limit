package summarizer

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// TextRank performs unsupervised extractive summarization using a
// PageRank-style algorithm over a sentence similarity graph.
// Based on "TextRank: Bringing Order into Texts" (Mihalcea & Tarau, 2004).

const (
	trDamping     = 0.85
	trIterations  = 10
	trConvergence = 0.0001
)

// textRankSummarize selects the most central sentences within the budget.
func (s *Summarizer) textRankSummarize(content string) string {
	sentences := splitSentencesTR(content)
	if len(sentences) < 3 {
		return s.extractiveSummarize(content)
	}

	// Build similarity graph
	n := len(sentences)
	scores := make([]float64, n)
	for i := range scores {
		scores[i] = 1.0
	}

	tokens := make([]map[string]bool, n)
	for i, s := range sentences {
		tokens[i] = tokenizeTR(s)
	}

	// Adjacency weights (symmetric)
	weight := make([][]float64, n)
	for i := range weight {
		weight[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			w := jaccardTR(tokens[i], tokens[j])
			weight[i][j] = w
			weight[j][i] = w
		}
	}

	// PageRank iteration
	for iter := 0; iter < trIterations; iter++ {
		newScores := make([]float64, n)
		for i := 0; i < n; i++ {
			sum := 0.0
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				wSum := 0.0
				for k := 0; k < n; k++ {
					wSum += weight[j][k]
				}
				if wSum > 0 {
					sum += weight[j][i] / wSum * scores[j]
				}
			}
			newScores[i] = (1-trDamping)/float64(n) + trDamping*sum
		}
		if convergedTR(scores, newScores) {
			break
		}
		copy(scores, newScores)
	}

	// Select top sentences within budget
	type idxScore struct {
		idx   int
		score float64
	}
	ranked := make([]idxScore, n)
	for i, s := range scores {
		ranked[i] = idxScore{i, s}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	maxLen := int(float64(len(content)) * s.cfg.MaxRatio)
	totalLen := 0
	selected := make(map[int]bool)

	for _, r := range ranked {
		sLen := len(sentences[r.idx])
		if totalLen+sLen > maxLen && len(selected) > 0 {
			continue
		}
		selected[r.idx] = true
		totalLen += sLen
	}

	// Reassemble in original order
	var result []string
	for i, s := range sentences {
		if selected[i] {
			result = append(result, s)
		}
	}

	if len(result) == 0 {
		return content
	}
	return strings.Join(result, " ")
}

func splitSentencesTR(text string) []string {
	paragraphs := strings.Split(text, "\n\n")
	var sentences []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Split on sentence-ending punctuation followed by space+uppercase or end of text
		var current strings.Builder
		for i, ch := range p {
			current.WriteRune(ch)
			if isSentenceEndTR(ch) {
				// Check if this is really a sentence boundary
				next := byte(0)
				if i+1 < len(p) {
					next = p[i+1]
				}
				if next == 0 || next == ' ' || next == '\n' {
					s := strings.TrimSpace(current.String())
					if s != "" {
						sentences = append(sentences, s)
					}
					current.Reset()
				}
			}
		}
		// Remaining text
		s := strings.TrimSpace(current.String())
		if s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

func isSentenceEndTR(ch rune) bool {
	return ch == '.' || ch == '!' || ch == '?'
}

func tokenizeTR(s string) map[string]bool {
	tokens := make(map[string]bool)
	var current strings.Builder
	for _, ch := range strings.ToLower(s) {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			current.WriteRune(ch)
		} else {
			w := current.String()
			if len(w) > 2 {
				tokens[w] = true
			}
			current.Reset()
		}
	}
	w := current.String()
	if len(w) > 2 {
		tokens[w] = true
	}
	return tokens
}

func jaccardTR(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func convergedTR(old, new []float64) bool {
	for i := range old {
		if math.Abs(old[i]-new[i]) > trConvergence {
			return false
		}
	}
	return true
}
