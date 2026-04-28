# วิธีตั้งค่า Claude Code ให้คุยกับ Gateway แบบ Interactive

## ภาพรวม

คู่มือนี้อธิบายวิธีตั้งค่า Claude Code CLI บนเครื่อง remote ให้ส่ง request ผ่าน ARL gateway แทนกา��ไ� Anthropic API ตรง

ใช้ **profile key** (`arl_*` token) แทน raw OAuth token -- gateway จะ resolve profile เป็น account pool อัตโนมัติ

---

## สิ่งที่ต้องมี

| รายการ | รายละเอียด |
|---|---|
| Claude Code CLI | v2.1.118+ |
| Gateway | ARL gateway รันอยู่ (เช่น `http://192.168.5.62:9000`) |
| Profile key | `arl_*` token สร้างจาก gateway API |
| Node.js | v20+ (Claude Code requirement) |

---

## ขั้นตอนที่ 1: สร้าง Profile และ Token บน Gateway

รันบนเครื่องที่ gateway ทำงาน (เช่น 192.168.5.62):

```bash
# สร้าง profile (เชื่อมกับ claude-oauth provider)
curl -X POST http://localhost:9000/v1/profiles/ \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-remote",
    "target": "claude-oauth",
    "provider": "claude-oauth"
  }'

# สร้าง arl_ token
curl -X POST http://localhost:9000/v1/profiles/my-remote/tokens \
  -H 'Content-Type: application/json' \
  -d '{"keyName": "my-laptop"}'
# ได้ผลลัพธ์: {"token": "arl_xxxxxxxx..."}
```

เก็บ `arl_*` token นี้ไว้ใช้ในขั้นตอนถัดไป

---

## ขั้นตอนที่ 2: ตั้งค่า settings.json บน Remote

SSH เข้าเครื่อง remote แล้วสร้าง/แก้ไข `~/.claude/settings.json`:

```bash
python3 << 'SCRIPT'
import json

ARL_TOKEN = "arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1"
GATEWAY_URL = "http://192.168.5.62:9000"

settings = {
    "env": {
        "ANTHROPIC_BASE_URL": GATEWAY_URL,
        "ANTHROPIC_API_KEY": ARL_TOKEN,
        "CLAUDE_CODE_MAX_OUTPUT_TOKENS": "200000",
        "BASH_DEFAULT_TIMEOUT_MS": "1800000",
        "API_TIMEOUT_MS": "3000000",
        "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
        "CLAUDE_CODE_DISABLE_ANALYTICS": "1"
    },
    "permissions": {
        "allow": [
            "Bash",
            "Read",
            "Edit",
            "Write",
            "WebFetch",
            "Grep",
            "Glob",
            "LS",
            "MultiEdit",
            "NotebookRead",
            "NotebookEdit",
            "TodoRead",
            "TodoWrite",
            "WebSearch"
        ],
        "deny": [
            "Bash(rm -rf /)"
        ],
        "defaultMode": "acceptEdits"
    },
    "hooks": {},
    "autoUpdatesChannel": "stable",
    "apiKeyHelper": "echo $ANTHROPIC_API_KEY"
}

import os
path = os.path.expanduser("~/.claude/settings.json")
with open(path, "w") as f:
    json.dump(settings, f, indent=2)
print("Settings written to", path)
SCRIPT
```

### สิ่งสำคัญใน settings

| Key | ค่า | หมายเหตุ |
|---|---|---|
| `ANTHROPIC_BASE_URL` | `http://GATEWAY:PORT` | URL ของ gateway |
| `ANTHROPIC_API_KEY` | `arl_xxxxxxxx...` | Profile key จาก gateway |
| `apiKeyHelper` | `echo $ANTHROPIC_API_KEY` | **จำเป็น** - ทำให้ interactive mode อ่าน token จาก env |
| `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | `1` | ปิด telemetry |
| `CLAUDE_CODE_DISABLE_ANALYTICS` | `1` | ปิด analytics |

> **หมายเหตุ**: `apiKeyHelper` จำเป็นสำหรับ interactive mode (`claude` ไม่มี flag) - ถ้าไม่มีจะขึ้น "Not logged in" แม้ `ANTHROPIC_API_KEY` จะตั้งอยู่ใน env

---

## ขั้นตอนที่ 3: ทดสอบ

```bash
# ทดสอบ pipe mode (ต้องตอบกลับเร็ว ~4s)
echo "say ok" | claude -p

