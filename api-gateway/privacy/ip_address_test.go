package privacy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoundtrip_IPAddress(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	body := makeBody("The server at 192.168.1.100 is down")

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	if result == nil {
		t.Fatal("no PII detected for IP address")
	}

	t.Logf("HasSecrets: %v, HasPII: %v", result.HasSecrets, result.HasPII)
	t.Logf("SecretsCtx.Mapping: %v", result.SecretsCtx.Mapping)
	t.Logf("PIICtx.Mapping: %v", result.PIICtx.Mapping)

	assert.True(t, result.HasPII, "expected HasPII=true for IP_ADDRESS")

	var ph string
	for k := range result.PIICtx.Mapping {
		ph = k
		break
	}
	assert.NotEmpty(t, ph, "expected a PII placeholder")
	assert.Contains(t, ph, "IP_ADDRESS", "placeholder should contain IP_ADDRESS")

	// Test non-streaming unmask
	t.Run("non_streaming", func(t *testing.T) {
		response := `{"id":"msg_123","content":[{"type":"text","text":"I see the IP ` + ph + ` in your message"}]}`
		unmasked := p.UnmaskResponse([]byte(response), result)
		unmaskedStr := string(unmasked)
		t.Logf("unmasked: %s", unmaskedStr)
		assert.NotContains(t, unmaskedStr, ph, "placeholder should not survive unmask")
		assert.Contains(t, unmaskedStr, "192.168.1.100", "original IP should be restored")

		// Verify JSON is still valid
		var parsed map[string]any
		assert.NoError(t, json.Unmarshal(unmasked, &parsed))
	})

	// Test streaming unmask - complete placeholder in single chunk
	t.Run("streaming_single_chunk", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)
		assert.True(t, unmasker.HasContexts())

		text := "I see the IP " + ph + " in your message"
		out := unmasker.ProcessChunk(text)
		t.Logf("input: %q", text)
		t.Logf("output: %q", out)

		flushed := unmasker.Flush()
		fullOutput := out + flushed
		t.Logf("full output: %s", fullOutput)

		assert.NotContains(t, fullOutput, ph, "placeholder should not survive streaming unmask")
		assert.Contains(t, fullOutput, "192.168.1.100", "original IP should be restored")
	})

	// Test streaming unmask - placeholder split across chunks
	t.Run("streaming_split", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)

		mid := len(ph) / 2
		if mid == 0 {
			mid = 1
		}

		chunk1 := "The IP is " + ph[:mid]
		out1 := unmasker.ProcessChunk(chunk1)
		t.Logf("chunk1: %q -> %q", chunk1, out1)

		chunk2 := ph[mid:] + " end"
		out2 := unmasker.ProcessChunk(chunk2)
		t.Logf("chunk2: %q -> %q", chunk2, out2)

		flushed := unmasker.Flush()
		t.Logf("flushed: %q", flushed)

		fullOutput := out1 + out2 + flushed
		t.Logf("full output: %s", fullOutput)

		assert.NotContains(t, fullOutput, ph, "placeholder should not survive split streaming unmask")
		assert.Contains(t, fullOutput, "192.168.1.100", "original IP should be restored")
	})

	// Test streaming unmask - placeholder split at "[[" boundary
	t.Run("streaming_split_at_bracket", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)

		chunk1 := "IP: "
		out1 := unmasker.ProcessChunk(chunk1)
		t.Logf("chunk1: %q -> %q", chunk1, out1)

		// Split right at "[["
		chunk2 := "[[" + ph[2:]
		out2 := unmasker.ProcessChunk(chunk2)
		t.Logf("chunk2: %q -> %q", chunk2, out2)

		chunk3 := " done"
		out3 := unmasker.ProcessChunk(chunk3)
		t.Logf("chunk3: %q -> %q", chunk3, out3)

		flushed := unmasker.Flush()
		t.Logf("flushed: %q", flushed)

		fullOutput := out1 + out2 + out3 + flushed
		t.Logf("full output: %s", fullOutput)

		assert.NotContains(t, fullOutput, ph, "placeholder should not survive bracket-split unmask")
		assert.Contains(t, fullOutput, "192.168.1.100", "original IP should be restored")
	})
}

