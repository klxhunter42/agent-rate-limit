package privacy

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/extractors"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/pii"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/secrets"
)

type Config struct {
	Enabled           bool
	SecretsEnabled    bool
	MaxScanChars      int
	SecretEntities    []string
	PIIEnabled        bool
	PresidioURL       string
	PIIScoreThreshold float64
	PIIEntities       []string
	PIILanguage       string
}

type Pipeline struct {
	cfg            *Config
	secretDetector *secrets.SecretDetector
	presidioClient *pii.PresidioClient
	metrics        *Metrics
}

type MaskResult struct {
	MaskedBody []byte
	SecretsCtx *masking.MaskContext
	PIICtx     *masking.MaskContext
	HasSecrets bool
	HasPII     bool
}

func NewPipeline(cfg *Config, m *Metrics) *Pipeline {
	p := &Pipeline{cfg: cfg, metrics: m}

	if cfg.SecretsEnabled {
		if len(cfg.SecretEntities) > 0 {
			p.secretDetector = secrets.NewDetector(cfg.SecretEntities, cfg.MaxScanChars)
		} else {
			p.secretDetector = secrets.DefaultDetector()
		}
	}

	if cfg.PIIEnabled {
		p.presidioClient = pii.NewPresidioClient(
			cfg.PresidioURL,
			cfg.PIIScoreThreshold,
			cfg.PIIEntities,
			cfg.PIILanguage,
		)
		if p.presidioClient.HealthCheck() {
			slog.Info("presidio PII analyzer connected", "url", cfg.PresidioURL)
		} else {
			slog.Warn("presidio PII analyzer unreachable, PII detection disabled", "url", cfg.PresidioURL)
			p.presidioClient = nil
		}
	}

	return p
}

func (p *Pipeline) MaskRequest(body []byte) (*MaskResult, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	spans := extractors.ExtractTextSpans(payload)
	if len(spans) == 0 {
		return nil, nil
	}

	result := &MaskResult{}
	secretsCtx := masking.NewMaskContext()
	piiCtx := masking.NewMaskContext()
	totalSecrets := 0
	totalPII := 0

	for _, span := range spans {
		text := span.Text

		// Detect and mask secrets.
		if p.secretDetector != nil && text != "" {
			start := time.Now()
			det := p.secretDetector.Detect(text)
			elapsed := time.Since(start)
			if p.metrics != nil {
				p.metrics.ObserveMaskDuration("secrets_detect", elapsed)
			}

			if det.Detected {
				totalSecrets += len(det.Matches)
				start = time.Now()
				maskRes := secrets.MaskSecrets(text, det.Locations, secretsCtx)
				elapsed = time.Since(start)
				if p.metrics != nil {
					p.metrics.ObserveMaskDuration("mask", elapsed)
				}
				text = maskRes.MaskedText
			}

			for _, m := range det.Matches {
				if p.metrics != nil {
					p.metrics.IncSecretsDetected(string(m.Type), m.Count)
				}
			}
		}

		// Detect and mask PII on secrets-masked text.
		if p.presidioClient != nil && text != "" {
			start := time.Now()
			piiResult := p.presidioClient.Detect(text)
			elapsed := time.Since(start)
			if p.metrics != nil {
				p.metrics.ObserveMaskDuration("pii_detect", elapsed)
			}

			if piiResult.HasPII {
				totalPII += len(piiResult.Entities)
				start = time.Now()
				maskRes := pii.MaskPII(text, piiResult.Entities, piiCtx)
				elapsed = time.Since(start)
				if p.metrics != nil {
					p.metrics.ObserveMaskDuration("mask", elapsed)
				}
				text = maskRes.MaskedText
			}

			for _, e := range piiResult.Entities {
				if p.metrics != nil {
					p.metrics.IncPIIDetected(e.EntityType)
				}
			}
		}

		// Apply masked text back to payload.
		if text != span.Text {
			applyMaskedToPayload(payload, span, text)
		}
	}

	if totalSecrets == 0 && totalPII == 0 {
		return nil, nil
	}

	result.HasSecrets = totalSecrets > 0
	result.HasPII = totalPII > 0
	result.SecretsCtx = secretsCtx
	result.PIICtx = piiCtx

	maskedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result.MaskedBody = maskedBody

	if p.metrics != nil {
		p.metrics.IncMaskRequests(result.HasSecrets, result.HasPII)
	}

	return result, nil
}