# ทดสอบ curl โดยตรงกับ gateway
curl -sf -w '\nHTTP:%{http_code} Time:%{time_total}s' \
  -X POST http://192.168.5.62:9000/v1/messages \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $ANTHROPIC_API_KEY" \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"claude-haiku-4-5-20251001","max_tokens":64,"messages":[{"role":"user","content":"say hi"}]}'
# ควรได้ HTTP:200, Time < 5s
```

---

## ขั้นตอนที่ 4: รัน Claude Code แบบ Interactive

```bash
cd /path/to/your/project
claude
```

ทุก request จะส่งผ่าน gateway ไป Anthropic API อัตโนมัติ

### ใช้โหมดต่างๆ

```bash
# Interactive mode (ปกติ)
claude

# One-shot prompt
claude "explain this code"

# Resume session ล่าสุด
claude --resume

# เลือก model เอง
claude --model claude-haiku-4-5-20251001
```

---

## ขั้นตอนที่ 5: เพิ่ม PATH (ทางเลือก)

ถ้า `claude` ยังไม่อยู่ใน PATH:

```bash
echo 'export PATH="/opt/homebrew/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

---

## การทำงานของ Gateway

```
Claude Code CLI (remote)
    |
    | HTTP POST /v1/messages
    | Authorization: Bearer arl_xxx (profile key)
    v
ARL Gateway
    |
    | 1. resolve arl_ token -> profile
    | 2. เลือก account จาก accountIds pool
    | 3. refresh OAuth token ถ้าหมดอายุ
    | 4. rewrite sonnet/opus -> haiku (หลีกเลี่ยง 429)
    | 5. clamp max_tokens ตาม model limit
    | 6. Privacy masking (PasteGuard)
    | 7. ส่งไป Anthropic ด้วย OAuth Bearer
    v
Anthropic API (api.anthropic.com)
    |
    | Response SSE
    v
Gateway: unmask + relay -> Claude Code
```

### สิ่งที่ gateway ทำให้

| ฟีเจอร์ | รายละเอียด |
|---|---|
| Auto-retry 429 | ลองใหม่สูงสุด 3 ครั้ง พร้อมหมุน account |
| Model rewrite | sonnet/opus -> haiku อัตโนมัติ (หลีกเลี่ยง org-level 429) |
| max_tokens clamp | จำกัดตาม model limit (haiku: 200K) |
| Privacy masking | ซ่อน secrets/PII ก่อนส่ง |
| OAuth token refresh | Refresh อัตโนมัติเมื่อใกล้หมดอายุ |
| Account pool | หมุน account ตาม utilization |

---

## การแก้ไขปัญหา

### "Not logged in" ใน interactive mode

เพิ่ม `apiKeyHelper` ใน `~/.claude/settings.json`:

```bash
python3 -c "
import json
s = json.load(open('$HOME/.claude/settings.json'))
s['apiKeyHelper'] = 'echo \$ANTHROPIC_API_KEY'
json.dump(s, open('$HOME/.claude/settings.json','w'), indent=2)
"
```

### Claude Code ไม่อ่าน settings.json

ตรวจสอบ:

```bash
# auth status
claude auth status
# ควรได้: apiKeySource: "ANTHROPIC_API_KEY"

# ตรวจ path
cat ~/.claude/settings.json | python3 -m json.tool
```

### Response ช้า (>30s)

อาจเป็น token refresh ครั้งแรก. ลอง request ซ้ำ:

```bash
# request ที่ 2 ควรเร็วขึ้น (< 5s)
echo "say ok" | claude -p
```

### ดู gateway logs

```bash
# จากเครื่อง gateway
docker logs arl-gateway --tail 50 -f
```

### ดู rate limit status

```bash
docker exec arl-dragonfly redis-cli KEYS 'arl:ratelimit:*'
```

---

## หมายเหตุ

- Gateway rewrite sonnet/opus -> haiku อัตโนมัติเพื่อหลีกเลี่ยง org-level rate limit
- ใช้ `arl_*` profile key แทน raw OAuth token -- gateway จะจัดการ token refresh เอง
- Privacy filter (PasteGuard) ซ่อน secrets และ PII ก่อนส่งไป Anthropic
- `apiKeyHelper` จำเป็นสำหรับ interactive mode - ถ้าไม่มีจะขึ้น "Not logged in"
