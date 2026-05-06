# Vision Image Compression POC Results

## Overview

POC testing to find the optimal image compression configuration for the gateway's vision pipeline. Tested all combinations of model, format, quality, and dimension against Z.AI's GLM vision models.

**Date:** 2026-05-06
**Gateway:** arl-gateway via Caddy proxy (port 9000)
**Upstream:** api.z.ai (Anthropic-compatible endpoint)
**Test images:**
- **Round 1+2:** Synthetic dashboard (PIL-generated, 1200x600, dark theme, basic text/rectangles)
- **Round 3:** Real photo - city skyline at twilight (1600x1200, from camera)
- **Round 4:** Same city skyline (speed optimization with prompt engineering)

---

## Round 1: Synthetic Dashboard (PIL-generated)

**Image:** 1200x600 dark theme dashboard with sidebar menu and stats
**Prompt:** "List the menu items, stats/numbers, and title from this dashboard screenshot. Be precise and exact."
**Keywords (10):** home, settings, users, logout, 1234, 567, 12345, dashboard, menu, revenue
**Models tested:** glm-5.1, glm-4.6v, glm-4.5v

### Results (sorted by accuracy DESC, size ASC)

| #   | Model    | Format   | Quality   | Dim   | Pixels   | Raw Size   | Accuracy   | Time   | Status   |
|-----|----------|----------|-----------|-------|----------|------------|------------|--------|----------|
| 1   | glm-4.5v | JPEG     | 75        | 1200  | 1200x600 | 19,895b    | **60%**    | 4.6s   | ok       |
| 2   | glm-5.1  | JPEG     | 75        | 1024  | 1024x512 | 14,117b    | 50%        | 4.5s   | ok       |
| 3   | glm-4.5v | JPEG     | 60        | 768   | 768x384  | 8,176b     | 40%        | 4.8s   | ok       |
| 4   | glm-4.6v | JPEG     | 60        | 1200  | 1200x600 | 18,611b    | 40%        | 6.4s   | ok       |
| 5   | glm-4.6v | JPEG     | 60        | 1024  | 1024x512 | 12,696b    | 30%        | 8.6s   | ok       |
| 6   | glm-4.6v | JPEG     | 75        | 1024  | 1024x512 | 14,117b    | 30%        | 4.7s   | ok       |
| 7   | glm-4.5v | JPEG     | 75        | 1024  | 1024x512 | 14,117b    | 30%        | 5.6s   | ok       |
| 8   | glm-4.5v | JPEG     | 60        | 1200  | 1200x600 | 18,611b    | 30%        | 4.7s   | ok       |
| 9   | glm-5.1  | JPEG     | 75        | 1200  | 1200x600 | 19,895b    | 30%        | 4.8s   | ok       |
| 10  | glm-4.6v | JPEG     | 60        | 768   | 768x384  | 8,176b     | 20%        | 6.4s   | ok       |
| 11  | glm-4.5v | JPEG     | 60        | 1024  | 1024x512 | 12,696b    | 20%        | 4.1s   | ok       |
| 12  | glm-4.6v | JPEG     | 75        | 1200  | 1200x600 | 19,895b    | 20%        | 7.4s   | ok       |

> **Note:** Gateway crashed after test #12 due to rapid sequential bimg/libvips processing (memory pressure). Remaining tests (JPEG q85, WebP, PNG, AVIF) returned connection errors. This is a gateway stability issue, not a format compatibility issue. Re-tested in Round 2.

### Sample Response (best: glm-4.5v JPEG q75 dim1200, 60%)

```
**title:** dashboard

**menu items:**
- home
- analytics
- reports
- settings

**stats/numbers:**
- total users: 1,254
- revenue: $45,230
- orders: 320
- growth: 12.5%
```

**Observation:** Model hallucinated some values (1,254 instead of 1,234; $45,230 instead of $12,345) but correctly identified layout elements (dashboard, home, settings, menu). PIL-generated text has no anti-aliasing, making OCR unreliable for numbers.

---

## Round 2: Synthetic Dashboard - Additional Formats

Same image as Round 1, testing formats that were missed due to gateway crash.
**Models tested:** glm-4.6v, glm-4.5v (skipped glm-5.1 due to slow timeouts)
**Added:** 1-second delay between requests to prevent gateway crash.

### Results

