package pii

import (
	"log/slog"
	"regexp"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

type DetectResult struct {
	Entities []masking.PIIEntity
	HasPII   bool
	ScanMs   int64
}

// RegexDetector finds PII entities using compiled regex patterns.
// Replaces the slow Presidio HTTP container (7-14s per call) with <1ms regex.
//
// Supported entities:
//   - EMAIL_ADDRESS: standard email format
//   - PHONE_NUMBER: international phone numbers
//   - CREDIT_CARD: Visa, Mastercard, Amex, Discover
//   - SSN: US Social Security Number
//   - IBAN: International Bank Account Number
//   - IP_ADDRESS: IPv4 addresses
//   - THAI_NATIONAL_ID: Thai citizen ID (13 digits with dashes)
//   - THAI_PHONE: Thai phone numbers (0x-xxx-xxxx or +66x-xxx-xxxx)
type RegexDetector struct {
	entities []string
}

var (
	emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRegex = regexp.MustCompile(`(?:\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`)

	// Credit card: Visa (4xxx), Mastercard (5xxx/2xxx), Amex (34/37), Discover (6011/65)
	creditCardRegex = regexp.MustCompile(`\b(?:4\d{3}|5[1-5]\d{2}|2[2-7]\d{2}|3[47]\d{2}|6011|65\d{2})[ -]?\d{4}[ -]?\d{4}[ -]?\d{3,4}\b`)
	// US SSN: xxx-xx-xxxx
	ssnRegex = regexp.MustCompile(`\b\d{3}[ -]\d{2}[ -]\d{4}\b`)
	// IBAN: 2 letter country code + 2 check digits + up to 30 alphanum
	ibanRegex = regexp.MustCompile(`\b[A-Z]{2}\d{2}[- ]?[A-Z0-9]{4}([- ]?[A-Z0-9]{1,4}){1,7}\b`)
	// IPv4
	ipv4Regex = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\b`)
	// Thai national ID: x-xxxx-xxxxx-xx-x
	thaiIDRegex = regexp.MustCompile(`\b\d{1}[- ]?\d{4}[- ]?\d{5}[- ]?\d{2}[- ]?\d{1}\b`)
	// Thai phone: 0[2-9]x-xxx-xxxx, 0[2-9]-xxxx-xxxx, +66-[2-9]x-xxx-xxxx, etc.
	thaiPhoneRegex = regexp.MustCompile(`(?:\+66[- ]?|0)[2-9]\d?[- ]?\d{3,4}[- ]?\d{3,4}`)
	// URL detection for false-positive filtering
	urlRegex = regexp.MustCompile(`https?://[^\s"'<>]+`)
)

func NewRegexDetector(entities []string) *RegexDetector {
	return &RegexDetector{entities: entities}
}

// overlapsURL checks if span [start, end) overlaps with any URL in urlSpans.
func overlapsURL(urlSpans [][]int, start, end int) bool {
	for _, u := range urlSpans {
		if start < u[1] && end > u[0] {
			return true
		}
	}
	return false
}

func (d *RegexDetector) Detect(text string) DetectResult {
	if text == "" {
		return DetectResult{}
	}

	start := time.Now()
	var entities []masking.PIIEntity
	urlSpans := urlRegex.FindAllStringIndex(text, -1)

	for _, entity := range d.entities {
		switch entity {
		case "EMAIL_ADDRESS":
			for _, m := range emailRegex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "EMAIL_ADDRESS",
					Start:      m[0],
					End:        m[1],
					Score:      0.95,
				})
			}
		case "PHONE_NUMBER":
			for _, m := range phoneRegex.FindAllStringIndex(text, -1) {
				if overlapsURL(urlSpans, m[0], m[1]) {
					continue
				}
				entities = append(entities, masking.PIIEntity{
					EntityType: "PHONE_NUMBER",
					Start:      m[0],
					End:        m[1],
					Score:      0.90,
				})
			}
		case "CREDIT_CARD":
			for _, m := range creditCardRegex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "CREDIT_CARD",
					Start:      m[0],
					End:        m[1],
					Score:      0.95,
				})
			}
		case "SSN":
			for _, m := range ssnRegex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "SSN",
					Start:      m[0],
					End:        m[1],
					Score:      0.90,
				})
			}
		case "IBAN":
			for _, m := range ibanRegex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "IBAN",
					Start:      m[0],
					End:        m[1],
					Score:      0.90,
				})
			}
		case "IP_ADDRESS":
			for _, m := range ipv4Regex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "IP_ADDRESS",
					Start:      m[0],
					End:        m[1],
					Score:      0.80,
				})
			}
		case "THAI_NATIONAL_ID":
			for _, m := range thaiIDRegex.FindAllStringIndex(text, -1) {
				entities = append(entities, masking.PIIEntity{
					EntityType: "THAI_NATIONAL_ID",
					Start:      m[0],
					End:        m[1],
					Score:      0.90,
				})
			}
		case "THAI_PHONE":
			for _, m := range thaiPhoneRegex.FindAllStringIndex(text, -1) {
				if overlapsURL(urlSpans, m[0], m[1]) {
					continue
				}
				entities = append(entities, masking.PIIEntity{
					EntityType: "THAI_PHONE",
					Start:      m[0],
					End:        m[1],
					Score:      0.90,
				})
			}
		}
	}

	elapsed := time.Since(start).Milliseconds()
	if len(entities) > 0 {
		slog.Info("pii detect",
			"text_len", len(text),
			"entities_found", len(entities),
			"ms", elapsed,
		)
	}

	return DetectResult{
		Entities: entities,
		HasPII:   len(entities) > 0,
		ScanMs:   elapsed,
	}
}
