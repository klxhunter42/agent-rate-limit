package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMetrics creates a Metrics instance for tests.
func newTestMetrics(t *testing.T) *metrics.Metrics {
	t.Helper()
	return metrics.New(func() float64 { return 0 }, map[string][2]float64{
		"glm-5.1": {0.5, 1.5},
	})
}

// newTestHandler creates a Handler with nil dependencies.
// Only use for testing code paths that short-circuit before touching those deps.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := &config.Config{
		EnablePromptInjection: false,
		EnableSmartMaxTokens:  false,
		MaxRequestBody:        10 * 1024 * 1024,
	}
	m := newTestMetrics(t)
	return &Handler{metrics: m, cfg: cfg}
}

// ---------------------------------------------------------------------------
// validateChatRequest
// ---------------------------------------------------------------------------

func TestValidateChatRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     ChatRequest
		wantErr string
		mutate  bool
	}{
		{
			name:    "missing agent_id",
			req:     ChatRequest{Messages: []map[string]any{{"role": "user", "content": "hi"}}},
			wantErr: "agent_id is required",
		},
		{
			name:    "empty messages",
			req:     ChatRequest{AgentID: "test", Messages: []map[string]any{}},
			wantErr: "messages must be non-empty",
		},
		{
			name:    "valid request with defaults",
			req:     ChatRequest{AgentID: "test", Messages: []map[string]any{{"role": "user", "content": "hi"}}},
			wantErr: "",
			mutate:  true,
		},
		{
			name: "valid request with values",
			req: ChatRequest{
				AgentID:     "test",
				Messages:    []map[string]any{{"role": "user", "content": "hi"}},
				MaxTokens:   2048,
				Temperature: 0.8,
				Model:       "glm-5-turbo",
				Provider:    "custom",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqCopy := tt.req
			err := validateChatRequest(&reqCopy)
			assert.Equal(t, tt.wantErr, err)
			if tt.mutate {
				assert.Equal(t, 1024, reqCopy.MaxTokens)
				assert.Equal(t, 0.7, reqCopy.Temperature)
				assert.Equal(t, "glm-5", reqCopy.Model)
				assert.Equal(t, "glm", reqCopy.Provider)
			}
		})
	}
}

func TestValidateChatRequestDefaults(t *testing.T) {
	req := &ChatRequest{
		AgentID:  "test",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}
	err := validateChatRequest(req)
	assert.Equal(t, "", err)
	assert.Equal(t, 1024, req.MaxTokens)
	assert.Equal(t, 0.7, req.Temperature)
	assert.Equal(t, "glm-5", req.Model)
	assert.Equal(t, "glm", req.Provider)
}

func TestValidateChatRequestZeroValuesBecomeDefaults(t *testing.T) {
	req := &ChatRequest{
		AgentID:     "test",
		Messages:    []map[string]any{{"role": "user", "content": "hi"}},
		MaxTokens:   0,
		Temperature: 0,
		Model:       "",
		Provider:    "",
	}
	err := validateChatRequest(req)
	assert.Equal(t, "", err)
	assert.Equal(t, 1024, req.MaxTokens)
	assert.Equal(t, 0.7, req.Temperature)
	assert.Equal(t, "glm-5", req.Model)
	assert.Equal(t, "glm", req.Provider)
}

func TestValidateChatRequestNegativeTemperature(t *testing.T) {
	req := &ChatRequest{
		AgentID:     "test",
		Messages:    []map[string]any{{"role": "user", "content": "hi"}},
		Temperature: -0.5,
	}
	err := validateChatRequest(req)
	assert.Equal(t, "", err)
	assert.Equal(t, 0.7, req.Temperature)
}

// ---------------------------------------------------------------------------
// injectSystemPrompt
// ---------------------------------------------------------------------------

