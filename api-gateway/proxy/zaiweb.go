package proxy

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
)

// Patterns for detecting media content in Z.AI responses.
var (
	imageURLPattern = regexp.MustCompile(`!\[.*?\]\((https?://[^\s)]+)\)`)
	imageTagPattern = regexp.MustCompile(`<image[^>]*url=["']([^"']+)["']`)
	imageSrcPattern = regexp.MustCompile(`src=["'](https?://[^\s"']+)["']`)
)

// ZAIWebProxy routes requests through chat.z.ai's web chat API with
// HMAC-SHA256 request signing, mimicking the browser frontend.
type ZAIWebProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics

	mu        sync.RWMutex
	token     string // JWT Bearer token
	userID    string // extracted from JWT
	feVersion string // scraped frontend version
}

func NewZAIWebProxy(cfg *config.Config, m *metrics.Metrics) *ZAIWebProxy {
	p := &ZAIWebProxy{
		cfg:     cfg,
		client:  SharedClient(0),
		metrics: m,
	}
	if cfg.ZAIWebToken != "" {
		p.token = cfg.ZAIWebToken
		p.userID = extractUserID(cfg.ZAIWebToken)
	}
	go p.refreshFEVersion()
	go p.refreshFEVersionLoop()
	return p
}

func (p *ZAIWebProxy) SetToken(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = token
	p.userID = extractUserID(token)
}

func (p *ZAIWebProxy) GetToken() (string, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.token, p.userID
}

// fetchAnonymousToken gets a free anonymous token from chat.z.ai.
func (p *ZAIWebProxy) fetchAnonymousToken() (string, error) {
	req, _ := http.NewRequest("GET", "https://chat.z.ai/api/v1/auths/", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anonymous auth: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode anonymous token: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("empty anonymous token")
	}
	return result.Token, nil
}

// FE version scraping

var feVersionRegex = regexp.MustCompile(`prod-fe-[.\d]+`)

func (p *ZAIWebProxy) refreshFEVersion() {
	resp, err := p.client.Get("https://chat.z.ai/")
	if err != nil {
		slog.Warn("zaiweb: failed to scrape FE version", "error", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	match := feVersionRegex.Find(body)
	if match != nil {
		p.mu.Lock()
		p.feVersion = string(match)
		p.mu.Unlock()
		slog.Info("zaiweb: FE version scraped", "version", string(match))
	}
}

func (p *ZAIWebProxy) refreshFEVersionLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		p.refreshFEVersion()
	}
}

func (p *ZAIWebProxy) GetFEVersion() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.feVersion != "" {
		return p.feVersion
	}
	return "prod-fe-0.0.1"
}

// JWT user ID extraction

func extractUserID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload := parts[1]
	payload = strings.ReplaceAll(payload, "-", "+")
	payload = strings.ReplaceAll(payload, "_", "/")
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return ""
	}
	return claims.ID
}

// HMAC-SHA256 request signing

const signingKey = "key-@@@@)))()((9))-xxxx&&&%%%%%"

func generateSignature(userID, requestID string, timestamp int64, content string) string {
	period := timestamp / (5 * 60 * 1000)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(fmt.Sprintf("%d", period)))
	firstHmac := hex.EncodeToString(mac.Sum(nil))

	requestInfo := fmt.Sprintf("requestId,%s,timestamp,%d,user_id,%s", requestID, timestamp, userID)
	contentBase64 := base64.StdEncoding.EncodeToString([]byte(content))
	signData := fmt.Sprintf("%s|%s|%d", requestInfo, contentBase64, timestamp)

	mac2 := hmac.New(sha256.New, []byte(firstHmac))
	mac2.Write([]byte(signData))
	return hex.EncodeToString(mac2.Sum(nil))
}

// Model name mapping: user-facing -> chat.z.ai upstream name

