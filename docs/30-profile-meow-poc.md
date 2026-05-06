# Profile: meow

| Field | Value |
|-------|-------|
| Target | gemini-oauth |
| Accounts | 1 |
| Default Model | gemini-2.5-flash |

## Available Models

| Model | Context | Thinking | $/1M In | $/1M Out | Usable |
|-------|---------|----------|---------|----------|--------|
| gemini-2.5-flash | 1M | budget | $0.15 | $0.60 | Yes |
| gemini-2.5-pro | 1M | budget | $1.25 | $10.00 | No (429) |

## Feature Support (gemini-2.5-flash)

| Feature | Result | Detail |
|---------|--------|--------|
| Chat | Pass | |
| Streaming | Fail | Gateway: streaming not supported for gemini-oauth |
| Thinking | Skip | 429 before testable |
| Vision | Partial | Accepts base64 input, returns empty text |
| Tool use | Pass | Anthropic schema auto-converted to Gemini |
| Multi-turn | Pass | Context retained across turns |
| Concurrency | Fail | 1 req per ~30-40s per account |

## Rate Limit

- Quota resets every ~30-40s per account
- 429 error: `RESOURCE_EXHAUSTED` - "You have exhausted your capacity on this model"
- Gateway cooldown: 2 min on 429
- Throughput with 1 account: ~1-2 req/min

## Usage

```bash
curl -X POST https://ai.klxhub.com/v1/messages \
  -H "x-api-key: arl_<token>" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "max_tokens": 50,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```
