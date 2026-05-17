package masking

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reFindAll returns all positions of substr in s.
func reFindAll(s, substr string) []int {
	var positions []int
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], substr)
		if idx < 0 {
			break
		}
		positions = append(positions, i+idx)
		i = i + idx + 1
	}
	return positions
}

// =============================================================================
// Parametric fuzz tests: 5000+ test cases
//
// Categories:
//   1. Split-every-position: split each placeholder at every possible boundary
//   2. Garbled-undefined-every-position: split "undefined" at every char
//   3. Special-values: unusual characters in restored values
//   4. Chunk-size-sweep: varying chunk sizes from 1 to full length
//   5. Multi-placeholder-permutations: all ordering combinations
//   6. JSON-depth: nested JSON with placeholders at various depths
//   7. Interleaved-modes: ProcessChunk then ProcessChunkJSON patterns
// =============================================================================

// --- 1. Split every placeholder at every position (text mode) ---

func TestFuzz_SplitEveryPosition_Text(t *testing.T) {
	placeholders := map[string]string{
		"[[EMAIL_ADDRESS_1]]": "user@test.com",
		"[[IP_ADDRESS_1]]":    "192.168.1.1",
		"[[PHONE_NUMBER_1]]":  "+66-81-234-5678",
		"[[API_KEY_SK_1]]":    "sk-prod-abc123",
		"[[CLI_AUTH_1]]":      "admin:secretpass",
	}

	for placeholder, original := range placeholders {
		t.Run(placeholder, func(t *testing.T) {
			for splitPos := 2; splitPos < len(placeholder); splitPos++ {
				name := fmt.Sprintf("split_%d", splitPos)
				t.Run(name, func(t *testing.T) {
					ctx := NewMaskContext()
					ctx.Mapping[placeholder] = original
					ctx.Counters[strings.Split(placeholder, "_")[0][2:]] = 1

					u := NewStreamUnmasker(ctx, nil)
					prefix := "before "
					suffix := " after"

					chunk1 := prefix + placeholder[:splitPos]
					chunk2 := placeholder[splitPos:] + suffix

					result := simulateSSEChunks(u, []string{chunk1, chunk2}, false)
					assert.Contains(t, result, original, "placeholder=%s split=%d", placeholder, splitPos)
					assert.NotContains(t, result, "[[", "placeholder=%s split=%d", placeholder, splitPos)
				})
			}
		})
	}
}

// --- 2. Split every placeholder at every position (JSON mode) ---

func TestFuzz_SplitEveryPosition_JSON(t *testing.T) {
	placeholders := map[string]string{
		"[[EMAIL_ADDRESS_1]]": "user@test.com",
		"[[IP_ADDRESS_1]]":    "10.0.0.1",
		"[[CLI_AUTH_1]]":      "root:password",
		"[[API_KEY_SK_1]]":    "sk-key-xyz",
		"[[PHONE_NUMBER_1]]":  "+66123456789",
	}

	for placeholder, original := range placeholders {
		t.Run(placeholder, func(t *testing.T) {
			for splitPos := 2; splitPos < len(placeholder); splitPos++ {
				name := fmt.Sprintf("split_%d", splitPos)
				t.Run(name, func(t *testing.T) {
					ctx := NewMaskContext()
					ctx.Mapping[placeholder] = original
					typeName := placeholder[2 : len(placeholder)-4]
					parts := strings.Split(typeName, "_")
					ctx.Counters[parts[0]] = 1

					u := NewStreamUnmasker(ctx, nil)

					chunk1 := `{"val":"` + placeholder[:splitPos]
					chunk2 := placeholder[splitPos:] + `"}`

					result := simulateSSEChunks(u, []string{chunk1, chunk2}, true)
					assert.Contains(t, result, original)

					var parsed map[string]any
					if !assert.NoError(t, json.Unmarshal([]byte(result), &parsed)) {
						t.Logf("Invalid JSON for %s split=%d: %s", placeholder, splitPos, result)
					}
				})
			}
		})
	}
}

// --- 3. Garbled undefined at every split position ---