var zaiWebModelMap = map[string]string{
	// Chat models
	"glm-5":          "glm-5",
	"glm-5.1":        "glm-5.1",
	"glm-5-turbo":    "glm-5-turbo",
	"glm-4.7":        "glm-4.7",
	"glm-4.7-flashx": "glm-4.7-flashx",
	"glm-4.6":        "GLM-4-6-API-V1",
	"glm-4.5":        "0727-360B-API",
	"glm-4.5-x":      "glm-4.5-x",
	"glm-4.5-air":    "0727-106B-API",
	"glm-4.5-airx":   "glm-4.5-airx",
	"glm-4-32b":      "glm-4-32b",
	"glm-4.5-flash":  "glm-4.5-flash",
	"glm-4.7-flash":  "glm-4.7-flash",
	// Vision models
	"glm-5v-turbo":    "glm-5v-turbo",
	"glm-ocr":         "glm-ocr",
	"glm-4.6v":        "glm-4.6v",
	"glm-4.6v-flashx": "glm-4.6v-flashx",
	"glm-4.5v":        "glm-4.5v",
	"glm-4.6v-flash":  "glm-4.6v-flash",
}

func mapModelToZAI(model string) string {
	if upstream, ok := zaiWebModelMap[model]; ok {
		return upstream
	}
	return model
}

