# Z.AI Vision Routing -- Image Format Bug Fix

## Problem: Sending images to Z.AI results in 400 "Invalid API parameter" (code 1210)

## Root Cause

`filterUnsupportedContent()` converts image blocks from Anthropic format to GLM native format because initially vision requests were sent to `open.bigmodel.cn` (Zhipu native) which requires GLM format. After changing the route to send all Z.AI vision to `api.z.ai` (Anthropic-compatible endpoint), this format conversion causes Z.AI to reject the request because this endpoint only accepts Anthropic format.

## Before (bug)

```
Client (Claude Code)
|
| POST /v1/messages
| messages: [{"role":"user","content":[
|   {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}},
|   {"type":"text","text":"What color?"}
| ]}]
|
v
Gateway handler.go
|
+- filterUnsupportedContent() <- line 735
|  image block: type "image" -> type "image_url" <- BROKEN
|  {"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}
|
+- HasImageContent() -> true
+- selectVisionModel() -> "glm-4.6v"
|
v
trySidecarOrDirect()
|
v
api.z.ai/v1/messages <- Anthropic-compatible endpoint
|
X 400 "Invalid API parameter"
  doesn't recognize type "image_url" -> must be type "image"
```

## After (fix)

```
Client (Claude Code)
|
| POST /v1/messages
| messages: [{"role":"user","content":[
|   {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}},
|   {"type":"text","text":"What color?"}
| ]}]
|
v
Gateway handler.go
|
+- filterUnsupportedContent() <- line 735
|  only removes "server_tool_use" blocks
|  image block: no conversion, keep type "image" <- FIXED
|
+- HasImageContent() -> true
+- selectVisionModel() -> "glm-4.6v"
|
v
trySidecarOrDirect()
|
v
api.z.ai/v1/messages <- Anthropic-compatible endpoint
|
OK 200 {"model":"glm-4.6v","content":[{"type":"text","text":"blue"}]}
```

## Changes

| Point | Before | After |
|---|---|---|
| `filterUnsupportedContent()` | Removes `server_tool_use` + converts `image` to `image_url` | Removes `server_tool_use` only |
| Vision endpoint | Sent to `open.bigmodel.cn` (GLM native) + format conversion | Sent to `api.z.ai` (Anthropic-compatible) + no format conversion |
| Image format received by upstream | `image_url` (GLM) -> 400 error | `image` (Anthropic) -> 200 OK |

## Related Files

| File | Relevant Section |
|---|---|
| `api-gateway/handler/handler.go` | `filterUnsupportedContent()`, `HasImageContent()`, vision routing block |
| `api-gateway/proxy/anthropic.go` | `HasImageContent()`, `rewriteImageToGLMFormat()` (no longer called) |
| `api-gateway/config/config.go` | `VisionModelLimits`, `UPSTREAM_VISION_MODEL_LIMITS` |
| `api-gateway/middleware/adaptive_limiter.go` | Vision limits separated from language limits |

## Lesson Learned

- When changing upstream endpoint, verify that middleware transforming payload is still correct
- `filterUnsupportedContent` was written only for Zhipu native (`open.bigmodel.cn`)
- After migrating to api.z.ai (Anthropic-compatible), no format conversion needed because client already sends Anthropic format

## Image Compression: WebP Bug Fix

### Problem

Image responses from GLM models were incorrect (hallucinated content). Root cause: gateway compressed images to WebP format (`bimg.WEBP`) but Zhipu GLM models cannot interpret WebP data, resulting in the model hallucinating about image content.

### Symptoms

- Model describes image content that doesn't match the actual image
- Model may return image URL instead of analyzing content
- Affects all image types (PNG, JPEG) after WebP re-encoding

### Fix

Changed compression output from `bimg.WEBP` to `bimg.JPEG` with a size guard:

```go
// Before: WebP (GLM can't read)
newImage, err := img.Process(bimg.Options{Quality: quality, Type: bimg.WEBP})
src["media_type"] = "image/webp"

// After: JPEG (universal compatibility) + size guard
newImage, err := img.Process(bimg.Options{Quality: quality, Type: bimg.JPEG})
if len(newData) >= len(data) { continue }  // skip if compression increased size
src["media_type"] = "image/jpeg"
```

Also fixed: Prometheus `counter cannot decrease` panic when JPEG compression produced larger output than input (savedBytes < 0). The size guard ensures `RecordImageCompression` is only called with positive saved values.

### Files Changed

| File | Change |
|---|---|
| `handler/handler.go` | `bimg.WEBP` -> `bimg.JPEG`, added size guard, `image/webp` -> `image/jpeg` |
| `metrics/metrics.go` | `ImageCompressions`, `ImageBytesSaved`, `ImageBytesOriginal` counters |

## Additional Routing: ZAI OpenAI Models

Some Z.AI models (configured via `ZAI_OPENAI_MODELS` env var) are routed through the OpenAI-compatible endpoint (`ZAI_OPENAI_URL`) instead of the Anthropic-compatible endpoint. For these models, `AnthropicToOpenAI()` conversion is applied, including image block conversion via `convertImageBlock()` in `proxy/anthropic.go`. This path is separate from the standard vision routing described above.
