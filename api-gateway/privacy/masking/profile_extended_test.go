package masking

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Extended profile tests: 100+ additional edge cases
// =============================================================================

// --- CC Profile Extended ---

func TestCCProfile_ThreeWaySplit(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"[[EMAIL",
		"_ADDRESS",
		"_1]]",
	}, false)
	assert.Contains(t, result, "a@b.com")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_PlaceholderAtExactBoundary(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+66812345678"
	ctx.Counters["PHONE_NUMBER"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Call [[PHONE_NUMBER_1]",
		"] now",
	}, false)
	assert.Contains(t, result, "+66812345678")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_PlaceholderInURL(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-key123"
	ctx.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"https://api.example.com?key=[[API_KEY_SK_1]]&v=2",
	}, false)
	assert.Contains(t, result, "sk-key123")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_PlaceholderInMarkdown(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "dev@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Contact: `[[EMAIL_ADDRESS_1]]` or **[[EMAIL_ADDRESS_1]]**",
	}, false)
	assert.Equal(t, 2, strings.Count(result, "dev@test.com"))
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_PlaceholderAtStartOfStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"[[IP_ADDRESS_1]] is the gateway",
	}, false)
	assert.Contains(t, result, "10.0.0.1")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_PlaceholderAtEndOfStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.2"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Gateway is [[IP_ADDRESS_2]]",
	}, false)
	assert.Contains(t, result, "10.0.0.2")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_ConsecutivePlaceholders(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+66812345678"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["PHONE_NUMBER"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"[[EMAIL_ADDRESS_1]][[PHONE_NUMBER_1]]",
	}, false)
	assert.Contains(t, result, "a@b.com")
	assert.Contains(t, result, "+66812345678")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_LongTextAroundPlaceholder(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "x@y.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	longPrefix := strings.Repeat("abcd ", 100)
	longSuffix := strings.Repeat("efgh ", 100)

	result := simulateSSEChunks(u, []string{
		longPrefix + "[[EMAIL_ADDRESS_1]]" + longSuffix,
	}, false)
	assert.Contains(t, result, "x@y.com")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_NoPlaceholdersInStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"This is plain text with no placeholders.",
		"Another chunk of text.",
	}, false)
	assert.Equal(t, "This is plain text with no placeholders.Another chunk of text.", result)
}

func TestCCProfile_ManySmallChunks(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.1.1"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// Split into many 3-char chunks
	text := "IP is [[IP_ADDRESS_1]] ok"
	var chunks []string
	for i := 0; i < len(text); i += 3 {
		end := i + 3
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[i:end])
	}

	result := simulateSSEChunks(u, chunks, false)
	assert.Contains(t, result, "192.168.1.1")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_SecretsInJSON_RoundTrip(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "admin:p@ss!w0rd"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"cmd":"curl -u [[CLI_AUTH_1]] http://api"}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "curl -u admin:p@ss!w0rd http://api", parsed["cmd"])
}

func TestCCProfile_MultipleSameType(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.2"
	ctx.Mapping["[[IP_ADDRESS_3]]"] = "10.0.0.3"
	ctx.Counters["IP_ADDRESS"] = 3
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"IPs: [[IP_ADDRESS_1]], [[IP_ADDRESS_2]], [[IP_ADDRESS_3]]",
	}, false)
	assert.Contains(t, result, "10.0.0.1")
	assert.Contains(t, result, "10.0.0.2")
	assert.Contains(t, result, "10.0.0.3")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_ThinkingDelta_SplitPlaceholder(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+66-999-888"
	ctx.Counters["PHONE_NUMBER"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Thinking... phone=[[PHONE_N",
		"UMBER_1]] done",
	}, false)
	assert.Contains(t, result, "+66-999-888")
	assert.NotContains(t, result, "[[")
}

func TestCCProfile_FlushAfterPartialBuffer(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "flush@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// Partial placeholder left in buffer
	r := u.ProcessChunk("End: [[EMAIL_AD")
	assert.Equal(t, "End: ", r)

	// Flush should drain and strip
	remaining := u.Flush()
	remaining = StripLeftoverPlaceholders(remaining)
	assert.NotContains(t, remaining, "[[EMAIL_ADDRESS_1]]")
}

func TestCCProfile_PlaceholderInCodeBlock(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-test-key"
	ctx.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"```python\napi_key = '[[API_KEY_SK_1]]'\n```",
	}, false)
	assert.Contains(t, result, "sk-test-key")
	assert.NotContains(t, result, "[[")
}

