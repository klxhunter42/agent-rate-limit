# Optimizer Before/After Comparison
# Z.AI glm-5.1 | $1.4/M input | $4.4/M output

---

## Verbose System Prompt
Baseline: **252** in / **512** out = **$0.002606**

| # | Stage | Before In | After In | In Saved | Before Out | After Out | Out Saved | Before $ | After $ | Saved |
|--:|-------|----------:|---------:|---------:|----------:|---------:|----------:|---------:|--------:|------:|
| 1 | **Semantic + TextComp** | 252 | 37 | -215 (-85%) | 512 | 512 | - | $0.002606 | $0.002305 | +11.6% |
| 2 | All except Caveman | 252 | 50 | -202 (-80%) | 512 | 512 | - | $0.002606 | $0.002323 | +10.9% |
| 3 | Text Compression | 252 | 59 | -193 (-77%) | 512 | 512 | - | $0.002606 | $0.002335 | +10.4% |
| 4 | Pordee (Thai) | 252 | 60 | -192 (-76%) | 512 | 512 | - | $0.002606 | $0.002337 | +10.3% |
| 5 | Semantic + Sketch | 252 | 101 | -151 (-60%) | 512 | 512 | - | $0.002606 | $0.002394 | +8.1% |
| 6 | Caveman | 252 | 114 | -138 (-55%) | 512 | 512 | - | $0.002606 | $0.002412 | +7.4% |
| 7 | All Stages ON | 252 | 155 | -97 (-38%) | 512 | 512 | - | $0.002606 | $0.002470 | +5.2% |
| 8 | Semantic + Caveman | 252 | 156 | -96 (-38%) | 512 | 512 | - | $0.002606 | $0.002471 | +5.2% |
| 9 | Semantic Dedup | 252 | 229 | -23 (-9%) | 512 | 512 | - | $0.002606 | $0.002573 | +1.2% |
| 10 | Chunker | 252 | 252 | - | 512 | 512 | - | $0.002606 | $0.002606 | +0.0% |
| 11 | Sketch Dedup | 252 | 252 | - | 512 | 512 | - | $0.002606 | $0.002606 | +0.0% |

---

## K8s Debug Logs
Baseline: **819** in / **256** out = **$0.002273**

| # | Stage | Before In | After In | In Saved | Before Out | After Out | Out Saved | Before $ | After $ | Saved |
|--:|-------|----------:|---------:|---------:|----------:|---------:|----------:|---------:|--------:|------:|
| 1 | **Text Compression** | 819 | 51 | -768 (-94%) | 256 | 260 | +4 (+2%) | $0.002273 | $0.001215 | +46.5% |
| 2 | Caveman | 819 | 745 | -74 (-9%) | 256 | 52 | -204 (-80%) | $0.002273 | $0.001272 | +44.0% |
| 3 | Caveman + ToolComp | 819 | 745 | -74 (-9%) | 256 | 75 | -181 (-71%) | $0.002273 | $0.001373 | +39.6% |
| 4 | Chunker | 819 | 691 | -128 (-16%) | 256 | 97 | -159 (-62%) | $0.002273 | $0.001394 | +38.7% |
| 5 | All except Caveman | 819 | 745 | -74 (-9%) | 256 | 92 | -164 (-64%) | $0.002273 | $0.001448 | +36.3% |
| 6 | Semantic + Caveman | 819 | 873 | +54 (+7%) | 256 | 129 | -127 (-50%) | $0.002273 | $0.001790 | +21.3% |
| 7 | All Stages ON | 819 | 873 | +54 (+7%) | 256 | 142 | -114 (-45%) | $0.002273 | $0.001847 | +18.7% |
| 8 | Semantic + Sketch | 819 | 690 | -129 (-16%) | 256 | 244 | -12 (-5%) | $0.002273 | $0.002040 | +10.3% |
| 9 | Semantic + TextComp | 819 | 690 | -129 (-16%) | 256 | 266 | +10 (+4%) | $0.002273 | $0.002136 | +6.0% |
| 10 | Tool Compression | 819 | 819 | - | 256 | 230 | -26 (-10%) | $0.002273 | $0.002159 | +5.0% |
| 11 | Sketch Dedup | 819 | 819 | - | 256 | 253 | -3 (-1%) | $0.002273 | $0.002260 | +0.6% |
| 12 | Semantic Dedup | 819 | 690 | -129 (-16%) | 256 | 433 | +177 (+69%) | $0.002273 | $0.002871 | -26.3% |

