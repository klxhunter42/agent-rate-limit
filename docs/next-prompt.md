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

### 2. ✅ Test Docker Claude Code container end-to-end
- Gateway health check ✅ ผ่าน
- Token provisioning ✅ สำเร็จ
- meow (haiku): "I'm ready to help!" ✅
- test (gemini): "I need more information..." ✅

**Issue found & fixed:** Claude Code 2.1.117 sends adaptive thinking headers
- Solution: `"alwaysThinkingEnabled": false` in settings
- Updated entrypoint script ให้ preserve model + thinking settings
- Tested both profiles successfully

## สิ่งที่ยังไม่ได้ทำ

### 1. Claude Code IDE session ผ่าน gateway (ตัวจริง)
ทดสอบใน IDE session (ไม่ใช่ docker container)
ต้อง restart Claude Code session after changing settings-local.json

### 3. Sonnet/Opus ผ่าน gateway
ตอนนี้ติด 429 rate limit จาก Anthropic (per-account)
ต้องรอ rate limit reset แล้วลองใหม่
