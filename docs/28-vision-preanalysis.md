# Vision Pre-Analysis Pipeline

## Overview

Vision pre-analysis converts image content into accurate text descriptions before the main model processes the request. This solves GLM's inability to correctly analyze images through the Anthropic-format proxy layer.

**Two models involved:**
- **Vision model**: `glm-4.6v` - Zhipu's vision model, called directly via OpenAI chat completions API
- **Main model**: `glm-5`, `glm-5.1`, or whatever the user requested - receives text-only payload after pre-analysis

## Architecture

```
Claude Code (client)
  │
  │ POST /v1/messages (base64 images in payload)
  ▼
┌──────────────────────────────────────────────────────────────────┐
│ API Gateway                                                      │
│                                                                  │
│  1. Parse & detect images (hasImages=true)                       │
│              │                                                   │
│  2. compressLargeImages()                                        │
│     - JPEG quality 75                                            │
│     - Max dimension 1600px                                       │
│     - Compress only if > threshold                               │
│              │                                                   │
│  3. Route to Z.AI provider? (decision.ProviderID == "zai")       │
│     └─ YES + VISION_PRE_ANALYSIS_ENABLED                         │
│              │                                                   │
│  4. preAnalyzeImages() ─────────────────────────────────────┐    │
│     │                                                        │   │
│     ├─ extractVisionContent()                                │   │
│     │   - Extract base64 URIs from last user message         │   │
│     │   - Extract user text prompt                           │   │
│     │                                                        │   │
│     ├─ callVisionAnalysisParallel() ─── goroutine per image  │   │
│     │   │                                                    │   │
│     │   ├── goroutine 0 ──► Zhipu Vision API (glm-4.6v)     │    │
│     │   ├── goroutine 1 ──► Zhipu Vision API (glm-4.6v)     │    │
│     │   ├── goroutine 2 ──► Zhipu Vision API (glm-4.6v)     │    │
│     │   ├── goroutine 3 ──► Zhipu Vision API (glm-4.6v)     │    │
│     │   └── goroutine N ──► Zhipu Vision API (glm-4.6v)     │    │
│     │       │                                                │   │
│     │       │  OpenAI chat completions format                │   │
│     │       │  Headers: X-Title, Accept-Language             │   │
│     │       │  stream: false                                 │   │
│     │       │  max_tokens: 8192                              │   │
│     │       │  thinking: disabled                            │   │
│     │       ▼                                                │   │
│     │   Collect results (partial failures OK)                │   │
│     │   Combine: [Image 1]: desc\n\n[Image 2]: desc...      │    │
│     │                                                        │   │
│     ├─ replaceImagesWithDescription()                        │   │
│     │   - Replace image blocks with text:                    │   │
│     │     "user text\n\n[Image Analysis]: combined desc"     │   │
│     │                                                        │   │
│     ├─ stripOldImageDescriptions()                           │   │
│     │   - Remove [Image Analysis]: from older messages       │   │
│     │   - Keep only the latest (saves tokens on next turn)   │   │
│     │                                                        │   │
│     └─ Return text-only payload                              │   │
│              │                                                   │
│  5. hasImages = false → normal text routing                      │
│     (optimizer, privacy, upstream proxy)                         │
│                                                                  │
│  6. Forward to main model (glm-5, glm-5.1, etc)                  │
│     - Sees only text, no images                                  │
│     - Prompt injection tells model to trust [Image Analysis]:    │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
  │
  ▼
Z.AI Anthropic API (main model)
```

## Single Image Vision API Call

Each parallel goroutine makes this API call to Zhipu:

```
POST https://open.bigmodel.cn/api/paas/v4/chat/completions

Authorization: Bearer <api_key>
Content-Type: application/json
X-Title: 4.5V MCP Local
Accept-Language: en-US,en

{
  "model": "glm-4.6v",
  "messages": [
    {"role": "system", "content": "<visionAnalysisPrompt>"},
    {"role": "user", "content": [
      {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}},
      {"type": "text", "text": "<user prompt>"}
    ]}
  ],
  "stream": false,
  "max_tokens": 8192,
  "temperature": 0.8,
  "top_p": 0.6
}
```

The system prompt is copied verbatim from `@z_ai/mcp-server`'s `GENERAL_IMAGE_ANALYSIS_PROMPT` which produces accurate descriptions.

## Error Handling

```
preAnalyzeImages()
  │
  ├─ extractVisionContent fails (no images, no user msg)
  │   └─ return original body, analyzed=false → direct proxy fallback
  │
  ├─ callVisionAnalysisParallel()
  │   │
  │   ├─ ALL images fail
  │   │   └─ Replace images with error text placeholder
  │   │      "[Image Analysis]: Unable to analyze. Error: ..."
  │   │      → Main model still gets user text, can respond
  │   │      → User does NOT need to resend
  │   │
  │   └─ SOME images fail
  │       └─ Use successful descriptions + note about failures
  │          "[Image 1]: ...\n\n[Image 2]: ...\n\n[Note: image 3 failed: ...]"
  │
  ├─ json.Marshal fails
  │   └─ return original body, analyzed=false → direct proxy fallback
  │
  └─ Success
      └─ return text-only payload, analyzed=true
```

## Configuration

| Env Var                          | Default    | Description                         |
|----------------------------------|------------|-------------------------------------|
| `VISION_PRE_ANALYSIS_ENABLED`    | `true`     | Enable/disable pre-analysis         |
| `VISION_PRE_ANALYSIS_MODEL`      | `glm-4.6v` | Vision model for analysis           |
| `VISION_PRE_ANALYSIS_MAX_TOKENS` | `8192`     | Max output tokens per description   |
| `VISION_PRE_ANALYSIS_TEMP`       | `0.8`      | Temperature for vision API          |
| `VISION_PRE_ANALYSIS_TOP_P`      | `0.6`      | Top-P for vision API                |
| `VISION_PRE_ANALYSIS_THINKING`   | `false`    | Enable thinking mode (adds latency) |
| `VISION_PRE_ANALYSIS_TIMEOUT`    | `120s`     | Per-call timeout                    |

## Performance

### Before (sequential, thinking ON)
- 5 images: **48s** (single API call with all images)

### After (parallel, thinking OFF)
- 5 images: **24s** wall time (5 concurrent API calls)
- Per-image: 10-24s depending on complexity
- Savings: ~75% vs sequential with same params

### Why thinking=false
Thinking mode generates hidden reasoning tokens before the actual description. For image description tasks:
- ON: more detailed but 2-3x slower (48s for 5 images)
- OFF: accurate enough for code/UI screenshots, much faster

### Why parallel
Zhipu vision API processes one image per call at ~10-24s. Sending N images sequentially = N * avg_time. Sending N parallel = max_time (time of slowest single call).

## Token Optimization

`stripOldImageDescriptions()` removes `[Image Analysis]:` blocks from all user messages except the latest. This prevents description text from accumulating across conversation turns:

```
Turn 1: [Image Analysis]: 3000 chars description  ← kept
Turn 2: [Image Analysis]: 3000 chars description  ← kept, Turn 1 stripped
Turn 3: text only                                   ← no stripping needed
```

Without stripping, a 10-turn conversation with images would carry 30,000+ chars of stale descriptions.

## Files

| File                   | Purpose                                                 |
|------------------------|---------------------------------------------------------|
| `handler/vision.go`    | Pre-analysis logic (extract, analyze, replace, strip)   |
| `handler/handler.go`   | Pipeline integration (compress → pre-analyze → route)   |
| `config/config.go`     | Configuration with env var defaults                     |