// --- Lotuss Profile Extended ---

func TestLotussProfile_MultipleToolCalls(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[API_KEY_SK_1]]"] = "sk-key-1"
	sec.Mapping["[[CLI_AUTH_1]]"] = "user:pass"
	sec.Counters["API_KEY_SK"] = 1
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"tool":"read","args":"key=[[API_KEY_SK_1]]"}`,
		`{"tool":"write","args":"auth=[[CLI_AUTH_1]]"}`,
	}, true)

	assert.Contains(t, result, "sk-key-1")
	assert.Contains(t, result, "user:pass")
	assert.NotContains(t, result, "[[")

	// Both must be valid JSON
	var p1, p2 map[string]any
	lines := strings.Split(result, `}{`)
	require.NoError(t, json.Unmarshal([]byte(lines[0]+"}"), &p1))
	require.NoError(t, json.Unmarshal([]byte("{"+lines[1]), &p2))
}

func TestLotussProfile_NestedJSON(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "root:secret"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"config":{"auth":"[[CLI_AUTH_1]]","tls":true}}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	config := parsed["config"].(map[string]any)
	assert.Equal(t, "root:secret", config["auth"])
}

func TestLotussProfile_SplitAtOpenBracket(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "split@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Contact [[",
		"EMAIL_ADDRESS_1]] now",
	}, false)
	assert.Contains(t, result, "split@test.com")
	assert.NotContains(t, result, "[[")
}

func TestLotussProfile_UnicodeInToolCallArgs(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "ทดสอบ@thai.co.th"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"msg":"ส่งอีเมลถึง [[EMAIL_ADDRESS_1]]"}`,
	}, true)

	assert.Contains(t, result, "ทดสอบ@thai.co.th")
	assert.NotContains(t, result, "[[")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
}

func TestLotussProfile_PasswordSpecialChars(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "user:p@$$w0rd!#$%"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"auth":"[[CLI_AUTH_1]]"}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "user:p@$$w0rd!#$%", parsed["auth"])
}

func TestLotussProfile_URLInContent(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-abc"
	ctx.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Visit https://api.io?key=[[API_KEY_SK_1]]&env=prod for docs",
	}, false)
	assert.Contains(t, result, "sk-abc")
	assert.Contains(t, result, "https://api.io?key=")
}

func TestLotussProfile_EmptyContent(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "e@t.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{"", "", "[[EMAIL_ADDRESS_1]]", ""}, false)
	assert.Contains(t, result, "e@t.com")
}

func TestLotussProfile_MultipleContentDeltas(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+66123"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["PHONE_NUMBER"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Email: [[EMAIL_ADDRESS_1]]. ",
		"Phone: [[PHONE_NUMBER_1]]. ",
		"Contact us.",
	}, false)
	assert.Contains(t, result, "a@b.com")
	assert.Contains(t, result, "+66123")
	assert.Contains(t, result, "Contact us.")
	assert.NotContains(t, result, "[[")
}

func TestLotussProfile_SplitTwice(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "twice@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"[[EMAIL",
		"_ADDRESS",
		"_1]] done",
	}, false)
	assert.Contains(t, result, "twice@test.com")
	assert.NotContains(t, result, "[[")
}

func TestLotussProfile_GarbledAfterUnmask(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// After undefined fallback replaces, garbled remains
	result := simulateSSEChunks(u, []string{
		"undefinedundefined extra",
	}, false)
	assert.NotContains(t, result, "undefinedundefined")
}

