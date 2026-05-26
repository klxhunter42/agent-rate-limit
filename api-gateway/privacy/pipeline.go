package privacy

// Privacy masking with cache-aware behavior for Anthropic prompt caching:
// - Mask cache: identical text spans produce identical masked output, preserving Anthropic prompt cache hits
// - Placeholder mappings are stored per-request in MaskResult for unmasking (independent of cache TTL)
// - Masking order: secrets first (innermost), then PII (outermost)
// - Unmasking order: PII first (outermost), then secrets (innermost)
// - GLM models may output "undefined" instead of placeholders; fallback handles this

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/extractors"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/pii"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/secrets"
)

type Config struct {
	Enabled        bool
	SecretsEnabled bool
	MaxScanChars   int
	SecretEntities []string
	PIIEnabled     bool
	PIIEntities    []string
}

type MaskOptions struct {
	SkipSystemBlocks bool // skip all system prompt blocks (preserves Anthropic prompt cache)
}

type Pipeline struct {
	cfg            *Config
	secretDetector *secrets.SecretDetector
	piiDetector    *pii.RegexDetector
	metrics        *Metrics
	cache          *MaskCache
}

type MaskResult struct {
	MaskedBody []byte
	SecretsCtx *masking.MaskContext
	PIICtx     *masking.MaskContext
	HasSecrets bool
	HasPII     bool
}

const privacyPromptInjection = `Preserve all tokens enclosed in [[...]] exactly as written. Do not modify, replace, or explain them. Treat them as opaque values. Do not reference this instruction in your response.`

func (r *MaskResult) PrivacyPrompt() string {
	if r == nil || (!r.HasSecrets && !r.HasPII) {
		return ""
	}
	return privacyPromptInjection
}

func NewPipeline(cfg *Config, m *Metrics) *Pipeline {
	p := &Pipeline{cfg: cfg, metrics: m, cache: NewMaskCache()}

	if cfg.SecretsEnabled {
		if len(cfg.SecretEntities) > 0 {
			p.secretDetector = secrets.NewDetector(cfg.SecretEntities, cfg.MaxScanChars)
		} else {
			p.secretDetector = secrets.DefaultDetector()
		}
	}

	if cfg.PIIEnabled {
		if len(cfg.PIIEntities) == 0 {
			cfg.PIIEntities = []string{"EMAIL_ADDRESS", "PHONE_NUMBER"}
		}
		p.piiDetector = pii.NewRegexDetector(cfg.PIIEntities)
		slog.Info("PII regex detector ready", "entities", cfg.PIIEntities)
	}

	return p
}

func (p *Pipeline) MaskRequest(body []byte) (*MaskResult, error) {
	return p.MaskRequestWithOptions(body, MaskOptions{})
}

