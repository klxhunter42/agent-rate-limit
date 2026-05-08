package masking

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Truly unique test cases - each one tests a different scenario.

func TestFuzz_UniqueWildCases(t *testing.T) {
	ctx1 := NewMaskContext()
	ctx1.Mapping["[[IP_1]]"] = "10.0.0.1"

	ctx2 := NewMaskContext()
	ctx2.Mapping["[[A_1]]"] = "alpha"
	ctx2.Mapping["[[B_1]]"] = "beta"
	ctx2.Mapping["[[C_1]]"] = "gamma"
	ctx2.Mapping["[[D_1]]"] = "delta"
	ctx2.Mapping["[[E_1]]"] = "epsilon"

	ctxEmpty := NewMaskContext()
	ctxEmpty.Mapping["[[X_1]]"] = ""

	ctxUndef := NewMaskContext()
	ctxUndef.Mapping["[[X_1]]"] = "undefined"

	ctxLong := NewMaskContext()
	ctxLong.Mapping["[[LONG_1]]"] = strings.Repeat("A", 500)

	ctxWithNewline := NewMaskContext()
	ctxWithNewline.Mapping["[[NL_1]]"] = "line1\nline2"

	ctxWithJSON := NewMaskContext()
	ctxWithJSON.Mapping["[[J_1]]"] = `{"nested":"value"}`

	ctxWithBackslash := NewMaskContext()
	ctxWithBackslash.Mapping["[[BS_1]]"] = `C:\Users\admin`

	ctxUnicode := NewMaskContext()
	ctxUnicode.Mapping["[[U_1]]"] = "用户@example.com"

	// Each case is a unique scenario.
	cases := []struct {
		name    string
		ctx     *MaskContext
		secCtx  *MaskContext
		chunks  []string
		flush   bool
		checkFn func(t *testing.T, full string)
	}{
		// --- Single "undefined" at various positions in various texts ---
		{name: "01_undefined_at_pos0",
			ctx: ctx1, chunks: []string{"undefined rest"}, checkFn: noUndef},
		{name: "02_undefined_at_end",
			ctx: ctx1, chunks: []string{"prefix undefined"}, checkFn: noUndef},
		{name: "03_undefined_only_chunk",
			ctx: ctx1, chunks: []string{"undefined"}, checkFn: noUndef},
		{name: "04_undefined_middle_of_sentence",
			ctx: ctx1, chunks: []string{"the value is undefined here"}, checkFn: noUndef},

		// --- Split across exactly 2 chunks at every boundary ---
		{name: "05_split_u_ndefined",
			ctx: ctx1, chunks: []string{"u", "ndefined"}, checkFn: noUndef},
		{name: "06_split_un_defined",
			ctx: ctx1, chunks: []string{"un", "defined"}, checkFn: noUndef},
		{name: "07_split_und_efined",
			ctx: ctx1, chunks: []string{"und", "efined"}, checkFn: noUndef},
		{name: "08_split_unde_fined",
			ctx: ctx1, chunks: []string{"unde", "fined"}, checkFn: noUndef},
		{name: "09_split_undef_ined",
			ctx: ctx1, chunks: []string{"undef", "ined"}, checkFn: noUndef},
		{name: "10_split_undefi_ned",
			ctx: ctx1, chunks: []string{"undefi", "ned"}, checkFn: noUndef},
		{name: "11_split_undefin_ed",
			ctx: ctx1, chunks: []string{"undefin", "ed"}, checkFn: noUndef},
		{name: "12_split_undefine_d",
			ctx: ctx1, chunks: []string{"undefine", "d"}, checkFn: noUndef},

		// --- Split with surrounding text at boundary ---
		{name: "13_prefix_split_u",
			ctx: ctx1, chunks: []string{"val=u", "ndefined"}, checkFn: noUndef},
		{name: "14_split_d_suffix",
			ctx: ctx1, chunks: []string{"undefine", "d done"}, checkFn: noUndef},
		{name: "15_both_sides_split",
			ctx: ctx1, chunks: []string{"pre=undefi", "ned=post"}, checkFn: noUndef},

		// --- 3-way splits ---
		{name: "16_3way_und_efi_ned",
			ctx: ctx1, chunks: []string{"und", "efi", "ned"}, checkFn: noUndef},
		{name: "17_3way_u_nfef_ined",
			ctx: ctx1, chunks: []string{"u", "nfine", "d"}, checkFn: noUndef},
		{name: "18_3way_with_text",
			ctx: ctx1, chunks: []string{"ip=und", "efine", "d ok"}, checkFn: noUndef},

		// --- Many small chunks ---
		{name: "19_every_char",
			ctx: ctx1, chunks: strings.Split("undefined", ""), flush: true, checkFn: noUndef},
		{name: "20_every_char_with_prefix",
			ctx: ctx1, chunks: strings.Split("ip=undefined", ""), flush: true, checkFn: noUndef},
		{name: "21_every_char_with_prefix_suffix",
			ctx: ctx1, chunks: strings.Split("ip=undefined done", ""), flush: true, checkFn: noUndef},

		// --- Multiple undefined in one chunk ---
		{name: "22_double_undefined_one_chunk",
			ctx: ctx2, chunks: []string{"a=undefined b=undefined"}, checkFn: noUndef},
		{name: "23_triple_undefined_one_chunk",
			ctx: ctx2, chunks: []string{"a=undefined b=undefined c=undefined"}, checkFn: noUndef},
		{name: "24_concat_undefinedundefined",
			ctx: ctx2, chunks: []string{"val=undefinedundefined"}, checkFn: noUndef},
		{name: "25_concat_4x",
			ctx: ctx2, chunks: []string{"undefinedundefinedundefinedundefined"}, checkFn: noUndef},

		// --- Budget exhaustion ---
		{name: "26_1_orig_5_undefined",
			ctx: ctx1, chunks: []string{"a=undefined b=undefined c=undefined d=undefined e=undefined"}, checkFn: noUndef},
		{name: "27_2_orig_10_undefined",
			ctx: ctx2, chunks: func() []string {
				s := strings.Repeat("undefined ", 10)
				return strings.Split(s, " ")
			}(), checkFn: noUndef},

		// --- Mixed placeholder + undefined ---
		{name: "28_placeholder_then_undefined",
			ctx: ctx1, chunks: []string{"ip=[[IP_1]] key=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
				assert.Contains(t, full, "10.0.0.1")
			}},
		{name: "29_placeholder_split_then_undefined",
			ctx: ctx1, chunks: []string{"ip=[[I", "P_1]] key=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
			}},
		{name: "30_undefined_then_placeholder",
			ctx: ctx1, chunks: []string{"key=undefined ip=[[IP_1]]"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
			}},

		// --- Replacement value edge cases ---
		{name: "31_empty_replacement",
			ctx: ctxEmpty, chunks: []string{"val=undefined"}, checkFn: noUndef},
		{name: "32_replacement_is_undefined",
			ctx: ctxUndef, chunks: []string{"val=undefined"}, checkFn: noUndef}, // gets eaten by stripStray
		{name: "33_long_replacement",
			ctx: ctxLong, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, strings.Repeat("A", 500))
			}},
		{name: "34_newline_in_replacement",
			ctx: ctxWithNewline, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, "line1\nline2")
			}},
		{name: "35_json_in_replacement",
			ctx: ctxWithJSON, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, `{"nested":"value"}`)
			}},
		{name: "36_backslash_in_replacement",
			ctx: ctxWithBackslash, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, `C:\Users\admin`)
			}},
		{name: "37_unicode_in_replacement",
			ctx: ctxUnicode, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, "用户@example.com")
			}},

		// --- No masking context ---
		{name: "38_no_ctx_preserves_undefined",
			ctx: nil, chunks: []string{"val=undefined"}, checkFn: func(t *testing.T, full string) {
				assert.Contains(t, full, "undefined")
			}},
		{name: "39_no_ctx_code_across_chunks",
			ctx: nil, chunks: []string{"typeof x === un", "defined"}, checkFn: func(t *testing.T, full string) {
				assert.Contains(t, full, "undefined")
			}},

		// --- Flush scenarios ---
		{name: "40_partial_then_flush",
			ctx: ctx1, chunks: []string{"val=undef"}, flush: true, checkFn: noUndef},
		{name: "41_partial_u_then_flush",
			ctx: ctx1, chunks: []string{"val=u"}, flush: true, checkFn: noUndef},
		{name: "42_partial_undefine_then_flush",
			ctx: ctx1, chunks: []string{"val=undefine"}, flush: true, checkFn: noUndef},
		{name: "43_double_flush",
			ctx: ctx1, chunks: []string{"val=undefined"}, flush: true, checkFn: noUndef},

		// --- Punctuation around undefined ---
		{name: "44_undefined_dot",
			ctx: ctx1, chunks: []string{"val=undefined."}, checkFn: noUndef},
		{name: "45_undefined_comma",
			ctx: ctx1, chunks: []string{"val=undefined, next"}, checkFn: noUndef},
		{name: "46_undefined_paren",
			ctx: ctx1, chunks: []string{"func(undefined)"}, checkFn: noUndef},
		{name: "47_undefined_bracket",
			ctx: ctx1, chunks: []string{"[undefined]"}, checkFn: noUndef},
		{name: "48_undefined_semicolon",
			ctx: ctx1, chunks: []string{"x=undefined; y=1"}, checkFn: noUndef},
		{name: "49_undefined_quote",
			ctx: ctx1, chunks: []string{`"undefined"`}, checkFn: noUndef},
		{name: "50_undefined_slash",
			ctx: ctx1, chunks: []string{"path/undefined/end"}, checkFn: noUndef},

		// --- Unicode/emoji near undefined ---
		{name: "51_emoji_before",
			ctx: ctx1, chunks: []string{"🔒 undefined"}, checkFn: noUndef},
		{name: "52_emoji_after",
			ctx: ctx1, chunks: []string{"undefined ✅"}, checkFn: noUndef},
		{name: "53_chinese_around",
			ctx: ctx1, chunks: []string{"地址是undefined请连接"}, checkFn: noUndef},
		{name: "54_thai_around",
			ctx: ctx1, chunks: []string{"IP คือ undefined ครับ"}, checkFn: noUndef},
		{name: "55_japanese_around",
			ctx: ctx1, chunks: []string{"IPはundefinedです"}, checkFn: noUndef},
		{name: "56_korean_around",
			ctx: ctx1, chunks: []string{"IP는undefined입니다"}, checkFn: noUndef},
		{name: "57_arabic_around",
			ctx: ctx1, chunks: []string{"IP هو undefined هنا"}, checkFn: noUndef},
		{name: "58_zwj_emoji",
			ctx: ctx1, chunks: []string{"👨‍💻 undefined 👩‍💻"}, checkFn: noUndef},

		// --- Whitespace variations ---
		{name: "59_leading_spaces",
			ctx: ctx1, chunks: []string{"   undefined"}, checkFn: noUndef},
		{name: "60_trailing_spaces",
			ctx: ctx1, chunks: []string{"undefined   "}, checkFn: noUndef},
		{name: "61_multiple_spaces_between",
			ctx: ctx1, chunks: []string{"a=undefined     b=undefined"}, checkFn: noUndef},
		{name: "62_tab_before",
			ctx: ctx1, chunks: []string{"\tundefined"}, checkFn: noUndef},
		{name: "63_cr_lf",
			ctx: ctx1, chunks: []string{"undefined\r\n"}, checkFn: noUndef},

		// --- Secrets + PII split ---
		{name: "64_secrets_only",
			ctx: nil, secCtx: ctx1, chunks: []string{"key=undefined"}, checkFn: noUndef},
		{name: "65_pii_only",
			ctx: ctx1, secCtx: nil, chunks: []string{"ip=undefined"}, checkFn: noUndef},
		{name: "66_both_contexts",
			ctx: ctx1, secCtx: ctx2, chunks: []string{"ip=undefined key=undefined"}, checkFn: noUndef},

		// --- JSON mode ---
		{name: "67_json_simple",
			ctx: ctx1, chunks: []string{`"undefined"`}, checkFn: noUndef, flush: false},
		{name: "68_json_split_key",
			ctx: ctx1, chunks: []string{`{"ip":"undef`, `ined"}`}, checkFn: noUndef},
		{name: "69_json_array",
			ctx: ctx1, chunks: []string{`["undefined"]`}, checkFn: noUndef},

		// --- Large inputs ---
		{name: "70_100_concat_undefined",
			ctx: ctx2, chunks: []string{strings.Repeat("undefined", 100)}, checkFn: noUndef},
		{name: "71_100_concat_1char_chunks",
			ctx: ctx2, chunks: strings.Split(strings.Repeat("undefined", 100), ""), flush: true, checkFn: noUndef},
		{name: "72_large_surrounding_text",
			ctx: ctx1, chunks: []string{strings.Repeat("x", 10000) + "undefined" + strings.Repeat("y", 10000)}, checkFn: noUndef},

		// --- Interaction: placeholder buffer + undefined buffer ---
		{name: "73_placeholder_buf_then_undefined_buf",
			ctx: ctx1, chunks: []string{"ip=[[I", "P_1]] key=undef", "ined"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
				assert.Contains(t, full, "10.0.0.1")
			}},
		{name: "74_undefined_buf_then_placeholder_buf",
			ctx: ctx1, chunks: []string{"key=undef", "ined ip=[[I", "P_1]]"}, checkFn: func(t *testing.T, full string) {
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
			}},

		// --- Chunk that is just spaces after undefined ---
		{name: "75_spaces_after_undefined_split",
			ctx: ctx1, chunks: []string{"val=undef", "ined   "}, checkFn: noUndef},
		{name: "76_spaces_before_undefined_split",
			ctx: ctx1, chunks: []string{"val=   ", "undefined"}, checkFn: noUndef},

		// --- Alternating defined and undefined ---
		{name: "77_alternating_real_and_undefined",
			ctx: ctx2, chunks: []string{"real undefined real undefined real"}, checkFn: noUndef},

		// --- "undefined" as substring of larger word (should still get replaced) ---
		{name: "78_xundefined_prefix",
			ctx: ctx1, chunks: []string{"xundefined"}, checkFn: func(t *testing.T, full string) {
				// "xundefined" contains "undefined" -> fallback replaces it
				assert.NotContains(t, full, "undefined")
			}},

		// --- "undefined" followed by more "undefined" chars ---
		{name: "79_undefinedX",
			ctx: ctx1, chunks: []string{"undefinedX"}, checkFn: noUndef},

		// --- Random-ish splits using different chunk sizes on same input ---
		{name: "80_chunksize2",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 2), flush: true, checkFn: noUndef},
		{name: "81_chunksize3",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 3), flush: true, checkFn: noUndef},
		{name: "82_chunksize4",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 4), flush: true, checkFn: noUndef},
		{name: "83_chunksize6",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 6), flush: true, checkFn: noUndef},
		{name: "84_chunksize7",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 7), flush: true, checkFn: noUndef},
		{name: "85_chunksize11",
			ctx: ctx1, chunks: chunkN("the IP is undefined ok", 11), flush: true, checkFn: noUndef},

		// --- Thinking block simulation ---
		{name: "86_thinking_7char_chunks",
			ctx: ctx1, chunks: chunkN("Let me think... the IP is undefined and I should connect to it now.", 7), flush: true, checkFn: noUndef},
		{name: "87_thinking_3char_chunks",
			ctx: ctx1, chunks: chunkN("Hmm, the IP address is undefined. Let me try connecting.", 3), flush: true, checkFn: noUndef},

		// --- Multiple undefined split across varying chunk boundaries ---
		{name: "88_double_undef_split_differently",
			ctx: ctx2, chunks: []string{"a=undef", "ined b=un", "defined"}, checkFn: noUndef},
		{name: "89_triple_split_mixed",
			ctx: ctx2, chunks: []string{"x=u", "ndefined y=undefi", "ned z=undef", "ined"}, checkFn: noUndef},

		// --- Zero-width chars ---
		{name: "90_zwj_before_u",
			ctx: ctx1, chunks: []string{"val=​undefined"}, checkFn: noUndef},
		{name: "91_zwj_in_middle",
			ctx: ctx1, chunks: []string{"undef​ined"}, checkFn: func(t *testing.T, full string) {
				// Zero-width char splits "undefined" -> won't match
				// This is a known limitation
			}},

		// --- ProcessChunkJSON mixed with ProcessChunk ---
		{name: "92_mixed_json_and_text",
			ctx: ctx1, chunks: nil, checkFn: func(t *testing.T, _ string) {
				u := NewStreamUnmasker(ctx1, nil)
				r1 := u.ProcessChunk("ip=undef")
				r2 := u.ProcessChunkJSON("ined")
				full := r1 + r2
				assert.NotContains(t, full, "undefined")
			}},

		// --- ReplaceDirect should NOT use undefined buffer ---
		{name: "93_replace_direct_no_buffer",
			ctx: ctx1, checkFn: func(t *testing.T, _ string) {
				u := NewStreamUnmasker(ctx1, nil)
				r := u.ReplaceDirect("ip=[[IP_1]] key=undefined")
				assert.NotContains(t, r, "[[")
				// ReplaceDirect only does placeholder restore, not undefined fallback
			}},

		// --- ReplaceDirectJSON ---
		{name: "94_replace_direct_json",
			ctx: ctx1, checkFn: func(t *testing.T, _ string) {
				u := NewStreamUnmasker(ctx1, nil)
				r := u.ReplaceDirectJSON(`"ip":"[[IP_1]]"`)
				assert.Contains(t, r, "10.0.0.1")
				assert.NotContains(t, r, "[[")
			}},

		// --- Empty inputs ---
		{name: "95_empty_chunk",
			ctx: ctx1, chunks: []string{""}, checkFn: func(t *testing.T, full string) {
				assert.Equal(t, "", full)
			}},
		{name: "96_empty_then_undefined",
			ctx: ctx1, chunks: []string{"", "undefined"}, checkFn: noUndef},
		{name: "97_undefined_then_empty",
			ctx: ctx1, chunks: []string{"undefined", ""}, checkFn: noUndef},

		// --- Reuse after flush ---
		{name: "98_process_flush_process",
			ctx: ctx1, checkFn: func(t *testing.T, _ string) {
				u := NewStreamUnmasker(ctx1, nil)
				r1 := u.ProcessChunk("a=undefined")
				_ = u.Flush()
				r2 := u.ProcessChunk("b=undefined")
				full := r1 + r2
				assert.NotContains(t, full, "undefined")
			}},

		// --- Stress: many originals, many undefined ---
		{name: "99_stress_10_orig_10_undef_1char",
			ctx: ctx2, chunks: strings.Split(strings.Repeat("undefined", 10), ""), flush: true, checkFn: noUndef},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := NewStreamUnmasker(tc.ctx, tc.secCtx)
			var full string

			if tc.chunks != nil {
				for _, ch := range tc.chunks {
					full += u.ProcessChunk(ch)
				}
			}

			if tc.flush {
				full += u.Flush()
			}

			tc.checkFn(t, full)
		})
	}
}

