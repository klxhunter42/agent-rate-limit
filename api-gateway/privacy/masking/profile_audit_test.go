package masking

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Profile-specific SSE stream simulation tests
//
// cc (Claude Code)  -> Anthropic format, relayStreamWithTracking path
//   SSE events: content_block_delta {text_delta, thinking_delta, input_json_delta}
//   content_block_start, content_block_stop, message_delta
//
// example-provider -> OpenAI format, relayOpenAIStream path
//   SSE data: {"choices":[{"delta":{"content":"..."}}]}
//   tool_calls: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"..."}}]}}]}
//
// kimi -> Anthropic format, non-transparent proxy path
//   Same SSE format as cc but via convertOpenAIStreamResponse
// =============================================================================

// --- Shared helpers ---

func makePIICtx() *MaskContext {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+66-81-234-5678"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["PHONE_NUMBER"] = 1
	return ctx
}

func makeSecretsCtx() *MaskContext {
	ctx := NewMaskContext()
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-prod-key-abc123def456"
	ctx.Mapping["[[CLI_AUTH_1]]"] = "admin:secretpass"
	ctx.Counters["API_KEY_SK"] = 1
	ctx.Counters["CLI_AUTH"] = 1
	return ctx
}

func makeBothCtx() (*MaskContext, *MaskContext) {
	return makePIICtx(), makeSecretsCtx()
}

// simulateSSEChunks feeds chunks through ProcessChunk/ProcessChunkJSON
// and returns the concatenated output.
func simulateSSEChunks(u *StreamUnmasker, chunks []string, json bool) string {
	var sb strings.Builder
	for _, chunk := range chunks {
		var result string
		if json {
			result = u.ProcessChunkJSON(chunk)
		} else {
			result = u.ProcessChunk(chunk)
		}
		sb.WriteString(result)
	}
	if remaining := u.Flush(); remaining != "" {
		remaining = SanitizeGarbledOutput(remaining)
		remaining = StripLeftoverPlaceholders(remaining)
		sb.WriteString(remaining)
	}
	return sb.String()
}

// =============================================================================
// Profile: cc (Claude Code) - Anthropic relayStreamWithTracking path
// Uses text_delta, thinking_delta, input_json_delta
// =============================================================================

