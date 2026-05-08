package masking

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// generateContext builds a MaskContext with n originals.
func generateContext(name string, n int) *MaskContext {
	if n == 0 {
		return nil
	}
	ctx := NewMaskContext()
	vals := []string{"10.0.0.1", "sk-proj-abc123", "admin@corp.com",
		"hunter2", "+66-81-234-5678", "AKIA5FOSAMPLE", "my-secret-pw",
		"github_pat_123", "jwt_eyJhbG", "ssh-rsa AAAAB3"}
	for i := 0; i < n && i < len(vals); i++ {
		key := fmt.Sprintf("[[VAR_%03d]]", i)
		ctx.Mapping[key] = vals[i]
	}
	return ctx
}

// splitIntoChunks splits input into chunks of given size.
func splitIntoChunks(input string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = 1
	}
	var chunks []string
	for i := 0; i < len(input); i += chunkSize {
		end := i + chunkSize
		if end > len(input) {
			end = len(input)
		}
		chunks = append(chunks, input[i:end])
	}
	return chunks
}

// TestFuzz_1000UndefinedCases runs 1000 parametric subtests.
func TestFuzz_1000UndefinedCases(t *testing.T) {
	chunkSizes := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20}
	numOriginals := []int{0, 1, 2, 3, 5, 8}
	numUndefined := []int{0, 1, 2, 3, 4, 5, 6, 8}
	contextTypes := []string{"pii", "secrets", "both", "none"}
	prefixes := []string{"", "val=", "IP: ", "data: ", "x="}
	suffixes := []string{"", " done", " end", ".", ",", " ok"}
	specialInsets := []string{
		"",           // no inset
		" ",          // space between
		"\n",         // newline between
		"\t",         // tab between
		" and ",      // word between
		" | ",        // pipe between
		"🚀",          // emoji between
		"คือ",        // Thai between
		"\"quoted\"", // quoted
	}

	caseID := 0
	for _, cs := range chunkSizes {
		for _, no := range numOriginals {
			for _, nu := range numUndefined {
				for _, ct := range contextTypes {
					caseID++
					if caseID > 1000 {
						break
					}

					// Pick prefix/suffix/inset based on caseID for variety
					prefix := prefixes[caseID%len(prefixes)]
					suffix := suffixes[caseID%len(suffixes)]
					inset := specialInsets[caseID%len(specialInsets)]

					name := fmt.Sprintf("case_%04d/chunk_%d/orig_%d/undef_%d/ctx_%s",
						caseID, cs, no, nu, ct)
					t.Run(name, func(t *testing.T) {
						// Build contexts
						var piiCtx, secCtx *MaskContext
						switch ct {
						case "pii":
							piiCtx = generateContext("P", no)
						case "secrets":
							secCtx = generateContext("S", no)
						case "both":
							half := no / 2
							if half == 0 && no > 0 {
								half = 1
							}
							piiCtx = generateContext("P", half)
							secCtx = generateContext("S", no-half)
						case "none":
							// both nil
						}

						u := NewStreamUnmasker(piiCtx, secCtx)

						// Build input: prefix + (undefined + inset)*nu + suffix
						parts := []string{prefix}
						for i := 0; i < nu; i++ {
							parts = append(parts, "undefined")
							if i < nu-1 {
								parts = append(parts, inset)
							}
						}
						parts = append(parts, suffix)
						input := strings.Join(parts, "")

						// Process in chunks
						chunks := splitIntoChunks(input, cs)
						var full string
						for _, ch := range chunks {
							full += u.ProcessChunk(ch)
						}
						full += u.Flush()

						hasContext := (piiCtx != nil && len(piiCtx.Mapping) > 0) ||
							(secCtx != nil && len(secCtx.Mapping) > 0)

						if !hasContext {
							// Without masking: "undefined" should be preserved as-is
							if nu > 0 {
								assert.Contains(t, full, "undefined",
									"no masking context must preserve 'undefined'")
							}
						} else {
							// With masking: "undefined" must be cleaned
							assert.NotContains(t, full, "undefined",
								"masking active, undefined must be replaced")
							assert.NotContains(t, full, "[[",
								"no leftover placeholders")
						}
					})
				}
			}
		}
	}
}