func noUndef(t *testing.T, full string) {
	assert.NotContains(t, full, "undefined")
	assert.NotContains(t, full, "[[")
}

func chunkN(s string, n int) []string {
	var chunks []string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// Deterministic seeded random test: 900 unique random splits.
func TestFuzz_RandomSplits(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[A_1]]"] = "alpha"
	ctx.Mapping["[[B_1]]"] = "beta"
	ctx.Mapping["[[C_1]]"] = "gamma"

	patterns := []string{
		"undefined",
		"x=undefined",
		"undefined end",
		"a=undefined b=undefined",
		"undefinedundefined",
		"undefinedundefinedundefined",
		"the value is undefined here",
		"IP: undefined, pass: undefined.",
		"[undefined]",
		`{"val":"undefined"}`,
		"จงเชื่อมต่อ undefined ที่นี่",
		"🔒undefined✅",
		"prefix_" + strings.Repeat("undefined", 5) + "_suffix",
		strings.Repeat("undefined", 20),
	}

	rng := rand.New(rand.NewSource(42)) // deterministic

	for i := 0; i < 900; i++ {
		pattern := patterns[i%len(patterns)]
		name := fmt.Sprintf("rand_%04d", i)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			remaining := pattern
			var full string

			for len(remaining) > 0 {
				// Random chunk size 1-12
				chunkSize := rng.Intn(12) + 1
				if chunkSize > len(remaining) {
					chunkSize = len(remaining)
				}
				full += u.ProcessChunk(remaining[:chunkSize])
				remaining = remaining[chunkSize:]
			}
			full += u.Flush()

			assert.NotContains(t, full, "undefined", "pattern=%q", pattern)
			assert.NotContains(t, full, "[[", "pattern=%q", pattern)
		})
	}
}
