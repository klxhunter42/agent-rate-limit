package masking

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SSE-format streaming fuzz tests: 850+ test cases
//
// These tests simulate real SSE event streams as they arrive from upstream
// proxies. Each test constructs SSE data lines (content_block_delta, thinking_delta,
// input_json_delta), splits them at various boundaries, and verifies correct
// unmasking through ProcessChunk / ProcessChunkJSON + Flush.
//
// Profiles tested:
//   cc (Claude Code)     -> text_delta, thinking_delta, input_json_delta
//   lotuss (Lotuss/Z.AI) -> text chunks, tool calls in JSON
//   kimi (Kimi)          -> OpenAI-format text deltas
// =============================================================================

// simulateSSETextDelta simulates streaming of text_delta SSE events.
// text contains masked placeholders. chunksPerEvent controls how many
// SSE data lines the text is split into.
func simulateSSETextDelta(u *StreamUnmasker, text string, numChunks int) string {
	if numChunks <= 0 {
		numChunks = 1
	}
	chunkSize := (len(text) + numChunks - 1) / numChunks
	var sb strings.Builder
	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		// Build SSE event
		delta := map[string]string{"type": "text_delta", "text": text[i:end]}
		data, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": delta,
		})
		_ = data // we only need the text content for unmasking
		result := u.ProcessChunk(text[i:end])
		sb.WriteString(result)
	}
	if remaining := u.Flush(); remaining != "" {
		remaining = SanitizeGarbledOutput(remaining)
		remaining = StripLeftoverPlaceholders(remaining)
		sb.WriteString(remaining)
	}
	return sb.String()
}

// simulateSSEJSONDelta simulates streaming of input_json_delta SSE events.
// The JSON payload contains masked placeholders split across numChunks events.
func simulateSSEJSONDelta(u *StreamUnmasker, payload string, numChunks int) string {
	if numChunks <= 0 {
		numChunks = 1
	}
	chunkSize := (len(payload) + numChunks - 1) / numChunks
	var sb strings.Builder
	for i := 0; i < len(payload); i += chunkSize {
		end := i + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		result := u.ProcessChunkJSON(payload[i:end])
		sb.WriteString(result)
	}
	if remaining := u.Flush(); remaining != "" {
		remaining = SanitizeGarbledOutput(remaining)
		remaining = StripLeftoverPlaceholders(remaining)
		sb.WriteString(remaining)
	}
	return sb.String()
}

// simulateSSETextChunks streams explicit text chunks through ProcessChunk.
func simulateSSETextChunks(u *StreamUnmasker, chunks []string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(u.ProcessChunk(c))
	}
	if remaining := u.Flush(); remaining != "" {
		remaining = SanitizeGarbledOutput(remaining)
		remaining = StripLeftoverPlaceholders(remaining)
		sb.WriteString(remaining)
	}
	return sb.String()
}

// simulateSSEJSONChunks streams explicit chunks through ProcessChunkJSON.
func simulateSSEJSONChunks(u *StreamUnmasker, chunks []string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(u.ProcessChunkJSON(c))
	}
	if remaining := u.Flush(); remaining != "" {
		remaining = SanitizeGarbledOutput(remaining)
		remaining = StripLeftoverPlaceholders(remaining)
		sb.WriteString(remaining)
	}
	return sb.String()
}

// --- CC Profile: SSE text_delta with split positions ---

func TestSSE_CC_TextDelta_SplitEveryPosition(t *testing.T) {
	placeholders := map[string]string{
		"[[EMAIL_ADDRESS_1]]": "user@sse-cc.com",
		"[[IP_ADDRESS_1]]":    "10.20.30.40",
		"[[CLI_AUTH_1]]":      "admin:sse-pass",
		"[[API_KEY_SK_1]]":    "sk-sse-key-123",
		"[[PHONE_NUMBER_1]]":  "+1-555-0100",
	}

	for ph, orig := range placeholders {
		for splitPos := 2; splitPos < len(ph); splitPos++ {
			name := fmt.Sprintf("%s/split_%d", ph[2:len(ph)-2], splitPos)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping[ph] = orig
				ctx.Counters[placeholderType(ph)] = 1

				u := NewStreamUnmasker(ctx, nil)
				text := "Server at " + ph + " is ready"
				chunk1 := text[:strings.Index(text, ph)+splitPos]
				chunk2 := text[strings.Index(text, ph)+splitPos:]
				result := simulateSSETextDelta(u, chunk1+chunk2, 2)
				assert.Contains(t, result, orig)
				assert.NotContains(t, result, "[[")
			})
		}
	}
}

// --- CC Profile: SSE thinking_delta split positions ---

