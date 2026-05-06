package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

// visionAnalysisPrompt is the system prompt from @z_ai/mcp-server's GENERAL_IMAGE_ANALYSIS_PROMPT.
// This prompt produces accurate image descriptions with glm-4.6v when called via
// OpenAI chat completions format with thinking mode enabled.
const visionAnalysisPrompt = `You are an advanced AI vision assistant with comprehensive image understanding capabilities. Your strength lies in being adaptable—you can analyze any visual content and provide insights tailored to what the user specifically needs, whether that's identifying objects, understanding context, extracting information, or offering detailed descriptions.

<task>
Your task is to analyze the provided image according to the user's specific instructions and provide a detailed, accurate response that addresses their needs. Since this is a general-purpose tool, your analysis approach should be guided by what the user is asking for rather than following a predetermined template.
</task>

<approach>
Begin by carefully examining the entire image to understand what it contains. Identify all significant elements—objects, people, text, symbols, backgrounds, and any other visual components. Notice the composition, layout, and how elements relate to each other. Understand the context—what type of image is this, and what might be its purpose or origin?

Pay close attention to the user's specific request in their prompt. What exactly are they asking you to do? Are they asking you to:
- Identify or describe something specific in the image?
- Analyze the image for certain characteristics or qualities?
- Extract specific information or data visible in the image?
- Understand the context or meaning behind what's shown?
- Compare elements within the image?
- Make inferences or draw conclusions from what you observe?

Tailor your analysis depth and focus to match their request. If they're asking about a specific detail, focus on that detail while providing necessary context. If they're asking for a comprehensive overview, be thorough and systematic. If they're asking a specific question, answer it directly and provide supporting observations.

Consider the details that matter for the user's specific need. If analyzing visual aesthetics, pay attention to colors, composition, lighting, and style. If extracting information, be precise and systematic in capturing all relevant data. If identifying objects or elements, be specific about what you see and where it is located.

Be accurate and honest in your observations. Only state what you can confidently observe in the image. If something is unclear, ambiguous, or outside your ability to determine from the visual alone, indicate this rather than guessing. Distinguish between direct observations (what you can clearly see) and inferences (what you deduce based on context or common patterns).

Provide context and explanation where helpful. Don't just list observations—help the user understand what they mean or why they matter. If you notice something significant or interesting beyond what they specifically asked about, mention it, as it might be valuable to them.

Organize your response logically based on the user's request. If they asked a straightforward question, answer it clearly first before providing supporting details. If they asked for a comprehensive analysis, structure your response in a way that builds understanding progressively.
</approach>

<output_structure>
Structure your response to be clear and immediately useful:

Start with a **Main Response** section that directly addresses the user's request. Answer their question, provide the analysis they asked for, or extract the information they need. Be clear and specific.

Follow with **Detailed Observations** that provide relevant details supporting your main response or offering additional context. Organize these logically—perhaps by location in the image, by category of observation, or by importance. Include specific details that enhance understanding or might be useful for the user's purpose.

If appropriate, include a **Context & Analysis** section where you interpret what you've observed or provide insights. This is where you move beyond pure description to understanding. What does the image suggest or communicate? What patterns or relationships do you notice? What conclusions can be drawn?

If there are other observations that might be valuable but weren't directly requested, include them in an **Additional Notes** section.
</output_structure>

Your goal is to be genuinely helpful by providing exactly the information and analysis the user needs, presented in a clear, organized, and insightful manner. Adapt your response to their specific situation rather than forcing their request into a predetermined format.`

// extractVisionContent extracts base64 image data URIs and user text from the last user message.
func extractVisionContent(payload map[string]any) (imageURIs []string, userText string, err error) {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return nil, "", fmt.Errorf("no messages in payload")
	}

	// Find last user message.
	var lastUserMsg map[string]any
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			lastUserMsg = m
			break
		}
	}
	if lastUserMsg == nil {
		return nil, "", fmt.Errorf("no user message found")
	}

	content, ok := lastUserMsg["content"].([]any)
	if !ok {
		// Content might be a plain string.
		if s, ok := lastUserMsg["content"].(string); ok {
			return nil, s, nil
		}
		return nil, "", fmt.Errorf("user message has no content")
	}

	for _, block := range content {
		cb, ok := block.(map[string]any)
		if !ok {
			continue
		}
		t, _ := cb["type"].(string)

		switch t {
		case "image":
			// Anthropic format: source.data + source.media_type
			src, _ := cb["source"].(map[string]any)
			if src != nil {
				data, _ := src["data"].(string)
				mediaType, _ := src["media_type"].(string)
				if data != "" {
					if mediaType == "" {
						mediaType = "image/jpeg"
					}
					imageURIs = append(imageURIs, "data:"+mediaType+";base64,"+data)
				}
			}
		case "image_url":
			// OpenAI/Z.AI format: image_url.url
			iu, _ := cb["image_url"].(map[string]any)
			if iu != nil {
				url, _ := iu["url"].(string)
				if url != "" {
					if strings.HasPrefix(url, "http") {
						// External URL: download and convert to base64.
						b64 := proxy.FetchImageAsBase64(url)
						if b64 == "" {
							continue
						}
						imageURIs = append(imageURIs, b64)
					} else {
						imageURIs = append(imageURIs, url)
					}
				}
			}
		case "text":
			if txt, _ := cb["text"].(string); txt != "" {
				userText += txt
			}
		}
	}

	if len(imageURIs) == 0 {
		return nil, userText, fmt.Errorf("no images found in last user message")
	}
	return imageURIs, userText, nil
}

