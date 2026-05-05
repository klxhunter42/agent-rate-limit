package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/bandit"
	"github.com/klxhunter/agent-rate-limit/api-gateway/cache"
	"github.com/klxhunter/agent-rate-limit/api-gateway/caveman"
	"github.com/klxhunter/agent-rate-limit/api-gateway/chunker"
	"github.com/klxhunter/agent-rate-limit/api-gateway/delta"
	"github.com/klxhunter/agent-rate-limit/api-gateway/disclosure"
	"github.com/klxhunter/agent-rate-limit/api-gateway/filter"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/packer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/prefetcher"
	"github.com/klxhunter/agent-rate-limit/api-gateway/sketch"
	"github.com/klxhunter/agent-rate-limit/api-gateway/summarizer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/compcache"
	"github.com/klxhunter/agent-rate-limit/api-gateway/textcomp"
	"github.com/klxhunter/agent-rate-limit/api-gateway/toolcomp"
	"github.com/klxhunter/agent-rate-limit/api-gateway/toolfilter"
	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/warmstart"
	"github.com/klxhunter/agent-rate-limit/api-gateway/waste"
)

// Optimizers holds all 13 token optimization components.
// Nil fields mean the feature is disabled.
type Optimizers struct {
	Chunker    *chunker.Chunker
	Packer     *packer.Packer
	Disclosure *disclosure.Disclosure
	Prefetcher *prefetcher.Prefetcher
	Bandit     *bandit.Bandit
	Summarizer *summarizer.Summarizer
	Delta      *delta.Delta
	Sketch     *sketch.Sketch
	Waste      *waste.WasteDetector
	Filter     *filter.Filter
	Cache      *cache.EvictionManager
	WarmStart  *warmstart.WarmStart
	Caveman    *caveman.CavemanPipeline
	TextComp   *textcomp.TextComp
	ToolComp *toolcomp.ToolComp
	ToolFilter *toolfilter.ToolFilter
	CompCache *compcache.CompCache
}

// OptimizeSystemPrompt applies the full optimization pipeline to system prompt text.
// budgetLevel: 0=green, 1=yellow, 2=red
// model: used for budget tracking and model-specific optimizations
func (o *Optimizers) OptimizeSystemPrompt(text string, m *metrics.Metrics, budgetLevel int, model string, transparent bool) string {
	if text == "" {
		return text
	}
	totalSaved := 0

	// Semantic dedup (F7 - always available via tokenizer)
	func() {
		start := time.Now()
		opt, saved := tokenizer.DeduplicateSemantic(text, 0.7)
		if saved > 0 {
			text = opt
			m.RecordOptimization("semantic_dedup", saved, "input")
			m.RecordOptimizationDuration("semantic_dedup", time.Since(start).Seconds())
			totalSaved += saved
		}
	}()

	// Chunker (F1)
	if o.Chunker != nil {
		start := time.Now()
		opt, saved := o.Chunker.ChunkAndReorder(context.Background(), text)
		if saved > 0 {
			text = opt
			m.RecordOptimization("chunker", saved, "input")
			m.RecordOptimizationDuration("chunker", time.Since(start).Seconds())
			totalSaved += saved
		}
	}

	// Delta encoding (F8)
	if o.Delta != nil {
		start := time.Now()
		encoded, saved, ok := o.Delta.Encode(context.Background(), "sys:"+model, text)
		if ok && saved > 0 {
			text = encoded
			m.RecordOptimization("delta", saved, "input")
			m.RecordOptimizationDuration("delta", time.Since(start).Seconds())
			totalSaved += saved
		}
	}

	// Sketch dedup (F9)
	if o.Sketch != nil {
		start := time.Now()
		isDup, _, saved := o.Sketch.CheckAndStore(context.Background(), model, text)
		if isDup && saved > 0 {
			m.RecordOptimization("sketch_dedup", saved, "input")
			m.RecordOptimizationDuration("sketch", time.Since(start).Seconds())
		}
	}

	// Summarizer (F6) - only on red budget
	if o.Summarizer != nil && budgetLevel >= 2 {
		start := time.Now()
		opt, saved := o.Summarizer.Summarize(context.Background(), text, budgetLevel)
		if saved > 0 {
			text = opt
			m.RecordOptimization("summarizer", saved, "input")
			m.RecordOptimizationDuration("summarizer", time.Since(start).Seconds())
			totalSaved += saved
		}
	}

	// Intent filter (F13)
	if o.Filter != nil {
		start := time.Now()
		intent := o.Filter.ClassifyIntent(nil)
		opt, saved := o.Filter.FilterResponse(text, intent)
		if saved > 0 {
			text = opt
			m.RecordOptimization("intent_filter", saved, "input")
			m.RecordOptimizationDuration("intent_filter", time.Since(start).Seconds())
			totalSaved += saved
		}
	}

	// TextComp regex compression (F17) - removes filler/verbose text, safe for all modes
	if o.TextComp != nil {
		start := time.Now()
		opt, saved := o.TextComp.Compress(text)
		if saved > 0 {
			text = opt
			m.RecordOptimization("textcomp", saved, "input")
			m.RecordOptimizationDuration("textcomp", time.Since(start).Seconds())
			totalSaved += saved
		}
	}

	// Caveman compression (F16) — skip for transparent: adds input tokens
	if !transparent {
		if o.Caveman != nil {
			shouldCompress, tier := o.Caveman.ShouldCompress(text, budgetLevel)
			if shouldCompress {
				start := time.Now()
				compressed, _ := o.Caveman.Compress("", tier)
				if compressed != "" {
					text = compressed
					m.RecordOptimization("caveman", 0, "input")
					m.RecordOptimizationDuration("caveman", time.Since(start).Seconds())
				}
			}
		}
	}

	if totalSaved > 0 {
		tokensSaved := float64(totalSaved) / 4.0
		m.RecordTokensSaved(int(tokensSaved + 0.5), "input")
		costSavings := tokensSaved * 3.0 / 1_000_000
		m.RecordCostSavings(costSavings)
	}

	return text
}