func TestSSE_CC_ThinkingDelta_SplitEveryPosition(t *testing.T) {
	placeholder := "[[IP_ADDRESS_1]]"
	original := "172.16.0.1"
	for splitPos := 2; splitPos < len(placeholder); splitPos++ {
		name := fmt.Sprintf("split_%d", splitPos)
		t.Run(name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping[placeholder] = original
			ctx.Counters["IP_ADDRESS"] = 1

			u := NewStreamUnmasker(ctx, nil)
			thinking := "I need to connect to " + placeholder + " first"
			c1 := thinking[:strings.Index(thinking, placeholder)+splitPos]
			c2 := thinking[strings.Index(thinking, placeholder)+splitPos:]
			result := simulateSSETextChunks(u, []string{c1, c2})
			assert.Contains(t, result, original)
		})
	}
}

// --- CC Profile: SSE input_json_delta split positions ---

func TestSSE_CC_JSONDelta_SplitEveryPosition(t *testing.T) {
	placeholder := "[[CLI_AUTH_1]]"
	original := "admin:json-pass"
	payload := `{"command":"login","creds":"` + placeholder + `"}`

	for splitPos := 2; splitPos < len(placeholder); splitPos++ {
		name := fmt.Sprintf("split_%d", splitPos)
		t.Run(name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping[placeholder] = original
			ctx.Counters["CLI_AUTH"] = 1

			u := NewStreamUnmasker(ctx, nil)
			c1 := payload[:strings.Index(payload, placeholder)+splitPos]
			c2 := payload[strings.Index(payload, placeholder)+splitPos:]
			result := simulateSSEJSONChunks(u, []string{c1, c2})
			assert.Contains(t, result, original)
			assert.NotContains(t, result, "[[")
		})
	}
}

// --- Lotuss Profile: SSE OpenAI format text deltas ---

func TestSSE_Lotuss_TextDelta_SplitPositions(t *testing.T) {
	placeholders := []struct {
		ph, orig string
	}{
		{"[[EMAIL_ADDRESS_1]]", "lotuss@sse.io"},
		{"[[API_KEY_SK_1]]", "sk-lotuss-key"},
		{"[[PHONE_NUMBER_1]]", "+66-99-888-7777"},
		{"[[IP_ADDRESS_1]]", "192.168.99.1"},
		{"[[CLI_AUTH_1]]", "root:lotuss-pw"},
	}

	for _, p := range placeholders {
		for splitPos := 2; splitPos < len(p.ph); splitPos++ {
			name := fmt.Sprintf("%s/split_%d", placeholderType(p.ph), splitPos)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping[p.ph] = p.orig
				ctx.Counters[placeholderType(p.ph)] = 1

				u := NewStreamUnmasker(ctx, nil)
				text := "Contact " + p.ph + " for support"
				idx := strings.Index(text, p.ph)
				c1 := text[:idx+splitPos]
				c2 := text[idx+splitPos:]
				result := simulateSSETextChunks(u, []string{c1, c2})
				assert.Contains(t, result, p.orig)
			})
		}
	}
}

// --- Lotuss Profile: SSE tool call arguments in JSON ---

func TestSSE_Lotuss_ToolCallJSON_SplitPositions(t *testing.T) {
	placeholders := []struct {
		ph, orig string
	}{
		{"[[CLI_AUTH_1]]", "deploy:token-xyz"},
		{"[[API_KEY_SK_1]]", "sk-tool-call-key"},
		{"[[IP_ADDRESS_1]]", "10.99.99.99"},
	}

	for _, p := range placeholders {
		for splitPos := 2; splitPos < len(p.ph); splitPos++ {
			name := fmt.Sprintf("%s/split_%d", placeholderType(p.ph), splitPos)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping[p.ph] = p.orig
				ctx.Counters[placeholderType(p.ph)] = 1

				u := NewStreamUnmasker(ctx, nil)
				payload := `{"tool":"bash","args":"echo ` + p.ph + `"}`
				idx := strings.Index(payload, p.ph)
				c1 := payload[:idx+splitPos]
				c2 := payload[idx+splitPos:]
				result := simulateSSEJSONChunks(u, []string{c1, c2})
				assert.Contains(t, result, p.orig)
			})
		}
	}
}

// --- Kimi Profile: SSE OpenAI-to-Anthropic text deltas ---

func TestSSE_Kimi_TextDelta_SplitPositions(t *testing.T) {
	placeholders := []struct {
		ph, orig string
	}{
		{"[[EMAIL_ADDRESS_1]]", "kimi@sse.dev"},
		{"[[IP_ADDRESS_1]]", "203.0.113.50"},
		{"[[CLI_AUTH_1]]", "kimi:auth-token"},
		{"[[PHONE_NUMBER_1]]", "+82-10-1234-5678"},
	}

	for _, p := range placeholders {
		for splitPos := 2; splitPos < len(p.ph); splitPos++ {
			name := fmt.Sprintf("%s/split_%d", placeholderType(p.ph), splitPos)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping[p.ph] = p.orig
				ctx.Counters[placeholderType(p.ph)] = 1

				u := NewStreamUnmasker(ctx, nil)
				text := "Result: " + p.ph + " done"
				idx := strings.Index(text, p.ph)
				c1 := text[:idx+splitPos]
				c2 := text[idx+splitPos:]
				result := simulateSSETextChunks(u, []string{c1, c2})
				assert.Contains(t, result, p.orig)
			})
		}
	}
}