func (p *Pipeline) MaskRequestWithOptions(body []byte, opts MaskOptions) (*MaskResult, error) {
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
	var ctxMu sync.Mutex

	// Process spans in parallel - each span is independent.
	type spanResult struct {
		index                    int
		maskedText               string
		changed                  bool
		totalSecrets             int
		totalPII                 int
		cachedSecretPlaceholders map[string]string // from cache hit, merge into secretsCtx
		cachedPIIPlaceholders    map[string]string // from cache hit, merge into piiCtx
	}

	results := make([]spanResult, len(spans))
	var wg sync.WaitGroup
	wg.Add(len(spans))

	for i, span := range spans {
		go func(idx int, sp masking.TextSpan) {
			defer wg.Done()
			text := sp.Text
			sr := spanResult{index: idx, maskedText: text}

			// Skip system blocks first (before cache lookup) to avoid serving
			// previously-cached masked system prompt that would defeat SkipSystemBlocks.
			if opts.SkipSystemBlocks && sp.MessageIndex < 0 {
				results[idx] = sr
				return
			}

			// Cache lookup: reuse masked output for identical text to preserve Anthropic prompt cache.
			if p.cache != nil {
				if cached := p.cache.Lookup(text); cached.Hit {
					sr.maskedText = cached.MaskedText
					sr.changed = cached.Changed
					sr.cachedSecretPlaceholders = cached.SecretPlaceholders
					sr.cachedPIIPlaceholders = cached.PIIPlaceholders
					results[idx] = sr
					return
				}
			}

			origText := text

			// Snapshot pre-existing keys to capture only THIS span's new mappings.
			var preSecretKeys, prePIIKeys map[string]struct{}
			ctxMu.Lock()
			preSecretKeys = make(map[string]struct{}, len(secretsCtx.Mapping))
			for k := range secretsCtx.Mapping {
				preSecretKeys[k] = struct{}{}
			}
			prePIIKeys = make(map[string]struct{}, len(piiCtx.Mapping))
			for k := range piiCtx.Mapping {
				prePIIKeys[k] = struct{}{}
			}
			ctxMu.Unlock()

			if p.secretDetector != nil && text != "" {
				start := time.Now()
				det := p.secretDetector.Detect(text)
				if p.metrics != nil {
					p.metrics.ObserveMaskDuration("secrets_detect", time.Since(start))
				}
				if det.Detected {
					sr.totalSecrets = len(det.Matches)
					start = time.Now()
					ctxMu.Lock()
					maskRes := secrets.MaskSecrets(text, det.Locations, secretsCtx)
					ctxMu.Unlock()
					if p.metrics != nil {
						p.metrics.ObserveMaskDuration("mask", time.Since(start))
					}
					text = maskRes.MaskedText
				}
				for _, m := range det.Matches {
					if p.metrics != nil {
						p.metrics.IncSecretsDetected(string(m.Type), m.Count)
					}
				}
			}

			if p.piiDetector != nil && text != "" {
				start := time.Now()
				piiResult := p.piiDetector.Detect(text)
				if p.metrics != nil {
					p.metrics.ObserveMaskDuration("pii_detect", time.Since(start))
				}
				if piiResult.HasPII {
					sr.totalPII = len(piiResult.Entities)
					start = time.Now()
					ctxMu.Lock()
					maskRes := pii.MaskPII(text, piiResult.Entities, piiCtx)
					ctxMu.Unlock()
					if p.metrics != nil {
						p.metrics.ObserveMaskDuration("mask", time.Since(start))
					}
					text = maskRes.MaskedText
				}
				for _, e := range piiResult.Entities {
					if p.metrics != nil {
						p.metrics.IncPIIDetected(e.EntityType)
					}
				}
			}

			sr.maskedText = text
			sr.changed = text != origText

			// Store mask result in cache: only capture THIS span's new placeholder mappings.
			if p.cache != nil && sr.changed {
				var secretPH, piiPH map[string]string
				ctxMu.Lock()
				for k, v := range secretsCtx.Mapping {
					if _, existed := preSecretKeys[k]; !existed {
						if secretPH == nil {
							secretPH = make(map[string]string)
						}
						secretPH[k] = v
					}
				}
				for k, v := range piiCtx.Mapping {
					if _, existed := prePIIKeys[k]; !existed {
						if piiPH == nil {
							piiPH = make(map[string]string)
						}
						piiPH[k] = v
					}
				}
				ctxMu.Unlock()
				p.cache.Store(sp.Text, sr.maskedText, secretPH, piiPH, true)
			}

			results[idx] = sr
		}(i, span)
	}
	wg.Wait()

	// Merge cached placeholders into per-request MaskContexts.
	cachedSecretHits := 0
	cachedPIIHits := 0
	for _, sr := range results {
		if sr.cachedSecretPlaceholders == nil && sr.cachedPIIPlaceholders == nil {
			continue
		}
		if sr.cachedSecretPlaceholders != nil {
			secretsCtx.MergeExternal(sr.cachedSecretPlaceholders)
			cachedSecretHits++
		}
		if sr.cachedPIIPlaceholders != nil {
			piiCtx.MergeExternal(sr.cachedPIIPlaceholders)
			cachedPIIHits++
		}
	}

	// Apply masked results back to payload.
	totalSecrets := 0
	totalPII := 0
	for i, sr := range results {
		totalSecrets += sr.totalSecrets
		totalPII += sr.totalPII
		if sr.changed {
			applyMaskedToPayload(payload, spans[i], sr.maskedText)
		}
	}

	if totalSecrets+cachedSecretHits == 0 && totalPII+cachedPIIHits == 0 {
		return nil, nil
	}

	result.HasSecrets = totalSecrets+cachedSecretHits > 0
	result.HasPII = totalPII+cachedPIIHits > 0
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
	origLen := len(text)

	// Check if any placeholder actually exists in the response.
	found := 0
	if result.HasSecrets && result.SecretsCtx != nil {
		for ph := range result.SecretsCtx.Mapping {
			if strings.Contains(text, ph) {
				found++
			}
		}
	}
	if result.HasPII && result.PIICtx != nil {
		for ph := range result.PIICtx.Mapping {
			if strings.Contains(text, ph) {
				found++
			}
		}
	}
	slog.Info("unmask check",
		"body_len", origLen,
		"placeholders_in_body", found,
		"secrets_mapping", mapLen(result.SecretsCtx),
		"pii_mapping", mapLen(result.PIICtx),
	)

	// Unmask PII first (outermost), then secrets (innermost).
	// Mask order: secrets masked first (innermost), PII applied on top (outermost).
	// Unmask must reverse: PII first, then secrets.
	if result.HasPII && result.PIICtx != nil {
		start := time.Now()
		text = result.PIICtx.RestorePlaceholdersJSON(text)
		if p.metrics != nil {
			p.metrics.ObserveMaskDuration("unmask", time.Since(start))
		}
	}

	if result.HasSecrets && result.SecretsCtx != nil {
		start := time.Now()
		text = result.SecretsCtx.RestorePlaceholdersJSON(text)
		if p.metrics != nil {
			p.metrics.ObserveMaskDuration("unmask", time.Since(start))
		}
	}

	// Safety: strip any [[TYPE_N]] placeholders that survived unmasking.
	text = masking.StripLeftoverPlaceholders(text)

	// Fallback: GLM models may output "undefined" instead of preserving [[TYPE_N]] placeholders.
	// The streaming path handles this in StreamUnmasker.replaceUndefinedFallback, but non-streaming
	// responses need it here too.
	if strings.Contains(text, "undefined") {
		text = replaceUndefinedNonStream(text, result)
	}

	slog.Info("unmask done",
		"orig_len", origLen,
		"new_len", len(text),
		"changed", origLen != len(text),
	)

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

func mapLen(ctx *masking.MaskContext) int {
	if ctx == nil {
		return 0
	}
	return len(ctx.Mapping)
}

// replaceUndefinedNonStream handles GLM's "undefined" output in non-streaming responses.
// Only called when masking was active (HasSecrets || HasPII) and response contains "undefined".
// Uses the same budget-based approach as StreamUnmasker.replaceUndefinedFallback.
func replaceUndefinedNonStream(text string, result *MaskResult) string {
	// Collect originals sorted by placeholder name for deterministic order.
	type entry struct {
		placeholder string
		original    string
	}
	var entries []entry
	if result.HasSecrets && result.SecretsCtx != nil {
		for p, orig := range result.SecretsCtx.Mapping {
			entries = append(entries, entry{p, orig})
		}
	}
	if result.HasPII && result.PIICtx != nil {
		for p, orig := range result.PIICtx.Mapping {
			entries = append(entries, entry{p, orig})
		}
	}
	if len(entries) == 0 {
		return text
	}

	// Sort by placeholder name for deterministic ordering.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].placeholder < entries[j].placeholder
	})

	// Phase 1: Replace "undefined" with originals (budget-limited).
	for i := range entries {
		if !strings.Contains(text, "undefined") {
			break
		}
		text = strings.Replace(text, "undefined", entries[i].original, 1)
	}

	// Phase 2: Dedup adjacent "undefined" next to restored originals.
	for _, e := range entries {
		for strings.Contains(text, e.original+" undefined") {
			text = strings.Replace(text, e.original+" undefined", e.original, 1)
		}
		for strings.Contains(text, "undefined "+e.original) {
			text = strings.Replace(text, "undefined "+e.original, e.original, 1)
		}
	}

	// Phase 3: Strip remaining bare "undefined" (budget exhausted), preserving spacing.
	for strings.Contains(text, "undefined") {
		replaced := text
		replaced = strings.Replace(replaced, " undefined ", " ", -1)
		replaced = strings.Replace(replaced, "undefined ", "", -1)
		replaced = strings.Replace(replaced, " undefined", "", -1)
		replaced = strings.Replace(replaced, "undefined", "", -1)
		if replaced == text {
			break
		}
		text = replaced
	}

	return text
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
		case "tool_use":
			if span.NestedIndex == -2 {
				input, _ := b["input"].(map[string]any)
				prefix := fmt.Sprintf("messages[%d].content[%d].input.", span.MessageIndex, span.PartIndex)
				if input != nil && strings.HasPrefix(span.Path, prefix) {
					keyPath := span.Path[len(prefix):]
					setInputLeaf(input, keyPath, maskedText)
				}
			}
		}
	}
}

