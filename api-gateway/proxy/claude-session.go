package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
)

// ClaudeSessionProxy proxies requests through claude.ai's web API using browser session cookies.
type ClaudeSessionProxy struct {
	client  *http.Client
	metrics *metrics.Metrics
}

func NewClaudeSessionProxy(m *metrics.Metrics) *ClaudeSessionProxy {
	return &ClaudeSessionProxy{
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

type claudeOrg struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// getOrgID fetches the first organization UUID from claude.ai.
func (p *ClaudeSessionProxy) getOrgID(ctx context.Context, cookie string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://claude.ai/api/organizations", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get orgs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("get orgs returned %d: %s", resp.StatusCode, string(body))
	}

	var orgs []claudeOrg
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return "", fmt.Errorf("decode orgs: %w", err)
	}
	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found")
	}
	return orgs[0].UUID, nil
}

// createConversation creates a new chat conversation on claude.ai.
func (p *ClaudeSessionProxy) createConversation(ctx context.Context, cookie, orgID string) (string, error) {
	chatUUID := uuid.New().String()
	payload := map[string]string{"name": "", "uuid": chatUUID}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://claude.ai/api/organizations/%s/chat_conversations", orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setBrowserHeaders(req, cookie)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("create conversation returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode conversation: %w", err)
	}
	chatID, _ := result["uuid"].(string)
	if chatID == "" {
		return "", fmt.Errorf("no uuid in conversation response")
	}
	return chatID, nil
}

// deleteConversation removes a conversation from claude.ai.
func (p *ClaudeSessionProxy) deleteConversation(ctx context.Context, cookie, orgID, chatID string) {
	url := fmt.Sprintf("https://claude.ai/api/organizations/%s/chat_conversations/%s", orgID, chatID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Cookie", cookie)

	resp, err := p.client.Do(req)
	if err != nil {
		slog.Warn("failed to delete claude conversation", "chat_id", chatID, "error", err)
		return
	}
	resp.Body.Close()
}

// extractPrompt converts an Anthropic Messages API payload to a single prompt string for claude.ai.
func extractPrompt(payload map[string]any) string {
	msgs, _ := payload["messages"].([]any)
	var parts []string
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		content := m["content"]

		switch v := content.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s: %s", role, v))
		case []any:
			for _, block := range v {
				cb, ok := block.(map[string]any)
				if !ok {
					continue
				}
				t, _ := cb["type"].(string)
				if t == "text" {
					text, _ := cb["text"].(string)
					parts = append(parts, fmt.Sprintf("%s: %s", role, text))
				}
			}
		}
	}

	// Prepend system prompt if present.
	if sys, ok := payload["system"]; ok {
		switch v := sys.(type) {
		case string:
			parts = append([]string{fmt.Sprintf("system: %s", v)}, parts...)
		case []any:
			var sysTexts []string
			for _, s := range v {
				if sm, ok := s.(map[string]any); ok {
					if t, _ := sm["text"].(string); t != "" {
						sysTexts = append(sysTexts, t)
					}
				}
			}
			if len(sysTexts) > 0 {
				parts = append([]string{fmt.Sprintf("system: %s", strings.Join(sysTexts, "\n"))}, parts...)
			}
		}
	}

	// Return only the last user message for claude.ai (it's a single-turn completion).
	// Find the last assistant+user pair if multi-turn.
	lastUserIdx := -1
	for i, part := range parts {
		if strings.HasPrefix(part, "user: ") {
			lastUserIdx = i
		}
	}
	if lastUserIdx >= 0 {
		// Include context from previous messages if present.
		start := 0
		if lastUserIdx > 3 {
			start = lastUserIdx - 3 // include last few messages for context
		}
		return strings.Join(parts[start:], "\n\n")
	}

	return strings.Join(parts, "\n\n")
}

// extractModel maps Anthropic model names to claude.ai model identifiers.
func extractModel(payload map[string]any) string {
	model, _ := payload["model"].(string)
	// claude.ai uses the same model identifiers, pass through.
	if model == "" {
		return ""
	}
	return model
}

// ProxySession proxies a request through claude.ai's web API using session cookies.
func (p *ClaudeSessionProxy) ProxySession(w http.ResponseWriter, r *http.Request, cookie string, body []byte, model string, isStream bool, feedback FeedbackFunc) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}

	// Get org ID.
	orgID, err := p.getOrgID(r.Context(), cookie)
	if err != nil {
		return fmt.Errorf("get org: %w", err)
	}

	// Create conversation.
	chatID, err := p.createConversation(r.Context(), cookie, orgID)
	if err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.deleteConversation(cleanupCtx, cookie, orgID, chatID)
	}()

	// Build completion request.
	prompt := extractPrompt(payload)
	claudeModel := extractModel(payload)
	completionPayload := map[string]any{
		"prompt":      prompt,
		"timezone":    "UTC",
		"attachments": []any{},
		"files":       []any{},
	}
	if claudeModel != "" {
		completionPayload["model"] = claudeModel
	}

	completionBody, _ := json.Marshal(completionPayload)
	completionURL := fmt.Sprintf("https://claude.ai/api/organizations/%s/chat_conversations/%s/completion", orgID, chatID)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, completionURL, bytes.NewReader(completionBody))
	if err != nil {
		return fmt.Errorf("create completion request: %w", err)
	}
	setBrowserHeaders(req, cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Referer", fmt.Sprintf("https://claude.ai/chat/%s", chatID))
	req.Header.Set("Origin", "https://claude.ai")

	start := time.Now()
	resp, err := p.client.Do(req)
	rtt := time.Since(start)
	if err != nil {
		return fmt.Errorf("completion request: %w", err)
	}
	defer resp.Body.Close()

	if feedback != nil {
		feedback(resp.StatusCode, rtt, resp.Header)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == 429 {
			p.metrics.Inc429()
		}
		return fmt.Errorf("completion returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Convert claude.ai SSE to Anthropic Messages API format.
	msgID := "msg_" + chatID
	return p.convertSessionSSE(w, resp, msgID, model)
}

// convertSessionSSE reads claude.ai SSE and converts to Anthropic Messages API SSE format.
func (p *ClaudeSessionProxy) convertSessionSSE(w http.ResponseWriter, resp *http.Response, msgID, model string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	const maxSSELineSize = 256 * 1024
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	var inputTokens, outputTokens int

	// Send message_start.
	startEvent := fmt.Sprintf(`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","content":[],"model":"%s","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		msgID, model)
	fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startEvent)

	// Send content_block_start.
	fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")

	if flusher != nil {
		flusher.Flush()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Check for error.
		if errObj, ok := chunk["error"].(map[string]any); ok {
			slog.Warn("claude.ai stream error", "error", errObj)
			break
		}

		completion, _ := chunk["completion"].(string)
		if completion == "" {
			// Check for stop_reason or other signals.
			if _, hasStop := chunk["stop_reason"]; hasStop {
				break
			}
			continue
		}

		outputTokens += len(completion) / 4 // rough estimate

		escaped, _ := json.Marshal(completion)
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Close events.
	fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if flusher != nil {
		flusher.Flush()
	}

	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(model, inputTokens, outputTokens)
	}

	return nil
}

func setBrowserHeaders(req *http.Request, cookie string) {
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("DNT", "1")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("TE", "trailers")
}
