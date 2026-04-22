# Next Session Prompt

เปิด `docs/claude-oauth-research.md` อ่านก่อน แล้วทำต่อ:

## สิ่งที่ทำเสร็จแล้ว
- Gateway OAuth bearer auth ทำงานถูกต้อง (`Authorization: Bearer` + `anthropic-beta: oauth-2025-04-20`)
- Meow profile ใช้ `claude-haiku-4-5-20251001` ผ่าน `claude-oauth` provider ได้แล้ว
- Test profile ใช้ `gemini-2.5-flash` ผ่าน `gemini-oauth` provider ได้แล้ว
- Docker Claude Code container + entrypoint script พร้อมใช้
- Claude OAuth research docs อยู่ที่ `docs/claude-oauth-research.md`

## สิ่งที่เสร็จแล้ว

### 4. ✅ Gateway token refresh สำหรับ Claude OAuth
Fixed `token-refresh.go` ให้ใช้ JSON body สำหรับ claude-oauth provider
- claude-oauth: ใช้ JSON body ที่ `/v1/oauth/token`
- gemini-oauth: ยังใช้ form-urlencoded
Build test ผ่าน (no errors)

## สิ่งที่ยังไม่ได้ทำ

### 1. Test Claude Code CLI ผ่าน gateway ด้วย haiku
settings-meow.json มี haiku ตั้งแล้ว แต่ยังไม่ได้ restart Claude Code session
ต้อง restart Claude Code แล้ว verify ว่า haiku ทำงานผ่าน gateway

### 2. Test Docker Claude Code container end-to-end
docker-compose.yml มี claude-code-meow service พร้อม (profile: test-client)
```
DOCKER_DEFAULT_PLATFORM=linux/arm64 docker-compose --profile test-client run --rm claude-code-meow help
```
ต้อง verify:
- Gateway health check ผ่าน
- Token provisioning สำเร็จ
- Model ที่ตั้งใน settings ทำงาน (haiku)

### 3. Sonnet/Opus ผ่าน gateway
ตอนนี้ติด 429 rate limit จาก Anthropic (per-account)
ต้องรอ rate limit reset แล้วลองใหม่ หรือใช้ตอนที่ไม่มี Claude Code session อื่นใช้ sonnet อยู่