func TestRoundtrip_IPAddress_StreamingJSON(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	body := makeBody("Server 10.0.0.5 has issue")
	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	if result == nil {
		t.Fatal("no PII detected")
	}

	var ph string
	for k := range result.PIICtx.Mapping {
		ph = k
		break
	}
	t.Logf("placeholder: %s -> %s", ph, result.PIICtx.Mapping[ph])

	// Simulate Anthropic SSE format
	t.Run("anthropic_sse_text", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)

		// Simulate chunked text delta
		sseData := fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Server %s has issue"}}`, ph)
		var evt struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		assert.NoError(t, json.Unmarshal([]byte(sseData), &evt))

		evt.Delta.Text = unmasker.ProcessChunk(evt.Delta.Text)
		t.Logf("unmasked text: %s", evt.Delta.Text)

		flushed := unmasker.Flush()
		fullText := evt.Delta.Text + flushed

		assert.NotContains(t, fullText, ph)
		assert.Contains(t, fullText, "10.0.0.5")
	})

	t.Run("anthropic_sse_thinking", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)

		thinkingText := "User mentions " + ph
		out := unmasker.ProcessChunk(thinkingText)
		flushed := unmasker.Flush()
		fullText := out + flushed

		assert.NotContains(t, fullText, ph)
		assert.Contains(t, fullText, "10.0.0.5")
	})

	t.Run("anthropic_sse_partial_json", func(t *testing.T) {
		unmasker := p.NewStreamUnmasker(result)

		partial := fmt.Sprintf(`{"server":"%s"}`, ph)
		out := unmasker.ReplaceDirectJSON(partial)
		t.Logf("partial_json: %q -> %q", partial, out)

		assert.NotContains(t, out, ph)
		assert.Contains(t, out, "10.0.0.5")
	})
}

func TestRoundtrip_IPAddress_MultipleIPs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	content := "From 192.168.1.1 to 10.0.0.5 via 172.16.0.1"
	body := makeBody(content)

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	if result == nil {
		t.Fatal("no PII detected for multiple IPs")
	}

	t.Logf("PII mapping: %v", result.PIICtx.Mapping)
	assert.True(t, result.HasPII)
	assert.GreaterOrEqual(t, len(result.PIICtx.Mapping), 2, "expected at least 2 IP placeholders")

	// Build response with all placeholders
	response := "Found IPs: "
	for ph := range result.PIICtx.Mapping {
		response += ph + " "
	}

	// Non-streaming
	unmasked := p.UnmaskResponse([]byte(response), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	for ph := range result.PIICtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "placeholder %s survived", ph)
	}
	assert.Contains(t, unmaskedStr, "192.168.1.1")
	assert.Contains(t, unmaskedStr, "10.0.0.5")

	// Streaming
	unmasker := p.NewStreamUnmasker(result)
	chunk := strings.TrimSuffix(response, " ")
	out := unmasker.ProcessChunk(chunk)
	flushed := unmasker.Flush()
	fullOutput := out + flushed
	t.Logf("streaming output: %s", fullOutput)

	for ph := range result.PIICtx.Mapping {
		assert.NotContains(t, fullOutput, ph, "placeholder %s survived streaming", ph)
	}
}

func TestRoundtrip_IPAddress_WithSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	content := `DB_HOST=192.168.1.100 SECRET='mysecret12345678'`
	body := makeBody(content)

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	if result == nil {
		t.Fatal("no secrets or PII detected")
	}

	t.Logf("HasSecrets: %v, HasPII: %v", result.HasSecrets, result.HasPII)
	t.Logf("Secrets: %v", result.SecretsCtx.Mapping)
	t.Logf("PII: %v", result.PIICtx.Mapping)

	// Build response with all placeholders
	response := "Found: "
	for ph := range result.SecretsCtx.Mapping {
		response += ph + " "
	}
	for ph := range result.PIICtx.Mapping {
		response += ph + " "
	}

	// Non-streaming unmask
	unmasked := p.UnmaskResponse([]byte(response), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	for ph := range result.SecretsCtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "secret placeholder %s survived", ph)
	}
	for ph := range result.PIICtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "PII placeholder %s survived", ph)
	}

	// Streaming unmask
	unmasker := p.NewStreamUnmasker(result)
	out := unmasker.ProcessChunk(response)
	flushed := unmasker.Flush()
	fullOutput := out + flushed
	t.Logf("streaming: %s", fullOutput)

	for ph := range result.SecretsCtx.Mapping {
		assert.NotContains(t, fullOutput, ph, "secret placeholder %s survived streaming", ph)
	}
	for ph := range result.PIICtx.Mapping {
		assert.NotContains(t, fullOutput, ph, "PII placeholder %s survived streaming", ph)
	}
}
