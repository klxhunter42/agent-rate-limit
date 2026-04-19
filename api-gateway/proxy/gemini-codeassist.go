package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
)

// codeAssistEndpoint moved to cfg.GeminiCodeAssistEndpoint

type GeminiCodeAssistProxy struct {
	client   *http.Client
	metrics  *metrics.Metrics
	endpoint string
	defaultModel string
}

func NewGeminiCodeAssistProxy(m *metrics.Metrics, codeAssistEndpoint, defaultModel string) *GeminiCodeAssistProxy {
	return &GeminiCodeAssistProxy{
		endpoint: codeAssistEndpoint,
		defaultModel: defaultModel,
		client: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		metrics: m,
	}
}

// ---------- Anthropic -> Gemini conversion ----------

// Code Assist wraps standard Gemini payload inside a "request" field.
type codeAssistRequest struct {
	Model              string        `json:"model"`
	Project            string        `json:"project,omitempty"`
	Request            geminiRequest `json:"request"`
	EnabledCreditTypes []string      `json:"enabled_credit_types,omitempty"`
}

// Code Assist wraps standard Gemini response inside a "response" field.
type codeAssistResponse struct {
	Response         *geminiResponse `json:"response,omitempty"`
	TraceID          string          `json:"traceId,omitempty"`
	ConsumedCredits  []any           `json:"consumedCredits,omitempty"`
	RemainingCredits []any           `json:"remainingCredits,omitempty"`
	Error            *geminiError    `json:"error,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool     `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text         string            `json:"text,omitempty"`
	InlineData   *geminiInlineData `json:"inlineData,omitempty"`
	FunctionCall *geminiFuncCall   `json:"functionCall,omitempty"`
	FunctionResp *geminiFuncResp   `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFuncCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFuncResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
}

type geminiGenConfig struct {
	Temperature     float64  `json:"temperature,omitempty"`
	TopP            float64  `json:"topP,omitempty"`
	TopK            float64  `json:"topK,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations"`
}

type geminiFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type geminiConversionResult struct {
	Model   string
	Request geminiRequest
}

func anthropicToGemini(payload map[string]any, defaultModel string) geminiConversionResult {
	model := defaultModel
	if m, ok := payload["model"].(string); ok {
		model = mapModelToGemini(m)
	}

	req := geminiRequest{}

	// System instruction
	if sys, ok := payload["system"].(string); ok && sys != "" {
		req.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: sys}},
		}
	} else if sysArr, ok := payload["system"].([]any); ok {
		var sysText string
		for _, s := range sysArr {
			if sm, ok := s.(map[string]any); ok {
				if t, ok := sm["text"].(string); ok {
					sysText += t
				}
			}
		}
		if sysText != "" {
			req.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: sysText}},
			}
		}
	}

	// Messages -> Contents
	if msgs, ok := payload["messages"].([]any); ok {
		for _, msg := range msgs {
			m, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			gRole := "user"
			if role == "assistant" {
				gRole = "model"
			}

			content := m["content"]
			parts := contentToParts(content)
			if len(parts) > 0 {
				req.Contents = append(req.Contents, geminiContent{
					Role:  gRole,
					Parts: parts,
				})
			}
		}
	}

	// Generation config
	req.GenerationConfig = &geminiGenConfig{}
	if v, ok := payload["temperature"].(float64); ok {
		req.GenerationConfig.Temperature = v
	}
	if v, ok := payload["top_p"].(float64); ok {
		req.GenerationConfig.TopP = v
	}
	if v, ok := payload["top_k"].(float64); ok {
		req.GenerationConfig.TopK = v
	}
	if v, ok := payload["max_tokens"].(float64); ok {
		req.GenerationConfig.MaxOutputTokens = int(v)
	} else if v, ok := payload["max_tokens"].(int); ok {
		req.GenerationConfig.MaxOutputTokens = v
	}
	if ss, ok := payload["stop_sequences"].([]any); ok {
		for _, s := range ss {
			if str, ok := s.(string); ok {
				req.GenerationConfig.StopSequences = append(req.GenerationConfig.StopSequences, str)
			}
		}
	}

	// Tools
	if tools, ok := payload["tools"].([]any); ok {
		var decls []geminiFuncDecl
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if fn, ok := tm["function"].(map[string]any); ok {
				name, _ := fn["name"].(string)
				desc, _ := fn["description"].(string)
				decls = append(decls, geminiFuncDecl{Name: name, Description: desc})
			} else if name, ok := tm["name"].(string); ok {
				desc, _ := tm["description"].(string)
				decls = append(decls, geminiFuncDecl{Name: name, Description: desc})
			}
		}
		if len(decls) > 0 {
			req.Tools = []geminiTool{{FunctionDeclarations: decls}}
		}
	}

	return geminiConversionResult{Model: model, Request: req}
}

func contentToParts(content any) []geminiPart {
	switch c := content.(type) {
	case string:
		if c != "" {
			return []geminiPart{{Text: c}}
		}
		return nil
	case []any:
		var parts []geminiPart
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			switch typ {
			case "text":
				if text, ok := m["text"].(string); ok {
					parts = append(parts, geminiPart{Text: text})
				}
			case "image":
				if src, ok := m["source"].(map[string]any); ok {
					if srcType, ok := src["type"].(string); ok && srcType == "base64" {
						mediaType, _ := src["media_type"].(string)
						data, _ := src["data"].(string)
						parts = append(parts, geminiPart{
							InlineData: &geminiInlineData{MimeType: mediaType, Data: data},
						})
					}
				}
			case "tool_use":
				name, _ := m["name"].(string)
				args, _ := m["input"].(map[string]any)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFuncCall{Name: name, Args: args},
				})
			case "tool_result":
				name, _ := m["tool_use_id"].(string)
				resp := map[string]any{}
				if content, ok := m["content"].(string); ok {
					resp["result"] = content
				} else if contentArr, ok := m["content"].([]any); ok {
					var text string
					for _, item := range contentArr {
						if cm, ok := item.(map[string]any); ok {
							if t, ok := cm["text"].(string); ok {
								text += t
							}
						}
					}
					resp["result"] = text
				}
				parts = append(parts, geminiPart{
					FunctionResp: &geminiFuncResp{Name: name, Response: resp},
				})
			}
		}
		return parts
	}
	return nil
}

func mapModelToGemini(model string) string {
	mappings := map[string]string{
		"gemini-2.5-pro":        "models/gemini-2.5-pro-preview-05-06",
		"gemini-2.5-flash":      "models/gemini-2.5-flash-preview-05-20",
		"gemini-2.0-flash":      "models/gemini-2.0-flash",
		"gemini-2.0-flash-lite": "models/gemini-2.0-flash-lite",
		"gemini-1.5-pro":        "models/gemini-1.5-pro",
		"gemini-1.5-flash":      "models/gemini-1.5-flash",
	}
	if mapped, ok := mappings[model]; ok {
		return mapped
	}
	if strings.HasPrefix(model, "models/") {
		return model
	}
	return "models/" + model
}

// ---------- Gemini -> Anthropic conversion ----------

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	UsageMeta  *geminiUsageMeta  `json:"usageMetadata,omitempty"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content      *geminiContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
}

