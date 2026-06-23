package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/redis/go-redis/v9"
)

type MCPServerConfig struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Status   string `json:"status"`
}

var MCPServers = map[string]MCPServerConfig{
	"web_reader": {
		Name:     "Web Reader",
		Endpoint: "https://api.z.ai/api/mcp/web_reader/mcp",
		Status:   "working",
	},
	"web_search_prime": {
		Name:     "Web Search Prime",
		Endpoint: "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Status:   "working",
	},
	"zread": {
		Name:     "Zread",
		Endpoint: "https://api.z.ai/api/mcp/zread/mcp",
		Status:   "working",
	},
}

type MCPProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics
	keyPool *KeyPool
	rdb     *redis.Client
}

func NewMCPProxy(cfg *config.Config, m *metrics.Metrics, kp *KeyPool, rdb *redis.Client) *MCPProxy {
	return &MCPProxy{
		cfg:     cfg,
		client:  SharedClient(0),
		metrics: m,
		keyPool: kp,
		rdb:     rdb,
	}
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (p *MCPProxy) ProxyMCP(w http.ResponseWriter, r *http.Request, serverName string) {
	start := time.Now()

	srv, ok := MCPServers[serverName]
	if !ok {
		writeMCPError(w, nil, -32601, fmt.Sprintf("unknown MCP server: %s", serverName))
		return
	}

	if p.keyPool == nil {
		writeMCPError(w, nil, -32000, "no available API key")
		return
	}

	apiKey, ok := p.keyPool.Acquire()
	if !ok {
		writeMCPError(w, nil, -32000, "no available API key")
		return
	}

	// When key pool is empty (passthrough mode), fall back to client's own key.
	if apiKey == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if apiKey == "" {
		writeMCPError(w, nil, -32000, "no available API key")
		return
	}

	currentKey := apiKey
	defer p.keyPool.ReportSuccess(currentKey)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeMCPError(w, nil, -32700, "failed to read request body")
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeMCPError(w, nil, -32700, "invalid JSON-RPC")
		return
	}

	toolName := extractToolName(req.Method, req.Params)

	accountID := extractAccountID(r)

	if !p.checkRateLimit(r.Context(), accountID) {
		p.recordMetrics(serverName, toolName, "rate_limited", start)
		writeMCPError(w, req.ID, -32000, "MCP rate limit exceeded")
		return
	}

	if toolName != "" && req.Method == "tools/call" {
		cached, hit := p.getCache(r.Context(), serverName, toolName, req.Params)
		if hit {
			p.recordMetrics(serverName, toolName, "cache_hit", start)
			p.metrics.MCPCacheHits.WithLabelValues(serverName, toolName).Inc()
			w.Header().Set("Content-Type", "application/json")
			w.Write(cached)
			return
		}
		p.metrics.MCPCacheMisses.WithLabelValues(serverName, toolName).Inc()
	}

	respBody, status, err := p.doUpstream(r.Context(), srv.Endpoint, currentKey, body)
	if err != nil {
		p.recordMetrics(serverName, toolName, "error", start)
		p.keyPool.Report429(apiKey)

		for attempt := 1; attempt <= p.cfg.MCPMaxRetries; attempt++ {
			backoff := jitteredBackoff(500*time.Millisecond, attempt, p.cfg.RetryBackoffJitter)
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			select {
			case <-r.Context().Done():
				writeMCPError(w, req.ID, -32000, "request cancelled during retry")
				return
			case <-time.After(backoff):
			}

			retryKey, rok := p.keyPool.Acquire()
			if !rok {
				break
			}
			respBody, status, err = p.doUpstream(r.Context(), srv.Endpoint, retryKey, body)
			if err == nil {
				p.keyPool.ReportSuccess(retryKey)
				currentKey = retryKey
				break
			}
			p.keyPool.Report429(retryKey)
		}
		if err != nil {
			writeMCPError(w, req.ID, -32000, fmt.Sprintf("upstream error after retries: %v", err))
			return
		}
	}

	if toolName != "" && req.Method == "tools/call" && status == 200 {
		p.storeCache(r.Context(), serverName, toolName, req.Params, respBody)
	}

	p.recordMetrics(serverName, toolName, fmt.Sprintf("%d", status), start)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBody)
}