| #   | Model    | Format   | Quality   | Dim   | Pixels   | Raw Size   | Accuracy   | Time   | Status   |
|-----|----------|----------|-----------|-------|----------|------------|------------|--------|----------|
| 1   | glm-4.6v | JPEG     | 85        | 1024  | 1024x512 | 16,187b    | **60%**    | 3.6s   | ok       |
| 2   | glm-4.6v | WebP     | 75        | 1024  | 1024x512 | **3,132b** | 50%        | 7.7s   | ok       |
| 3   | glm-4.5v | WebP     | 75        | 1024  | 1024x512 | **3,132b** | 50%        | 4.3s   | ok       |
| 4   | glm-4.6v | WebP     | 75        | 1200  | 1200x600 | 4,216b     | 50%        | 5.3s   | ok       |
| 5   | glm-4.6v | WebP     | 75        | 768   | 768x384  | **2,060b** | 40%        | 6.4s   | ok       |
| 6   | glm-4.6v | AVIF     | 85        | 1024  | 1024x512 | 3,710b     | 40%        | 6.1s   | ok       |
| 7   | glm-4.5v | WebP     | 85        | 1024  | 1024x512 | 3,900b     | 40%        | 5.1s   | ok       |
| 8   | glm-4.5v | WebP     | 75        | 1200  | 1200x600 | 4,216b     | 40%        | 5.5s   | ok       |
| 9   | glm-4.6v | PNG      | 0         | 768   | 768x384  | 10,234b    | 40%        | 3.7s   | ok       |
| 10  | glm-4.6v | JPEG     | 85        | 768   | 768x384  | 10,828b    | 40%        | 4.4s   | ok       |
| 11  | glm-4.6v | WebP     | 85        | 768   | 768x384  | 2,586b     | 30%        | 5.9s   | ok       |
| 12  | glm-4.6v | AVIF     | 85        | 1200  | 1200x600 | 3,548b     | 30%        | 7.3s   | ok       |
| 13  | glm-4.5v | AVIF     | 85        | 1024  | 1024x512 | 3,710b     | 30%        | 7.6s   | ok       |
| 14  | glm-4.6v | WebP     | 85        | 1024  | 1024x512 | 3,900b     | 30%        | 7.2s   | ok       |
| 15  | glm-4.6v | WebP     | 85        | 1200  | 1200x600 | 5,124b     | 30%        | 3.5s   | ok       |
| 16  | glm-4.5v | WebP     | 85        | 1200  | 1200x600 | 5,124b     | 30%        | 6.0s   | ok       |
| 17  | glm-4.6v | PNG      | 0         | 1200  | 1200x600 | 12,386b    | 30%        | 7.1s   | ok       |
| 18  | glm-4.5v | PNG      | 0         | 1200  | 1200x600 | 12,386b    | 30%        | 5.4s   | ok       |
| 19  | glm-4.5v | PNG      | 0         | 1024  | 1024x512 | 15,723b    | 30%        | 4.4s   | ok       |
| 20  | glm-4.6v | JPEG     | 85        | 1200  | 1200x600 | 22,327b    | 30%        | 5.0s   | ok       |
| 21  | glm-4.5v | WebP     | 75        | 768   | 768x384  | 2,060b     | 20%        | 5.5s   | ok       |
| 22  | glm-4.5v | WebP     | 85        | 768   | 768x384  | 2,586b     | 20%        | 6.3s   | ok       |
| 23  | glm-4.5v | AVIF     | 85        | 1200  | 1200x600 | 3,548b     | 20%        | 6.7s   | ok       |
| 24  | glm-4.5v | JPEG     | 85        | 768   | 768x384  | 10,828b    | 20%        | 4.0s   | ok       |
| 25  | glm-4.5v | PNG      | 0         | 768   | 768x384  | 10,234b    | 10%        | 4.8s   | ok       |
| 26  | glm-4.6v | PNG      | 0         | 1024  | 1024x512 | 15,723b    | 10%        | 7.6s   | ok       |
| 27  | glm-4.5v | JPEG     | 85        | 1024  | 1024x512 | 16,187b    | 10%        | 3.1s   | ok       |
| 28  | glm-4.5v | JPEG     | 85        | 1200  | 1200x600 | 22,327b    | 10%        | 3.6s   | ok       |

### Key Finding: All formats work

WebP, PNG, and AVIF all produce valid responses. The earlier "WebP breaks GLM" theory was incorrect. WebP q75 at 3,132b achieves 50% accuracy - same as JPEG at 14,117b (4.5x larger).

### Size Comparison (1024x512, same accuracy ~50%)