---

## 25-Tool Manifest
Baseline: **1,083** in / **4** out = **$0.001534**

| # | Stage | Before In | After In | In Saved | Before Out | After Out | Out Saved | Before $ | After $ | Saved |
|--:|-------|----------:|---------:|---------:|----------:|---------:|----------:|---------:|--------:|------:|
| 1 | **Tool Filter** | 1,083 | 470 | -613 (-57%) | 4 | 15 | +11 (+275%) | $0.001534 | $0.000724 | +52.8% |
| 2 | All Stages ON | 1,083 | 524 | -559 (-52%) | 4 | 4 | - | $0.001534 | $0.000751 | +51.0% |
| 3 | Semantic Dedup | 1,083 | 1,332 | +249 (+23%) | 4 | 14 | +10 (+250%) | $0.001534 | $0.001926 | -25.6% |

---

## Thai Prompt
Baseline: **221** in / **256** out = **$0.001436**

| # | Stage | Before In | After In | In Saved | Before Out | After Out | Out Saved | Before $ | After $ | Saved |
|--:|-------|----------:|---------:|---------:|----------:|---------:|----------:|---------:|--------:|------:|
| 1 | **Semantic Dedup** | 221 | 221 | - | 256 | 256 | - | $0.001436 | $0.001436 | +0.0% |
| 2 | Pordee (Thai) | 221 | 221 | - | 256 | 256 | - | $0.001436 | $0.001436 | +0.0% |
| 3 | Caveman | 221 | 276 | +55 (+25%) | 256 | 256 | - | $0.001436 | $0.001513 | -5.4% |
| 4 | All Stages ON | 221 | 276 | +55 (+25%) | 256 | 256 | - | $0.001436 | $0.001513 | -5.4% |

---

## Recommendation Matrix

| Workload | #1 Pick | Savings | #2 Pick | Savings | Avoid |
|----------|---------|--------:|---------|--------:|-------|
| Verbose System Prompt | **Semantic + TextComp** | +11.6% | All except Caveman | +10.9% | - |
| K8s Debug Logs | **Text Compression** | +46.5% | Caveman | +44.0% | Semantic Dedup (-26.3%) |
| 25-Tool Manifest | **Tool Filter** | +52.8% | All Stages ON | +51.0% | Semantic Dedup (-25.6%) |
| Thai Prompt | **Semantic Dedup** | 0.0% | Pordee (Thai) | 0.0% | All Stages ON (-5.4%) |

---

## Key Findings

- **Text Compression** (-77~94% input) - ลด input ได้มากสุด ทุก payload
- **Caveman** (in +9~25%, out -80%) - ลด output ได้มาก คุ้มเพราะ output แพง 3.1x
- **Tool Filter** (-57% input) - ตัด tool 25->10 tools ประหยัดสุดกับ agent
- **Tool Compression** (-10% output) - บีบ tool_result เหมาะกับ multi-turn
- **Chunker** (-62% output k8s) - ดีเฉพาะ payload ที่มี log ซ้ำ
- **Sketch Dedup** (~0%) - ไม่ช่วยกับ payload เหล่านี้
- **Semantic Dedup** (out +69% k8s) - อันตราย: output พอง แย่กว่า baseline
- **Pordee** (0% thai) - ไม่ช่วยกับ Thai prompt จริง
- **All Stages ON** (-16% avg) - ดีกว่า baseline แต่แย่กว่า stage เดี่ยว
- **Best Pair: Caveman+ToolComp** (-40% k8s) - ดีที่สุดสำหรับ multi-turn

_Real Z.AI glm-5.1 API responses via optimizer-per-stage-poc.py_