func TestCCProfile_TextDelta_FullRoundTrip(t *testing.T) {
	piiCtx, secCtx := makeBothCtx()
	u := NewStreamUnmasker(piiCtx, secCtx)

	// Simulate: upstream returns text with masked placeholders
	chunks := []string{
		"Hello, your email is [[EMAIL_ADDRESS_1]] ",
		"and your API key is [[API_KEY_SK_1]].",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "user@example.com")
	assert.Contains(t, result, "sk-prod-key-abc123def456")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_TextDelta_SplitPlaceholder(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Placeholder split across SSE chunks
	chunks := []string{
		"Email: [[EMAIL_AD",
		"DRESS_1]] done",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "user@example.com")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_InputJSONDelta_ToolCallRoundTrip(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[CLI_AUTH_1]]"] = "admin:secretpass"
	secCtx.Counters["CLI_AUTH"] = 1

	u := NewStreamUnmasker(nil, secCtx)

	// Simulate tool call arguments with masked secret split across chunks
	chunks := []string{
		`{"command":"curl -u [[CLI_A`,
		`UTH_1]] https://api.example.com"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, "admin:secretpass")
	assert.NotContains(t, result, "[[CLI")

	// Verify result is valid JSON
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "curl -u admin:secretpass https://api.example.com", parsed["command"])
}

func TestCCProfile_InputJSONDelta_IPAddressInToolArgs(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.5.111"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Real bug: CLI_AUTH or IP in tool call arguments split across chunks
	chunks := []string{
		`{"file":"/etc/hosts","content":"127.0.0.1 localhost\n[[IP_ADDR`,
		`ESS_1]] api-server\n"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, "192.168.5.111")
	assert.NotContains(t, result, "[[IP")

	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
}

func TestCCProfile_ThinkingDelta_MaskedContent(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[PHONE_NUMBER_1]]"] = "+66-81-234-5678"
	piiCtx.Counters["PHONE_NUMBER"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Thinking block contains PII
	chunks := []string{
		"The user's phone is [[PHONE_NUMB",
		"ER_1]], I should be careful.",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "+66-81-234-5678")
	assert.NotContains(t, result, "[[PHONE")
}

func TestCCProfile_CrossBlockBufferContamination(t *testing.T) {
	// H3+H4: Simulate block index change in relayStreamWithTracking
	// Block 0 (text) ends with partial placeholder, block 1 (thinking) starts fresh
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	// Block 0: text delta with partial placeholder at end
	u := NewStreamUnmasker(piiCtx, nil)
	r1 := u.ProcessChunk("Email: [[EMAIL_AD")
	assert.Equal(t, "Email: ", r1)

	// Proxy detects block index change, calls Flush()
	flushed := u.Flush()
	flushed = SanitizeGarbledOutput(flushed)
	flushed = StripLeftoverPlaceholders(flushed)

	// Partial placeholder should be stripped (safety net)
	assert.NotContains(t, flushed, "[[EMAIL_ADDRESS_1]]")
}

func TestCCProfile_MultiplePlaceholdersInOneChunk(t *testing.T) {
	piiCtx, secCtx := makeBothCtx()
	u := NewStreamUnmasker(piiCtx, secCtx)

	chunks := []string{
		"Email: [[EMAIL_ADDRESS_1]], Key: [[API_KEY_SK_1]], Auth: [[CLI_AUTH_1]], Phone: [[PHONE_NUMBER_1]]",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "user@example.com")
	assert.Contains(t, result, "sk-prod-key-abc123def456")
	assert.Contains(t, result, "admin:secretpass")
	assert.Contains(t, result, "+66-81-234-5678")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_GLMUndefinedFallback(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.5.111"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// GLM outputs "undefined" instead of placeholder
	chunks := []string{
		"The server IP is undefined",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "192.168.5.111")
	assert.NotContains(t, result, "undefined")
}

func TestCCProfile_GLMConcatenatedUndefined(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	piiCtx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.2"
	piiCtx.Counters["IP_ADDRESS"] = 2

	u := NewStreamUnmasker(piiCtx, nil)

	// GLM outputs concatenated undefinedundefinedVALUE
	chunks := []string{
		"IPs: undefinedundefined10.0.0.",
		"9 undefinedundefined undefined10.0.0.2",
	}

	result := simulateSSEChunks(u, chunks, false)
	// After undefined fallback, should have original values
	assert.Contains(t, result, "10.0.0.1")
	assert.Contains(t, result, "10.0.0.2")
}

func TestCCProfile_StripLeftoverPlaceholders(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Response contains both known and unknown placeholders
	chunks := []string{
		"Email: [[EMAIL_ADDRESS_1]] unknown: [[MANGLED_PLACEHOLDER_99]]",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "user@example.com")
	assert.NotContains(t, result, "[[MANGLED")
}

func TestCCProfile_NoMasking_Passthrough(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)

	// No masking context - text passes through unchanged
	chunks := []string{
		"typeof x === \"undefined\" is valid JavaScript",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "typeof x === \"undefined\"")
}

// =============================================================================
// Profile: example - OpenAI relayOpenAIStream path
// Uses {"choices":[{"delta":{"content":"..."}}]} format
// Tool calls via tool_calls delta
// =============================================================================

func TestLotussProfile_ContentDelta_FullRoundTrip(t *testing.T) {
	piiCtx, secCtx := makeBothCtx()
	u := NewStreamUnmasker(piiCtx, secCtx)

	// OpenAI SSE content chunks
	chunks := []string{
		"Your key is [[API_KEY_SK_1]] ",
		"and email is [[EMAIL_ADDRESS_1]].",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "sk-prod-key-abc123def456")
	assert.Contains(t, result, "user@example.com")
	assert.NotContains(t, result, "[[")
}

func TestLotussProfile_ToolCallArguments_RoundTrip(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-prod-key-abc123def456"
	secCtx.Counters["API_KEY_SK"] = 1

	u := NewStreamUnmasker(nil, secCtx)

	// OpenAI tool_calls function.arguments with masked key split across chunks
	chunks := []string{
		`{"name":"deploy","arguments":"key=[[API_KEY_SK_`,
		`1]]&env=prod"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, "sk-prod-key-abc123def456")
	assert.NotContains(t, result, "[[API")

	// Must be valid JSON after unmask
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
}

func TestLotussProfile_SplitContentAcrossChunks(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "admin@example.co.th"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Content split into many small chunks (common with streaming)
	chunks := []string{
		"Con",
		"tact: [[",
		"EMAIL_ADDRESS",
		"_1]] for ",
		"support",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "admin@example.co.th")
	assert.NotContains(t, result, "[[")
}

func TestLotussProfile_ToolUseJSONEscape(t *testing.T) {
	// M2: Verify tool_use id/name are JSON-safe
	// This tests the json.Marshal behavior used in openai.go

	testCases := []struct {
		name  string
		input string
	}{
		{"normal", "call_abc123"},
		{"with_quotes", `call_"inject`},
		{"with_backslash", `call\path`},
		{"with_newline", "call\nline"},
		{"with_html", "<script>alert(1)</script>"},
		{"with_unicode", "call_สวัสดี"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			escaped, err := json.Marshal(tc.input)
			require.NoError(t, err)

			// Round-trip must preserve original
			var unescaped string
			require.NoError(t, json.Unmarshal(escaped, &unescaped))
			assert.Equal(t, tc.input, unescaped)

			// Must be quoted JSON string
			assert.Equal(t, byte('"'), escaped[0])
			assert.Equal(t, byte('"'), escaped[len(escaped)-1])
		})
	}
}

func TestLotussProfile_StripperFlushThenUnmask(t *testing.T) {
	// H2: stripper.Flush() output must go through unmasker
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "172.16.0.1"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Simulate: stripper accumulates text containing placeholder, then flush
	flushText := "Server at [[IP_ADDRESS_1]] is down"
	result := u.ProcessChunk(flushText)
	assert.Contains(t, result, "172.16.0.1")
	assert.NotContains(t, result, "[[IP")
}

func TestLotussProfile_GLMGarbledOutput(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-key123"
	secCtx.Counters["API_KEY_SK"] = 1

	u := NewStreamUnmasker(secCtx, nil)

	// GLM garbled output: undefinedundefined
	chunks := []string{
		"Result: undefinedundefined done",
	}

	result := simulateSSEChunks(u, chunks, false)
	// Garbled undefined should be cleaned
	assert.NotContains(t, result, "undefinedundefined")
}

// =============================================================================
// Profile: kimi - Anthropic format, non-transparent path
// Uses same SSE format as cc but via convertOpenAIStreamResponse
// =============================================================================

func TestKimiProfile_TextDelta_RoundTrip(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "dev@kimi.ai"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		"Contact [[EMAIL_ADDRESS_1]] ",
		"for API access.",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "dev@kimi.ai")
	assert.NotContains(t, result, "[[")
}

func TestKimiProfile_ThinkingAndTextBlocks(t *testing.T) {
	piiCtx, secCtx := makeBothCtx()
	u := NewStreamUnmasker(piiCtx, secCtx)

	// Block 0: thinking
	thinkResult := u.ProcessChunk("User key is [[API_KEY_SK_1]]")
	assert.Contains(t, thinkResult, "sk-prod-key-abc123def456")

	// Block boundary: flush
	_ = u.Flush()

	// Block 1: text
	textResult := u.ProcessChunk("Email [[EMAIL_ADDRESS_1]] confirmed.")
	assert.Contains(t, textResult, "user@example.com")
	assert.NotContains(t, textResult, "[[")
}

func TestKimiProfile_PartialJSON_RoundTrip(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[CLI_AUTH_1]]"] = "root:password123"
	secCtx.Counters["CLI_AUTH"] = 1

	u := NewStreamUnmasker(nil, secCtx)

	// partial_json delta with masked credential
	chunks := []string{
		`{"auth":"[[CLI_A`,
		`UTH_1]]","host":"api.kimi.moonshot.cn"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, "root:password123")
	assert.NotContains(t, result, "[[CLI")

	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
}

func TestKimiProfile_MultipleSecretsInJSON(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-kimi-key-abc"
	secCtx.Mapping["[[CLI_AUTH_2]]"] = "admin:pass456"
	secCtx.Counters["API_KEY_SK"] = 1
	secCtx.Counters["CLI_AUTH"] = 2

	u := NewStreamUnmasker(nil, secCtx)

	chunks := []string{
		`{"key":"[[API_KEY_SK_1]]","auth":"[[CLI_AUTH_2]]"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, "sk-kimi-key-abc")
	assert.Contains(t, result, "admin:pass456")
	assert.NotContains(t, result, "[[")
}

func TestKimiProfile_NoContexts_PreservesUndefined(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)

	chunks := []string{
		"if (typeof window !== \"undefined\") { ... }",
	}

	result := simulateSSEChunks(u, chunks, false)
	// No masking active, "undefined" should be preserved
	assert.Contains(t, result, "undefined")
}