| Format   | Quality   | Raw Size   | Relative to WebP  |
|----------|-----------|------------|-------------------|
| WebP     | 75        | 3,132b     | 1x (baseline)     |
| AVIF     | 85        | 3,710b     | 1.2x              |
| WebP     | 85        | 3,900b     | 1.2x              |
| JPEG     | 60        | 12,696b    | 4.0x              |
| JPEG     | 75        | 14,117b    | 4.5x              |
| JPEG     | 85        | 16,187b    | 5.2x              |
| PNG      | 0         | 15,723b    | 5.0x              |

---

## Round 3: Real Photo (City Skyline)

**Image:** Beautiful city wallpaper - twilight cityscape with skyline reflected in water (1600x1200)
**Prompt:** "Describe this image in detail. List every element you can see: sky, buildings, water, lights, colors, time of day, architectural features. Be specific and thorough."
**Keywords (20):** sky, building, city, light, tower, water, bridge, night, reflection, cloud, skyscraper, illuminated, urban, blue, golden, sunset, horizon, modern, glass, landscape
**Models tested:** glm-5.1, glm-4.6v, glm-4.5v

### Results (sorted by accuracy DESC)

| #   | Model    | Format   | Quality   | Dim   | Pixels    | Raw Size   | Accuracy   | Time   | Status   | Found   | Missed                                                   |
|-----|----------|----------|-----------|-------|-----------|------------|------------|--------|----------|---------|----------------------------------------------------------|
| 1   | glm-4.6v | JPEG     | 75        | 1600  | 1600x1200 | 134,067b   | **90%**    | 23.7s  | ok       | 18/20   | night, landscape                                         |
| 2   | glm-4.6v | JPEG     | 75        | 2048  | 1600x1200 | 134,067b   | **90%**    | 24.5s  | ok       | 18/20   | bridge, landscape                                        |
| 3   | glm-5.1  | WebP     | 75        | 1600  | 1600x1200 | 68,240b    | 85%        | 24.3s  | ok       | 17/20   | bridge, landscape, golden                                |
| 4   | glm-4.5v | JPEG     | 75        | 1600  | 1600x1200 | 134,067b   | 85%        | 20.2s  | ok       | 17/20   | bridge, landscape, horizon                               |
| 5   | glm-5.1  | JPEG     | 75        | 1600  | 1600x1200 | 134,067b   | 80%        | 26.6s  | ok       | 16/20   | bridge, cloud, landscape, sunset                         |
| 6   | glm-5.1  | JPEG     | 75        | 2048  | 1600x1200 | 134,067b   | 80%        | 25.0s  | ok       | 16/20   | bridge, cloud, landscape, sunset                         |
| 7   | glm-4.5v | JPEG     | 75        | 2048  | 1600x1200 | 134,067b   | 80%        | 24.0s  | ok       | 16/20   | bridge, cloud, landscape, horizon                        |
| 8   | glm-4.5v | WebP     | 75        | 1600  | 1600x1200 | 68,240b    | 75%        | 21.5s  | ok       | 15/20   | bridge, landscape, sunset, horizon, golden               |
| 9   | glm-4.6v | JPEG     | 85        | 1600  | 1600x1200 | 170,606b   | 75%        | 13.9s  | ok       | 15/20   | bridge, cloud, landscape, sunset, golden                 |
| 10  | glm-4.6v | PNG      | 0         | 1600  | 1600x1200 | 1,330,427b | 75%        | 15.4s  | ok       | 15/20   | bridge, cloud, landscape, sunset, golden                 |
| 11  | glm-4.5v | PNG      | 0         | 1600  | 1600x1200 | 1,330,427b | 75%        | 17.9s  | ok       | 15/20   | bridge, cloud, landscape, sunset, horizon                |
| 12  | glm-4.5v | JPEG     | 85        | 1600  | 1600x1200 | 170,606b   | 70%        | 14.3s  | ok       | 14/20   | bridge, cloud, landscape, sunset, horizon, golden        |
| 13  | glm-4.6v | WebP     | 75        | 1600  | 1600x1200 | 68,240b    | 65%        | 26.6s  | ok       | 13/20   | bridge, night, cloud, landscape, sunset, horizon, golden |
| 14  | glm-5.1  | JPEG     | 85        | 1600  | 1600x1200 | 170,606b   | 55%        | 9.8s   | ok       | 11/20   | (many)                                                   |
| 15  | glm-5.1  | PNG      | 0         | 1600  | 1600x1200 | 1,330,427b | 0%         | 120.1s | err      | 0/20    | all (timeout)                                            |

> **Note:** Gateway crashed after test #15 (PNG 1.3MB overloaded bimg memory). All subsequent tests (dim2048/1024/768) returned connection errors. Same pattern as Round 1.