// callVisionAnalysis sends images to Zhipu's vision API with MCP-style parameters
// and returns the text description. Non-streaming, synchronous call.
func (h *Handler) callVisionAnalysisSingle(ctx context.Context, imageURIs []string, userPrompt string, apiKey string) (string, error) {
	// Build OpenAI-format content blocks.
	contentBlocks := make([]any, 0, len(imageURIs)+1)
	for _, uri := range imageURIs {
		contentBlocks = append(contentBlocks, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": uri},
		})
	}
	if userPrompt == "" {
		userPrompt = "Describe this image in detail."
	}
	contentBlocks = append(contentBlocks, map[string]any{
		"type": "text",
		"text": userPrompt,
	})

	reqBody := map[string]any{
		"model": h.cfg.VisionPreAnalysisModel,
		"messages": []any{
			map[string]any{"role": "system", "content": visionAnalysisPrompt},
			map[string]any{"role": "user", "content": contentBlocks},
		},
		"stream":      false,
		"max_tokens":  h.cfg.VisionPreAnalysisMaxTokens,
		"temperature": h.cfg.VisionPreAnalysisTemp,
		"top_p":       h.cfg.VisionPreAnalysisTopP,
	}
	if h.cfg.VisionPreAnalysisThinking {
		reqBody["thinking"] = map[string]any{"type": "enabled"}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal vision request: %w", err)
	}

	timeout := h.cfg.VisionPreAnalysisTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.NativeVisionURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create vision request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "4.5V MCP Local")
	req.Header.Set("Accept-Language", "en-US,en")

	resp, err := proxy.SharedClient(timeout).Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vision response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision API returned %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 500)]))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse vision response: %w", err)
	}

	choices, _ := result["choices"].([]any)
	if len(choices) == 0 {
		return "", fmt.Errorf("vision API returned no choices")
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	content, _ := message["content"].(string)
	if content == "" {
		return "", fmt.Errorf("vision API returned empty content")
	}

	return content, nil
}

// callVisionAnalysisParallel analyzes each image in parallel and returns combined descriptions.
// No concurrency cap - partial failures are handled gracefully.
func (h *Handler) callVisionAnalysisParallel(ctx context.Context, imageURIs []string, userPrompt string, apiKey string) (string, error) {
	if len(imageURIs) == 1 {
		return h.callVisionAnalysisSingle(ctx, imageURIs, userPrompt, apiKey)
	}

	type result struct {
		idx  int
		text string
		err  error
		ms   int64
	}

	var wg sync.WaitGroup
	results := make(chan result, len(imageURIs))

	for i, uri := range imageURIs {
		wg.Add(1)
		go func(idx int, imageURI string) {
			defer wg.Done()
			start := time.Now()
			imgLen := len(imageURI)
			desc, err := h.callVisionAnalysisSingle(ctx, []string{imageURI}, userPrompt, apiKey)
			elapsed := time.Since(start).Milliseconds()
			slog.Info("vision pre-analysis: single image result", "image_idx", idx, "image_bytes", imgLen, "duration_ms", elapsed, "error", err)
			results <- result{idx: idx, text: desc, err: err, ms: elapsed}
		}(i, uri)
	}

	wg.Wait()
	close(results)

	var parts []string
	var errs []string
	var maxMs int64
	for r := range results {
		if r.ms > maxMs {
			maxMs = r.ms
		}
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("image[%d]: %v", r.idx, r.err))
			continue
		}
		if len(imageURIs) > 1 {
			parts = append(parts, fmt.Sprintf("[Image %d]: %s", r.idx+1, r.text))
		} else {
			parts = append(parts, r.text)
		}
	}

	slog.Info("vision pre-analysis: parallel results",
		"total_images", len(imageURIs),
		"success", len(parts),
		"failed", len(errs),
		"wall_ms", maxMs,
	)

	if len(errs) == len(imageURIs) {
		return "", fmt.Errorf("all %d image analyses failed: %s", len(imageURIs), strings.Join(errs, "; "))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no successful image analyses")
	}

	combined := strings.Join(parts, "\n\n")
	if len(errs) > 0 {
		combined += "\n\n[Note: some images failed to analyze: " + strings.Join(errs, "; ") + "]"
	}
	return combined, nil
}

