package tokenizer

import (
	"fmt"
	"strings"
	"testing"
)

// TestOptimizerEffectiveness measures real-world token savings.
func TestOptimizerEffectiveness(t *testing.T) {
	// Simulate a realistic system prompt (CLAUDE.md + skills + context).
	systemPrompt := buildRealisticSystemPrompt()
	_ = buildRealisticUserMessage()

	t.Run("SystemPrompt_WhitespaceOptimization", func(t *testing.T) {
		optimized, saved := OptimizeWhitespace(systemPrompt)
		origTokens := QuickEstimateTokens(systemPrompt)
		optTokens := QuickEstimateTokens(optimized)
		pctSavings := float64(0)
		if origTokens > 0 {
			pctSavings = float64(origTokens-optTokens) / float64(origTokens) * 100
		}
		fmt.Printf("  Original:   %d chars, ~%d tokens\n", len(systemPrompt), origTokens)
		fmt.Printf("  Optimized:  %d chars, ~%d tokens\n", len(optimized), optTokens)
		fmt.Printf("  Saved:      %d tokens (%.1f%%)\n", saved, pctSavings)
	})

	t.Run("SystemPrompt_Deduplication", func(t *testing.T) {
		deduped, saved := DeduplicateSentences(systemPrompt)
		origTokens := QuickEstimateTokens(systemPrompt)
		dedupTokens := QuickEstimateTokens(deduped)
		pctSavings := float64(0)
		if origTokens > 0 {
			pctSavings = float64(origTokens-dedupTokens) / float64(origTokens) * 100
		}
		fmt.Printf("  Original:   %d chars, ~%d tokens\n", len(systemPrompt), origTokens)
		fmt.Printf("  Deduped:    %d chars, ~%d tokens\n", len(deduped), dedupTokens)
		fmt.Printf("  Saved:      %d tokens (%.1f%%)\n", saved, pctSavings)
	})

	t.Run("Combined_Whitespace+Dedup", func(t *testing.T) {
		origTokens := QuickEstimateTokens(systemPrompt)
		step1, wsSaved := OptimizeWhitespace(systemPrompt)
		step2, dedupSaved := DeduplicateSentences(step1)
		finalTokens := QuickEstimateTokens(step2)
		totalSaved := wsSaved + dedupSaved
		pctSavings := float64(0)
		if origTokens > 0 {
			pctSavings = float64(origTokens-finalTokens) / float64(origTokens) * 100
		}
		fmt.Printf("  Original:    %d tokens\n", origTokens)
		fmt.Printf("  After WS:    %d tokens (saved %d)\n", QuickEstimateTokens(step1), wsSaved)
		fmt.Printf("  After Dedup: %d tokens (saved %d)\n", finalTokens, dedupSaved)
		fmt.Printf("  Total saved: %d tokens (%.1f%%)\n", totalSaved, pctSavings)
	})

	t.Run("TokenEstimation_ByContentType", func(t *testing.T) {
		code := "func main() {\n\tfmt.Println(\"hello\")\n\treturn 42\n}"
		json := `{"model":"glm-5","messages":[{"role":"user","content":"hello"}],"stream":true}`
		md := "# Title\n\nSome **bold** text\n\n- item 1\n- item 2\n\n```\ncode\n```\n"
		text := "Hello world this is plain text with no special formatting at all."

		codeTokens := EstimateTokens(code)
		jsonTokens := EstimateTokens(json)
		mdTokens := EstimateTokens(md)
		textTokens := EstimateTokens(text)

		fmt.Printf("  Code (%d chars): ~%d tokens\n", len(code), codeTokens)
		fmt.Printf("  JSON (%d chars): ~%d tokens\n", len(json), jsonTokens)
		fmt.Printf("  Markdown (%d chars): ~%d tokens\n", len(md), mdTokens)
		fmt.Printf("  Text (%d chars): ~%d tokens\n", len(text), textTokens)
	})

	t.Run("HeadTailTruncation", func(t *testing.T) {
		// 200-line conversation history.
		lines := make([]string, 200)
		for i := range lines {
			lines[i] = fmt.Sprintf("Line %d: This is a typical conversation message with some content about various topics.", i+1)
		}
		text := strings.Join(lines, "\n")

		maxChars := 5000
		result := TruncateHeadTail(text, maxChars, 0.4)
		origTokens := QuickEstimateTokens(text)
		resultTokens := QuickEstimateTokens(result)

		fmt.Printf("  Original: %d chars, ~%d tokens\n", len(text), origTokens)
		fmt.Printf("  Truncated: %d chars, ~%d tokens (%.1f%% reduction)\n",
			len(result), resultTokens,
			float64(origTokens-resultTokens)/float64(origTokens)*100)
	})

	t.Run("ModelCapabilities_Coverage", func(t *testing.T) {
		models := []string{
			"claude-opus-4-7", "claude-sonnet-4-6", "claude-haiku-4-5-20251001",
			"gpt-4o", "gpt-4o-mini",
			"gemini-2.5-pro", "gemini-2.5-flash",
			"glm-5.1", "glm-5", "glm-4.6v",
			"unknown-model",
		}
		fmt.Println("  Model              | Provider  | Context | MaxOut")
		fmt.Println("  -------------------|-----------|---------|-------")
		for _, m := range models {
			cap := GetModelCapabilities(m)
			fmt.Printf("  %-18s | %-9s | %7d | %5d\n", m, cap.Provider, cap.ContextWindow, cap.MaxOutputTokens)
		}
	})

	t.Run("BudgetTracking_Thresholds", func(t *testing.T) {
		budget := NewTokenBudget("claude-opus-4-7")
		levels := []struct {
			addTokens int
			expected  string
		}{
			{50000, "GREEN"},
			{50000, "GREEN"},  // 100K/200K = 50%
			{30000, "YELLOW"}, // 130K/200K = 65%
			{20000, "YELLOW"}, // 150K/200K = 75%
			{10000, "RED"},    // 160K/200K = 80%
		}
		for _, l := range levels {
			budget.AddTokens(l.addTokens, 0)
			level := "GREEN"
			if budget.Level() == BudgetYellow {
				level = "YELLOW"
			} else if budget.Level() == BudgetRed {
				level = "RED"
			}
			fmt.Printf("  After +%d: %d/%d tokens (%.0f%%) = %s (expected %s)\n",
				l.addTokens, budget.UsedTokens, budget.ContextLimit, budget.PercentUsed(), level, l.expected)
		}
	})
}