func TestLotussProfile_SecretAndPIITogether(t *testing.T) {
	pii := NewMaskContext()
	pii.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@lotuss.com"
	pii.Counters["EMAIL_ADDRESS"] = 1
	sec := NewMaskContext()
	sec.Mapping["[[API_KEY_SK_1]]"] = "sk-prod"
	sec.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(pii, sec)

	result := simulateSSEChunks(u, []string{
		"User [[EMAIL_ADDRESS_1]] key [[API_KEY_SK_1]]",
	}, false)
	assert.Contains(t, result, "user@lotuss.com")
	assert.Contains(t, result, "sk-prod")
	assert.NotContains(t, result, "[[")
}

// --- Kimi Profile Extended ---

func TestKimiProfile_ArrayOfPlaceholders(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.2"
	ctx.Counters["IP_ADDRESS"] = 2
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"ips":["[[IP_ADDRESS_1]]","[[IP_ADDRESS_2]]"]}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	ips := parsed["ips"].([]any)
	assert.Equal(t, "10.0.0.1", ips[0])
	assert.Equal(t, "10.0.0.2", ips[1])
}

func TestKimiProfile_SplitAtClosingBrackets(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "admin:pw"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"a":"[[CLI_AUTH_1]`,
		`]","b":1}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "admin:pw", parsed["a"])
}

func TestKimiProfile_LongJSONPayload(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "user:longpassword123"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	// Build a large JSON with placeholder
	obj := make(map[string]any)
	for i := 0; i < 50; i++ {
		obj[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
	}
	obj["auth"] = "[[CLI_AUTH_1]]"
	payload, _ := json.Marshal(obj)

	result := simulateSSEChunks(u, []string{string(payload)}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "user:longpassword123", parsed["auth"])
}

func TestKimiProfile_CommandStringInToolArgs(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "root:pass"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"command":"ssh [[CLI_AUTH_`,
		`1]]@server 'ls -la'"}`,
	}, true)

	assert.Contains(t, result, "root:pass")
	assert.NotContains(t, result, "[[")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
}

func TestKimiProfile_NullInJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "null@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"email":"[[EMAIL_ADDRESS_1]]","phone":null}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "null@test.com", parsed["email"])
	assert.Nil(t, parsed["phone"])
}

func TestKimiProfile_MultilineStringInJSON(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[API_KEY_SK_1]]"] = "sk-multi\nline\nkey"
	sec.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"key":"[[API_KEY_SK_1]]"}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "sk-multi\nline\nkey", parsed["key"])
}

func TestKimiProfile_Base64EncodedSecret(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "YWRtaW46cGFzcw=="
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"b64":"[[CLI_AUTH_1]]"}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "YWRtaW46cGFzcw==", parsed["b64"])
}

func TestKimiProfile_EmptyOriginalValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = ""
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Email: [[EMAIL_ADDRESS_1]] done",
	}, false)
	assert.NotContains(t, result, "[[")
}

func TestKimiProfile_BooleanInJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "bool@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"email":"[[EMAIL_ADDRESS_1]]","active":true,"verified":false}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "bool@test.com", parsed["email"])
	assert.Equal(t, true, parsed["active"])
}

func TestKimiProfile_NumericInJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = "+6612345678"
	ctx.Counters["PHONE_NUMBER"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"phone":"[[PHONE_NUMBER_1]]","port":8080}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "+6612345678", parsed["phone"])
}