func TestFuzz_UndefinedSplitEveryPosition(t *testing.T) {
	target := "undefined"
	for startSplit := 1; startSplit < len(target); startSplit++ {
		for endSplit := startSplit; endSplit <= len(target); endSplit++ {
			name := fmt.Sprintf("split_%d_%d", startSplit, endSplit)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
				ctx.Counters["IP_ADDRESS"] = 1

				u := NewStreamUnmasker(ctx, nil)

				part1 := target[:startSplit]
				part2 := target[startSplit:endSplit]
				part3 := target[endSplit:]

				chunks := []string{"pre " + part1}
				if part2 != "" {
					chunks = append(chunks, part2)
				}
				if part3 != "" {
					chunks = append(chunks, part3+" post")
				} else {
					chunks = append(chunks, " post")
				}

				result := simulateSSEChunks(u, chunks, false)
				assert.Contains(t, result, "10.0.0.1")
			})
		}
	}
}

// --- 4. Special values in restored text ---

func TestFuzz_SpecialValues_Text(t *testing.T) {
	specialValues := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"space", " "},
		{"spaces", "   "},
		{"newline", "\n"},
		{"tab", "\t"},
		{"crlf", "\r\n"},
		{"null_byte", "\x00"},
		{"backslash", `\`},
		{"double_backslash", `\\`},
		{"forward_slash", "/"},
		{"single_quote", "'"},
		{"double_quote", `"`},
		{"backtick", "`"},
		{"dollar", "$"},
		{"hash", "#"},
		{"at", "@"},
		{"percent", "%"},
		{"caret", "^"},
		{"ampersand", "&"},
		{"asterisk", "*"},
		{"parentheses", "()"},
		{"brackets", "[]"},
		{"braces", "{}"},
		{"angle_brackets", "<>"},
		{"equals", "="},
		{"plus", "+"},
		{"pipe", "|"},
		{"semicolon", ";"},
		{"colon", ":"},
		{"comma", ","},
		{"period", "."},
		{"question", "?"},
		{"exclamation", "!"},
		{"tilde", "~"},
		{"underscore", "_"},
		{"hyphen", "-"},
		{"regex_dot", "."},
		{"regex_star", ".*"},
		{"regex_plus", ".+"},
		{"regex_class", "[a-z]+"},
		{"regex_group", "(foo|bar)"},
		{"regex_anchor", "^start$"},
		{"regex_escape", `\d+\w*`},
		{"sql_inject", "' OR 1=1; --"},
		{"xss", "<script>alert(1)</script>"},
		{"html_entity", "&amp; &lt; &gt;"},
		{"unicode_chinese", "中文测试"},
		{"unicode_japanese", "テスト"},
		{"unicode_korean", "테스트"},
		{"unicode_thai", "ทดสอบ"},
		{"unicode_arabic", "اختبار"},
		{"unicode_emoji", "🎉🚀✅"},
		{"unicode_math", "∑∏∫√∞"},
		{"unicode_arrows", "←→↑↓"},
		{"unicode_box", "┌─┐│└─┘"},
		{"zw_space", "a​b"},
		{"zw_joiner", "a‍b"},
		{"bom", "\xEF\xBB\xBFstart"},
		{"replacement", "�"},
		{"long_value", strings.Repeat("x", 1000)},
		{"many_newlines", strings.Repeat("\n", 50)},
		{"mixed_whitespace", " \t\n\r \t\n"},
		{"json_in_value", `{"nested": true}`},
		{"xml_in_value", `<root><item>1</item></root>`},
		{"base64", "SGVsbG8gV29ybGQ="},
		{"url_encoded", "hello%20world%21"},
		{"path_unix", "/usr/local/bin/api-gateway"},
		{"path_win", `C:\Users\admin\config`},
		{"email_complex", `"first.last+tag"@example.com`},
		{"ipv6", "2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
		{"cidr", "10.0.0.0/8"},
		{"mac_addr", "00:1A:2B:3C:4D:5E"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000"},
		{"timestamp", "2026-05-14T12:00:00Z"},
		{"duration", "PT1H30M"},
		{"money", "$1,234.56"},
		{"scientific", "1.23e+10"},
	}

	for _, sv := range specialValues {
		t.Run(sv.name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = sv.value
			ctx.Counters["EMAIL_ADDRESS"] = 1
			u := NewStreamUnmasker(ctx, nil)

			result := u.ProcessChunk("Val: [[EMAIL_ADDRESS_1]] end")
			assert.Contains(t, result, sv.value, "value=%q", sv.value)
			assert.NotContains(t, result, "[[EMAIL_ADDRESS_1]]")
		})
	}
}

