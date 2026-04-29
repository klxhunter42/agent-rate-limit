package tokenizer

import (
	"math"
	"strings"
	"unicode"
)

// JaccardSimilarity computes Jaccard similarity between two texts using shingle sets.
func JaccardSimilarity(a, b string, shingleSize int) float64 {
	if a == "" || b == "" {
		return 0
	}
	if shingleSize < 1 {
		shingleSize = 3
	}
	setA := shingleSet(tokenizeWords(a), shingleSize)
	setB := shingleSet(tokenizeWords(b), shingleSize)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func tokenizeWords(text string) []string {
	var words []string
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			if b.Len() > 0 {
				words = append(words, b.String())
				b.Reset()
			}
		}
	}
	if b.Len() > 0 {
		words = append(words, b.String())
	}
	return words
}

func shingleSet(words []string, size int) map[string]bool {
	set := make(map[string]bool, len(words))
	for i := 0; i <= len(words)-size; i++ {
		var b strings.Builder
		for j := 0; j < size; j++ {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(words[i+j])
		}
		set[b.String()] = true
	}
	return set
}

// DeduplicateSemantic removes near-duplicate sentences using Jaccard similarity.
func DeduplicateSemantic(text string, threshold float64) (string, int) {
	if privacyPlaceholder.MatchString(text) {
		return text, 0
	}
	if threshold <= 0 || threshold >= 1 {
		threshold = 0.7
	}

	origTokens := QuickEstimateTokens(text)
	segments := SplitCodeBlocks(text)
	var b strings.Builder
	for _, seg := range segments {
		if seg.IsCode {
			b.WriteString(seg.Text)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(dedupSemanticProse(seg.Text, threshold))
		b.WriteByte('\n')
	}
	result := strings.TrimSpace(b.String())
	saved := origTokens - QuickEstimateTokens(result)
	if saved < 0 {
		saved = 0
	}
	return result, saved
}

func dedupSemanticProse(text string, threshold float64) string {
	sentences := splitSentences(text)
	if len(sentences) <= 1 {
		return text
	}

	type kept struct {
		text       string
		normalized string
	}
	var keptList []kept
	var b strings.Builder

	for _, s := range sentences {
		norm := normalizeForDedup(s)
		if len(norm) < 10 {
			b.WriteString(s)
			continue
		}
		isDup := false
		for _, k := range keptList {
			if len(k.normalized) >= 10 {
				sim := jaccardFast(norm, k.normalized)
				if sim >= threshold {
					isDup = true
					break
				}
			}
		}
		if !isDup {
			keptList = append(keptList, kept{text: s, normalized: norm})
			b.WriteString(s)
		}
	}
	return b.String()
}

// jaccardFast computes Jaccard on word sets (no shingling) for sentence-level dedup.
func jaccardFast(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 1 {
			set[w] = true
		}
	}
	return set
}

// LevenshteinSimilarity computes normalized Levenshtein similarity (0-1).
func LevenshteinSimilarity(a, b string) float64 {
	la, lb := len(a), len(b)
	if la == 0 && lb == 0 {
		return 1
	}
	if la == 0 || lb == 0 {
		return 0
	}
	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}
	dist := levenshteinDist(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

func levenshteinDist(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// CosineSimilarity computes cosine similarity between two float64 vectors.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