// TestFuzz_1000EverySplitPosition tests every split position for various
// input patterns with surrounding text.
func TestFuzz_1000EverySplitPosition(t *testing.T) {
	patterns := []struct {
		name   string
		prefix string
		suffix string
	}{
		{"bare", "", ""},
		{"with_prefix", "val=", ""},
		{"with_suffix", "", " done"},
		{"both_sides", "x=", " y"},
		{"long_prefix", "the server IP is ", " please connect"},
		{"json", `"key":"`, `"}`},
		{"markdown", "**IP:** ", " **done**"},
		{"code", "connect(", ")"},
		{"url", "http://", ":8080/api"},
		{"thai", "IP คือ ", " ครับ"},
		{"emoji", "🔒 ", " ✅"},
		{"newline_prefix", "line1\n", "\nline2"},
		{"tabs", "col1\t", "\tcol3"},
		{"parens", "func(", ")"},
		{"brackets", "[", "]"},
		{"braces", "{", "}"},
		{"angle", "<", ">"},
		{"equals", "x=", ""},
		{"comma", "", ", next"},
		{"dot", "", "."},
	}

	ctx := generateContext("X", 5)
	caseID := 0

	for _, pat := range patterns {
		input := pat.prefix + "undefined" + pat.suffix
		for split := 1; split <= len(input)-1; split++ {
			caseID++
			if caseID > 1000 {
				break
			}
			name := fmt.Sprintf("case_%04d/%s/split_%d", caseID, pat.name, split)
			t.Run(name, func(t *testing.T) {
				u := NewStreamUnmasker(ctx, nil)
				r1 := u.ProcessChunk(input[:split])
				r2 := u.ProcessChunk(input[split:])
				f := u.Flush()
				full := r1 + r2 + f
				assert.NotContains(t, full, "undefined",
					"undefined must be replaced for pattern %s split at %d", pat.name, split)
				assert.NotContains(t, full, "[[", "no leftover placeholders")
			})
		}
		if caseID > 1000 {
			break
		}
	}
}

// TestFuzz_1000ProcessChunkJSON does the same fuzzing for the JSON path.
func TestFuzz_1000ProcessChunkJSON(t *testing.T) {
	chunkSizes := []int{1, 2, 3, 4, 5, 7, 9, 13}
	numOriginals := []int{1, 2, 4}
	numUndefined := []int{1, 2, 3, 5}
	jsonWrappers := []struct {
		name   string
		wrap   func(s string) string
		unwrap func(s string) bool // check if result is valid
	}{
		{"quoted", func(s string) string { return `"` + s + `"` }, func(s string) bool { return true }},
		{"json_value", func(s string) string { return `{"ip":"` + s + `"}` }, func(s string) bool { return true }},
		{"array", func(s string) string { return `["` + s + `"]` }, func(s string) bool { return true }},
		{"nested", func(s string) string { return `{"a":{"b":"` + s + `"}}` }, func(s string) bool { return true }},
	}

	caseID := 0
	for _, cs := range chunkSizes {
		for _, no := range numOriginals {
			for _, nu := range numUndefined {
				for _, w := range jsonWrappers {
					caseID++
					if caseID > 1000 {
						break
					}

					name := fmt.Sprintf("case_%04d/chunk_%d/orig_%d/undef_%d/wrap_%s",
						caseID, cs, no, nu, w.name)
					t.Run(name, func(t *testing.T) {
						ctx := generateContext("J", no)
						u := NewStreamUnmasker(ctx, nil)

						parts := make([]string, nu)
						for i := range parts {
							parts[i] = "undefined"
						}
						inner := strings.Join(parts, ", ")
						input := w.wrap(inner)

						chunks := splitIntoChunks(input, cs)
						var full string
						for _, ch := range chunks {
							full += u.ProcessChunkJSON(ch)
						}
						full += u.Flush()

						assert.NotContains(t, full, "undefined",
							"JSON path must clean undefined")
						assert.NotContains(t, full, "[[", "no leftover placeholders")
					})
				}
			}
		}
	}
}