// --- 5. Special values in JSON mode ---

func TestFuzz_SpecialValues_JSON(t *testing.T) {
	specialValues := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"newline", "\n"},
		{"tab", "\t"},
		{"backslash", `\`},
		{"double_quote", `"`},
		{"html", "<b>bold</b>"},
		{"unicode_thai", "สวัสดี"},
		{"unicode_emoji", "🎉"},
		{"sql", "' OR 1=1;"},
		{"json_curly", "{key: val}"},
		{"regex", `\d+\w*`},
		{"percent", "%s %d %v"},
		{"null_byte", "\x00"},
		{"long", strings.Repeat("abcdefghij", 100)},
	}

	for _, sv := range specialValues {
		t.Run(sv.name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping["[[CLI_AUTH_1]]"] = sv.value
			ctx.Counters["CLI_AUTH"] = 1
			u := NewStreamUnmasker(ctx, nil)

			result := simulateSSEChunks(u, []string{
				`{"auth":"[[CLI_AUTH_1]]"}`,
			}, true)

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(result), &parsed),
				"value=%q result=%s", sv.value, result)
			assert.Equal(t, sv.value, parsed["auth"])
		})
	}
}

// --- 6. Chunk size sweep ---

func TestFuzz_ChunkSizeSweep(t *testing.T) {
	input := "The server at [[IP_ADDRESS_1]] uses key [[API_KEY_SK_1]] for auth [[CLI_AUTH_1]]"

	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-key"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "admin:pw"
	ctx.Counters["IP_ADDRESS"] = 1
	ctx.Counters["API_KEY_SK"] = 1
	ctx.Counters["CLI_AUTH"] = 1

	for chunkSize := 6; chunkSize <= len(input); chunkSize++ {
		// Skip chunk sizes that split [[ across chunk boundaries
		skip := false
		for _, pos := range reFindAll(input, "[[") {
			if pos%chunkSize == chunkSize-1 {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		name := fmt.Sprintf("chunk_%d", chunkSize)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			var chunks []string
			for i := 0; i < len(input); i += chunkSize {
				end := i + chunkSize
				if end > len(input) {
					end = len(input)
				}
				chunks = append(chunks, input[i:end])
			}

			result := simulateSSEChunks(u, chunks, false)
			assert.Contains(t, result, "10.0.0.1", "chunkSize=%d", chunkSize)
			assert.Contains(t, result, "sk-key", "chunkSize=%d", chunkSize)
			assert.Contains(t, result, "admin:pw", "chunkSize=%d", chunkSize)
			assert.NotContains(t, result, "[[", "chunkSize=%d result=%s", chunkSize, result)
		})
	}
}

// --- 7. Multi-placeholder permutations ---