### Best Response (glm-4.6v JPEG q75 dim1600, 90%)

```
based on the image provided, here is a detailed and thorough description of every visible element:

**time of day and sky**
*   **time of day:** twilight / dusk. the image captures the "blue hour,"
    the period just after sunset or just before sunrise.
*   **sky:** the sky is a rich gradient. the upper portion is a deep, dark
    navy blue, which gently fades into softer, lighter shades of cerulean
    and periwinkle as it meets the horizon. there are no visible stars,
    moon, or clouds.

**water**
*   ...
```

**Missed keywords:** `night` (model said "twilight/dusk"), `landscape` (model said "cityscape"). Both are semantically correct descriptions.

---

## Round 4: Speed Optimization (Prompt Engineering)

**Image:** Same city skyline at twilight (1600x1200, from Round 3)
**Goal:** Reduce response time from 20-27s to under 5s while maintaining >90% accuracy
**Models tested:** glm-4.6v (best model from Round 3)

### Prompt Variants Tested

| Prompt       | max_tokens  | Strategy                                                  |
|--------------|-------------|-----------------------------------------------------------|
| **keywords** | 300         | Checklist of expected elements, ask model to confirm each |
| detailed     | 800         | Open-ended "describe everything"                          |
| concise      | 300         | "Describe briefly"                                        |
| bullet       | 400         | "List elements as bullet points"                          |
| short        | 200         | "Name 5 things in this image"                             |

### Results: Keywords Prompt (glm-4.6v, JPEG q75)

| Dim      | Pixels    | Raw Size   | Accuracy   | Time     | Found   |
|----------|-----------|------------|------------|----------|---------|
| **512**  | 512x384   | 21KB       | **90%**    | **4.1s** | 18/20   |
| **768**  | 768x576   | 35KB       | **90%**    | **4.3s** | 18/20   |
| **1024** | 1024x768  | 55KB       | **90%**    | **4.5s** | 18/20   |
| **1280** | 1280x960  | 78KB       | **90%**    | **5.1s** | 18/20   |
| **1600** | 1600x1200 | 134KB      | **95%**    | **5.9s** | 19/20   |

### Results: Other Prompts (glm-4.6v, JPEG q75, dim1600)

| Prompt   | max_tokens  | Accuracy   | Time         | Notes                        |
|----------|-------------|------------|--------------|------------------------------|
| keywords | 300         | **90-95%** | **4.1-5.9s** | Best speed/accuracy tradeoff |
| detailed | 800         | 70-85%     | 20-27s       | Slow, variable accuracy      |
| concise  | 300         | 45-65%     | 2.5-5s       | Fast but unreliable          |
| bullet   | 400         | 75-80%     | 8-12s        | Moderate                     |
| short    | 200         | 50-70%     | 3-6s         | Unreliable                   |

### Key Findings

1. **Prompt > pixel size for speed.** The "keywords" checklist prompt cuts response time from 20+ seconds to 4-5 seconds regardless of pixel dimensions.
2. **Keywords prompt "cheats".** It tells the model what to look for, so it works well for known image types but not for arbitrary/unknown images. For unknown images, the model still needs enough time to analyze.
3. **Dim 512 is sufficient for known images.** With keywords prompt, 512x384 (21KB) achieves 90% accuracy at 4.1s - 6x smaller and 6x faster than the Round 3 best.
4. **Dim 1600 + keywords = 95%.** Full resolution with keywords prompt achieves the highest accuracy at 5.9s.

### Caveat: Keywords Prompt is Not Generic

The keywords prompt works by providing a checklist of expected elements:

```
Identify these specific elements in the image. For each one, respond YES if present, NO if not:
1. sky 2. building 3. city 4. light ...
```

This is effective for **known image types** (screenshots, dashboards, expected photos) but does not help with **arbitrary/unknown images** where you don't know what keywords to provide.

---

## Summary: Cross-Round Analysis

### By Model (best accuracy per model, any format/dimension)

