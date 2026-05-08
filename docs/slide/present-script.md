# AI Gateway - Presentation Script (Thai)

---

## Slide 1: Cover Page

สวัสดีครับ วันนี้จะมานำเสนอ AI Gateway ซึ่งเป็นระบบ Multi-Provider Proxy ที่ออกแบบมาเพื่อเป็นตัวกลางระหว่าง AI Client อย่าง Claude Code หรือ AI Agent ต่างๆ กับ AI Provider ที่อยู่ด้านหลัง

จุดเด่นหลักคือ Smart Routing หรือการกระจาย request อัตโนมัติ รองรับหลาย provider มีระบบ Privacy Pipeline และติดตามค่าใช้จ่ายแบบ real-time

---

## Slide 2: Why AI Gateway?

มาดูปัญหากันก่อนครับ

ปัญหาแรก API Hammering - AI Agent ส่ง request รัวๆ จน provider rate limit แตก ทั้งที่ยังมี account อื่นที่ว่างอยู่

ปัญหาที่สอง Single Point of Failure - ใช้ account เดียว พอโดน 429 ทั้งทีมใช้งานไม่ได้เลย เพราะ rate limit มันติดที่ระดับ organization

ปัญหาที่สาม Zero Visibility - ไม่รู้ว่าใครใช้เท่าไหร่ ค่าใช้จ่ายเท่าไหร่ จะเกิน budget เมื่อไหร่ก็ไม่รู้

ทางออกของเราคือทำ Transparent Proxy ที่ทุก byte ผ่าน gateway โดยไม่ถูกแก้ไข มี Account Pool ที่หมุนเวียนใช้งานอัตโนมัติ และรองรับหลาย provider พร้อม failover

---

## Slide 3: System Architecture

มาดู architecture ครับ

ด้านบนสุดคือ Clients - Claude Code, AI Agents, CI/CD pipelines

ตรงกลางคือ Gateway ตัวหลัก เขียนด้วย Go รันที่ port 8080 ข้างในประกอบด้วยหลาย module: Authentication, Profile Routing, PasteGuard privacy pipeline, Account Pool, Rate Limiter, และ Proxy

มี 2 เส้นทาง processing - เส้น Sync path สำหรับ SSE streaming ไปยัง provider โดยตรง และเส้น Async path ที่ผ่าน Dragonfly Queue ไปยัง Python Worker ซึ่งรัน 50 coroutines สำหรับงานหนัก

ด้านล่างสุดคือ Provider ต่างๆ ตั้งแต่ Z.AI, OpenAI, Gemini, OpenRouter, DeepSeek และอีก 12 provider

---

## Slide 4: Claude OAuth - How We Connect

ส่วนนี้สำคัญมากครับ เราใช้ PKCE Flow ในการ authenticate กับ Anthropic แทนที่จะใช้ API Key แบบเดิมๆ

ขั้นตอนคือ Gateway สร้าง PKCE verifier และ challenge ผู้ใช้ authorize ผ่าน platform.claude.com แล้ว Gateway แลก code เป็น access token กับ refresh token จากนั้นเก็บใน Dragonfly และ refresh อัตโนมัติเมื่อใกล้หมดอายุ

ข้อดีของ PKCE คือไม่ต้องมี client secret ป้องกัน code interception attack ได้

ใน setup จริงเรามีหลาย Claude account ใน pool refresh token ทุก 30 นาที พอเจอ 429 ก็หมุนไป account ถัดไปอัตโนมัติ แถมยัง rewrite model จาก sonnet/opus เป็น haiku เพื่อหลีกเลี่ยง org-level rate limit ได้อีกด้วย

---

## Slide 5: Multi-Provider Routing

การ routing ทำงานจาก model prefix ครับ

ถ้าขึ้นต้นด้วย claude ก็ส่งไป Claude OAuth ด้วย Bearer token, glm ก็ส่งไป Z.AI, gpt หรือ o1 ก็ไป OpenAI, gemini ไป Google, deepseek ไป DeepSeek และอะไรก็ตามที่ไม่ match ก็ส่งไป OpenRouter

Flow คือ resolve provider, เลือก account จาก pool โดยชอบ account ที่ utilization ต่ำกว่า 80%, ส่งเป็น transparent proxy, ถ้าเจอ 429 ก็ cooldown 60 วินาทีแล้วลอง account ถัดไป

จุดสำคัญคือ provider isolation - API key ของ Claude จะไม่มีทางถูกส่งไป OpenAI เด็ดขาด

---

## Slide 6: Account Pool & Utilization

Algorithm การเลือก account ครับ