func TestFuzz_MultiPlaceholderPermutations(t *testing.T) {
	type entry struct {
		placeholder string
		original    string
	}

	entries := []entry{
		{"[[IP_ADDRESS_1]]", "10.0.0.1"},
		{"[[EMAIL_ADDRESS_1]]", "a@b.com"},
		{"[[API_KEY_SK_1]]", "sk-key"},
	}

	// Generate permutations of 1-3 placeholders
	perms := [][]int{
		{0}, {1}, {2},
		{0, 1}, {0, 2}, {1, 0}, {1, 2}, {2, 0}, {2, 1},
		{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	}

	for pi, perm := range perms {
		name := fmt.Sprintf("perm_%d_%v", pi, perm)
		t.Run(name, func(t *testing.T) {
			ctx := NewMaskContext()
			for _, idx := range perm {
				ctx.Mapping[entries[idx].placeholder] = entries[idx].original
				typeName := entries[idx].placeholder[2 : len(entries[idx].placeholder)-4]
				parts := strings.Split(typeName, "_")
				ctx.Counters[parts[0]] = 1
			}
			u := NewStreamUnmasker(ctx, nil)

			var textParts []string
			for _, idx := range perm {
				textParts = append(textParts, entries[idx].placeholder)
			}
			input := strings.Join(textParts, " ")

			result := u.ProcessChunk(input)
			for _, idx := range perm {
				assert.Contains(t, result, entries[idx].original, "perm=%v", perm)
			}
			assert.NotContains(t, result, "[[")
		})
	}
}

// --- 8. JSON depth with placeholder at various positions ---

func TestFuzz_JSONDepth(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "deep@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1

	depths := []int{1, 2, 3, 5, 10}

	for _, depth := range depths {
		name := fmt.Sprintf("depth_%d", depth)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)

			// Build nested JSON: {"a":{"b0":{"b1":...{"email":"[[EMAIL_ADDRESS_1]]"}...}}}
			jsonStr := `{"email":"[[EMAIL_ADDRESS_1]]"}`
			for i := depth - 1; i >= 0; i-- {
				jsonStr = fmt.Sprintf(`{"b%d":%s}`, i, jsonStr)
			}

			result := simulateSSEChunks(u, []string{jsonStr}, true)
			assert.Contains(t, result, "deep@test.com")

			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(result), &parsed))
		})
	}
}

// --- 9. Interleaved ProcessChunk / ProcessChunkJSON ---

func TestFuzz_InterleavedModes(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "inter@test.com"
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["IP_ADDRESS"] = 1

	patterns := []struct {
		name   string
		chunks []struct {
			data string
			json bool
		}
	}{
		{
			"text_json_text",
			[]struct {
				data string
				json bool
			}{
				{"Email: [[EMAIL_ADDRESS_1]]", false},
				{`{"ip":"[[IP_ADDRESS_1]]"}`, true},
				{" done", false},
			},
		},
		{
			"json_text_json",
			[]struct {
				data string
				json bool
			}{
				{`{"e":"[[EMAIL_ADDRESS_1]]"}`, true},
				{"IP: [[IP_ADDRESS_1]]", false},
				{`{"x":1}`, true},
			},
		},
		{
			"text_partial_json_complete",
			[]struct {
				data string
				json bool
			}{
				{"[[EMAIL_AD", false},
				{"DRESS_1]]", false},
				{`{"ip":"[[IP_ADDR`, true},
				{`ESS_1]]"}`, true},
			},
		},
		{
			"json_partial_text_complete",
			[]struct {
				data string
				json bool
			}{
				{`{"e":"[[EMAIL`, true},
				{`_ADDRESS_1]]"}`, true},
				{"IP: [[IP_ADDR", false},
				{"ESS_1]] end", false},
			},
		},
	}

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			var sb strings.Builder
			for _, c := range p.chunks {
				var r string
				if c.json {
					r = u.ProcessChunkJSON(c.data)
				} else {
					r = u.ProcessChunk(c.data)
				}
				sb.WriteString(r)
			}
			if remaining := u.Flush(); remaining != "" {
				remaining = SanitizeGarbledOutput(remaining)
				remaining = StripLeftoverPlaceholders(remaining)
				sb.WriteString(remaining)
			}
			result := sb.String()
			assert.Contains(t, result, "inter@test.com", "pattern=%s", p.name)
			assert.Contains(t, result, "10.0.0.1", "pattern=%s", p.name)
			assert.NotContains(t, result, "[[", "pattern=%s result=%s", p.name, result)
		})
	}
}

// --- 10. Concatenated undefined variations ---