type geminiUsageMeta struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func geminiToAnthropic(gResp geminiResponse, model string, stream bool) map[string]any {
	stopReason := "end_turn"
	if gResp.Candidates != nil && len(gResp.Candidates) > 0 {
		switch gResp.Candidates[0].FinishReason {
		case "MAX_TOKENS":
			stopReason = "max_tokens"
		case "STOP":
			stopReason = "end_turn"
		case "SAFETY":
			stopReason = "stop_sequence"
		}
	}

	var content []map[string]any
	if gResp.Candidates != nil && len(gResp.Candidates) > 0 && gResp.Candidates[0].Content != nil {
		for _, part := range gResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": part.Text,
				})
			}
			if part.FunctionCall != nil {
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    fmt.Sprintf("toolu_%s", part.FunctionCall.Name),
					"name":  part.FunctionCall.Name,
					"input": part.FunctionCall.Args,
				})
			}
		}
	}
	if content == nil {
		content = []map[string]any{{"type": "text", "text": ""}}
	}

	inputTokens := 0
	outputTokens := 0
	if gResp.UsageMeta != nil {
		inputTokens = gResp.UsageMeta.PromptTokenCount
		outputTokens = gResp.UsageMeta.CandidatesTokenCount
	}

	return map[string]any{
		"type":          "message",
		"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"role":          "assistant",
		"content":       content,
		"model":         model,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
}

