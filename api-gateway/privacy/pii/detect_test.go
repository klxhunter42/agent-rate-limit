package pii

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

func TestPresidioClient_Detect(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		c := NewPresidioClient("http://localhost:5002", 0.7, []string{"PERSON"}, "en")
		result := c.Detect("")
		assert.False(t, result.HasPII)
	})

	t.Run("successful detection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/analyze", r.URL.Path)
			assert.Equal(t, "POST", r.Method)

			var req presidioRequest
			json.NewDecoder(r.Body).Decode(&req)
			assert.Equal(t, "John Smith lives here", req.Text)

			resp := presidioResponse{
				{EntityType: "PERSON", Start: 0, End: 10, Score: 0.85},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		c := NewPresidioClient(server.URL, 0.7, []string{"PERSON"}, "en")
		result := c.Detect("John Smith lives here")
		assert.True(t, result.HasPII)
		assert.Len(t, result.Entities, 1)
		assert.Equal(t, "PERSON", result.Entities[0].EntityType)
		assert.Equal(t, 0, result.Entities[0].Start)
		assert.Equal(t, 10, result.Entities[0].End)
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		c := NewPresidioClient(server.URL, 0.7, []string{"PERSON"}, "en")
		result := c.Detect("some text")
		assert.False(t, result.HasPII)
	})

	t.Run("connection refused", func(t *testing.T) {
		c := NewPresidioClient("http://localhost:1", 0.7, []string{"PERSON"}, "en")
		result := c.Detect("some text")
		assert.False(t, result.HasPII)
	})

	t.Run("invalid entity filtered", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := presidioResponse{
				{EntityType: "PERSON", Start: -1, End: 5, Score: 0.9},
				{EntityType: "EMAIL", Start: 10, End: 5, Score: 0.8},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		c := NewPresidioClient(server.URL, 0.7, []string{"PERSON", "EMAIL"}, "en")
		result := c.Detect("some text")
		assert.False(t, result.HasPII)
		assert.Len(t, result.Entities, 0)
	})
}

func TestPresidioClient_HealthCheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/health", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		c := NewPresidioClient(server.URL, 0.7, nil, "en")
		assert.True(t, c.HealthCheck())
	})

	t.Run("unhealthy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		c := NewPresidioClient(server.URL, 0.7, nil, "en")
		assert.False(t, c.HealthCheck())
	})
}

func TestMaskPII(t *testing.T) {
	t.Run("no entities", func(t *testing.T) {
		result := MaskPII("hello", nil, nil)
		assert.Equal(t, "hello", result.MaskedText)
	})

	t.Run("single entity", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		entities := []masking.PIIEntity{
			{EntityType: "PERSON", Start: 0, End: 10, Score: 0.9},
		}
		result := MaskPII("John Smith is here", entities, ctx)
		assert.Contains(t, result.MaskedText, "[[PERSON_1]]")
		assert.Equal(t, "John Smith", ctx.Mapping["[[PERSON_1]]"])
	})

	t.Run("dedup same entity", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		entities := []masking.PIIEntity{
			{EntityType: "PERSON", Start: 0, End: 10, Score: 0.9},
			{EntityType: "PERSON", Start: 14, End: 24, Score: 0.85},
		}
		text := "John Smith xx John Smith!!"
		result := MaskPII(text, entities, ctx)
		assert.Contains(t, result.MaskedText, "[[PERSON_1]]")
	})
}
