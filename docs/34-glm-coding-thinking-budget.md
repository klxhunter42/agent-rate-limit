# Coding บน GLM Gateway: ตั้ง `ZAI_THINKING_BUDGET`

แนะนำ `ZAI_THINKING_BUDGET=6000` สำหรับ coding ทั่วไป — sweet spot ระหว่างความเร็วและคุณภาพ reasoning

## ทำไมต้องตั้ง

Claude Code ส่ง `thinking:{budget_tokens:50000}` ให้ glm ทุก request ถ้า gateway ส่งต่อไป z.ai เลย glm จะคิด 20-75s (extended thinking) ทั้งที่ task เบา gateway strip/cap ให้แทน (`ZAI_THINKING_BUDGET`)

## ค่าที่เหมาะกับแต่ละงาน

| budget | latency/turn | คุณภาพ reasoning | เหมาะกับ |
|--------|-------------|------------------|---------|
| `0` | ~1-3s ⚡ | ไม่คิดเลย | typo, งานเบามาก, ถามข้อมูล |
| **`6000`** | **~3-6s** ✅ | **พอประมาณ** | **coding ทั่วไป (แนะนำ)** |
| `12000` | ~6-12s | ดี | debug ซับซ้อน, refactor ใหญ่ |
| `50000` (default Claude Code) | ~20-75s 🐌 | เกินไป | ไม่คุ้มเวลา |

## ทำไม 6000 คือ sweet spot สำหรับ coding

- **budget 0 (strip หมด):** glm กระโดดตอบเลย → งานเบา OK แต่ debug ยากจะเดาผิด → แก้วน → รวมแล้วช้ากว่า
- **budget 6000:** glm คิด 3-6s (พอเข้าใจปัญหา) แล้วตอบ → คุณภาพดี เวลาสมเหตุสมผล
- **budget 50000:** คิดเกินจำเป็น 20-75s/turn → 10 turn = ~7-12 นาทีรอ + cost token สูง

z.ai/glm เองไม่ได้ประโยชน์จาก extended thinking แบบ Claude แท้ (docs/26: "Z.AI has no extended thinking") คือ thinking ที่คืนมาไม่ใช่ reasoning คุณภาพสูง แค่ generate token เยอะเปล่า ๆ เลย cap ไว้ดีกว่า

## ตั้งค่าบน server 111

```bash
ssh klxhunter@192.168.5.111
cd ~/services/agent-rate-limit
# เพิ่ม/แก้ใน .env
echo "ZAI_THINKING_BUDGET=6000" >> .env   # หรือแก้บรรทัดที่มีอยู่
# recreate (ต้อง --force-recreate ไม่งั้น env ใหม่ไม่เข้า)
docker-compose up -d --force-recreate arl-gateway
```

## ขอบเขต
GLM/zai-only (gate `isZAIProvider`) **claude-oauth ไม่กระทบ**

## ดูเพิ่ม
- [CHANGELOG 2026-06-22b](../CHANGELOG.md) — root cause + fix detail
- [Troubleshooting: gateway ช้า](13-troubleshooting.md)