// =============================================================================
// Cross-profile: edge cases and regression tests
// =============================================================================

func TestCrossProfile_PlaceholderInJSONValue(t *testing.T) {
	// Placeholder inside a JSON string value must be properly escaped
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = `user"with"quotes@example.com`
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		`{"email":"[[EMAIL_ADDRESS_1]]"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	// The restored value must produce valid JSON
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Equal(t, `user"with"quotes@example.com`, parsed["email"])
}

func TestCrossProfile_NewlinesInRestoredValue(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "key\nwith\nnewlines"
	secCtx.Counters["API_KEY_SK"] = 1

	u := NewStreamUnmasker(nil, secCtx)

	chunks := []string{
		`{"key":"[[API_KEY_SK_1]]"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "key\nwith\nnewlines", parsed["key"])
}

func TestCrossProfile_TabInRestoredValue(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[PHONE_NUMBER_1]]"] = "+66\t81\t234"
	piiCtx.Counters["PHONE_NUMBER"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		`{"phone":"[[PHONE_NUMBER_1]]"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
}

func TestCrossProfile_UnicodeInRestoredValue(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "สมชาย@example.co.th"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		"อีเมล: [[EMAIL_ADDRESS_1]]",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "สมชาย@example.co.th")
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_BackslashInRestoredValue(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[CLI_AUTH_1]]"] = `domain\user:pass`
	secCtx.Counters["CLI_AUTH"] = 1

	u := NewStreamUnmasker(nil, secCtx)

	chunks := []string{
		`{"auth":"[[CLI_AUTH_1]]"}`,
	}

	result := simulateSSEChunks(u, chunks, true)
	assert.Contains(t, result, `domain\\user:pass`)

	var parsed map[string]any
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
}

func TestCrossProfile_TwoLayerMasking_PIIContainsSecret(t *testing.T) {
	// Simulate: secrets masked first (innermost), PII masked on top (outermost)
	// Unmask must reverse: PII first, then secrets
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-secret"
	secCtx.Counters["API_KEY_SK"] = 1

	piCtx := NewMaskContext()
	piCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "admin@example.com"
	piCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piCtx, secCtx)

	chunks := []string{
		"User [[EMAIL_ADDRESS_1]] key [[API_KEY_SK_1]]",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "admin@example.com")
	assert.Contains(t, result, "sk-secret")
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_FlushDrainsAllBuffers(t *testing.T) {
	piiCtx, secCtx := makeBothCtx()
	u := NewStreamUnmasker(piiCtx, secCtx)

	// Feed partial PII placeholder (text mode)
	_ = u.ProcessChunk("Email: [[EMAIL_AD")
	// Feed partial secrets placeholder (JSON mode)
	_ = u.ProcessChunkJSON(`{"key":"[[API_KEY_SK`)

	// Flush must drain both text and JSON buffers
	flushed := u.Flush()
	flushed = StripLeftoverPlaceholders(flushed)

	// Both partial placeholders should be stripped
	assert.NotContains(t, flushed, "[[EMAIL_ADDRESS_1]]")
	assert.NotContains(t, flushed, "[[API_KEY_SK_1]]")
}

func TestCrossProfile_EmptyChunks(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "test@test.com"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		"",
		"[[EMAIL_ADDRESS_1]]",
		"",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "test@test.com")
}