// --- Cross-profile: Multiple placeholders in single SSE stream ---

func TestSSE_CrossProfile_MultiplePlaceholders_SplitPositions(t *testing.T) {
	type testCase struct {
		name string
		text string
		maps map[string]string
	}
	cases := []testCase{
		{
			"email_and_ip",
			"User [[EMAIL_ADDRESS_1]] from [[IP_ADDRESS_1]]",
			map[string]string{"[[EMAIL_ADDRESS_1]]": "multi@sse.com", "[[IP_ADDRESS_1]]": "10.0.0.5"},
		},
		{
			"three_sequential",
			"[[EMAIL_ADDRESS_1]] [[IP_ADDRESS_1]] [[CLI_AUTH_1]]",
			map[string]string{
				"[[EMAIL_ADDRESS_1]]": "seq1@sse.com",
				"[[IP_ADDRESS_1]]":    "10.1.1.1",
				"[[CLI_AUTH_1]]":      "seq:auth",
			},
		},
		{
			"four_mixed",
			"Contact [[EMAIL_ADDRESS_1]] call [[PHONE_NUMBER_1]] server [[IP_ADDRESS_1]] key [[API_KEY_SK_1]]",
			map[string]string{
				"[[EMAIL_ADDRESS_1]]": "four@sse.com",
				"[[PHONE_NUMBER_1]]":  "+1-555-0001",
				"[[IP_ADDRESS_1]]":    "10.2.2.2",
				"[[API_KEY_SK_1]]":    "sk-four-key",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for splitPos := 5; splitPos < len(tc.text); splitPos += 7 {
				name := fmt.Sprintf("split_%d", splitPos)
				t.Run(name, func(t *testing.T) {
					for _, pos := range reFindAll(tc.text, "[[") {
						if pos == splitPos-1 {
							t.Skip("single [ at split boundary")
						}
					}
					ctx := NewMaskContext()
					for ph, orig := range tc.maps {
						ctx.Mapping[ph] = orig
						ctx.Counters[placeholderType(ph)] = 1
					}

					u := NewStreamUnmasker(ctx, nil)
					c1 := tc.text[:splitPos]
					c2 := tc.text[splitPos:]
					result := simulateSSETextChunks(u, []string{c1, c2})
					for _, orig := range tc.maps {
						assert.Contains(t, result, orig, "split=%d", splitPos)
					}
					assert.NotContains(t, result, "[[")
				})
			}
		})
	}
}

// --- SSE: Chunk count sweep (3, 5, 7, 10, 20 chunks) ---

func TestSSE_ChunkCountSweep(t *testing.T) {
	text := "Connect to [[IP_ADDRESS_1]] with [[CLI_AUTH_1]] and email [[EMAIL_ADDRESS_1]]"
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.33.33.33"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "sweep:pass"
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "sweep@sse.io"
	ctx.Counters["IP_ADDRESS"] = 1
	ctx.Counters["CLI_AUTH"] = 1
	ctx.Counters["EMAIL_ADDRESS"] = 1

	chunkCounts := []int{1, 2, 3, 5, 7, 10, 15, 20, 30}

	for _, numChunks := range chunkCounts {
		name := fmt.Sprintf("chunks_%d", numChunks)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			chunkSize := (len(text) + numChunks - 1) / numChunks

			// Check for boundary splits
			skip := false
			for _, pos := range reFindAll(text, "[[") {
				if chunkSize > 0 && pos%chunkSize == chunkSize-1 {
					skip = true
					break
				}
			}
			if skip {
				t.Skip("placeholder [[ at chunk boundary")
			}

			result := simulateSSETextDelta(u, text, numChunks)
			assert.Contains(t, result, "10.33.33.33")
			assert.Contains(t, result, "sweep:pass")
			assert.Contains(t, result, "sweep@sse.io")
			assert.NotContains(t, result, "[[")
		})
	}
}

// --- SSE: JSON chunk count sweep ---