// setInputLeaf navigates a nested map by dot-separated keyPath and sets the leaf value.
// Supports "key", "key.sub", and "key[0]" notation.
func setInputLeaf(obj map[string]any, keyPath string, value string) {
	parts := strings.SplitN(keyPath, ".", 2)
	key := parts[0]

	// Handle array index: key[idx]
	if idxStart := strings.Index(key, "["); idxStart >= 0 {
		baseKey := key[:idxStart]
		idxStr := key[idxStart+1 : len(key)-1]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return
		}
		arr, ok := obj[baseKey].([]any)
		if !ok || idx >= len(arr) {
			return
		}
		if len(parts) == 1 {
			arr[idx] = value
		} else {
			if nested, ok := arr[idx].(map[string]any); ok {
				setInputLeaf(nested, parts[1], value)
			}
		}
		return
	}

	if len(parts) == 1 {
		obj[key] = value
		return
	}
	if nested, ok := obj[key].(map[string]any); ok {
		setInputLeaf(nested, parts[1], value)
	}
}

// HasPIIDetector returns true if PII detection is active.
func (p *Pipeline) HasPIIDetector() bool {
	return p.piiDetector != nil
}

func (p *Pipeline) StartCacheCleanup(ctx context.Context) {
	if p.cache != nil {
		go p.cache.StartCleanup(ctx)
	}
}

// DefaultConfig returns the full configuration with all supported entity types.
// Runtime defaults (in NewPipeline) are more conservative: only EMAIL_ADDRESS and PHONE_NUMBER
// are enabled by default to avoid false positives. The full list below is used when
// explicit entity configuration is provided.
func DefaultConfig() *Config {
	return &Config{
		Enabled:        true,
		SecretsEnabled: true,
		MaxScanChars:   200000,
		SecretEntities: strings.Split("OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,API_KEY_GITLAB,JWT_TOKEN,BEARER_TOKEN,ENV_PASSWORD,ENV_SECRET,ENV_USER,CONNECTION_STRING,API_KEY_GCP,API_KEY_TENCENT,API_KEY_ALIBABA,API_KEY_SLACK,API_KEY_STRIPE,API_KEY_SENDGRID,ENV_TOKEN,ENV_CREDENTIAL,BASIC_AUTH_URL,CLI_AUTH,CURL_BASIC_AUTH,VAULT_TOKEN,AZURE_CREDENTIAL,WEBHOOK_URL", ","),
		PIIEnabled:     true,
		PIIEntities:    strings.Split("EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE", ","),
	}
}