// extractUserContent gets the text content of the last user message.
func extractUserContent(payload map[string]any) string {
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		content := msg["content"]
		switch c := content.(type) {
		case string:
			return c
		case []any:
			var parts []string
			for _, block := range c {
				if b, ok := block.(map[string]any); ok {
					if t, ok := b["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// convertToZAIWebFormat converts Anthropic-format messages to chat.z.ai format.
// Z.AI ignores system role, so system prompts are injected as user+assistant pairs.
func convertToZAIWebFormat(payload map[string]any, model string) map[string]any {
	upstreamModel := mapModelToZAI(model)

	var zaiMessages []map[string]any
	msgs, _ := payload["messages"].([]any)

	if sys, ok := payload["system"]; ok {
		var sysText string
		switch v := sys.(type) {
		case string:
			sysText = v
		case []any:
			var parts []string
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			sysText = strings.Join(parts, "\n\n")
		}
		if sysText != "" {
			zaiMessages = append(zaiMessages,
				map[string]any{"role": "user", "content": "[System Instructions]\n" + sysText},
				map[string]any{"role": "assistant", "content": "Understood. I will follow these instructions."},
			)
		}
	}

	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		content := m["content"]

		switch c := content.(type) {
		case []any:
			var parts []string
			for _, block := range c {
				if b, ok := block.(map[string]any); ok {
					switch b["type"] {
					case "text":
						if t, ok := b["text"].(string); ok {
							parts = append(parts, t)
						}
					case "image":
						parts = append(parts, "[image attached]")
					case "tool_use":
						if name, ok := b["name"].(string); ok {
							parts = append(parts, fmt.Sprintf("[tool: %s]", name))
						}
					case "tool_result":
						if t, ok := b["content"].(string); ok {
							parts = append(parts, t)
						}
					}
				}
			}
			content = strings.Join(parts, "\n")
		}

		zaiMessages = append(zaiMessages, map[string]any{
			"role":    role,
			"content": content,
		})
	}

	userContent := extractUserContent(payload)
	chatID := generateUUID()

	// Parse model suffixes: glm-5-search, glm-5-thinking, glm-5-tools
	features := map[string]bool{
		"image_generation": true,
		"web_search":       false,
		"auto_web_search":  false,
		"preview_mode":     true,
		"enable_thinking":  false,
	}
	if strings.Contains(model, "-search") {
		features["web_search"] = true
		features["auto_web_search"] = true
	}
	if strings.Contains(model, "-thinking") {
		features["enable_thinking"] = true
	}

	return map[string]any{
		"stream":           true,
		"model":            upstreamModel,
		"messages":         zaiMessages,
		"signature_prompt": userContent,
		"params":           map[string]any{},
		"features":         features,
		"chat_id":          chatID,
		"id":               chatID,
	}
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ProxyZAIWeb proxies a request through chat.z.ai's web chat API with signing.
func (p *ZAIWebProxy) ProxyZAIWeb(
	w http.ResponseWriter, r *http.Request,
	body []byte, model string,
	isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
) error {
	token, userID := p.GetToken()

	if token == "" {
		slog.Info("zaiweb: no token configured, fetching anonymous token")
		anonToken, err := p.fetchAnonymousToken()
		if err != nil {
			return fmt.Errorf("anonymous token: %w", err)
		}
		p.SetToken(anonToken)
		token = anonToken
		userID = extractUserID(anonToken)
		slog.Info("zaiweb: anonymous token acquired", "user_id", userID)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}

	zaiReq := convertToZAIWebFormat(payload, model)
	userContent := extractUserContent(payload)

	zaiBody, err := json.Marshal(zaiReq)
	if err != nil {
		return fmt.Errorf("marshal zai request: %w", err)
	}

	requestID := generateUUID()
	timestamp := time.Now().UnixMilli()
	signature := generateSignature(userID, requestID, timestamp, userContent)

	chatID, _ := zaiReq["chat_id"].(string)
	feVersion := p.GetFEVersion()

	targetURL := fmt.Sprintf(
		"https://chat.z.ai/api/v2/chat/completions?timestamp=%d&requestId=%s&user_id=%s&version=0.0.1&platform=web&token=%s&current_url=https://chat.z.ai/c/%s&pathname=/c/%s&signature_timestamp=%d",
		timestamp, requestID, userID, token, chatID, chatID, timestamp,
	)

	req, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(zaiBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-FE-Version", feVersion)
	req.Header.Set("X-Signature", signature)
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Origin", "https://chat.z.ai")
	req.Header.Set("Referer", "https://chat.z.ai/c/"+chatID)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("zaiweb request: %w", err)
	}
	defer resp.Body.Close()

	rtt := time.Since(start)
	slog.Info("zaiweb response", "status", resp.StatusCode, "model", model, "rtt_ms", rtt.Milliseconds())

	if feedback != nil {
		feedback(resp.StatusCode, rtt, resp.Header)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("zaiweb %d: %s", resp.StatusCode, string(respBody))
	}

	if !isStream {
		return p.handleNonStream(w, resp, model)
	}
	return p.handleStream(w, resp, model)
}

func (p *ZAIWebProxy) handleNonStream(w http.ResponseWriter, resp *http.Response, model string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var zaiResp map[string]any
	if err := json.Unmarshal(body, &zaiResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":            generateUUID(),
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []map[string]any{{"type": "text", "text": string(body)}},
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		})
		return nil
	}

	content := extractZAIContent(zaiResp)
	_, outputTokens := estimateZAITokens(zaiResp)

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"id":            generateUUID(),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       []map[string]any{{"type": "text", "text": content}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": outputTokens,
		},
	})
}

func (p *ZAIWebProxy) handleStream(w http.ResponseWriter, resp *http.Response, model string) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	msgID := generateUUID()

	writeSSE(w, flusher, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":          msgID,
			"type":        "message",
			"role":        "assistant",
			"model":       model,
			"content":     []any{},
			"stop_reason": nil,
		},
	})

	writeSSE(w, flusher, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	var totalContent string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}

		// Z.AI wraps [DONE] inside JSON: {"data":"[DONE]"}
		if inner, ok := chunk["data"].(string); ok && inner == "[DONE]" {
			break
		}

		// Skip non-content phases (usage, tool calls, done markers)
		if d, ok := chunk["data"].(map[string]any); ok {
			if phase, _ := d["phase"].(string); phase == "done" || phase == "other" {
				continue
			}
		}

		delta := extractZAIDelta(chunk)
		if delta == "" {
			continue
		}

		totalContent += delta

		writeSSE(w, flusher, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": delta,
			},
		})
	}

	writeSSE(w, flusher, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})

	outputTokens := len(totalContent) / 4
	if outputTokens == 0 {
		outputTokens = 1
	}

	writeSSE(w, flusher, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})

	writeSSE(w, flusher, "message_stop", map[string]any{
		"type": "message_stop",
	})

	return nil
}

