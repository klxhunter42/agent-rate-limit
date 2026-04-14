# Changelog

> สรุปการเปลี่ยนแปลงทั้งหมดของระบบ

---

## [2026-04-14] Bug fixes + Hardening

### ปัญหา: Global slot starvation (Critical)

เวลา request เข้ามาเยอะ ทุก request จะจับ "global slot" ไว้แล้วรอ model slot นาน 30 วินาที ทำให้ slot หมดทั้งระบบ คนอื่นเข้าไม่ได้เลย

**แก้:** ปล่อย global slot ก่อนรอ พอได้ model slot แล้วค่อยขอ global slot ใหม่

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### ปัญหา: Key pool RPM leak (High)

request ที่ body พังหรือ JSON ไม่ถูกต้อง ยังไปกิน RPM quota ของ API key ทิ้ง

**แก้:** ย้าย `keyPool.Acquire()` ไปหลัง validate body + parse JSON เรียบร้อย

**ไฟล์:** `api-gateway/handler/handler.go`

---

### ปัญหา: Retry backoff ไม่รับรู้ context cancel (Medium)

เวลา client disconnect ระหว่างรอ retry backoff ระบบยังนอนรอต่อเปล่าๆ

**แก้:** ใช้ `select` + `ctx.Done()` แทน `time.Sleep` ถ้า client ยกเลิกก็หยุดทันที

**ไฟล์:** `api-gateway/proxy/anthropic.go`

---

### ปัญหา: Feedback ยิงซ้ำเกิน

เวลาโดน 429 แล้ว retry แต่ละครั้ง feedback ไปลด limit ตลอด ทำให้ limit ตกลงไปเร็วเกิน

**แก้:** ยิง feedback ไป adaptive limiter แค่ retry ครั้งสุดท้าย

**ไฟล์:** `api-gateway/proxy/anthropic.go`

---

### ปัญหา: Docker-compose defaults ไม่ตรงกัน

gateway ตั้ง default limit ไว้ 2/15 แต่ worker ตั้ง 1/9 ถ้าไม่มี .env สองฝั่งทำงานไม่เหมือนกัน

**แก้:** ให้ตรงกันหมดเป็น `DEFAULT_LIMIT=1`, `GLOBAL_LIMIT=9`

**ไฟล์:** `docker-compose.yml`

---

### ปัญหา: Limiter status endpoint ไม่มี auth

ใครก็เข้าดูสถานะ limiter ได้

**แก้:** เพิ่ม `IsValidKey()` เช็ค API key ก่อนอนุญาต

**ไฟล์:** `api-gateway/proxy/key_pool.go`, `api-gateway/handler/handler.go`

---

### ปัญหา: Password ใน .env.example

เผลอใส่รหัสผ่านจริงในไฟล์ตัวอย่าง

**แก้:** เปลี่ยนเป็น `changeme`

**ไฟล์:** `.env.example`

---

## [2026-04-13] Adaptive Limiter + Probe Multiplier

### เพิ่ม: Adaptive concurrency limiter

ระบบจำกัดจำนวน concurrent request แบบ adaptive - ปรับ limit อัตโนมัติตาม feedback จาก upstream

**Algorithm (inspired by Envoy gradient controller):**
- โดน 429: limit ลด 50% (`limit * 0.5`)
- สำเร็จ: ใช้ gradient formula `(minRTT + buffer) / sampleRTT` เพิ่ม limit
- Cooldown 5 วินาทีหลัง 429 ก่อนเพิ่ม limit ใหม่
- จำค่าที่โดน 429 (`peakBefore429`) ไม่ให้ขยายเกิน แต่ลืมหลัง 5 นาที

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### เพิ่ม: Probe multiplier

ให้ limiter ลองขยาย limit สูงสุดได้ `N` เท่าของ initial limit เพื่อค้นหา upstream limit จริง

- Default: 5x (`UPSTREAM_PROBE_MULTIPLIER=5`)
- ถ้า initial limit = 2, probe max = 10
- โดน 429 ก็ลดลงเองตาม adaptive algorithm

**ไฟล์:** `api-gateway/config/config.go`, `api-gateway/main.go`

---

### เพิ่ม: Model fallback with priority

เวลา model ที่ขอเต็ม ระบบลองรอ 2 วินาทีก่อน แล้ว fallback ตาม priority:

- glm-5.1 (100) > glm-5-turbo (90) > glm-5 (80) > glm-4.7 (70) > glm-4.6 (60) > glm-4.5 (50)
- ข้าม model ที่ห่างกันเกิน 2 tier (gap >= 50)

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### เพิ่ม: Token metrics

Prometheus counters สำหรับติดตาม token usage:

- `api_gateway_token_input_total{model}` (gateway)
- `api_gateway_token_output_total{model}` (gateway)
- `ai_worker_token_input_total{provider,model}` (worker)
- `ai_worker_token_output_total{provider,model}` (worker)

**ไฟล์:** `api-gateway/metrics/metrics.go`, `api-gateway/proxy/anthropic.go`, `ai-worker/prom_metrics.py`

---

## Documentation

| ไฟล์ | สถานะ |
|------|--------|
| `docs/providers.md` | ใหม่ - คู่มือตั้งค่า 5 providers + free tier |
| `docs/architecture.md` | v1.5 - เพิ่ม adaptive limiter, probe, token metrics |
| `docs/known-issues.md` | v1.1 - เพิ่ม 4 fixed items |
| `docs/claude-code-proxy.md` | v2.3 - flow diagram ใหม่ |
| `docs/changelog.md` | ใหม่ - ไฟล์นี้ |