โหลด account ทั้งหมดจาก Redis แบ่งเป็นกลุ่ม low-util ที่ต่ำกว่า 80% กับ high-util ที่สูงกว่า จากนั้น route ไป low-util ก่อนแบบ round-robin ถ้า low-util ใช้หมดแล้วค่อยไป high-util พอโดน 429 ก็ cooldown 60 วินาทีแล้ว auto-recover

ตัวเลข scaling ให้ดูครับ - 1 Claude Pro account ได้ประมาณ 45 RPM, 2 account ก็ 90 RPM ถ้ารวมกับ OpenAI อีก account นึงก็จะได้ประมาณ 210 RPM และถ้ารวม provider ทั้งหมดก็จะได้ 270 RPM ขึ้นไป

---

## Slide 7: Profile-Based Routing

Profile คือ named config ที่เก็บใน Redis ประกอบด้วย model, provider, account pool, และ base URL

ระบบ token ทำงานแบบนี้ครับ - admin สร้าง profile เช่น team-a แล้ว generate profile token ออกมา ผู้ใช้เอา token นี้ไปใส่เป็น ANTHROPIC_AUTH_TOKEN ใน Claude Code settings พอ request เข้ามา Gateway ดึง profile token แล้ว lookup ใน Redis เพื่อ override routing

Use case ที่ใช้จริงคือ แบ่งทีม - junior ใช้ Haiku, senior ใช้ Sonnet/Opus คุม cost ด้วยการกำหนด model ถูกลงตาม profile แยก provider ตามทีม หรือใช้ทดสอบเปรียบเทียบ provider โดยไม่ต้องแก้ config ที่ client

---

## Slide 8: Client-Side Setup

ขั้นตอนการตั้งค่าที่ client ง่ายมากครับ 3 ขั้นตอน

ขั้นตอนที่ 1 - Admin สร้าง profile ด้วย POST /v1/profiles แล้วสร้าง token ด้วย POST /v1/profiles/my-team/tokens จะได้ profile token กลับมา

ขั้นตอนที่ 2 - แก้ไฟล์ settings.json ของ Claude Code ใส่ ANTHROPIC_BASE_URL ชี้ไปที่ gateway แล้วใส่ profile token เป็น ANTHROPIC_AUTH_TOKEN

ขั้นตอนที่ 3 - รัน claude ปกติเลย ทั้ง interactive mode และ pipe mode ทุก request จะผ่าน gateway อัตโนมัติ

เสร็จแล้วครับ ไม่ต้องแก้อะไรที่ client เพิ่มเติม

> **Note:** `apiKeyHelper` + `ANTHROPIC_API_KEY` is the legacy method. Use `ANTHROPIC_AUTH_TOKEN` instead.

---

## Slide 9: Feature Summary - Monitor, Optimize, Secure

แบ่งเป็น 3 แกนหลักครับ

Monitor - ติดตาม request latency, TTFB, token usage, cost แยกตาม provider model profile ติดตาม 429 event มี anomaly detection ด้วย Z-score, real-time event timeline และดู utilization ของแต่ละ account

Optimize - เลือก model อัตโนมัติผ่าน profile, ติดตาม prompt cache hit rate, เทียบราคา provider แบบ arbitrage, strip parameter ที่ไม่จำเป็นสำหรับ model ที่รองรับไม่ครบ, ลด whitespace ประหยัด 3-5%, deduplicate request ซ้ำด้วย hash และ Levenshtein, และ track token budget แบบ Green Yellow Red

Secure - PasteGuard masking ทั้ง regex และ NLP ป้องกัน secrets และ PII ไปถึง provider, IP filtering ด้วย CIDR whitelist, profile token auth ไม่ต้องเอา raw API key ไปวางที่เครื่อง client และ provider isolation เข้มงวด

---

## Slide 10: PasteGuard - Privacy Pipeline

นี่คือ feature ที่ผมภูมิใจมากครับ

PasteGuard ทำงาน 2 phase ก่อนส่ง request ไป provider

Phase 1 คือ Regex matching ทำงานภายใน sub-millisecond จับ pattern อย่าง API key, token, password, private key, connection string

Phase 2 คือ Presidio NLP จับข้อมูลส่วนบุคคล เช่น ชื่อบุคคล, email, เบอร์โทร, ที่อยู่, SSN

ทั้งหมดถูกแทนที่ด้วย placeholder แบบ reversible พอได้ response กลับมาก็ unmask ทุกอย่างคืน

สรุปสั้นๆ คือ AI provider จะไม่เห็น secrets หรือ PII ของเราเด็ดขาด ส่วน regex path ก็ไม่มีผลต่อ latency เลย

---

## Slide 11: Cost Tracking