| Model        | Best Accuracy  | Best Config                        | Round   | Time   | Image Type   |
|--------------|----------------|------------------------------------|---------|--------|--------------|
| **glm-4.6v** | **95%**        | JPEG q75 dim1600 + keywords prompt | 4       | 5.9s   | Real photo   |
| **glm-4.6v** | **90%**        | JPEG q75 dim1600                   | 3       | 23.7s  | Real photo   |
| **glm-4.6v** | **90%**        | JPEG q75 dim512 + keywords prompt  | 4       | 4.1s   | Real photo   |
| glm-4.5v     | 85%            | JPEG q75 dim1600                   | 3       | 20.2s  | Real photo   |
| glm-5.1      | 85%            | WebP q75 dim1600                   | 3       | 24.3s  | Real photo   |
| glm-4.6v     | 60%            | JPEG q85 dim1024                   | 2       | 3.6s   | Synthetic    |
| glm-4.5v     | 60%            | JPEG q75 dim1200                   | 1       | 4.6s   | Synthetic    |
| glm-5.1      | 50%            | JPEG q75 dim1024                   | 1       | 4.5s   | Synthetic    |

### By Format (best accuracy, glm-4.6v, real photo, full resolution)

| Format   | Quality   | Size         | Accuracy   | Time      |
|----------|-----------|--------------|------------|-----------|
| **JPEG** | **75**    | **134,067b** | **90%**    | **23.7s** |
| WebP     | 75        | 68,240b      | 65%        | 26.6s     |
| JPEG     | 85        | 170,606b     | 75%        | 13.9s     |
| PNG      | 0         | 1,330,427b   | 75%        | 15.4s     |

### JPEG Quality Comparison (glm-4.6v, real photo, dim1600)

| Quality   | Size         | Accuracy   | Time      | Notes                  |
|-----------|--------------|------------|-----------|------------------------|
| **75**    | **134,067b** | **90%**    | **23.7s** | Best accuracy          |
| 85        | 170,606b     | 75%        | 13.9s     | Larger, worse accuracy |

Counter-intuitive: q75 produces better accuracy than q85. Likely because q85 introduces more high-frequency artifacts that confuse the vision model, while q75 produces smoother output that's easier to interpret.

### Dimension Impact (glm-4.6v, JPEG q75, synthetic dashboard)

| Max Dim         | Pixels   | Size    | Accuracy   |
|-----------------|----------|---------|------------|
| 1200 (original) | 1200x600 | 18,611b | 40%        |
| 1024            | 1024x512 | 14,117b | 30%        |
| 768             | 768x384  | 8,176b  | 20%        |

For synthetic images, higher resolution helps. For real photos, 1600px (original) is best.

---

## Final Recommended Config

| Setting                  | Value                          | Reason                                           |
|--------------------------|--------------------------------|--------------------------------------------------|
| **Format**               | JPEG                           | Universal compatibility, best accuracy           |
| **Quality**              | 75                             | Outperforms q85 for accuracy, smaller size       |
| **Max dimension**        | 1600px                         | Full detail preserves accuracy                   |
| **Size guard**           | Skip if compressed >= original | Prevents Prometheus counter panic                |
| **Default vision model** | glm-4.6v                       | 90% accuracy on real photos (vs 80% for glm-5.1) |

### Speed vs Accuracy Matrix

| Speed Target               | Config                      | Accuracy   | Payload Size  |
|----------------------------|-----------------------------|------------|---------------|
| **<5s** (known images)     | dim512 + keywords prompt    | 90%        | 21KB          |
| **<6s** (best accuracy)    | dim1600 + keywords prompt   | 95%        | 134KB         |
| **~24s** (generic/unknown) | dim1600 + open-ended prompt | 90%        | 134KB         |

### Expected Bandwidth Savings

| Input Scenario             | Original Size  | After Compress        | Saving         |
|----------------------------|----------------|-----------------------|----------------|
| PNG screenshot 1600x900    | ~700KB-1.3MB   | ~80-134KB             | **80-90%**     |
| PNG screenshot 1024x768    | ~500KB         | ~64KB                 | **87%**        |
| JPEG photo 4000x3000       | ~5MB           | ~134KB (after resize) | **97%**        |
| JPEG photo 1600x1200 q85   | 170KB          | 134KB                 | **21%**        |
| WebP image (already small) | 68KB           | 134KB                 | (skip, larger) |

---

## Known Issues

### Gateway Crash Under Load

The gateway crashes after ~15 sequential vision requests due to memory pressure in bimg/libvips CGO processing. Symptoms: `Connection reset by peer`, `Broken pipe`.

**Workaround:** Add rate limiting or cooldown between vision requests.
**Root cause:** Likely memory leak in libvips when processing many images in rapid succession without GC recovery.

### Synthetic Image Low Accuracy

PIL-generated test images max out at 60% accuracy because:
- No anti-aliasing on text rendering
- Simple rectangles/shapes don't match training data distribution
- OCR on rendered text is unreliable

Real photos achieve 90% accuracy with the same models.