func extractZAIContent(resp map[string]any) string {
	if choices, ok := resp["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					return content
				}
			}
		}
	}
	if content, ok := resp["content"].(string); ok {
		return content
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func extractZAIDelta(chunk map[string]any) string {
	// Z.AI web format: {"type":"chat:completion","data":{"delta_content":"text","phase":"answer"}}
	if d, ok := chunk["data"].(map[string]any); ok {
		if dc, ok := d["delta_content"].(string); ok && dc != "" {
			return dc
		}
	}
	// OpenAI-compatible format: {"choices":[{"delta":{"content":"text"}}]}
	if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if delta, ok := choice["delta"].(map[string]any); ok {
				if content, ok := delta["content"].(string); ok {
					return content
				}
			}
		}
	}
	if delta, ok := chunk["delta"].(map[string]any); ok {
		if content, ok := delta["content"].(string); ok {
			return content
		}
	}
	if content, ok := chunk["content"].(string); ok {
		return content
	}
	return ""
}

func estimateZAITokens(resp map[string]any) (int, int) {
	content := extractZAIContent(resp)
	outputTokens := len(content) / 4
	if outputTokens == 0 {
		outputTokens = 1
	}
	return 0, outputTokens
}

// ProxyImageGeneration forwards to image.z.ai/api/proxy/images/generate.
// Uses JWT Bearer token; returns JSON with image URL.
func (p *ZAIWebProxy) ProxyImageGeneration(w http.ResponseWriter, r *http.Request, body []byte) error {
	token, _ := p.GetToken()
	if token == "" {
		anonToken, err := p.fetchAnonymousToken()
		if err != nil {
			return fmt.Errorf("anonymous token for image: %w", err)
		}
		p.SetToken(anonToken)
		token = anonToken
	}

	reqID := generateUUID()

	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://image.z.ai/api/proxy/images/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create image request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", reqID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://chat.z.ai")
	req.Header.Set("Referer", "https://chat.z.ai/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("image.z.ai request: %w", err)
	}
	defer resp.Body.Close()

	rtt := time.Since(start)
	slog.Info("image.z.ai response", "status", resp.StatusCode, "rtt_ms", rtt.Milliseconds())

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return fmt.Errorf("read image response: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	return nil
}

// ProxyAudioTTS forwards to audio.z.ai/api/v1/z-audio/tts/create.
// Uses JWT Bearer token; returns SSE audio stream.
func (p *ZAIWebProxy) ProxyAudioTTS(w http.ResponseWriter, r *http.Request, body []byte) error {
	token, userID := p.GetToken()

	if token == "" {
		anonToken, err := p.fetchAnonymousToken()
		if err != nil {
			return fmt.Errorf("anonymous token for audio: %w", err)
		}
		p.SetToken(anonToken)
		token = anonToken
		userID = extractUserID(anonToken)
	}

	// Inject user_id into request body if missing.
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse audio payload: %w", err)
	}
	if _, ok := payload["user_id"]; !ok {
		payload["user_id"] = userID
	}
	updatedBody, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://audio.z.ai/api/v1/z-audio/tts/create", bytes.NewReader(updatedBody))
	if err != nil {
		return fmt.Errorf("create audio request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Origin", "https://audio.z.ai")
	req.Header.Set("Referer", "https://audio.z.ai/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("audio.z.ai request: %w", err)
	}
	defer resp.Body.Close()

	rtt := time.Since(start)
	slog.Info("audio.z.ai response", "status", resp.StatusCode, "rtt_ms", rtt.Milliseconds())

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("audio.z.ai %d: %s", resp.StatusCode, string(respBody))
	}

	// Stream SSE response directly to client.
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)
		if strings.HasPrefix(line, "data: ") {
			flusher.Flush()
		}
	}

	return nil
}