func (p *MCPProxy) doUpstream(ctx context.Context, endpoint, apiKey string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}

	if len(respBody) == 0 {
		return nil, resp.StatusCode, fmt.Errorf("upstream returned empty response (status %d)", resp.StatusCode)
	}

	// Handle MCP Streamable HTTP SSE response (text/event-stream).
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		extracted, err := extractSSEJSONRPC(respBody)
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("SSE parse: %w (body: %s)", err, truncateBody(respBody, 200))
		}
		return extracted, resp.StatusCode, nil
	}

	if !json.Valid(respBody) {
		return nil, resp.StatusCode, fmt.Errorf("upstream returned invalid JSON (status %d, body: %s)", resp.StatusCode, truncateBody(respBody, 200))
	}

	return respBody, resp.StatusCode, nil
}

// extractSSEJSONRPC parses an SSE body and returns the final JSON-RPC response.
// SSE events are "data: <json>\n\n". The last event with an "id" field is the result.
func extractSSEJSONRPC(sseBody []byte) ([]byte, error) {
	var lastData []byte
	scanner := bufio.NewScanner(bytes.NewReader(sseBody))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lastData = []byte(strings.TrimPrefix(line, "data: "))
		} else if strings.HasPrefix(line, "data:") {
			lastData = []byte(strings.TrimPrefix(line, "data:"))
		}
	}

	if len(lastData) == 0 {
		return nil, fmt.Errorf("no data events found in SSE stream")
	}

	if !json.Valid(lastData) {
		return nil, fmt.Errorf("SSE data is not valid JSON: %s", truncateBody(lastData, 200))
	}

	// Verify it's a JSON-RPC response (has "id" field).
	var peek struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(lastData, &peek); err != nil {
		return nil, fmt.Errorf("SSE data JSON decode: %w", err)
	}
	if peek.ID == nil {
		slog.Warn("SSE final event has no id, returning anyway", "data", truncateBody(lastData, 100))
	}

	return lastData, nil
}

func (p *MCPProxy) checkRateLimit(ctx context.Context, accountID string) bool {
	if p.rdb == nil || accountID == "" {
		return true
	}

	key := fmt.Sprintf("arl:mcp:ratelimit:%s", accountID)
	window := time.Now().Truncate(time.Minute).Unix()

	count, err := p.rdb.Incr(ctx, fmt.Sprintf("%s:%d", key, window)).Result()
	if err != nil {
		return true
	}
	p.rdb.Expire(ctx, fmt.Sprintf("%s:%d", key, window), 2*time.Minute)

	p.metrics.MCPQuotaUsage.WithLabelValues(accountID).Set(float64(count))

	return int(count) <= p.cfg.MCPRateLimitPerMin
}

func (p *MCPProxy) getCache(ctx context.Context, server, tool string, params any) ([]byte, bool) {
	if p.rdb == nil {
		return nil, false
	}
	key := p.cacheKey(server, tool, params)
	val, err := p.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (p *MCPProxy) storeCache(ctx context.Context, server, tool string, params any, resp []byte) {
	if p.rdb == nil {
		return
	}
	key := p.cacheKey(server, tool, params)
	p.rdb.Set(ctx, key, resp, p.cfg.MCPCacheTTL)
}

func (p *MCPProxy) cacheKey(server, tool string, params any) string {
	raw, _ := json.Marshal(params)
	h := sha256.Sum256(raw)
	return fmt.Sprintf("arl:mcp:cache:%s:%s:%s", server, tool, hex.EncodeToString(h[:]))
}

func (p *MCPProxy) recordMetrics(server, tool, status string, start time.Time) {
	if tool == "" {
		tool = "_unknown"
	}
	p.metrics.MCPCallsTotal.WithLabelValues(server, tool, status).Inc()
	p.metrics.MCPCallDuration.WithLabelValues(server, tool).Observe(time.Since(start).Seconds())
}

func extractToolName(method string, params any) string {
	if method != "tools/call" {
		return ""
	}
	m, ok := params.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	return name
}

func extractAccountID(r *http.Request) string {
	if v := r.Header.Get("X-Account-ID"); v != "" {
		return v
	}
	if auth := r.Header.Get("Authorization"); len(auth) > 20 {
		h := sha256.Sum256([]byte(auth))
		return hex.EncodeToString(h[:8])
	}
	return "anonymous"
}

func writeMCPError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: message},
	})
}

func truncateBody(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}

func (p *MCPProxy) ListServers() []MCPServerConfig {
	servers := make([]MCPServerConfig, 0, len(MCPServers))
	for k, v := range MCPServers {
		servers = append(servers, MCPServerConfig{
			Name:     v.Name,
			Endpoint: fmt.Sprintf("/mcp/%s", k),
			Status:   v.Status,
		})
	}
	return servers
}