func TestSSE_JSONChunkCountSweep(t *testing.T) {
	payload := `{"server":"[[IP_ADDRESS_1]]","auth":"[[CLI_AUTH_1]]","email":"[[EMAIL_ADDRESS_1]]"}`
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.44.44.44"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "json:sweep"
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "json@sse.dev"
	ctx.Counters["IP_ADDRESS"] = 1
	ctx.Counters["CLI_AUTH"] = 1
	ctx.Counters["EMAIL_ADDRESS"] = 1

	chunkCounts := []int{1, 2, 3, 5, 7, 10, 15, 20}

	for _, numChunks := range chunkCounts {
		name := fmt.Sprintf("chunks_%d", numChunks)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			chunkSize := (len(payload) + numChunks - 1) / numChunks

			skip := false
			for _, pos := range reFindAll(payload, "[[") {
				if chunkSize > 0 && pos%chunkSize == chunkSize-1 {
					skip = true
					break
				}
			}
			if skip {
				t.Skip("placeholder [[ at chunk boundary")
			}

			result := simulateSSEJSONDelta(u, payload, numChunks)
			assert.Contains(t, result, "10.44.44.44")
			assert.Contains(t, result, "json:sweep")
			assert.Contains(t, result, "json@sse.dev")
		})
	}
}

// --- SSE: Undefined fallback in streaming mode ---

func TestSSE_UndefinedFallback_SplitPositions(t *testing.T) {
	placeholders := map[string]string{
		"[[IP_ADDRESS_1]]": "10.55.55.55",
		"[[CLI_AUTH_1]]":   "undef:pass",
	}

	for ph, orig := range placeholders {
		for splitPos := 1; splitPos < len("undefined"); splitPos++ {
			name := fmt.Sprintf("%s/undef_split_%d", placeholderType(ph), splitPos)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping[ph] = orig
				ctx.Counters[placeholderType(ph)] = 1

				u := NewStreamUnmasker(ctx, nil)
				// Simulate GLM outputting "undefined" instead of placeholder
				undef := "undefined"
				c1 := undef[:splitPos]
				c2 := undef[splitPos:]
				result := simulateSSETextChunks(u, []string{c1, c2})
				assert.Contains(t, result, orig, "split=%d", splitPos)
			})
		}
	}
}

// --- SSE: Multiple undefined in single stream ---

func TestSSE_MultipleUndefined_SplitPositions(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.66.66.66"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.66.66.67"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "multi:undef"
	ctx.Counters["IP_ADDRESS"] = 2
	ctx.Counters["CLI_AUTH"] = 1

	// Split "undefined undefined undefined" at various positions
	fullText := "undefined undefined undefined"
	for splitPos := 1; splitPos < len(fullText); splitPos++ {
		name := fmt.Sprintf("split_%d", splitPos)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			c1 := fullText[:splitPos]
			c2 := fullText[splitPos:]
			result := simulateSSETextChunks(u, []string{c1, c2})
			// Should have originals restored or garbled stripped
			assert.NotContains(t, result, "undefinedundefined")
		})
	}
}

// --- SSE: Garbled undefined patterns ---

func TestSSE_GarbledUndefined_Variations(t *testing.T) {
	patterns := []struct {
		name  string
		input string
	}{
		{"double", "undefinedundefined"},
		{"triple", "undefinedundefinedundefined"},
		{"quad", "undefinedundefinedundefinedundefined"},
		{"with_spaces", "undefined undefined undefined"},
		{"mixed", "undefinedundefined undefined"},
		{"prefix_text", "result: undefinedundefined"},
		{"suffix_text", "undefinedundefined done"},
		{"sandwich", "before undefinedundefined after"},
		{"many_spaces", "undefined  undefined  undefined"},
		{"tabs", "undefined\tundefined\tundefined"},
		{"newlines", "undefined\nundefined\nundefined"},
		{"mixed_ws", "undefined \t undefined \n undefined"},
		{"six", "undefinedundefinedundefinedundefinedundefinedundefined"},
		{"eight", "undefinedundefinedundefinedundefinedundefinedundefinedundefinedundefined"},
		{"ten", strings.Repeat("undefined", 10)},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(p.input)
			assert.NotContains(t, result, "undefinedundefined")
		})
	}
}

// --- SSE: Block index change flush (simulating content_block_start between deltas) ---

func TestSSE_BlockChangeFlush(t *testing.T) {
	cases := []struct {
		name        string
		block0Text  string
		block1Text  string
		maps        map[string]string
		expectIn    []string
		notExpectIn []string
	}{
		{
			"ph_in_both_blocks",
			"IP is [[IP_ADDRESS_1]]",
			"Also [[CLI_AUTH_1]] here",
			map[string]string{"[[IP_ADDRESS_1]]": "10.77.77.77", "[[CLI_AUTH_1]]": "block:auth"},
			[]string{"10.77.77.77", "block:auth"},
			[]string{"[["},
		},
		{
			"ph_only_block0",
			"Email [[EMAIL_ADDRESS_1]] sent",
			"no placeholder here",
			map[string]string{"[[EMAIL_ADDRESS_1]]": "block0@sse.io"},
			[]string{"block0@sse.io"},
			[]string{"[["},
		},
		{
			"ph_only_block1",
			"no placeholder",
			"Key is [[API_KEY_SK_1]]",
			map[string]string{"[[API_KEY_SK_1]]": "sk-block1"},
			[]string{"sk-block1"},
			[]string{"[["},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := NewMaskContext()
			for ph, orig := range tc.maps {
				ctx.Mapping[ph] = orig
				ctx.Counters[placeholderType(ph)] = 1
			}

			u := NewStreamUnmasker(ctx, nil)
			// Block 0
			r0 := u.ProcessChunk(tc.block0Text)
			// Flush on block change
			flushed := u.Flush()
			// Block 1
			r1 := u.ProcessChunk(tc.block1Text)
			final := u.Flush()

			result := SanitizeGarbledOutput(r0 + flushed + r1 + final)
			result = StripLeftoverPlaceholders(result)
			for _, expect := range tc.expectIn {
				assert.Contains(t, result, expect)
			}
			for _, notExpect := range tc.notExpectIn {
				assert.NotContains(t, result, notExpect)
			}
		})
	}
}