func buildRealisticSystemPrompt() string {
	// Simulate CLAUDE.md + skills loading (~typical system prompt).
	return `
# Global Claude Code Configuration
# Sources: claude-token-efficient, soul.md, awesome-openclaw-agents
# Context: ~/.claude/SOUL.md, ~/.claude/SKILL.md, ~/.claude/STYLE.md

## Agent Auto-Routing

When a task matches these patterns, automatically use the specified agent — no need for user to ask explicitly:

| Task pattern | Agent |
|---|---|
| incident / alert / sev / outage / on-call / pagerduty | ` + "`incident-responder`" + ` |
| runbook / procedure / steps for / on-call doc | ` + "`runbook-writer`" + ` |
| deploy / pipeline / release / rollback / DORA | ` + "`deploy-guardian`" + ` |
| CI/CD review / pipeline audit / github actions | ` + "`pipeline-auditor`" + ` |
| CVE / vuln / security scan / SAST / DAST | ` + "`vuln-scanner`" + ` |
| threat / attack / MITRE / advisory / zero-day | ` + "`threat-monitor`" + ` |
| IAM / permission / access / privilege / stale creds | ` + "`access-auditor`" + ` |
| audit infra / review manifest / k8s security / dockerfile | ` + "`security-reviewer`" + ` |
| dependency / npm / pip / go.sum / supply chain / license | ` + "`dependency-scanner`" + ` |
| log / trace / kibana / loki / cloudwatch / log pattern | ` + "`log-analyzer`" + ` |

If a task spans multiple agents, handle the primary concern first then call the secondary agent.

## Approach
- Think before acting. Read existing files before writing code.
- Be concise in output but thorough in reasoning.
- Prefer editing over rewriting whole files.
- Do not re-read files you have already read unless the file may have changed.
- Test your code before declaring done.
- No sycophantic openers or closing fluff.
- Keep solutions simple and direct.
- User instructions always override this file.

## Output
- Return code first. Explanation after, only if non-obvious.
- No inline prose. Use comments sparingly - only where logic is unclear.
- No boilerplate unless explicitly requested.

## Code Rules
- Simplest working solution. No over-engineering.
- No abstractions for single-use operations.
- No speculative features or "you might also want..."
- Read the file before modifying it. Never edit blind.
- No docstrings or type annotations on code not being changed.
- No error handling for scenarios that cannot happen.
- Three similar lines is better than a premature abstraction.

## Review Rules
- State the bug. Show the fix. Stop.
- No suggestions beyond the scope of the review.
- No compliments on the code before or after the review.

## Debugging Rules
- Never speculate about a bug without reading the relevant code first.
- State what you found, where, and the fix. One pass.
- If cause is unclear: say so. Do not guess.

## Debugging Rules
- Never speculate about a bug without reading the relevant code first.
- State what you found, where, and the fix. One pass.
- If cause is unclear: say so. Do not guess.

## Approach
- Think before acting. Read existing files before writing code.
- Be concise in output but thorough in reasoning.
- Prefer editing over rewriting whole files.
- Do not re-read files you have already read unless the file may have changed.

## Code Rules
- Simplest working solution. No over-engineering.
- No abstractions for single-use operations.
- No speculative features or "you might also want..."
- Read the file before modifying it. Never edit blind.

## Formatting
- No em dashes, smart quotes, or decorative Unicode symbols.
- Plain hyphens and straight quotes only.

## User Context
- Thanapat Taweerat - Senior DevOps/DevSecOps Engineer, 7+ years infra/platform experience.
- Current role: Lotus's (CP AXTRA / CP Group), leading AWS-to-Tencent Cloud migration.
- Deep expertise: Kubernetes, Terraform, Ansible, ArgoCD, HashiCorp Vault, ELK, Grafana/Prometheus, AWS, Tencent Cloud.
- Dev background: Node.js/Express, Go, PHP/Laravel. Understands both app and platform layers.
- Education: Computer Engineering, KMITL 2018.
- Skip basics. Go straight to trade-offs, gotchas, and production-relevant detail.

## Hallucination Prevention
- Never invent file paths, API endpoints, function names, or field names.
- If a value is unknown: return null or "UNKNOWN". Never guess.
- If a file or resource was not read: do not reference its contents.
- Accuracy over completeness.

## Token Efficiency
- Pipeline calls compound. Every token saved per call multiplies across runs.
- No explanatory text in agent output unless a human will read it.
- Return the minimum viable output that satisfies the task spec.

## Simple Formatting
- No em dashes, smart quotes, or decorative Unicode symbols.
- Plain hyphens and straight quotes only.
- Natural language characters (accented letters, CJK, etc.) are fine when the content requires them.
- Code output must be copy-paste safe.

## Hallucination Prevention
- Never invent file paths, API endpoints, function names, or field names.
- If a value is unknown: return null or "UNKNOWN". Never guess.
- If a file or resource was not read: do not reference its contents.
- Accuracy over completeness.

## Formatting
- No em dashes, smart quotes, or decorative Unicode symbols.
- Plain hyphens and straight quotes only.



`
}

func buildRealisticUserMessage() string {
	return "Can you check the Kubernetes deployment logs for the api-gateway service and find why it's returning 502 errors?"
}
