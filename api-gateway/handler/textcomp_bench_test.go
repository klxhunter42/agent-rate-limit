package handler

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/klxhunter/agent-rate-limit/api-gateway/textcomp"
)

// Benchmarks where the large-request prep time goes for zai: TextComp vs JSON
// round-trip. Run: go test ./handler/ -run XXX -Bench XXX -v  (or just call via Test).
func TestTextCompPrepBreakdown(t *testing.T) {
	tc := textcomp.New(textcomp.Config{Enabled: true, Mode: "balanced"})

	// ~300 blocks like a large Claude Code context.
	chunk := "This is a sample context line for load testing the gateway request preparation pipeline. It has some filler words like basically, actually, in order to, due to the fact that. " +
		"Also some code `code here` and a url https://example.com/path and a \"quoted string here\"."
	blocks := make([]string, 300)
	for i := range blocks {
		blocks[i] = chunk
	}
	totalChars := len(chunk) * 300

	// 1. TextComp on all blocks (sequential, current behavior).
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			for _, s := range blocks {
				_, _ = tc.Compress(s)
			}
		}
	})
	fmt.Printf("TextComp x300 SERIAL (%d chars): %v/op = %s total\n", totalChars, r.NsPerOp(), fmtDur(r.NsPerOp()))

	// Parallel via a worker pool (mirrors the handler fix).
	rp := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			workers := runtime.NumCPU()
			if workers > len(blocks) {
				workers = len(blocks)
			}
			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup
			out := make([]string, len(blocks))
			for i := range blocks {
				wg.Add(1)
				sem <- struct{}{}
				go func(i int) {
					defer wg.Done()
					defer func() { <-sem }()
					o, _ := tc.Compress(blocks[i])
					out[i] = o
				}(i)
			}
			wg.Wait()
			_ = out
		}
	})
	fmt.Printf("TextComp x300 PARALLEL (pool=%d): %v/op = %s total  -> %.1fx\n",
		runtime.NumCPU(), rp.NsPerOp(), fmtDur(rp.NsPerOp()), float64(r.NsPerOp())/float64(rp.NsPerOp()))

	// 2. JSON round-trip of an 841KB-ish payload.
	payload := map[string]any{
		"model":      "glm-5.2",
		"max_tokens": 16,
		"system":     chunk,
		"messages":   []any{},
	}
	msgs := []any{}
	for _, s := range blocks {
		msgs = append(msgs, map[string]any{"role": "user", "content": s})
	}
	payload["messages"] = msgs
	body, _ := json.Marshal(payload)
	fmt.Printf("payload size: %d bytes\n", len(body))

	r2 := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for n := 0; n < b.N; n++ {
			var p map[string]any
			_ = json.Unmarshal(body, &p)
			_, _ = json.Marshal(p)
		}
	})
	fmt.Printf("JSON unmarshal+marshal (%d bytes): %v/op = %s total\n", len(body), r2.NsPerOp(), fmtDur(r2.NsPerOp()))
}

func fmtDur(ns int64) string {
	switch {
	case ns < 1e6:
		return fmt.Sprintf("%.2fms", float64(ns)/1e6)
	default:
		return fmt.Sprintf("%.2fs", float64(ns)/1e9)
	}
}