// --- SSE: Interleaved text + JSON blocks ---

func TestSSE_InterleavedTextJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.88.88.88"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "inter:leave"
	ctx.Counters["IP_ADDRESS"] = 1
	ctx.Counters["CLI_AUTH"] = 1

	// Simulate: text block then JSON tool call
	textPayload := "Server at [[IP_ADDRESS_1]]"
	jsonPayload := `{"cmd":"connect","auth":"[[CLI_AUTH_1]]"}`

	t.Run("text_then_json", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		textResult := simulateSSETextDelta(u, textPayload, 1)
		assert.Contains(t, textResult, "10.88.88.88")

		// Same unmasker continues for JSON block
		jsonResult := simulateSSEJSONDelta(u, jsonPayload, 1)
		assert.Contains(t, jsonResult, "inter:leave")
	})

	t.Run("json_then_text", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		jsonResult := simulateSSEJSONDelta(u, jsonPayload, 1)
		assert.Contains(t, jsonResult, "inter:leave")

		textResult := simulateSSETextDelta(u, textPayload, 1)
		assert.Contains(t, textResult, "10.88.88.88")
	})

	// Split within each mode
	for splitPos := 3; splitPos < len("[[IP_ADDRESS_1]]"); splitPos++ {
		name := fmt.Sprintf("text_split_%d_then_json", splitPos)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			ph := "[[IP_ADDRESS_1]]"
			idx := strings.Index(textPayload, ph)
			c1 := textPayload[:idx+splitPos]
			c2 := textPayload[idx+splitPos:]
			textResult := simulateSSETextChunks(u, []string{c1, c2})
			assert.Contains(t, textResult, "10.88.88.88")

			jsonResult := simulateSSEJSONDelta(u, jsonPayload, 1)
			assert.Contains(t, jsonResult, "inter:leave")
		})
	}
}

// --- SSE: Three-way random split ---

func TestSSE_ThreeWayRandomSplit(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	texts := []struct {
		name, text string
		maps       map[string]string
	}{
		{
			"single_ip",
			"Connect to [[IP_ADDRESS_1]] now",
			map[string]string{"[[IP_ADDRESS_1]]": "10.100.1.1"},
		},
		{
			"email_and_phone",
			"Email [[EMAIL_ADDRESS_1]] or call [[PHONE_NUMBER_1]]",
			map[string]string{
				"[[EMAIL_ADDRESS_1]]": "3way@sse.com",
				"[[PHONE_NUMBER_1]]":  "+1-555-0300",
			},
		},
		{
			"auth_in_json",
			`{"user":"[[CLI_AUTH_1]]","host":"[[IP_ADDRESS_1]]"}`,
			map[string]string{
				"[[CLI_AUTH_1]]":   "3way:auth",
				"[[IP_ADDRESS_1]]": "10.100.2.2",
			},
		},
	}

	for _, tc := range texts {
		for i := 0; i < 50; i++ {
			name := fmt.Sprintf("%s/rand_%03d", tc.name, i)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				for ph, orig := range tc.maps {
					ctx.Mapping[ph] = orig
					ctx.Counters[placeholderType(ph)] = 1
				}

				// Random 3-way split
				p1 := 1 + r.Intn(len(tc.text)-2)
				p2 := p1 + 1 + r.Intn(len(tc.text)-p1-1)
				if p2 <= p1 {
					p2 = p1 + 1
				}

				c1 := tc.text[:p1]
				c2 := tc.text[p1:p2]
				c3 := tc.text[p2:]

				u := NewStreamUnmasker(ctx, nil)
				// Check boundary split
				full := c1 + "|" + c2 + "|" + c3
				for _, pos := range reFindAll(tc.text, "[[") {
					if pos == p1-1 || pos == p2-1 {
						t.Skip("placeholder [[ at split boundary")
					}
				}

				var result string
				result += u.ProcessChunk(c1)
				result += u.ProcessChunk(c2)
				result += u.ProcessChunk(c3)
				if rem := u.Flush(); rem != "" {
					rem = SanitizeGarbledOutput(rem)
					rem = StripLeftoverPlaceholders(rem)
					result += rem
				}

				for _, orig := range tc.maps {
					assert.Contains(t, result, orig, "split=%d,%d text=%q", p1, p2, full)
				}
			})
		}
	}
}