func TestInjectSystemPrompt(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		prompt   string
		expected any // expected value of payload["system"]
	}{
		{
			name:     "empty prompt does nothing",
			payload:  map[string]any{"model": "glm-5"},
			prompt:   "",
			expected: nil, // system key not added
		},
		{
			name:     "no system field - adds prompt as string",
			payload:  map[string]any{"model": "glm-5"},
			prompt:   "Be concise",
			expected: "Be concise",
		},
		{
			name:     "string system - prepends prompt",
			payload:  map[string]any{"system": "You are helpful"},
			prompt:   "Be concise",
			expected: "Be concise\n\nYou are helpful",
		},
		{
			name: "array system - prepends prompt block",
			payload: map[string]any{"system": []any{
				map[string]any{"type": "text", "text": "You are helpful"},
			}},
			prompt: "Be concise",
			expected: []any{
				map[string]any{"type": "text", "text": "Be concise"},
				map[string]any{"type": "text", "text": "You are helpful"},
			},
		},
		{
			name:     "empty prompt with string system",
			payload:  map[string]any{"system": "You are helpful"},
			prompt:   "",
			expected: "You are helpful",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := make(map[string]any)
			for k, v := range tt.payload {
				payload[k] = v
			}
			injectSystemPrompt(payload, tt.prompt)
			assert.Equal(t, tt.expected, payload["system"])
		})
	}
}

func TestInjectSystemPromptEmptyPayload(t *testing.T) {
	payload := map[string]any{}
	injectSystemPrompt(payload, "test prompt")
	assert.Equal(t, "test prompt", payload["system"])
}

// ---------------------------------------------------------------------------
// applySmartMaxTokens
// ---------------------------------------------------------------------------

func TestApplySmartMaxTokens(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		model    string
		expected int
	}{
		{
			name:     "explicit max_tokens preserved",
			payload:  map[string]any{"max_tokens": 100},
			model:    "glm-5.1",
			expected: 100,
		},
		{
			name:     "glm-5.1 gets 8192",
			payload:  map[string]any{},
			model:    "glm-5.1",
			expected: 8192,
		},
		{
			name:     "glm-5-turbo gets 4096",
			payload:  map[string]any{},
			model:    "glm-5-turbo",
			expected: 4096,
		},
		{
			name:     "glm-4.5 gets 4096",
			payload:  map[string]any{},
			model:    "glm-4.5",
			expected: 4096,
		},
		{
			name:     "unknown model gets fallback 4096",
			payload:  map[string]any{},
			model:    "unknown-model",
			expected: 4096,
		},
		{
			name:     "glm-5 gets 8192",
			payload:  map[string]any{},
			model:    "glm-5",
			expected: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := make(map[string]any)
			for k, v := range tt.payload {
				payload[k] = v
			}
			applySmartMaxTokens(payload, tt.model)
			assert.Equal(t, tt.expected, payload["max_tokens"])
		})
	}
}