func TestCrossProfile_SplitPlaceholderInChunks(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// Split placeholder at realistic SSE chunk boundaries
	chunks := []string{
		"text before [[IP_ADDR",
		"ESS_1]] text after",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "10.0.0.1")
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_ProcessChunkJSON_WithJSONWrappers(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@test.com"
	piiCtx.Counters["EMAIL_ADDRESS"] = 1

	tests := []struct {
		name    string
		chunks  []string
		wantVal string
	}{
		{
			"quoted string value",
			[]string{`"[[EMAIL_ADDRESS_1]]"`},
			"user@test.com",
		},
		{
			"json key-value",
			[]string{`{"email":"[[EMAIL_ADDRESS_1]]"}`},
			"user@test.com",
		},
		{
			"array element",
			[]string{`["[[EMAIL_ADDRESS_1]]"]`},
			"user@test.com",
		},
		{
			"split across chunks in json",
			[]string{`{"a":"[[EMAIL_AD`, `DRESS_1]]"}`},
			"user@test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := NewStreamUnmasker(piiCtx, nil)
			result := simulateSSEChunks(u, tt.chunks, true)
			assert.Contains(t, result, tt.wantVal)
			assert.NotContains(t, result, "[[")
		})
	}
}

// =============================================================================
// GLM mode tests (Z.AI models)
// =============================================================================

func TestGLMMode_GLMModel_UndefinedSplitAcrossChunks(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.1.100"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	// "undefined" split mid-word across chunks (real production bug)
	chunks := []string{
		"IP: undef",
		"ined done",
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "192.168.1.100")
}

func TestGLMMode_GLMModel_ConcatUndefinedWithPartialSplit(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	piiCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piiCtx, nil)

	chunks := []string{
		"IPs: undefinedundefined undef",
		"ined done",
	}

	result := simulateSSEChunks(u, chunks, false)
	// Should clean up garbled undefined and replace with original
	assert.NotContains(t, result, "undefinedundefined")
}

func TestGLMMode_SanitizeGarbledOutput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"double undefined", "undefinedundefined", ""},
		{"triple with spaces", "undefined undefined undefined", ""},
		{"with real text", "prefix undefinedundefined192.168.1.1", "prefix 192.168.1.1"},
		{"single undefined preserved", `typeof x === "undefined"`, `typeof x === "undefined"`},
		{"empty input", "", ""},
		{"no undefined", "hello world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}
