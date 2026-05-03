package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

func newTestMCPHandler(t *testing.T) *Handler {
	t.Helper()
	m := metrics.New(func() float64 { return 0 }, nil)
	cfg := &config.Config{
		MCPEnabled:         true,
		MCPCacheTTL:        time.Hour,
		MCPMaxRetries:      0,
		MCPRateLimitPerMin: 30,
	}
	kp := proxy.NewKeyPool([]string{"test-key"}, 60)
	mcpProxy := proxy.NewMCPProxy(cfg, m, kp, nil)

	return &Handler{
		metrics:  m,
		cfg:      cfg,
		keyPool:  kp,
		mcpProxy: mcpProxy,
	}
}

func TestMCPProxyHandle_MissingServer(t *testing.T) {
	h := newTestMCPHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/", strings.NewReader(`{}`))

	// Direct call with empty server name
	h.MCPProxyHandle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMCPProxyHandle_NilProxy(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(`{}`))
	h.MCPProxyHandle(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestMCPListServers_NilProxy(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	h.MCPListServers(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestMCPListServers_Success(t *testing.T) {
	h := newTestMCPHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	h.MCPListServers(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	servers, ok := resp["servers"].([]any)
	if !ok {
		t.Fatal("servers field missing or wrong type")
	}
	if len(servers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(servers))
	}
}

func TestMCPProxyHandle_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("expected Bearer auth, got: %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"content": "ok"},
		})
	}))
	defer ts.Close()

	// Override server endpoint
	proxy.MCPServers["web_reader"] = proxy.MCPServerConfig{
		Name: "Web Reader", Endpoint: ts.URL, Status: "working",
	}
	defer func() {
		proxy.MCPServers["web_reader"] = proxy.MCPServerConfig{
			Name: "Web Reader", Endpoint: "https://api.z.ai/api/mcp/web_reader/mcp", Status: "working",
		}
	}()

	h := newTestMCPHandler(t)

	r := chi.NewRouter()
	r.Post("/mcp/{server}", h.MCPProxyHandle)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"webReader","arguments":{"url":"https://example.com"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/web_reader", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
}
