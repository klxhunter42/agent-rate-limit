package privacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

const maskCacheTTL = 5 * time.Minute

type cacheEntry struct {
	maskedText         string
	secretPlaceholders map[string]string // placeholder -> original for secrets
	piiPlaceholders    map[string]string // placeholder -> original for PII
	changed            bool
	createdAt          time.Time
}

type MaskCache struct {
	mu    sync.RWMutex
	store map[string]*cacheEntry
}

func NewMaskCache() *MaskCache {
	return &MaskCache{store: make(map[string]*cacheEntry)}
}

func cacheKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8])
}

type CacheLookupResult struct {
	MaskedText         string
	SecretPlaceholders map[string]string // placeholder -> original for secrets
	PIIPlaceholders    map[string]string // placeholder -> original for PII
	Changed            bool
	Hit                bool
}

func (c *MaskCache) Lookup(text string) *CacheLookupResult {
	key := cacheKey(text)
	c.mu.RLock()
	entry, ok := c.store[key]
	c.mu.RUnlock()

	if !ok || time.Since(entry.createdAt) > maskCacheTTL {
		return &CacheLookupResult{Hit: false}
	}

	return &CacheLookupResult{
		MaskedText:         entry.maskedText,
		SecretPlaceholders: entry.secretPlaceholders,
		PIIPlaceholders:    entry.piiPlaceholders,
		Changed:            entry.changed,
		Hit:                true,
	}
}

func (c *MaskCache) Store(text, maskedText string, secretPH, piiPH map[string]string, changed bool) {
	key := cacheKey(text)
	c.mu.Lock()
	c.store[key] = &cacheEntry{
		maskedText:         maskedText,
		secretPlaceholders: secretPH,
		piiPlaceholders:    piiPH,
		changed:            changed,
		createdAt:          time.Now(),
	}
	c.mu.Unlock()
}

func (c *MaskCache) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.evict()
		}
	}
}

func (c *MaskCache) evict() {
	c.mu.Lock()
	expired := 0
	for k, v := range c.store {
		if time.Since(v.createdAt) > maskCacheTTL {
			delete(c.store, k)
			expired++
		}
	}
	total := len(c.store)
	c.mu.Unlock()

	if expired > 0 {
		slog.Info("mask cache eviction", "expired", expired, "remaining", total)
	}
}