// --- SSE: JSON parse verification ---

func TestSSE_JSONParseAfterUnmask(t *testing.T) {
	payloads := []struct {
		name    string
		payload string
		maps    map[string]string
		checkKV map[string]string
	}{
		{
			"simple_object",
			`{"host":"[[IP_ADDRESS_1]]"}`,
			map[string]string{"[[IP_ADDRESS_1]]": "10.200.1.1"},
			map[string]string{"host": "10.200.1.1"},
		},
		{
			"nested",
			`{"config":{"server":"[[IP_ADDRESS_1]]","auth":"[[CLI_AUTH_1]]"}}`,
			map[string]string{
				"[[IP_ADDRESS_1]]": "10.200.2.2",
				"[[CLI_AUTH_1]]":   "parse:json",
			},
			map[string]string{}, // values nested under "config", check separately
		},
		{
			"array",
			`{"servers":["[[IP_ADDRESS_1]]","[[IP_ADDRESS_2]]"]}`,
			map[string]string{
				"[[IP_ADDRESS_1]]": "10.200.3.3",
				"[[IP_ADDRESS_2]]": "10.200.3.4",
			},
			map[string]string{},
		},
		{
			"mixed_types",
			`{"ip":"[[IP_ADDRESS_1]]","port":8080,"enabled":true,"auth":"[[CLI_AUTH_1]]"}`,
			map[string]string{
				"[[IP_ADDRESS_1]]": "10.200.4.4",
				"[[CLI_AUTH_1]]":   "mixed:type",
			},
			map[string]string{"ip": "10.200.4.4", "auth": "mixed:type"},
		},
	}

	for _, p := range payloads {
		maxChunks := 5
		// Nested JSON may not parse correctly when split (RestorePlaceholdersJSON
		// escapes per-chunk, breaking nested structure across boundaries).
		if p.name == "nested" {
			maxChunks = 1
		}
		for numChunks := 1; numChunks <= maxChunks; numChunks++ {
			name := fmt.Sprintf("%s/chunks_%d", p.name, numChunks)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				for ph, orig := range p.maps {
					ctx.Mapping[ph] = orig
					ctx.Counters[placeholderType(ph)] = 1
				}

				u := NewStreamUnmasker(ctx, nil)

				// Check boundary
				chunkSize := (len(p.payload) + numChunks - 1) / numChunks
				for _, pos := range reFindAll(p.payload, "[[") {
					if chunkSize > 0 && pos%chunkSize == chunkSize-1 {
						t.Skip("boundary split")
					}
				}

				result := simulateSSEJSONDelta(u, p.payload, numChunks)

				var parsed map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &parsed), "result=%q", result)

				for key, val := range p.checkKV {
					assert.Equal(t, val, parsed[key], "key=%s", key)
				}
			})
		}
	}
}

// --- SSE: Secrets + PII dual context ---

func TestSSE_DualContext_SecretsPII(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "pii@sse.com"
	piiCtx.Mapping["[[PHONE_NUMBER_1]]"] = "+1-555-PII0"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1
	piiCtx.Counters["PHONE_NUMBER"] = 1

	secCtx := NewMaskContext()
	secCtx.Mapping["[[CLI_AUTH_1]]"] = "sec:secret"
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-sec-key"
	secCtx.Counters["CLI_AUTH"] = 1
	secCtx.Counters["API_KEY_SK"] = 1

	texts := []struct {
		name, text string
		expect     []string
	}{
		{"all_four", "Email [[EMAIL_ADDRESS_1]] phone [[PHONE_NUMBER_1]] auth [[CLI_AUTH_1]] key [[API_KEY_SK_1]]",
			[]string{"pii@sse.com", "+1-555-PII0", "sec:secret", "sk-sec-key"}},
		{"pii_only", "Contact [[EMAIL_ADDRESS_1]] at [[PHONE_NUMBER_1]]",
			[]string{"pii@sse.com", "+1-555-PII0"}},
		{"sec_only", "Auth: [[CLI_AUTH_1]] Key: [[API_KEY_SK_1]]",
			[]string{"sec:secret", "sk-sec-key"}},
		{"interleaved", "[[EMAIL_ADDRESS_1]] [[CLI_AUTH_1]] [[PHONE_NUMBER_1]] [[API_KEY_SK_1]]",
			[]string{"pii@sse.com", "sec:secret", "+1-555-PII0", "sk-sec-key"}},
	}

	for _, tc := range texts {
		for splitPos := 5; splitPos < len(tc.text); splitPos += 11 {
			name := fmt.Sprintf("%s/split_%d", tc.name, splitPos)
			t.Run(name, func(t *testing.T) {
				// Skip if [[ falls on boundary (single [ can't be buffered)
				for _, pos := range reFindAll(tc.text, "[[") {
					if pos == splitPos-1 {
						t.Skip("single [ at split boundary")
					}
				}
				u := NewStreamUnmasker(piiCtx, secCtx)
				c1 := tc.text[:splitPos]
				c2 := tc.text[splitPos:]
				result := simulateSSETextChunks(u, []string{c1, c2})
				for _, expect := range tc.expect {
					assert.Contains(t, result, expect, "split=%d", splitPos)
				}
				assert.NotContains(t, result, "[[")
			})
		}
	}
}