// OptimizeMessages applies lightweight optimization to message content (whitespace + dedup).
// Skips code blocks and privacy placeholders. Only applies to text content in user/assistant messages.
func (o *Optimizers) OptimizeMessages(messages []any, m *metrics.Metrics) {
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"]
		if !ok {
			continue
		}

		switch c := content.(type) {
		case string:
			if c == "" {
				continue
			}
			optimized, saved := tokenizer.OptimizeWhitespace(c)
			if saved > 0 {
				if opt2, s2 := tokenizer.DeduplicateSentences(optimized); s2 > 0 {
					optimized = opt2
					saved += s2
				}
				msgMap["content"] = optimized
				m.RecordOptimization("message_text", saved, "input")
			}
			// TextComp on string message content
			if o.TextComp != nil {
				if tc, ok := msgMap["content"].(string); ok && tc != "" {
					opt2, saved2 := o.TextComp.Compress(tc)
					if saved2 > 0 {
						msgMap["content"] = opt2
						m.RecordOptimization("message_textcomp", saved2, "input")
					}
				}
			}
		case []any:
			for _, block := range c {
				blockMap, ok := block.(map[string]any)
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "tool_use" {
					continue
				}
				// text blocks use "text" field, tool_result uses "content" field
				fields := []string{"text"}
				if blockType == "tool_result" {
					fields = []string{"content"}
				}
				for _, field := range fields {
					text, ok := blockMap[field].(string)
					if !ok || text == "" {
						continue
					}
					optimized, saved := tokenizer.OptimizeWhitespace(text)
					if saved > 0 {
						if opt2, s2 := tokenizer.DeduplicateSentences(optimized); s2 > 0 {
							optimized = opt2
							saved += s2
						}
						blockMap[field] = optimized
						m.RecordOptimization("message_block_"+blockType, saved, "input")
					}
				}
				// ToolComp format-aware compression for tool_result blocks
				if o.ToolComp != nil && blockType == "tool_result" {
					if tc, ok := blockMap["content"].(string); ok && tc != "" {
						opt, saved := o.ToolComp.Compress(tc)
						if saved > 0 {
							blockMap["content"] = opt
							m.RecordOptimization("toolcomp", saved, "input")
						}
					}
				}
			}
		}
	}
}

// PostProxyFeedback records telemetry after a proxy response completes.
func (o *Optimizers) PostProxyFeedback(sessionID, model string, input, output int) {
	// Prefetcher (F4)
	if o.Prefetcher != nil {
		o.Prefetcher.Record(context.Background(), sessionID, model)
	}

	// Waste detection (F11)
	if o.Waste != nil {
		o.Waste.RecordRequest(sessionID, model, input, output)
	}

	// Cache ROI (F14)
	if o.Cache != nil && input > 0 {
		saved := input / 4
		o.Cache.RecordHit(context.Background(), "session:"+sessionID, saved)
	}

	// Bandit feedback (F5)
	if o.Bandit != nil {
		reward := 1.0
		if output == 0 {
			reward = 0.0
		} else if input > 0 {
			reward = float64(output) / float64(input)
			if reward > 1.0 {
				reward = 1.0
			}
		}
		o.Bandit.Update(context.Background(), model, [10]float64{}, reward)
	}
}

// RecordWarmStart attempts warm start for a session.
func (o *Optimizers) RecordWarmStart(ctx context.Context, sessionID string, data map[string]any) {
	if o.WarmStart == nil {
		return
	}
	if o.WarmStart.WarmSession(ctx, sessionID, data) {
		slog.Info("warm start hit", "session", sessionID)
	}
}

// GetWasteFindings returns JSON waste findings for the API endpoint.
func (o *Optimizers) GetWasteFindings() string {
	if o.Waste == nil {
		return "[]"
	}
	return o.Waste.GetFindingsJSON()
}
