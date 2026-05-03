# Z.AI Flexible GLM Routing

## Overview

The gateway supports dual-endpoint routing for Z.AI (GLM) models:

- **Anthropic-compatible** (`api.z.ai/api/anthropic`): Default for most GLM models
- **OpenAI-compatible** (`api.z.ai/api/paas/v4/chat/completions`): For models that don't support Anthropic format

Routing is controlled by the `ZAI_OPENAI_MODELS` env var. Any GLM model listed there automatically routes through the OpenAI endpoint, no code changes needed.

## Architecture

```
Client Request
      |
      v
  handler.go (Messages)
      |
      +-- is GLM model? --> check ZAI_OPENAI_MODELS config
      |       |
      |       +-- YES (e.g., glm-5v-turbo) --> openaiProxy.ProxyOpenAI()
      |       |       --> ZAI_OPENAI_URL (api.z.ai/api/paas/v4/chat/completions)
      |       |
      |       +-- NO (e.g., glm-5.1) --> trySidecarOrDirect()
      |               --> UPSTREAM_URL (api.z.ai/api/anthropic)
      |
      +-- has images + GLM?
              |
              +-- model in ZAI_OPENAI_MODELS? --> openaiProxy (vision)
              +-- isNativeImageModel? --> anthropic proxy (no auto-select)
              +-- neither? --> auto-select vision model (glm-4.6v/4.6v-flashx)
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ZAI_OPENAI_URL` | `https://api.z.ai/api/paas/v4/chat/completions` | OpenAI-compatible endpoint URL |
| `ZAI_OPENAI_MODELS` | `glm-5v-turbo` | Comma-separated list of GLM models to route via OpenAI endpoint |
| `UPSTREAM_VISION_MODEL_LIMITS` | `glm-5.1:5,glm-4.6v:5,glm-4.5v:3,glm-4.6v-flashx:3,glm-5v-turbo:5` | Per-model vision concurrency limits |

### Adding a Model to OpenAI Routing

No code changes needed. Just add the model name to `ZAI_OPENAI_MODELS` in `.env`:

```env
ZAI_OPENAI_MODELS=glm-5v-turbo,glm-ocr,glm-future-model
```

### Example: Enable glm-5v-turbo

```env
# Already the default
ZAI_OPENAI_MODELS=glm-5v-turbo
ZAI_OPENAI_URL=https://api.z.ai/api/paas/v4/chat/completions
```

## Model Availability

### Works via api.z.ai (international endpoint)

| Model | Format | Pricing ($/1M tok) | Context | Vision | Route |
|---|---|---|---|---|---|
| glm-5.1 | Anthropic | $1.40/$4.40 | 128K | Yes | Anthropic (default) |
| glm-5 | Anthropic | $1.00/$3.20 | 128K | No | Anthropic (default) |
| glm-5-turbo | Anthropic | $1.20/$4.00 | 128K | No | Anthropic (default) |
| glm-5v-turbo | **OpenAI** | $1.20/$4.00 | **200K** | Yes | OpenAI (ZAI_OPENAI_MODELS) |
| glm-4.7 | Anthropic | $0.60/$2.20 | 128K | No | Anthropic (default) |
| glm-4.7-flashx | Anthropic | $0.07/$0.40 | 128K | No | Anthropic (default) |
| glm-4.6 | Anthropic | $0.60/$2.20 | 128K | No | Anthropic (default) |
| glm-4.5 | Anthropic | $0.60/$2.20 | 128K | No | Anthropic (default) |
| glm-4.5-x | Anthropic | $2.20/$8.90 | 128K | No | Anthropic (default) |
| glm-4.5-air | Anthropic | $0.20/$1.10 | 128K | No | Anthropic (default) |
| glm-4.5-airx | Anthropic | $1.10/$4.50 | 128K | No | Anthropic (default) |
| glm-4.6v | Anthropic | $0.30/$0.90 | 128K | Yes | Anthropic (vision) |
| glm-4.6v-flashx | Anthropic | $0.04/$0.40 | 128K | Yes | Anthropic (vision) |
| glm-4.5v | Anthropic | $0.60/$1.80 | 128K | Yes | Anthropic (vision) |
| glm-ocr | **OpenAI** | $0.03/$0.03 | 128K | Yes | Add to ZAI_OPENAI_MODELS |

### Domestic-only (open.bigmodel.cn, NOT on api.z.ai)

These models exist on the Chinese domestic endpoint only and are NOT accessible via the international api.z.ai API key:

| Model | Pricing | Context | Notes |
|---|---|---|---|
| glm-4-plus | ~$1.40/$5.70 | 128K | Legacy flagship |
| glm-4-long | ~$0.10/$0.10 | **1M** | Long-context |
| glm-z1-air/airx/flashx | Reasoning | - | Z1 reasoning series |
| glm-4v-plus-0111 | - | - | Multimodal (5 images + video) |
| codegeex-4 | - | - | Code completion |
| glm-4.1v-thinking-flashx | - | - | Unconfirmed |
| glm-4-assistant | - | - | Unconfirmed |

## Bugs Fixed

### VisionModelLimits not loaded from env

**File**: `config/config.go`
**Bug**: `VisionModelLimits` field was declared (line 34) but never populated in `Load()`.
**Fix**: Added `VisionModelLimits: parseModelLimits(envOr(...))` line after `ModelLimits` loading.

## Reference URLs

- Z.AI pricing: https://docs.z.ai/guides/overview/pricing
- Z.AI model overview: https://docs.z.ai/guides/overview/models
- GLM-5V-Turbo docs: https://docs.z.ai/guides/overview/models/glm-5v-turbo
- Zhipu domestic models: https://open.bigmodel.cn/dev/howuse/model
- Zhipu API reference: https://open.bigmodel.cn/dev/api/normal-model/glm-4

## Code Changes

| File | Change |
|---|---|
| `config/config.go` | Added `ZAIOpenAIURL`, `ZAIOpenAIModels` fields, `parseModelSet()`, fixed VisionModelLimits loading, updated pricing |
| `handler/handler.go` | Added ZAI OpenAI routing branch (text + vision), updated knownModels catalog, modelMaxTokens, isNativeImageModel |