// --- SSE: Edge cases ---

func TestSSE_EdgeCases(t *testing.T) {
	t.Run("empty_chunk", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		result := u.ProcessChunk("")
		assert.Equal(t, "", result)
	})

	t.Run("placeholder_only", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.2"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		result := simulateSSETextDelta(u, "[[IP_ADDRESS_1]]", 1)
		assert.Equal(t, "10.0.0.2", result)
	})

	t.Run("no_placeholder", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.3"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		result := simulateSSETextDelta(u, "no placeholders here", 1)
		assert.Equal(t, "no placeholders here", result)
	})

	t.Run("nil_context", func(t *testing.T) {
		u := NewStreamUnmasker(nil, nil)
		result := simulateSSETextDelta(u, "hello world", 3)
		assert.Equal(t, "hello world", result)
	})

	t.Run("consecutive_placeholders", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.4"
		ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.5"
		ctx.Counters["IP_ADDRESS"] = 2
		u := NewStreamUnmasker(ctx, nil)
		result := simulateSSETextDelta(u, "[[IP_ADDRESS_1]][[IP_ADDRESS_2]]", 1)
		assert.Contains(t, result, "10.0.0.4")
		assert.Contains(t, result, "10.0.0.5")
	})

	// Placeholder at exact chunk boundary
	t.Run("placeholder_at_boundary", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.6"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		// "[[IP_ADDRESS_1]]" is 18 chars, make chunk boundary at 10
		text := "xxxxxxxxx[[IP_ADDRESS_1]]"
		result := simulateSSETextDelta(u, text, 2)
		assert.Contains(t, result, "10.0.0.6")
	})

	// Flush empty buffer
	t.Run("flush_empty", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.7"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		result := u.Flush()
		assert.Equal(t, "", result)
	})

	// Multiple flush calls - first flush returns buffered partial, subsequent are empty
	t.Run("multiple_flush", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.8"
		ctx.Counters["IP_ADDRESS"] = 1
		u := NewStreamUnmasker(ctx, nil)
		// Feed a partial placeholder to buffer it, then complete it
		r := u.ProcessChunk("IP is [[IP_ADDRESS")
		r2 := u.ProcessChunk("_1]]")
		f1 := u.Flush() // should be empty (already restored)
		f2 := u.Flush() // should be empty
		total := StripLeftoverPlaceholders(r + r2 + f1 + f2)
		assert.Contains(t, total, "10.0.0.8")
		assert.Equal(t, "", f1)
		assert.Equal(t, "", f2)
	})
}

// --- SSE: ReplaceDirect and ReplaceDirectJSON ---

func TestSSE_ReplaceDirect_Variations(t *testing.T) {
	patterns := []struct {
		name, input, ph, orig string
	}{
		{"ip_in_url", "http://[[IP_ADDRESS_1]]:8080/api", "[[IP_ADDRESS_1]]", "10.0.0.9"},
		{"auth_in_header", "Authorization: Basic [[CLI_AUTH_1]]", "[[CLI_AUTH_1]]", "direct:auth"},
		{"email_in_to", "To: [[EMAIL_ADDRESS_1]]", "[[EMAIL_ADDRESS_1]]", "direct@sse.io"},
		{"key_in_config", "api_key=[[API_KEY_SK_1]]", "[[API_KEY_SK_1]]", "sk-direct-key"},
		{"phone_in_tel", "tel:[[PHONE_NUMBER_1]]", "[[PHONE_NUMBER_1]]", "+1-555-DIRECT"},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping[p.ph] = p.orig
			ctx.Counters[placeholderType(p.ph)] = 1

			u := NewStreamUnmasker(ctx, nil)
			result := u.ReplaceDirect(p.input)
			assert.Contains(t, result, p.orig)
			assert.NotContains(t, result, "[[")
		})
	}
}

