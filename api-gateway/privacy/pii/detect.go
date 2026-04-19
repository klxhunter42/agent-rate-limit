package pii

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

type PresidioClient struct {
	url            string
	scoreThreshold float64
	entities       []string
	language       string
	httpClient     *http.Client
}

type presidioRequest struct {
	Text           string   `json:"text"`
	Language       string   `json:"language"`
	Entities       []string `json:"entities"`
	ScoreThreshold float64  `json:"score_threshold"`
	ReturnDecision bool     `json:"return_decision"`
}

type presidioResponse []struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

type DetectResult struct {
	Entities []masking.PIIEntity
	HasPII   bool
	ScanMs   int64
}

func NewPresidioClient(url string, scoreThreshold float64, entities []string, language string) *PresidioClient {
	return &PresidioClient{
		url:            url,
		scoreThreshold: scoreThreshold,
		entities:       entities,
		language:       language,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *PresidioClient) Detect(text string) DetectResult {
	if text == "" {
		return DetectResult{}
	}

	reqBody := presidioRequest{
		Text:           text,
		Language:       c.language,
		Entities:       c.entities,
		ScoreThreshold: c.scoreThreshold,
		ReturnDecision: false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return DetectResult{}
	}

	start := time.Now()
	resp, err := c.httpClient.Post(c.url+"/analyze", "application/json", bytes.NewReader(data))
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return DetectResult{ScanMs: elapsed}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return DetectResult{ScanMs: elapsed}
	}

	var result presidioResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return DetectResult{ScanMs: elapsed}
	}

	entities := make([]masking.PIIEntity, 0, len(result))
	for _, e := range result {
		if e.Start < 0 || e.End <= e.Start {
			continue
		}
		entities = append(entities, masking.PIIEntity{
			EntityType: e.EntityType,
			Start:      e.Start,
			End:        e.End,
			Score:      e.Score,
		})
	}

	return DetectResult{
		Entities: entities,
		HasPII:   len(entities) > 0,
		ScanMs:   elapsed,
	}
}

func (c *PresidioClient) HealthCheck() bool {
	resp, err := c.httpClient.Get(c.url + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *PresidioClient) URL() string {
	return c.url
}

func (c *PresidioClient) fmt() string {
	return fmt.Sprintf("PresidioClient{url=%s, lang=%s, entities=%v}", c.url, c.language, c.entities)
}