func TestKimiProfile_EscapedCharsInValue(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = `path\to\file`
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	result := simulateSSEChunks(u, []string{
		`{"path":"[[CLI_AUTH_1]]"}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, `path\to\file`, parsed["path"])
}

// --- Cross-Profile Extended ---

func TestCrossProfile_WhitespaceBetweenChunks(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "ws@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"  [[EMAIL_ADDRESS_1]]  ",
	}, false)
	assert.Contains(t, result, "ws@test.com")
}

func TestCrossProfile_MixedModeTextThenJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "mix@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	r1 := u.ProcessChunk("Text: [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, r1, "mix@test.com")

	r2 := u.ProcessChunkJSON(`{"email":"[[EMAIL_ADDRESS_1]]"}`)
	assert.Contains(t, r2, "mix@test.com")
}

func TestCrossProfile_VeryLargeOriginalValue(t *testing.T) {
	ctx := NewMaskContext()
	longVal := strings.Repeat("x", 5000)
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = longVal
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"Value: [[EMAIL_ADDRESS_1]] end",
	}, false)
	assert.Contains(t, result, longVal)
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_MultipleFlushCalls(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "flush@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	_ = u.ProcessChunk("Start [[EMAIL_AD")
	r1 := u.Flush()
	r2 := u.Flush() // Second flush should return empty
	assert.NotEqual(t, "", r1)
	assert.Equal(t, "", r2)
}

func TestCrossProfile_ProcessAfterFlush(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "post@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	_ = u.ProcessChunk("Start [[EMAIL_AD")
	_ = u.Flush()

	// After flush, should work normally
	result := u.ProcessChunk("[[EMAIL_ADDRESS_1]] done")
	assert.Contains(t, result, "post@test.com")
}

func TestCrossProfile_JSONAfterTextFlush(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "jt@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	_ = u.ProcessChunk("Partial [[EMAIL_A")
	_ = u.Flush()

	result := u.ProcessChunkJSON(`{"e":"[[EMAIL_ADDRESS_1]]"}`)
	assert.Contains(t, result, "jt@test.com")
}

func TestCrossProfile_ValueContainsDoubleBrackets(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "use [[array]] syntax"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Tip: [[EMAIL_ADDRESS_1]] end")
	assert.Contains(t, result, "use [[array]] syntax")
}

func TestCrossProfile_ValueContainsUndefined(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "typeof x === \"undefined\""
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// When masking active, replaceUndefinedFallback could eat the "undefined" in the restored value
	// But it should NOT since the restored value is already in the output
	result := u.ProcessChunk("Code: [[IP_ADDRESS_1]]")
	assert.Contains(t, result, "typeof")
}

func TestCrossProfile_SpecialRegexCharsInValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[CLI_AUTH_1]]"] = "user^(.*)$pass"
	ctx.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Auth: [[CLI_AUTH_1]]")
	assert.Contains(t, result, "user^(.*)$pass")
}

func TestCrossProfile_HTMLEntitiesInValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[CLI_AUTH_1]]"] = "<script>alert('xss')</script>"
	ctx.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Input: [[CLI_AUTH_1]]")
	assert.Contains(t, result, "<script>alert('xss')</script>")
}

func TestCrossProfile_EmojiInValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "test🎉@emoji.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Email: [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, result, "test🎉@emoji.com")
}

func TestCrossProfile_RTLTextInValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "مرحب@arabic.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Email: [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, result, "مرحب@arabic.com")
}

func TestCrossProfile_ZeroWidthChars(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[CLI_AUTH_1]]"] = "pass​word" // zero-width space
	ctx.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Auth: [[CLI_AUTH_1]]")
	assert.Contains(t, result, "pass​word")
}

func TestCrossProfile_NestedJSONObject(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "nested@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"level1":{"level2":{"email":"[[EMAIL_ADDRESS_1]]"}}}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	l1 := parsed["level1"].(map[string]any)
	l2 := l1["level2"].(map[string]any)
	assert.Equal(t, "nested@test.com", l2["email"])
}

func TestCrossProfile_JSONArrayMultipleTypes(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "arr@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		`{"items":[1,"[[EMAIL_ADDRESS_1]]",true,null]}`,
	}, true)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	items := parsed["items"].([]any)
	assert.Equal(t, "arr@test.com", items[1])
}

func TestCrossProfile_InterleavedModes(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "inter@test.com"
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	r1 := u.ProcessChunk("Email [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, r1, "inter@test.com")

	r2 := u.ProcessChunkJSON(`{"ip":"[[IP_ADDRESS_1]]"}`)
	assert.Contains(t, r2, "10.0.0.1")

	r3 := u.ProcessChunk(" done")
	assert.Contains(t, r3, "done")
}

func TestCrossProfile_LongPlaceholderName(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[VERY_LONG_PLACEHOLDER_TYPE_NAME_123]]"] = "longval"
	ctx.Counters["VERY_LONG_PLACEHOLDER_TYPE_NAME"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Val: [[VERY_LONG_PLACEHOLDER_TYPE_NAME_123]]")
	assert.Contains(t, result, "longval")
}

func TestCrossProfile_ValueWithClosingBrackets(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[CLI_AUTH_1]]"] = "val]]ue"
	ctx.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("Auth: [[CLI_AUTH_1]] end")
	assert.Contains(t, result, "val]]ue")
}

func TestCrossProfile_SplitWithSurroundingBrackets(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "split@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// Chunks have brackets that could confuse parsing
	result := simulateSSEChunks(u, []string{
		"[[EMAIL",
		"_ADDRESS_1]] and [[EMAIL",
		"_ADDRESS_1]]",
	}, false)
	assert.Equal(t, 2, strings.Count(result, "split@test.com"))
}

func TestCrossProfile_PlaceholderFollowedByNonPlaceholder(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ProcessChunk("[[EMAIL_ADDRESS_1]] [[not a placeholder")
	assert.Contains(t, result, "a@b.com")
}

func TestCrossProfile_StripLeftoverPreservesNormalBrackets(t *testing.T) {
	result := StripLeftoverPlaceholders("array[0] = value")
	assert.Equal(t, "array[0] = value", result)
}

func TestCrossProfile_StripLeftoverRemovesUnknownPlaceholder(t *testing.T) {
	result := StripLeftoverPlaceholders("val [[UNKNOWN_TYPE_99]] end")
	assert.Equal(t, "val  end", result)
}

func TestCrossProfile_StripLeftoverMultipleUnknowns(t *testing.T) {
	result := StripLeftoverPlaceholders("[[A_1]] and [[B_2]] and [[C_3]]")
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_OnlyPlaceholderNoText(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "only@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{"[[EMAIL_ADDRESS_1]]"}, false)
	assert.Equal(t, "only@test.com", result)
}

// --- GLM Mode Extended ---

func TestGLMMode_UndefinedWithTabs(t *testing.T) {
	result := SanitizeGarbledOutput("undefined\tundefined")
	assert.Equal(t, "", result)
}

func TestGLMMode_UndefinedWithMixedWhitespace(t *testing.T) {
	result := SanitizeGarbledOutput("undefined \t undefined \n undefined")
	assert.Equal(t, "", result)
}

func TestGLMMode_UndefinedAtStart(t *testing.T) {
	result := SanitizeGarbledOutput("undefinedundefined hello")
	assert.Equal(t, "hello", result)
}

func TestGLMMode_UndefinedAtEnd(t *testing.T) {
	result := SanitizeGarbledOutput("hello undefinedundefined")
	assert.Equal(t, "hello ", result)
}

func TestGLMMode_TenConsecutiveUndefined(t *testing.T) {
	input := strings.Repeat("undefined", 10)
	result := SanitizeGarbledOutput(input)
	assert.Equal(t, "", result)
}

func TestGLMMode_UndefinedInJSON(t *testing.T) {
	result := SanitizeGarbledOutput(`{"val":"undefinedundefined"}`)
	assert.NotContains(t, result, "undefinedundefined")
}

func TestGLMMode_CodePatternsPreserved(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"typeof", `typeof x === "undefined"`, `typeof x === "undefined"`},
		{"void", "void 0 === undefined", "void 0 === undefined"},
		{"triple eq", "x === undefined", "x === undefined"},
		{"null coalesce", "x ?? undefined", "x ?? undefined"},
		{"not eq", "x !== undefined", "x !== undefined"},
		{"ternary", "x ? y : undefined", "x ? y : undefined"},
		{"return", "return undefined;", "return undefined;"},
		{"in array", "[undefined, 1, 2]", "[undefined, 1, 2]"},
		{"in object", `{k: undefined}`, `{k: undefined}`},
		{"default param", "function f(x = undefined)", "function f(x = undefined)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestGLMMode_TwoIndependentPairs(t *testing.T) {
	result := SanitizeGarbledOutput("a undefinedundefined b undefinedundefined c")
	assert.Equal(t, "a b c", result)
}

func TestGLMMode_LongStringWithManyUndefined(t *testing.T) {
	parts := make([]string, 20)
	for i := range parts {
		parts[i] = "undefined"
	}
	input := "start " + strings.Join(parts, "undefined") + " end"
	result := SanitizeGarbledOutput(input)
	assert.NotContains(t, result, "undefined")
}

func TestGLMMode_UndefinedImmediatelyNextToNumber(t *testing.T) {
	result := SanitizeGarbledOutput("valueundefinedundefined192.168.1.1")
	assert.Equal(t, "value192.168.1.1", result)
}

func TestGLMMode_EmptyAfterSanitize(t *testing.T) {
	result := SanitizeGarbledOutput("undefinedundefined")
	assert.Equal(t, "", result)
}

func TestGLMMode_SingleUndefinedVariousPositions(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"at start", "undefined is a keyword", "undefined is a keyword"},
		{"at end", "keyword is undefined", "keyword is undefined"},
		{"in middle", "the undefined value", "the undefined value"},
		{"in string", `"typeof x === 'undefined'"`, `"typeof x === 'undefined'"`},
		{"in comment", "// check undefined", "// check undefined"},
		{"in template", "`${x === undefined}`", "`${x === undefined}`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestGLMMode_CRLF(t *testing.T) {
	result := SanitizeGarbledOutput("undefined\r\nundefined")
	assert.Equal(t, "", result)
}

func TestGLMMode_BudgetExhaustion(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// More undefined than originals - budget should exhaust
	result := simulateSSEChunks(u, []string{
		"undefined undefined undefined undefined",
	}, false)
	// First undefined replaced with 10.0.0.1, rest stripped
	assert.Contains(t, result, "10.0.0.1")
	assert.Equal(t, 1, strings.Count(result, "10.0.0.1"))
}

func TestGLMMode_Phase2Dedup(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.1.1"
	ctx.Counters["IP_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	// Simulate model outputting undefined (not placeholder) that gets restored,
	// then adjacent "undefined" gets deduped
	result := simulateSSEChunks(u, []string{
		"undefined undefined",
	}, false)
	assert.Contains(t, result, "192.168.1.1")
}

func TestGLMMode_PartialUndefinedEveryPosition(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Counters["IP_ADDRESS"] = 1

	target := "undefined"
	for i := 1; i < len(target); i++ {
		name := fmt.Sprintf("split_at_%d", i)
		t.Run(name, func(t *testing.T) {
			u := NewStreamUnmasker(ctx, nil)
			result := simulateSSEChunks(u, []string{
				target[:i],
				target[i:],
			}, false)
			assert.Contains(t, result, "10.0.0.1")
		})
	}
}

func TestGLMMode_MultipleOriginalsMixedUndefined(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.2"
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "test@g.com"
	ctx.Counters["IP_ADDRESS"] = 2
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := simulateSSEChunks(u, []string{
		"undefined undefined undefined",
	}, false)
	assert.Contains(t, result, "10.0.0.1")
	assert.Contains(t, result, "10.0.0.2")
	assert.Contains(t, result, "test@g.com")
}

// --- ReplaceDirect / ReplaceDirectJSON tests ---

func TestCrossProfile_ReplaceDirect(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "direct@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ReplaceDirect("Email: [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, result, "direct@test.com")
	assert.NotContains(t, result, "[[")
}

func TestCrossProfile_ReplaceDirectJSON(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "quote\"me@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ReplaceDirectJSON(`{"e":"[[EMAIL_ADDRESS_1]]"}`)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, `quote"me@test.com`, parsed["e"])
}

func TestCrossProfile_ReplaceDirectNoContext(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)
	result := u.ReplaceDirect("no change")
	assert.Equal(t, "no change", result)
}

func TestCrossProfile_ReplaceDirectLeftover(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "known@test.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	result := u.ReplaceDirect("[[EMAIL_ADDRESS_1]] and [[UNKNOWN_99]]")
	assert.Contains(t, result, "known@test.com")
	assert.NotContains(t, result, "[[UNKNOWN_99]]")
}

// --- HasContexts tests ---

func TestCrossProfile_HasContexts(t *testing.T) {
	u1 := NewStreamUnmasker(nil, nil)
	assert.False(t, u1.HasContexts())

	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u2 := NewStreamUnmasker(ctx, nil)
	assert.True(t, u2.HasContexts())
}

// --- Flush edge cases ---

func TestCrossProfile_FlushEmpty(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)
	assert.Equal(t, "", u.Flush())
}