// ---------- Proxy ----------

func (p *GeminiCodeAssistProxy) ProxyCodeAssist(w http.ResponseWriter, r *http.Request, accessToken string, body []byte, model string, isStream bool, feedback FeedbackFunc) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}

	result := anthropicToGemini(payload, p.defaultModel)

	// Wrap in Code Assist envelope
	caReq := codeAssistRequest{
		Model:   result.Model,
		Request: result.Request,
	}
	gBody, err := json.Marshal(caReq)
	if err != nil {
		return fmt.Errorf("marshal gemini request: %w", err)
	}

	endpoint := p.endpoint + ":generateContent"
	if isStream {
		endpoint = p.endpoint + ":streamGenerateContent?alt=sse"
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(gBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	rtt := time.Since(start)
	if err != nil {
		return fmt.Errorf("code assist request: %w", err)
	}
	defer resp.Body.Close()

	if feedback != nil {
		feedback(resp.StatusCode, rtt, resp.Header)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("code assist error (%d): %s", resp.StatusCode, string(respBody))
	}

	if isStream {
		return p.streamResponse(w, resp, model)
	}
	return p.nonStreamResponse(w, resp, model)
}

func (p *GeminiCodeAssistProxy) nonStreamResponse(w http.ResponseWriter, resp *http.Response, model string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var caResp codeAssistResponse
	if err := json.Unmarshal(body, &caResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if caResp.Error != nil {
		return fmt.Errorf("code assist error: %s", caResp.Error.Message)
	}
	if caResp.Response == nil {
		return fmt.Errorf("code assist returned empty response")
	}
	gResp := caResp.Response

	if gResp.Error != nil {
		return fmt.Errorf("gemini error: %s", gResp.Error.Message)
	}

	anthropicResp := geminiToAnthropic(*gResp, model, false)
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(anthropicResp)
}

func (p *GeminiCodeAssistProxy) streamResponse(w http.ResponseWriter, resp *http.Response, model string) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// message_start
	writeSSE(w, flusher, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"type":        "message",
			"id":          msgID,
			"role":        "assistant",
			"content":     []any{},
			"model":       model,
			"stop_reason": nil,
			"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})

	// content_block_start
	writeSSE(w, flusher, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var totalTokens int
	_ = 0
	stopReason := "end_turn"

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var caChunk codeAssistResponse
		if err := json.Unmarshal([]byte(data), &caChunk); err != nil {
			continue
		}

		if caChunk.Error != nil {
			slog.Error("code assist stream error", "message", caChunk.Error.Message)
			break
		}
		if caChunk.Response == nil {
			continue
		}
		chunk := caChunk.Response

		if chunk.UsageMeta != nil {
			_ = chunk.UsageMeta.PromptTokenCount
			totalTokens = chunk.UsageMeta.CandidatesTokenCount
		}

		if len(chunk.Candidates) > 0 {
			cand := chunk.Candidates[0]
			if cand.FinishReason == "MAX_TOKENS" {
				stopReason = "max_tokens"
			}

			if cand.Content != nil {
				for _, part := range cand.Content.Parts {
					if part.Text != "" {
						writeSSE(w, flusher, "content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": 0,
							"delta": map[string]any{"type": "text_delta", "text": part.Text},
						})
					}
				}
			}
		}
	}

	// content_block_stop
	writeSSE(w, flusher, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})

	// message_delta
	writeSSE(w, flusher, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": totalTokens},
	})

	// message_stop
	writeSSE(w, flusher, "message_stop", map[string]any{"type": "message_stop"})

	return nil
}

func writeSSE(w io.Writer, flusher http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	flusher.Flush()
}