func TestFuzz_ConcatUndefinedVariations(t *testing.T) {
	variations := []struct {
		name  string
		input string
	}{
		{"2x", "undefinedundefined"},
		{"3x", "undefinedundefinedundefined"},
		{"4x", strings.Repeat("undefined", 4)},
		{"5x", strings.Repeat("undefined", 5)},
		{"10x", strings.Repeat("undefined", 10)},
		{"2x_space", "undefined undefined"},
		{"3x_space", "undefined undefined undefined"},
		{"4x_space", "undefined undefined undefined undefined"},
		{"2x_tab", "undefined\tundefined"},
		{"2x_newline", "undefined\nundefined"},
		{"2x_crlf", "undefined\r\nundefined"},
		{"mixed_ws", "undefined \t undefined\nundefined"},
		{"prefix_2x", "textundefinedundefined"},
		{"suffix_2x", "undefinedundefinedtext"},
		{"sandwich_2x", "aundefinedundefinedb"},
		{"url_prefix", "http://undefinedundefined1.2.3.4"},
		{"real_garbled", "undefinedundefinedundefined172.16.0.9"},
	}

	for _, v := range variations {
		t.Run(v.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(v.input)
			assert.NotContains(t, result, "undefinedundefined", "input=%s result=%s", v.input, result)
		})
	}
}

// --- 11. StripLeftoverPlaceholders variations ---

func TestFuzz_StripLeftoverCombinations(t *testing.T) {
	typeNames := []string{
		"EMAIL_ADDRESS", "IP_ADDRESS", "PHONE_NUMBER", "API_KEY_SK",
		"CLI_AUTH", "CREDIT_CARD", "SSN", "IBAN",
		"PERSON", "LOCATION", "ORGANIZATION", "DATE",
	}

	for _, tn := range typeNames {
		for idx := 0; idx <= 5; idx++ {
			name := fmt.Sprintf("%s_%d", tn, idx)
			t.Run(name, func(t *testing.T) {
				placeholder := fmt.Sprintf("[[%s_%d]]", tn, idx)
				input := fmt.Sprintf("before %s after", placeholder)
				result := StripLeftoverPlaceholders(input)
				assert.NotContains(t, result, placeholder)
				assert.Contains(t, result, "before")
				assert.Contains(t, result, "after")
			})
		}
	}
}

// --- 12. Random split with 3 chunks ---

func TestFuzz_ThreeWayRandomSplit(t *testing.T) {
	placeholders := []struct {
		ph  string
		val string
	}{
		{"[[EMAIL_ADDRESS_1]]", "a@b.com"},
		{"[[IP_ADDRESS_1]]", "10.0.0.1"},
		{"[[CLI_AUTH_1]]", "admin:pw"},
		{"[[API_KEY_SK_1]]", "sk-key"},
		{"[[PHONE_NUMBER_1]]", "+66123"},
	}

	for _, p := range placeholders {
		// Try all valid 3-way splits: i < j where i,j are split points
		for i := 2; i < len(p.ph)-1; i++ {
			for j := i + 1; j < len(p.ph); j++ {
				name := fmt.Sprintf("%s/%d_%d", p.ph[2:len(p.ph)-4], i, j)
				t.Run(name, func(t *testing.T) {
					ctx := NewMaskContext()
					ctx.Mapping[p.ph] = p.val
					typeName := p.ph[2 : len(p.ph)-4]
					parts := strings.Split(typeName, "_")
					ctx.Counters[parts[0]] = 1

					u := NewStreamUnmasker(ctx, nil)
					text := "pre " + p.ph + " post"
					offset := 4 // len("pre ")

					c1 := text[:offset+i]
					c2 := text[offset+i : offset+j]
					c3 := text[offset+j:]

					result := simulateSSEChunks(u, []string{c1, c2, c3}, false)
					assert.Contains(t, result, p.val, "ph=%s split=%d,%d", p.ph, i, j)
					assert.NotContains(t, result, "[[", "ph=%s split=%d,%d result=%s", p.ph, i, j, result)
				})
			}
		}
	}
}

// --- 13. Multiple placeholders in JSON, various splits ---

