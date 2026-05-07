package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
)

func newTestMCPProxy(upstream http.HandlerFunc) (*MCPProxy, *httptest.Server) {
	ts := httptest.NewServer(upstream)

	m := metrics.New(func() float64 { return 0 }, nil)
	cfg := &config.Config{
		MCPEnabled:         true,
		MCPCacheTTL:        1 * time.Hour,
		MCPMaxRetries:      1,
		MCPRateLimitPerMin: 30,
	}
	kp := NewKeyPool([]string{"test-key-1", "test-key-2"}, 60)

	proxy := NewMCPProxy(cfg, m, kp, nil)

	// Override server endpoints to point at test server.
	for k := range MCPServers {
		MCPServers[k] = MCPServerConfig{
			Name:     MCPServers[k].Name,
			Endpoint: ts.URL,
			Status:   "working",
		}
	}

	return proxy, ts
}

func TestMCPProxy_UnknownServer(t *testing.T) {
	proxy, ts := newTestMCPProxy(nil)
	defer ts.Close()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/nonexistent", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))

	proxy.ProxyMCP(w, r, "nonexistent")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestMCPProxy_SuccessfulToolsCall(t *testing.T) {
	upstreamResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "page content here"},
			},
		},
	}

	proxy, ts := newTestMCPProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("expected Bearer auth, got %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(upstreamResp)
	}))
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))

	proxy.ProxyMCP(w, r, "web_reader")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestMCPProxy_CacheHit(t *testing.T) {
	calls := 0
	upstreamResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]any{"content": "cached data"},
	}

	proxy, ts := newTestMCPProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(upstreamResp)
	}))
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`

	// First call - cache miss
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))
	proxy.ProxyMCP(w1, r1, "web_reader")

	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}

	// Second call - should hit cache (but rdb is nil so no cache)
	// Without Redis, cache is skipped. This tests the no-Redis path.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))
	proxy.ProxyMCP(w2, r2, "web_reader")

	// Without Redis, both calls go to upstream
	if calls != 2 {
		t.Fatalf("expected 2 upstream calls (no cache), got %d", calls)
	}
}

func TestMCPProxy_InvalidJSONRPC(t *testing.T) {
	proxy, ts := newTestMCPProxy(nil)
	defer ts.Close()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(`not json`))

	proxy.ProxyMCP(w, r, "web_reader")

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error -32700, got %v", resp.Error)
	}
}

func TestMCPProxy_RetryOnUpstreamError(t *testing.T) {
	cfg := &config.Config{
		MCPEnabled:         true,
		MCPCacheTTL:        1 * time.Hour,
		MCPMaxRetries:      0, // no retries for faster test
		MCPRateLimitPerMin: 30,
	}
	m := metrics.New(func() float64 { return 0 }, nil)
	kp := NewKeyPool([]string{"key1"}, 60)
	proxy := NewMCPProxy(cfg, m, kp, nil)

	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()

	// Override endpoint
	MCPServers["web_reader"] = MCPServerConfig{
		Name: "Web Reader", Endpoint: ts.URL, Status: "working",
	}
	defer func() {
		MCPServers["web_reader"] = MCPServerConfig{
			Name: "Web Reader", Endpoint: "https://api.z.ai/api/mcp/web_reader/mcp", Status: "working",
		}
	}()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))

	proxy.ProxyMCP(w, r, "web_reader")

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error after upstream failure")
	}
	if !strings.Contains(resp.Error.Message, "upstream error") {
		t.Fatalf("expected upstream error message, got: %s", resp.Error.Message)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retries), got %d", calls)
	}
}

func TestMCPProxy_SSEResponse(t *testing.T) {
	jsonResult := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello world"}]}}`
	sseBody := "event: message\ndata: " + jsonResult + "\n\n"

	proxy, ts := newTestMCPProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); !strings.Contains(accept, "text/event-stream") {
			t.Errorf("expected Accept header with text/event-stream, got %s", accept)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody))
	}))
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))

	proxy.ProxyMCP(w, r, "web_reader")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v, body: %s", err, w.Body.String())
	}
	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
	// Verify Content-Type is application/json (gateway normalizes SSE to JSON).
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %s", ct)
	}
}

func TestMCPProxy_SSEMultipleEvents(t *testing.T) {
	progressEvent := `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"abc","progress":50}}`
	resultEvent := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"page content"}]}}`
	sseBody := "event: message\ndata: " + progressEvent + "\n\nevent: message\ndata: " + resultEvent + "\n\n"

	proxy, ts := newTestMCPProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody))
	}))
	defer ts.Close()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))

	proxy.ProxyMCP(w, r, "web_reader")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %s, body: %s", err.Error(), w.Body.String())
	}
	if resp["error"] != nil {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestExtractSSEJSONRPC(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			"single event",
			"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
			false,
		},
		{
			"multiple events",
			"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
			false,
		},
		{
			"event with prefix",
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
			false,
		},
		{
			"empty body",
			"",
			true,
		},
		{
			"no data lines",
			"event: message\n\n",
			true,
		},
		{
			"invalid JSON in data",
			"data: not-json\n\n",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSSEJSONRPC([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("extractSSEJSONRPC() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !json.Valid(got) {
				t.Errorf("extractSSEJSONRPC() returned invalid JSON: %s", got)
			}
		})
	}
}

func TestExtractToolName(t *testing.T) {
	tests := []struct {
		method string
		params any
		want   string
	}{
		{"tools/call", map[string]any{"name": "webReader"}, "webReader"},
		{"tools/call", map[string]any{"name": "search_doc"}, "search_doc"},
		{"tools/list", nil, ""},
		{"initialize", nil, ""},
		{"tools/call", "not a map", ""},
		{"tools/call", map[string]any{}, ""},
	}

	for _, tt := range tests {
		got := extractToolName(tt.method, tt.params)
		if got != tt.want {
			t.Errorf("extractToolName(%q, %v) = %q, want %q", tt.method, tt.params, got, tt.want)
		}
	}
}

func TestCacheKey(t *testing.T) {
	cfg := &config.Config{MCPCacheTTL: time.Hour}
	m := metrics.New(func() float64 { return 0 }, nil)
	proxy := NewMCPProxy(cfg, m, nil, nil)

	key1 := proxy.cacheKey("web_reader", "webReader", map[string]any{"url": "https://a.com"})
	key2 := proxy.cacheKey("web_reader", "webReader", map[string]any{"url": "https://b.com"})
	key3 := proxy.cacheKey("web_reader", "webReader", map[string]any{"url": "https://a.com"})

	if key1 == key2 {
		t.Error("different params should produce different keys")
	}
	if key1 != key3 {
		t.Error("same params should produce same key")
	}
	if !strings.HasPrefix(key1, "arl:mcp:cache:web_reader:webReader:") {
		t.Errorf("key should have correct prefix, got: %s", key1)
	}
}

func TestListServers(t *testing.T) {
	cfg := &config.Config{MCPCacheTTL: time.Hour}
	m := metrics.New(func() float64 { return 0 }, nil)
	proxy := NewMCPProxy(cfg, m, nil, nil)

	servers := proxy.ListServers()
	if len(servers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(servers))
	}

	found := map[string]bool{}
	for _, s := range servers {
		found[s.Name] = true
		if !strings.HasPrefix(s.Endpoint, "/mcp/") {
			t.Errorf("endpoint should start with /mcp/, got: %s", s.Endpoint)
		}
	}
	for _, name := range []string{"Web Reader", "Web Search Prime", "Zread"} {
		if !found[name] {
			t.Errorf("missing server: %s", name)
		}
	}
}
