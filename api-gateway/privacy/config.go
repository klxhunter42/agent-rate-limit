package privacy

import (
	"os"
	"strconv"
	"strings"
)

func LoadConfig() *Config {
	cfg := DefaultConfig()

	if v := os.Getenv("PASTEGUARD_ENABLED"); v != "" {
		cfg.Enabled = strings.ToLower(v) == "true" || v == "1"
	}
	if v := os.Getenv("PASTEGUARD_SECRETS_ENABLED"); v != "" {
		cfg.SecretsEnabled = strings.ToLower(v) == "true" || v == "1"
	}
	if v := os.Getenv("PASTEGUARD_MAX_SCAN_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxScanChars = n
		}
	}
	if v := os.Getenv("PASTEGUARD_SECRET_ENTITIES"); v != "" {
		entities := strings.Split(v, ",")
		for i, e := range entities {
			entities[i] = strings.TrimSpace(e)
		}
		cfg.SecretEntities = entities
	}
	if v := os.Getenv("PASTEGUARD_PII_ENABLED"); v != "" {
		cfg.PIIEnabled = strings.ToLower(v) == "true" || v == "1"
	}
	if v := os.Getenv("PASTEGUARD_PRESIDIO_URL"); v != "" {
		cfg.PresidioURL = v
	}
	if v := os.Getenv("PASTEGUARD_PII_SCORE_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.PIIScoreThreshold = f
		}
	}
	if v := os.Getenv("PASTEGUARD_PII_ENTITIES"); v != "" {
		entities := strings.Split(v, ",")
		for i, e := range entities {
			entities[i] = strings.TrimSpace(e)
		}
		cfg.PIIEntities = entities
	}
	if v := os.Getenv("PASTEGUARD_PII_LANGUAGE"); v != "" {
		cfg.PIILanguage = v
	}

	return cfg
}