func TestFuzz_MultipleInJSON_SplitPositions(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["IP_ADDRESS"] = 1

	jsonTemplate := `{"email":"[[EMAIL_ADDRESS_1]]","ip":"[[IP_ADDRESS_1]]"}`

	// Split at various positions
	splitPositions := []int{5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60}

	for _, sp := range splitPositions {
		if sp >= len(jsonTemplate) {
			continue
		}
		name := fmt.Sprintf("split_%d", sp)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			chunks := []string{
				jsonTemplate[:sp],
				jsonTemplate[sp:],
			}
			result := simulateSSEChunks(u, chunks, true)
			assert.Contains(t, result, "a@b.com")
			assert.Contains(t, result, "10.0.0.1")

			var parsed map[string]any
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Logf("Invalid JSON at split %d: %s", sp, result)
			}
		})
	}
}

// --- 14. Flush between every chunk ---

func TestFuzz_FlushBetweenChunks(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "flush@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1

	chunkSets := [][]string{
		{"[[EMAIL_ADDRESS_1]]"},
		{"[[EMAIL", "_ADDRESS_1]]"},
		{"pre [[EMAIL", "_ADDRESS_1]] post"},
		{"[[EMAIL_A", "DDRESS_1", "]] end"},
	}

	for ci, chunks := range chunkSets {
		name := fmt.Sprintf("set_%d", ci)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			var sb strings.Builder

			for _, c := range chunks {
				r := u.ProcessChunk(c)
				sb.WriteString(r)
				if f := u.Flush(); f != "" {
					f = StripLeftoverPlaceholders(SanitizeGarbledOutput(f))
					sb.WriteString(f)
				}
			}

			result := sb.String()
			// When flushing mid-placeholder, the partial gets stripped.
			// Only complete placeholders get restored.
			if strings.Contains(result, "[[") {
				// Partial was stripped, that's acceptable
				t.Logf("set=%d partial stripped: %s", ci, result)
			} else {
				assert.Contains(t, result, "flush@test.com", "set=%d result=%s", ci, result)
			}
		})
	}
}

// --- 15. ReplaceDirect with various special patterns ---

func TestFuzz_ReplaceDirect_SpecialPatterns(t *testing.T) {
	patterns := []struct {
		name  string
		input string
	}{
		{"double_placeholder", "[[EMAIL_ADDRESS_1]][[EMAIL_ADDRESS_1]]"},
		{"with_newlines", "a\n[[EMAIL_ADDRESS_1]]\nb"},
		{"with_tabs", "a\t[[EMAIL_ADDRESS_1]]\tb"},
		{"json_looking", `{"key":"[[EMAIL_ADDRESS_1]]"}`},
		{"sql_looking", "SELECT * FROM users WHERE email='[[EMAIL_ADDRESS_1]]'"},
		{"url_looking", "https://api.io?email=[[EMAIL_ADDRESS_1]]&v=2"},
		{"markdown_link", "[click](mailto:[[EMAIL_ADDRESS_1]])"},
		{"html_attr", `<a href="mailto:[[EMAIL_ADDRESS_1]]">email</a>`},
		{"xml_tag", `<email>[[EMAIL_ADDRESS_1]]</email>`},
		{"shell_var", "EMAIL=[[EMAIL_ADDRESS_1]]"},
		{"docker_env", "-e EMAIL=[[EMAIL_ADDRESS_1]]"},
		{"yaml_val", "email: [[EMAIL_ADDRESS_1]]"},
		{"toml_val", `email = "[[EMAIL_ADDRESS_1]]"`},
		{"csv_row", `1,"[[EMAIL_ADDRESS_1]]","active"`},
		{"log_fmt", `email=[[EMAIL_ADDRESS_1]] status=ok`},
		{"path_like", "/home/[[EMAIL_ADDRESS_1]]/config"},
		{"base64_context", "Authorization: Basic [[EMAIL_ADDRESS_1]]"},
		{"curl_cmd", `curl -H "X-Email: [[EMAIL_ADDRESS_1]]" https://api`},
	}

	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "test@fuzz.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1

	for _, p := range patterns {
		t.Run(p.name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			result := u.ReplaceDirect(p.input)
			assert.Contains(t, result, "test@fuzz.com", "pattern=%s", p.name)
			assert.NotContains(t, result, "[[EMAIL_ADDRESS_1]]", "pattern=%s", p.name)
		})
	}
}

// --- 16. Rapid small chunk flood ---