// replaceImagesWithDescription replaces image content blocks in the last user message
// with a text block containing the vision description. Returns count of replaced images.
func replaceImagesWithDescription(payload map[string]any, description string, originalText string) int {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return 0
	}

	replaced := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "user" {
			continue
		}

		content, ok := m["content"].([]any)
		if !ok {
			continue
		}

		hasImages := false
		for _, block := range content {
			cb, ok := block.(map[string]any)
			if !ok {
				continue
			}
			t, _ := cb["type"].(string)
			if t == "image" || t == "image_url" {
				hasImages = true
				break
			}
		}
		if !hasImages {
			continue
		}

		// Build replacement text.
		var newText string
		if originalText != "" {
			newText = originalText + "\n\n[Image Analysis]: " + description
		} else {
			newText = "[Image Analysis]: " + description
		}

		m["content"] = []any{
			map[string]any{"type": "text", "text": newText},
		}
		replaced = len(content) // all blocks replaced
		break
	}
	return replaced
}

// stripOldImageDescriptions removes [Image Analysis]: blocks from all user messages
// except the last one. This reduces token usage in subsequent requests while
// keeping the current image description intact.
func stripOldImageDescriptions(payload map[string]any) int {
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return 0
	}

	stripped := 0
	for i := 0; i < len(msgs)-1; i++ {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "user" {
			continue
		}

		content, ok := m["content"]
		if !ok {
			continue
		}

		switch v := content.(type) {
		case string:
			if idx := strings.Index(v, "\n\n[Image Analysis]: "); idx >= 0 {
				m["content"] = v[:idx]
				stripped++
			} else if idx := strings.Index(v, "[Image Analysis]: "); idx >= 0 {
				if idx == 0 {
					m["content"] = ""
				} else {
					m["content"] = v[:idx]
				}
				stripped++
			}
		case []any:
			for _, block := range v {
				cb, ok := block.(map[string]any)
				if !ok || cb["type"] != "text" {
					continue
				}
				txt, _ := cb["text"].(string)
				if idx := strings.Index(txt, "\n\n[Image Analysis]: "); idx >= 0 {
					cb["text"] = txt[:idx]
					stripped++
				} else if idx := strings.Index(txt, "[Image Analysis]: "); idx >= 0 {
					if idx == 0 {
						cb["text"] = ""
					} else {
						cb["text"] = txt[:idx]
					}
					stripped++
				}
			}
		}
	}
	return stripped
}

// preAnalyzeImages performs MCP-style vision pre-analysis on the request payload.
// Extracts images, calls Zhipu vision API directly, and replaces image blocks
// with text descriptions. Returns modified body, the original model for main routing,
// and whether analysis succeeded. On failure, returns original body (graceful degradation).
func (h *Handler) preAnalyzeImages(r *http.Request, payload map[string]any, body []byte, apiKey string, originalModel string) (newBody []byte, model string, analyzed bool) {
	imageURIs, userText, err := extractVisionContent(payload)
	if err != nil {
		slog.Warn("vision pre-analysis: extract failed", "error", err)
		return body, originalModel, false
	}

	slog.Info("vision pre-analysis: calling API",
		"model", h.cfg.VisionPreAnalysisModel,
		"images", len(imageURIs),
		"userText_len", len(userText),
	)

	start := time.Now()
	description, err := h.callVisionAnalysisParallel(r.Context(), imageURIs, userText, apiKey)

	if err != nil {
		slog.Warn("vision pre-analysis: API call failed, replacing images with error text", "error", err, "duration_ms", time.Since(start).Milliseconds())
		h.metrics.RecordVisionPreAnalysis(false, time.Since(start).Seconds())

		// Don't fall back to direct proxy (which also fails for images).
		// Replace images with a text placeholder so the main model can still respond.
		errText := "[Image Analysis]: Unable to analyze the provided image(s). Error: " + err.Error()
		if userText != "" {
			errText = userText + "\n\n" + errText
		}
		replaceImagesWithDescription(payload, errText, userText)
		stripOldImageDescriptions(payload)

		newBody, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return body, originalModel, false
		}
		return newBody, originalModel, true
	}

	replaced := replaceImagesWithDescription(payload, description, userText)
	stripped := stripOldImageDescriptions(payload)

	newBody, err = json.Marshal(payload)
	if err != nil {
		slog.Warn("vision pre-analysis: marshal failed", "error", err)
		return body, originalModel, false
	}

	slog.Info("vision pre-analysis completed",
		"vision_model", h.cfg.VisionPreAnalysisModel,
		"main_model", originalModel,
		"images_replaced", replaced,
		"old_descriptions_stripped", stripped,
		"description_len", len(description),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	h.metrics.RecordVisionPreAnalysis(true, time.Since(start).Seconds())

	return newBody, originalModel, true
}