func (p *Pipeline) UnmaskResponse(body []byte, result *MaskResult) []byte {
	if result == nil || (!result.HasSecrets && !result.HasPII) {
		return body
	}

	text := string(body)

	// Unmask PII first (longest placeholders first).
	if result.HasPII && result.PIICtx != nil {
		start := time.Now()
		text = result.PIICtx.RestorePlaceholders(text)
		if p.metrics != nil {
			p.metrics.ObserveMaskDuration("unmask", time.Since(start))
		}
	}

	// Then unmask secrets.
	if result.HasSecrets && result.SecretsCtx != nil {
		start := time.Now()
		text = result.SecretsCtx.RestorePlaceholders(text)
		if p.metrics != nil {
			p.metrics.ObserveMaskDuration("unmask", time.Since(start))
		}
	}

	return []byte(text)
}

func (p *Pipeline) NewStreamUnmasker(result *MaskResult) *masking.StreamUnmasker {
	var piiCtx, secretsCtx *masking.MaskContext
	if result != nil && result.HasPII {
		piiCtx = result.PIICtx
	}
	if result != nil && result.HasSecrets {
		secretsCtx = result.SecretsCtx
	}
	return masking.NewStreamUnmasker(piiCtx, secretsCtx)
}

func applyMaskedToPayload(payload map[string]any, span masking.TextSpan, maskedText string) {
	if span.MessageIndex < 0 {
		// System prompt.
		switch v := payload["system"].(type) {
		case string:
			if span.PartIndex == 0 && span.NestedIndex == -1 {
				payload["system"] = maskedText
			}
		case []any:
			if span.PartIndex < len(v) {
				if b, ok := v[span.PartIndex].(map[string]any); ok {
					b["text"] = maskedText
				}
			}
		}
		return
	}

	msgs, _ := payload["messages"].([]any)
	if span.MessageIndex >= len(msgs) {
		return
	}
	msg, _ := msgs[span.MessageIndex].(map[string]any)

	content := msg["content"]
	switch v := content.(type) {
	case string:
		if span.PartIndex == 0 {
			msg["content"] = maskedText
		}
	case []any:
		if span.PartIndex >= len(v) {
			return
		}
		b, _ := v[span.PartIndex].(map[string]any)
		blockType, _ := b["type"].(string)

		switch blockType {
		case "text":
			b["text"] = maskedText
		case "tool_result":
			if span.NestedIndex >= 0 {
				cr, _ := b["content"].([]any)
				if span.NestedIndex < len(cr) {
					if nb, ok := cr[span.NestedIndex].(map[string]any); ok {
						nb["text"] = maskedText
					}
				}
			} else {
				if _, ok := b["content"].(string); ok {
					b["content"] = maskedText
				}
			}
		}
	}
}

// HasPresidio returns true if PII detection is active.
func (p *Pipeline) HasPresidio() bool {
	return p.presidioClient != nil
}

func DefaultConfig() *Config {
	return &Config{
		Enabled:           true,
		SecretsEnabled:    true,
		MaxScanChars:      200000,
		SecretEntities:    strings.Split("OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN", ","),
		PIIEnabled:        true,
		PresidioURL:       "http://arl-presidio:3000",
		PIIScoreThreshold: 0.7,
		PIIEntities:       strings.Split("PERSON,EMAIL_ADDRESS,PHONE_NUMBER", ","),
		PIILanguage:       "en",
	}
}