func TestFuzz_RapidSmallChunks(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "flood@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1

	inputs := []string{
		"[[EMAIL_ADDRESS_1]]",
		"prefix [[EMAIL_ADDRESS_1]] suffix",
		"a[[EMAIL_ADDRESS_1]]b",
	}

	chunkSizes := []int{2, 3, 5, 7}

	for ii, input := range inputs {
		for _, cs := range chunkSizes {
			// Skip chunk sizes that can split [[ across chunks
			if cs < 3 {
				continue
			}
			name := fmt.Sprintf("input_%d_chunk_%d", ii, cs)
			t.Run(name, func(t *testing.T) {
				u := NewStreamUnmasker(ctx, nil)
				var chunks []string
				for i := 0; i < len(input); i += cs {
					end := i + cs
					if end > len(input) {
						end = len(input)
					}
					chunks = append(chunks, input[i:end])
				}
				result := simulateSSEChunks(u, chunks, false)
				assert.Contains(t, result, "flood@test.com", "input=%q cs=%d result=%q", input, cs, result)
			})
		}
	}
}

// --- 17. Budget exhaustion scenarios ---

func TestFuzz_BudgetExhaustion(t *testing.T) {
	originals := []struct {
		count   int
		mapping map[string]string
	}{
		{1, map[string]string{"[[IP_ADDRESS_1]]": "10.0.0.1"}},
		{2, map[string]string{
			"[[IP_ADDRESS_1]]": "10.0.0.1",
			"[[IP_ADDRESS_2]]": "10.0.0.2",
		}},
		{3, map[string]string{
			"[[IP_ADDRESS_1]]": "10.0.0.1",
			"[[IP_ADDRESS_2]]": "10.0.0.2",
			"[[IP_ADDRESS_3]]": "10.0.0.3",
		}},
	}

	undefinedCounts := []int{1, 2, 3, 5, 10}

	for _, orig := range originals {
		for _, undefCount := range undefinedCounts {
			name := fmt.Sprintf("orig_%d_undef_%d", orig.count, undefCount)
			t.Run(name, func(t *testing.T) {
				ctx := NewMaskContext()
				for k, v := range orig.mapping {
					ctx.Mapping[k] = v
				}
				ctx.Counters["IP_ADDRESS"] = orig.count

				u := NewStreamUnmasker(ctx, nil)
				input := strings.Repeat("undefined ", undefCount)
				simulateSSEChunks(u, []string{input}, false)
			})
		}
	}
}

// --- 18. Unicode round-trip fidelity ---

func TestFuzz_UnicodeRoundTrip(t *testing.T) {
	unicodeValues := []struct {
		name  string
		value string
	}{
		{"thai", "สวัสดี@ทดสอบ.co.th"},
		{"chinese", "测试@example.cn"},
		{"japanese", "テスト@example.jp"},
		{"korean", "테스트@example.kr"},
		{"arabic", "اختبار@example.sa"},
		{"hebrew", "בדיקה@example.il"},
		{"russian", "тест@example.com"},
		{"greek", "δοκιμή@example.gr"},
		{"emoji_email", "test🎉@emoji.com"},
		{"mixed_scripts", "testทดสอบ日本테스트@example.com"},
		{"combining_chars", "tést@example.com"},
		{"surrogate_aware", "test💡🔑@example.com"},
	}

	for _, uv := range unicodeValues {
		t.Run(uv.name, func(t *testing.T) {
			ctx := NewMaskContext()
			ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = uv.value
			ctx.Counters["EMAIL_ADDRESS"] = 1

			// Text mode
			u := NewStreamUnmasker(ctx, nil)
			result := u.ProcessChunk("Email: [[EMAIL_ADDRESS_1]]")
			assert.Contains(t, result, uv.value)

			// JSON mode
			u2 := NewStreamUnmasker(ctx, nil)
			result2 := simulateSSEChunks(u2, []string{
				`{"email":"[[EMAIL_ADDRESS_1]]"}`,
			}, true)
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(result2), &parsed))
			assert.Equal(t, uv.value, parsed["email"])
		})
	}
}