func TestCrossProfile_FlushTwiceSecondEmpty(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "a@b.com"
	ctx.Counters["EMAIL_ADDRESS"] = 1
	u := NewStreamUnmasker(ctx, nil)

	_ = u.ProcessChunk("Partial [[EMAIL")
	first := u.Flush()
	second := u.Flush()
	assert.NotEqual(t, "", first)
	assert.Equal(t, "", second)
}

// --- StripLeftoverPlaceholders comprehensive ---

func TestStripLeftoverPlaceholders_Comprehensive(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"no brackets", "hello world", "hello world"},
		{"single brackets", "array[0]", "array[0]"},
		{"partial open", "[[INCOMPLETE", "[[INCOMPLETE"},
		{"complete placeholder", "[[EMAIL_ADDRESS_1]]", ""},
		{"multiple", "[[A_1]] [[B_2]]", " "},
		{"mixed", "keep [[REMOVE_1]] keep", "keep  keep"},
		{"nested brackets", "[[[A_1]]]", "[]"},
		{"adjacent", "[[A_1]][[B_2]]", ""},
		{"with newlines", "a\n[[X_1]]\nb", "a\n\nb"},
		{"empty string", "", ""},
		{"only spaces", "   ", "   "},
		{"underscore in type", "[[MY_CUSTOM_TYPE_99]]", ""},
		{"number only", "[[TYPE_0]]", ""},
		{"large index", "[[TYPE_999999]]", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripLeftoverPlaceholders(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// --- SanitizeGarbledOutput comprehensive ---

func TestSanitizeGarbledOutput_Comprehensive(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"empty", "", ""},
		{"no undefined", "hello", "hello"},
		{"single start", "undefined start", "undefined start"},
		{"single end", "end undefined", "end undefined"},
		{"double tight", "undefinedundefined", ""},
		{"double spaced", "undefined undefined", ""},
		{"triple tight", "undefinedundefinedundefined", ""},
		{"quad mixed", "undefined undefinedundefined undefined", ""},
		{"prefix double", "abcundefinedundefined", "abc"},
		{"suffix double", "undefinedundefinedxyz", "xyz"},
		{"sandwich", "aundefinedundefinedb", "ab"},
		{"5 consecutive", "undefinedundefinedundefinedundefinedundefined", ""},
		{"with newlines", "undefined\nundefined", ""},
		{"with tabs", "undefined\tundefined", ""},
		{"with crlf", "undefined\r\nundefined", ""},
		{"code safe 1", "if (x === undefined) return", "if (x === undefined) return"},
		{"code safe 2", "typeof window !== \"undefined\"", "typeof window !== \"undefined\""},
		{"code safe 3", "return undefined;", "return undefined;"},
		{"code safe 4", "const x = {a: undefined}", "const x = {a: undefined}"},
		{"real URL", "http://undefinedundefined192.168.1.1", "http://192.168.1.1"},
		{"real prefix", "prefix undefinedundefined192.168.1.1", "prefix 192.168.1.1"},
		{"pair separated by text", "undefined x undefined y", "undefined x undefined y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeGarbledOutput(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

// --- Multi-profile simulation: realistic streaming scenarios ---

func TestRealistic_CCStreamingSession(t *testing.T) {
	pii := NewMaskContext()
	pii.Mapping["[[EMAIL_ADDRESS_1]]"] = "admin@lotuss.com"
	pii.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	pii.Counters["EMAIL_ADDRESS"] = 1
	pii.Counters["IP_ADDRESS"] = 1
	sec := NewMaskContext()
	sec.Mapping["[[API_KEY_SK_1]]"] = "sk-prod-key-xyz"
	sec.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(pii, sec)

	// Simulate a realistic CC session: thinking, text, tool call
	thinking := u.ProcessChunk("I need to check server [[IP_ADDRESS_1]] with key [[API_KEY_SK_1]]")
	assert.Contains(t, thinking, "10.0.0.1")
	assert.Contains(t, thinking, "sk-prod-key-xyz")

	_ = u.Flush()

	text := u.ProcessChunk("Email results to [[EMAIL_ADDRESS_1]]")
	assert.Contains(t, text, "admin@lotuss.com")

	_ = u.Flush()

	toolArgs := u.ProcessChunkJSON(`{"cmd":"ssh admin@[[IP_ADDRESS_1]] -p 22"}`)
	assert.Contains(t, toolArgs, "10.0.0.1")
	assert.NotContains(t, toolArgs, "[[")
}

func TestRealistic_LotussStreaming(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[CLI_AUTH_1]]"] = "deploy:secret123"
	sec.Counters["CLI_AUTH"] = 1
	u := NewStreamUnmasker(nil, sec)

	// Realistic lotuss session: content + tool call
	content := u.ProcessChunk("Deploying with credentials [[CLI_AUTH_1]]")
	assert.Contains(t, content, "deploy:secret123")

	toolArgs := u.ProcessChunkJSON(`{"host":"api.lotuss.co.th","auth":"[[CLI_AUTH_1]]","env":"prod"}`)
	assert.Contains(t, toolArgs, "deploy:secret123")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolArgs), &parsed))
	assert.Equal(t, "deploy:secret123", parsed["auth"])
}

