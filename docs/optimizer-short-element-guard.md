# Short-Element Skip Guard - 2026-05-15

## Background

System prompt optimizer (`OptimizeSystemPrompt`) รัน 8+ stages: `semantic_dedup`, `chunker`, `delta`, `sketch`, `summarizer`, `textcomp`, `caveman`, `pordee`

หลังจาก array-aware refactor (2026-05-15) optimizer ทำงานแบบ per-element บน `system` array แทนที่จะ flatten เป็น string เดียว

Claude Code request แต่ละอันมี 3-4 system elements:

| Element | Length (chars) | Content |
|---------|---------------|---------|
| Privacy prompt | ~91 | `"...[[ENV_USER_3]]. You MUST preserve every token EXACTLY as written."` |
| Billing header | ~62 | `x-anhropic-billing-header` สำหรับ Bucket 3 rate limits |
| Claude identity | ~94 | Model identity string |
| System prompt | ~27,967 | Main system prompt (the only element worth optimizing) |

## Problem

### semantic_dedup ทำลาย privacy prompt

PoC ยืนยันว่า `semantic_dedup` CORRUPT privacy prompt - ลบ spaces, เพิ่มตัวอักษรเกิน:

```
Before: "...[[ENV_USER_3]]. You MUST preserve every token EXACTLY as written."
After:  "...[[ENV_USER_3]].You MUST preserve every token EXACTLY as writtenn"
                                                         ^space gone      ^extra 'n'
```

Privacy prompt เป็น machine-readable string ที่ต้อง preserve ทุกตัวอักษร -- optimizer stage ใดๆ ไม่ควรแตะ element พวกนี้เลย

### CPU waste

- billing header ปลอดภัยแต่ผ่าน 8 stages โดยไม่ได้ benefit อะไร
- `textcomp`, `caveman` ไม่ modify short elements แต่ยังกิน CPU cycle
- ทุก request เสีย ~15% optimizer calls ไปกับ elements ที่สั้นเกินไปจะ optimize

## Solution

Skip optimizer สำหรับ elements ที่สั้นกว่า 500 chars:

```go
if len(orig) < 500 {
    continue
}
```

### Why 500?

| Threshold consideration | Chars |
|------------------------|-------|
| All machine-readable elements (billing, privacy, identity) | < 200 |
| Any meaningful system prompt content | > 500 |
| Margin to avoid false positives | 300+ chars buffer |

## Before/After

### Before (ทุก element ผ่าน optimizer)

```
optimize_system_prompt_entry: len=91    (privacy prompt)
optimize_system_prompt_entry: len=62    (billing header)
optimize_system_prompt_entry: len=27967 (system prompt)
```

### After (short elements skip)

```
optimize_system_prompt_entry: len=27967 (system prompt only)
```

## Impact

| Metric | Before | After |
|--------|--------|-------|
| Privacy prompt integrity | CORRUPTED (semantic_dedup removes space, adds chars) | Preserved exactly |
| Billing header | Preserved (lucky) but wastes CPU | Preserved exactly, zero CPU waste |
| Optimizer calls per request | 3-4 elements x 8 stages = 24-32 | 1 element x 8 stages = 8 |
| CPU savings | - | ~15% fewer optimizer calls |
| Token savings | Only from 27K element | Unchanged |

## Test Coverage

| Test case | Description |
|-----------|-------------|
| Short element skip | element < 500 chars skipped, text unchanged |
| Long element optimization | element >= 500 chars optimized normally |
| Mixed array | short + long elements, only long gets optimized |
| Boundary | 499 chars (skip) vs 500 chars (optimize) |
| String format | plain string (no array) unaffected by guard |

## Architecture

```
System array:
  [0] billing header  (~62 chars)  -----> SKIP (< 500)
  [1] privacy prompt  (~91 chars)  -----> SKIP (< 500)
  [2] claude identity (~94 chars)  -----> SKIP (< 500)
  [3] system prompt   (~27K chars) -----> OPTIMIZE (>= 500)
                                           |
                                           v
                                      semantic_dedup
                                      chunker
                                      delta
                                      sketch
                                      summarizer
                                      textcomp
                                      caveman
                                      pordee
```

## Files Changed

| File | Change |
|------|--------|
| `handler/handler.go` | Added `len(orig) < 500` guard before optimizer call in per-element loop |