// ---------------------------------------------------------------------------
// writeJSON
// ---------------------------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		v          any
		wantStatus int
		wantHeader string
		wantBody   string
	}{
		{
			name:       "simple object",
			status:     200,
			v:          map[string]string{"msg": "ok"},
			wantStatus: 200,
			wantHeader: "application/json; charset=utf-8",
			wantBody:   "{\n  \"msg\": \"ok\"\n}\n",
		},
		{
			name:       "error response",
			status:     400,
			v:          map[string]string{"error": "bad request"},
			wantStatus: 400,
			wantHeader: "application/json; charset=utf-8",
			wantBody:   "{\n  \"error\": \"bad request\"\n}\n",
		},
		{
			name:       "array",
			status:     200,
			v:          []int{1, 2, 3},
			wantStatus: 200,
			wantHeader: "application/json; charset=utf-8",
			wantBody:   "[\n  1,\n  2,\n  3\n]\n",
		},
		{
			name:       "string",
			status:     200,
			v:          "hello",
			wantStatus: 200,
			wantHeader: "application/json; charset=utf-8",
			wantBody:   "\"hello\"\n",
		},
		{
			name:       "number",
			status:     200,
			v:          42,
			wantStatus: 200,
			wantHeader: "application/json; charset=utf-8",
			wantBody:   "42\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeJSON(rec, tt.status, tt.v)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantHeader, rec.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestWriteJSONHandlesComplexTypes(t *testing.T) {
	rec := httptest.NewRecorder()
	v := map[string]any{
		"number": 42,
		"float":  3.14,
		"bool":   true,
		"null":   nil,
		"array":  []int{1, 2, 3},
		"obj":    map[string]string{"key": "value"},
	}
	writeJSON(rec, 200, v)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"number": 42`)
	assert.Contains(t, rec.Body.String(), `"float": 3.14`)
}

// ---------------------------------------------------------------------------
// Health endpoint (no external deps needed)
// ---------------------------------------------------------------------------

func TestHealth(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp["status"])
}

// ---------------------------------------------------------------------------
// ChatCompletions endpoint - validation error paths (short-circuit before queue)
// ---------------------------------------------------------------------------

func TestChatCompletions(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantJSON   map[string]string
	}{
		{
			name:       "invalid JSON",
			body:       "{bad json",
			wantStatus: http.StatusBadRequest,
			wantJSON:   map[string]string{"error": "invalid JSON payload"},
		},
		{
			name:       "missing agent_id",
			body:       `{"messages":[{"role":"user","content":"hi"}]}`,
			wantStatus: http.StatusBadRequest,
			wantJSON:   map[string]string{"error": "agent_id is required"},
		},
		{
			name:       "empty messages",
			body:       `{"agent_id":"test","messages":[]}`,
			wantStatus: http.StatusBadRequest,
			wantJSON:   map[string]string{"error": "messages must be non-empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.ChatCompletions(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			var resp map[string]string
			err := json.NewDecoder(rec.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantJSON["error"], resp["error"])
		})
	}
}

// ---------------------------------------------------------------------------
// filterUnsupportedContent
// ---------------------------------------------------------------------------

func TestFilterUnsupportedContent(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		expected []any // expected content of first message
	}{
		{
			name: "strips server_tool_use blocks",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "hello"},
							map[string]any{"type": "server_tool_use", "id": "tu_123", "name": "bash", "input": map[string]any{"command": "ls"}},
							map[string]any{"type": "text", "text": "world"},
						},
					},
				},
			},
			expected: []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "text", "text": "world"},
			},
		},
		{
			name: "keeps image blocks and converts to GLM image_url format",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "aGVsbG8="}},
							map[string]any{"type": "server_tool_use", "id": "tu_456", "name": "analyze", "input": map[string]any{}},
						},
					},
				},
			},
			expected: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,aGVsbG8="}},
			},
		},
		{
			name: "HTTP URL image converted to GLM image_url format",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/img.png"}},
						},
					},
				},
			},
			expected: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/img.png"}},
			},
		},
		{
			name: "no messages key is no-op",
			payload: map[string]any{
				"model": "glm-5",
			},
			expected: nil,
		},
		{
			name: "empty content array stays empty",
			payload: map[string]any{
				"messages": []any{
					map[string]any{"role": "user", "content": []any{}},
				},
			},
			expected: []any{},
		},
		{
			name: "no unsupported blocks - all kept, images converted",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{"type": "text", "text": "hi"},
							map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/img.png"}},
							map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": "result"},
						},
					},
				},
			},
			expected: []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/img.png"}},
				map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": "result"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filterUnsupportedContent(tt.payload)
			msgs, _ := tt.payload["messages"].([]any)
			if tt.expected == nil {
				assert.Nil(t, msgs)
				return
			}
			require.Len(t, msgs, 1)
			m, ok := msgs[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.expected, m["content"])
		})
	}
}

func TestFilterUnsupportedContentMultipleMessages(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "server_tool_use", "id": "tu_1", "name": "bash", "input": map[string]any{}},
					map[string]any{"type": "text", "text": "result"},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": "output"},
					map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": "/9j/4AAQSkZJRg=="}},
				},
			},
		},
	}

	filterUnsupportedContent(payload)

	msgs := payload["messages"].([]any)
	// First message: server_tool_use stripped
	m1 := msgs[0].(map[string]any)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "result"},
	}, m1["content"])

	// Second message: all kept, base64 image converted to GLM format
	m2 := msgs[1].(map[string]any)
	assert.Equal(t, []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tu_1", "content": "output"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64,/9j/4AAQSkZJRg=="}},
	}, m2["content"])
}

// ---------------------------------------------------------------------------
// Messages endpoint - validation error paths (short-circuit before proxy/queue)
// ---------------------------------------------------------------------------

func TestMessagesEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStatus     int
		wantJSONSubstr string
	}{
		{
			name:           "invalid JSON",
			body:           "{bad",
			wantStatus:     http.StatusBadRequest,
			wantJSONSubstr: "invalid JSON payload",
		},
		{
			name:           "body too large",
			body:           string(bytes.Repeat([]byte("a"), 10*1024*1024+1)),
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantJSONSubstr: "exceeds 10MB limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)

			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			h.Messages(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantJSONSubstr != "" {
				assert.Contains(t, rec.Body.String(), tt.wantJSONSubstr)
			}
		})
	}
}

func TestAnalyzeImagePayload(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		wantBytes int
		wantCount int
	}{
		{
			name:      "no messages",
			payload:   map[string]any{},
			wantBytes: 0,
			wantCount: 0,
		},
		{
			name: "single base64 image",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"content": []any{
							map[string]any{"type": "text", "text": "describe this"},
							map[string]any{"type": "image", "source": map[string]any{
								"type":       "base64",
								"media_type": "image/png",
								"data":       strings.Repeat("a", 500000),
							}},
						},
					},
				},
			},
			wantBytes: 500000,
			wantCount: 1,
		},
		{
			name: "multiple base64 images",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"content": []any{
							map[string]any{"type": "image", "source": map[string]any{
								"type": "base64", "media_type": "image/png", "data": strings.Repeat("b", 1000000),
							}},
							map[string]any{"type": "image", "source": map[string]any{
								"type": "base64", "media_type": "image/jpeg", "data": strings.Repeat("c", 1000000),
							}},
						},
					},
				},
			},
			wantBytes: 2000000,
			wantCount: 2,
		},
		{
			name: "GLM image_url with base64 data",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"content": []any{
							map[string]any{"type": "image_url", "image_url": map[string]any{
								"url": "data:image/png;base64," + strings.Repeat("d", 300000),
							}},
						},
					},
				},
			},
			wantBytes: 300000,
			wantCount: 1,
		},
		{
			name: "URL image counted but zero bytes",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"content": []any{
							map[string]any{"type": "image_url", "image_url": map[string]any{
								"url": "https://example.com/img.png",
							}},
						},
					},
				},
			},
			wantBytes: 0,
			wantCount: 1,
		},
		{
			name: "text only no images",
			payload: map[string]any{
				"messages": []any{
					map[string]any{
						"content": []any{
							map[string]any{"type": "text", "text": "hello"},
						},
					},
				},
			},
			wantBytes: 0,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBytes, gotCount := analyzeImagePayload(tt.payload)
			assert.Equal(t, tt.wantBytes, gotBytes)
			assert.Equal(t, tt.wantCount, gotCount)
		})
	}
}

func TestSelectVisionModel(t *testing.T) {
	tests := []struct {
		name       string
		totalBytes int
		imageCount int
		want       string
	}{
		{name: "small single image", totalBytes: 200 * 1024, imageCount: 1, want: "glm-4.6v"},
		{name: "medium single image", totalBytes: 1500 * 1024, imageCount: 1, want: "glm-4.6v"},
		{name: "large single image", totalBytes: 3 * 1024 * 1024, imageCount: 1, want: "glm-4.6v-flashx"},
		{name: "3 small images", totalBytes: 100 * 1024, imageCount: 3, want: "glm-4.6v-flashx"},
		{name: "2 medium images", totalBytes: 1024 * 1024, imageCount: 2, want: "glm-4.6v"},
		{name: "2 large images", totalBytes: 2 * 1024 * 1024, imageCount: 2, want: "glm-4.6v-flashx"},
		{name: "5 screenshots", totalBytes: 500 * 1024, imageCount: 5, want: "glm-4.6v-flashx"},
		{name: "zero bytes zero images", totalBytes: 0, imageCount: 0, want: "glm-4.6v"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectVisionModel(tt.totalBytes, tt.imageCount))
		})
	}
}