func TestRealistic_KimiStreaming(t *testing.T) {
	sec := NewMaskContext()
	sec.Mapping["[[API_KEY_SK_1]]"] = "sk-kimi-abc"
	sec.Counters["API_KEY_SK"] = 1
	u := NewStreamUnmasker(nil, sec)

	// Kimi session: thinking with key, then tool call with key in JSON
	think := u.ProcessChunk("Use API key [[API_KEY_SK_1]]")
	assert.Contains(t, think, "sk-kimi-abc")

	_ = u.Flush()

	args := u.ProcessChunkJSON(`{"url":"https://api.moonshot.cn/v1","key":"[[API_KEY_SK_1]]"}`)
	assert.Contains(t, args, "sk-kimi-abc")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(args), &parsed))
	assert.Equal(t, "sk-kimi-abc", parsed["key"])
}

func TestRealistic_GLMGarbledStream(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "172.16.0.1"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "172.16.0.2"
	ctx.Counters["IP_ADDRESS"] = 2
	u := NewStreamUnmasker(ctx, nil)

	// Realistic GLM garbled output scenario
	result := simulateSSEChunks(u, []string{
		"Server: undefined",
		" undefinedundef",
		"ined172.16.0.",
		"9 undefined undefined done",
	}, false)

	// Should recover original values from undefined fallback
	assert.Contains(t, result, "172.16.0.1")
	assert.Contains(t, result, "172.16.0.2")
}

func TestRealistic_NoMaskingNormalCode(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)

	// Code output with legitimate undefined - should not be touched
	result := simulateSSEChunks(u, []string{
		"const x = typeof window !== 'undefined' ? window.innerWidth : null;",
		"const y = x !== undefined ? x * 2 : 0;",
		"if (result === undefined) return;",
	}, false)

	assert.Contains(t, result, "typeof window !== 'undefined'")
	assert.Contains(t, result, "x !== undefined")
	assert.Contains(t, result, "result === undefined")
}
