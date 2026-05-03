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
+- filterUnsupportedContent() <- line 734
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
+- filterUnsupportedContent() <- line 734
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