ระบบคำนวณ cost ทำงานต่อ request ครับ

ดึง token usage จาก response, lookup pricing table ตาม model, แล้วบันทึกลง Dragonfly แยกตาม hourly, daily, monthly และแบ่งตาม provider, model, account, profile

ตัวอย่างราคาครับ - Claude Opus 4.7 input $15 output $75 ต่อล้าน token, Sonnet 4.6 input $3 output $15, Haiku 4.5 input $0.80 output $4, GPT-4.1 input $2 output $8, Gemini 2.5 Pro input $1.25 output $10, และ DeepSeek V3 ถูกสุด input $0.27 output $1.10

Dashboard แสดง cost ต่อ day, week, month แยกตาม provider, profile พร้อม projected monthly cost

---

## Slide 12: 17 Providers & Fallback

เรารองรับ 17 provider ครับ - Anthropic, Claude, OpenAI, Google Gemini, Gemini OAuth, OpenRouter, GitHub Copilot, DeepSeek, Qwen, Kimi, Hugging Face, Ollama, Z.AI, AGY, Cursor, CodeBuddy, Kilo

Fallback chain ทำงานแบบนี้ - ลอง provider A ก่อน หมุน account เมื่อเจอ 429, ถ้าทุก account หมดแล้วก็ fallback ไป provider B พร้อม model mapping, ถ้า provider B ล่มก็ไป provider C, ถ้าทุก provider ล่มก็ exponential backoff สูงสุด 3 ครั้งแล้วค่อย return error

จุดสำคัญคือแต่ละ provider ใช้ format และ auth ต่างกัน - Claude ใช้ Native Anthropic format กับ PKCE Bearer, OpenAI และ OpenRouter ใช้ OpenAI-compatible format กับ API key, Gemini ใช้ Google AI format

---

## Slide 13: Claude Code Compatibility

เราทดสอบความเข้ากันได้กับ Claude Code ครบทุก feature ครับ

ทั้ง Read, Edit, Bash, Write, Streaming, Extended thinking, Image/Vision, MCP Servers, Multi-turn, Skills, Memory, NotebookEdit, TodoRead/TodoWrite ผ่านหมด

ทำไมถึงใช้ได้ เพราะ Gateway เป็น transparent proxy โดยสมบูรณ์ Skills ขยายที่ client, Memory เป็น local files, MCP ทำงานที่ client-side ทั้งหมด Gateway ไม่ยุ่งเกี่ยว

Feature อัตโนมัติที่เพิ่มเข้ามาคือ rewrite model จาก sonnet/opus เป็น haiku เพื่อหลีกเลี่ยง org-level rate limit, clamp max_tokens ตาม limit ของแต่ละ model, และ strip parameter ที่ model นั้นไม่รองรับ

---

## Slide 14: Dashboard & Observability

Dashboard มี 4 หน้าหลักครับ

Overview แสดง status cards, capacity bar, model utilization, event timeline แบบ real-time

Profiles สำหรับ CRUD profile, จัดการ account pool, generate token, พร้อม setup guide

Providers สำหรับจัดการ OAuth flow, API key, account CRUD

Usage & Cost แสดง analytics แบ่งตาม time bucket, cost ต่อ model, พร้อม projection

ด้าน monitoring เรามี 21 Prometheus metrics ครอบคลุม request duration, TTFB, token usage, cost, rate limit, anomaly detection ด้วย Z-score ring buffer ขนาด 1000 samples

Stack ที่ใช้คือ React 18, TypeScript, Vite, Tailwind และ shadcn/ui

---

## Slide 15: Key Differentiators

สรุป 6 จุดเด่นที่ทำให้ AI Gateway ต่างจาก solution อื่นครับ

อันดับ 1 Transparent Proxy - ไม่แก้ไข request body เลย ทำให้ AI client ตัวไหนก็ใช้ได้

อันดับ 2 Multi-Provider - รองรับ 17 provider พร้อม automatic failover ไม่มี vendor lock-in

อันดับ 3 PasteGuard - Privacy pipeline ที่ป้องกัน secrets และ PII ไม่ให้ไปถึง AI provider

อันดับ 4 Cost Optimization - Profile routing, ติดตาม cost ต่อ request, ประหยัดได้มากกว่า 95%

อันดับ 5 Account Pool - Utilization-aware routing, auto-cooldown, scale ได้โดยเพิ่ม account

อันดับ 6 Production Ready - 21 Prometheus metrics, anomaly detection, Grafana dashboards ครบจบ

---

## Slide 16: Thank You

ขอบคุณครับ

Repo อยู่ที่ github.com/klxhunter/agent-rate-limit

หากมีคำถามสามารถถามได้เลยครับ