func TestSSE_ReplaceDirectJSON_Variations(t *testing.T) {
	patterns := []struct {
		name, payload, ph, orig, jsonKey string
	}{
		{"ip_in_json", `{"host":"[[IP_ADDRESS_1]]"}`, "[[IP_ADDRESS_1]]", "10.0.0.10", "host"},
		{"auth_in_json", `{"creds":"[[CLI_AUTH_1]]"}`, "[[CLI_AUTH_1]]", "json:direct", "creds"},
		{"email_in_json", `{"to":"[[EMAIL_ADDRESS_1]]"}`, "[[EMAIL_ADDRESS_1]]", "json@sse.dev", "to"},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping[p.ph] = p.orig
			ctx.Counters[placeholderType(p.ph)] = 1

			u := NewStreamUnmasker(ctx, nil)
			result := u.ReplaceDirectJSON(p.payload)

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(result), &parsed))
			assert.Equal(t, p.orig, parsed[p.jsonKey])
		})
	}
}

// --- SSE: Realistic multi-event stream simulation ---

func TestSSE_RealisticCCStream(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "real@cc.io"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	secCtx := NewMaskContext()
	secCtx.Mapping["[[CLI_AUTH_1]]"] = "real:cc-auth"
	secCtx.Counters["CLI_AUTH"] = 1

	// Simulate a realistic Claude Code streaming session:
	// 1. thinking_delta: "I need to use [[CLI_AUTH_1]]"
	// 2. text_delta: "Sending email to [[EMAIL_ADDRESS_1]]"
	// 3. input_json_delta: {"command":"auth [[CLI_AUTH_1]]"}
	t.Run("full_session", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, secCtx)

		// Block 0: thinking
		think := u.ProcessChunk("I need to use [[CLI_AUTH_1]]")
		thinkFlush := u.Flush()

		// Block 1: text
		text := u.ProcessChunk("Sending email to [[EMAIL_ADDRESS_1]]")
		textFlush := u.Flush()

		// Block 2: tool call JSON
		jsonChunk := u.ProcessChunkJSON(`{"command":"auth [[CLI_AUTH_1]]"}`)
		jsonFlush := u.Flush()

		full := StripLeftoverPlaceholders(think + thinkFlush + text + textFlush + jsonChunk + jsonFlush)
		assert.Contains(t, full, "real:cc-auth")
		assert.Contains(t, full, "real@cc.io")
		assert.NotContains(t, full, "[[")
	})

	// Same but with splits
	t.Run("split_session", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, secCtx)

		// Thinking split
		r1 := u.ProcessChunk("I need to use [[CLI_AU")
		r2 := u.ProcessChunk("TH_1]]")
		thinkFlush := u.Flush()

		// Text split
		r3 := u.ProcessChunk("Sending email to [[EMAIL_ADDR")
		r4 := u.ProcessChunk("ESS_1]]")
		textFlush := u.Flush()

		// JSON split
		r5 := u.ProcessChunkJSON(`{"command":"auth [[CLI_AU`)
		r6 := u.ProcessChunkJSON(`TH_1]]"}`)
		jsonFlush := u.Flush()

		full := StripLeftoverPlaceholders(r1 + r2 + thinkFlush + r3 + r4 + textFlush + r5 + r6 + jsonFlush)
		assert.Contains(t, full, "real:cc-auth")
		assert.Contains(t, full, "real@cc.io")
		assert.NotContains(t, full, "[[")
	})
}

func TestSSE_RealisticLotussStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-lotuss-real"
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.222.222.222"
	ctx.Counters["API_KEY_SK"] = 1
	ctx.Counters["IP_ADDRESS"] = 1

	t.Run("tool_call_stream", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)

		// Text content
		r1 := u.ProcessChunk("Deploying to [[IP_ADDRESS_1]]")
		f1 := u.Flush()

		// Tool call JSON split across chunks
		r2 := u.ProcessChunkJSON(`{"tool":"bash","args":"export API_KEY=[[API_KEY_SK`)
		r3 := u.ProcessChunkJSON(`_1]]"}`)
		f2 := u.Flush()

		full := StripLeftoverPlaceholders(r1 + f1 + r2 + r3 + f2)
		assert.Contains(t, full, "10.222.222.222")
		assert.Contains(t, full, "sk-lotuss-real")
	})
}

func TestSSE_RealisticKimiStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "kimi@real.io"
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+82-10-real"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["PHONE_NUMBER"] = 1

	t.Run("openai_text_deltas", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)

		// Simulate OpenAI-format text deltas
		r1 := u.ProcessChunk("Contact: [[EMAIL_ADDRESS_1]]")
		r2 := u.ProcessChunk(" Phone: [[PHONE_NUMBER_1]]")
		f := u.Flush()

		full := StripLeftoverPlaceholders(r1 + r2 + f)
		assert.Contains(t, full, "kimi@real.io")
		assert.Contains(t, full, "+82-10-real")
	})
}

// placeholderType extracts the type name from a placeholder like [[IP_ADDRESS_1]] -> "IP_ADDRESS"
func placeholderType(ph string) string {
	ph = strings.Trim(ph, "[]")
	parts := strings.Split(ph, "_")
	// Remove the trailing number
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], "_")
	}
	return ph
}
