#!/usr/bin/env python3
"""Pordee controlled test - warm cache, 3 iterations per payload."""
import json, urllib.request, time

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"

def send(profile, messages):
    payload = json.dumps({"model": "glm-5.1", "max_tokens": 512, "messages": messages}).encode()
    headers = {"Content-Type": "application/json", "x-api-key": API_KEY, "X-Profile": profile}
    req = urllib.request.Request(f"{BASE}/v1/messages", data=payload, headers=headers, method="POST")
    resp = urllib.request.urlopen(req, timeout=120)
    body = json.loads(resp.read())
    u = body.get("usage", {})
    return u.get("input_tokens", 0), u.get("output_tokens", 0), u.get("cache_read_input_tokens", 0)

payloads = [
    ("Thai short", "Thai", [{"role": "user", "content": "อธิบาย Kubernetes Ingress ให้หน่อย"}]),
    ("Thai verbose", "Thai", [
        {"role": "user", "content": "คุณเป็นผู้ช่วย AI ที่มีความรู้ด้าน DevOps และ Kubernetes คุณต้องตอบคำถามด้วยความรู้ความสามารถอย่างเต็มที่ คุณต้องให้คำตอบที่ละเอียดและครบถ้วน คุณต้องอธิบายให้เข้าใจง่ายและชัดเจน คุณต้องใช้ภาษาที่เหมาะสมและเป็นทางการ คุณต้องให้ตัวอย่างประกอบเพื่อความเข้าใจ คุณต้องสรุปประเด็นสำคัญในตอนท้าย คุณต้องตรวจสอบความถูกต้องของข้อมูลก่อนตอบ คุณต้องจัดระเบียบคำตอบให้เป็นหมวดหมู่ คุณต้องใช้คำศัพท์เฉพาะทางที่ถูกต้อง คุณต้องอ้างอิงแหล่งข้อมูลที่เชื่อถือได้ คุณต้องให้คำแนะนำเพิ่มเติมสำหรับการศึกษาเพิ่มเติม คุณต้องพิจารณามุมมองหลายด้าน คุณต้องเปรียบเทียบข้อดีข้อเสีย คุณต้องแบ่งปันประสบการณ์จริง คุณต้องเน้นข้อควรระวังและ best practices คุณต้องแนะนำเครื่องมือที่เกี่ยวข้อง คุณต้องอธิบายข้อจำกัดและข้อควรพิจารณา คุณต้องให้ troubleshooting tips คุณต้องสรุปขั้นตอนการใช้งานจริง คุณต้องแนะนำการตั้งค่าเริ่มต้นที่เหมาะสม คุณต้องอธิบายความแตกต่างจากทางเลือกอื่น คุณต้องให้ข้อมูลเกี่ยวกับ performance และ scalability คุณต้องแนะนำวิธี monitoring และ debugging"},
        {"role": "assistant", "content": "เข้าใจแล้วครับ ฉันพร้อมช่วยเหลือคุณเรื่อง DevOps และ Kubernetes ครับ กรุณาถามได้เลย"},
        {"role": "user", "content": "ช่วยอธิบาย Kubernetes Ingress ให้หน่อย อยากรู้ว่ามันคืออะไร ทำงานยังไง ต่างจาก Service ยังไง และควรใช้เมื่อไหร่"},
    ]),
    ("English short", "English", [{"role": "user", "content": "Explain Kubernetes Ingress in simple terms"}]),
    ("Mixed TH+EN", "Mixed", [{"role": "user", "content": "Help me debug pod api-gw ที่ CrashLoopBackOff ตลอดเลย log ขึ้น connection refused to upstream:8443 ควรเช็คอะไรบ้าง"}]),
    ("Thai code gen", "Thai", [{"role": "user", "content": "เขียน Go HTTP server ให้หน่อย ที่มี health check endpoint และสามารถรองรับ graceful shutdown ได้"}]),
]

print("=== WARM-UP: 2 runs per profile per payload ===")
for label, lang, msg in payloads:
    for prof in ["pordee-test-base", "pordee-test-only"]:
        for w in range(2):
            try:
                send(prof, msg)
            except Exception:
                pass
            time.sleep(0.2)
    print(f"  {label}: warmed")

time.sleep(1)
print()
print("=== MEASURED RUNS (3 per payload, after warm cache) ===")
print(f"| # | Language | Payload       | B_In | P_In | In D   | B_Out | P_Out | Out D  | B_Cache | P_Cache | Verdict |")
print(f"|---|----------|---------------|------|------|--------|-------|-------|--------|---------|---------|---------|")

for i, (label, lang, msg) in enumerate(payloads):
    base_ins, pordee_ins = [], []
    base_outs, pordee_outs = [], []
    base_caches, pordee_caches = [], []

    for run in range(3):
        bi, bo, bc = send("pordee-test-base", msg)
        time.sleep(0.3)
        pi, po, pc = send("pordee-test-only", msg)
        time.sleep(0.3)
        base_ins.append(bi)
        base_outs.append(bo)
        base_caches.append(bc)
        pordee_ins.append(pi)
        pordee_outs.append(po)
        pordee_caches.append(pc)

    avg_bi = sum(base_ins) / 3
    avg_pi = sum(pordee_ins) / 3
    avg_bo = sum(base_outs) / 3
    avg_po = sum(pordee_outs) / 3
    avg_bc = sum(base_caches) / 3
    avg_pc = sum(pordee_caches) / 3

    in_d = ((avg_pi - avg_bi) / avg_bi * 100) if avg_bi > 0 else 0
    out_d = ((avg_po - avg_bo) / avg_bo * 100) if avg_bo > 0 else 0

    if in_d < -5:
        verdict = "SAVINGS"
    elif in_d > 5:
        verdict = "OVERHEAD"
    else:
        verdict = "NEUTRAL"

    print(f"| {i+1} | {lang:8s} | {label:13s} | {avg_bi:4.0f} | {avg_pi:4.0f} | {in_d:+5.1f}% | {avg_bo:5.0f} | {avg_po:5.0f} | {out_d:+5.1f}% | {avg_bc:7.0f} | {avg_pc:7.0f} | {verdict} |")

print()
print("=== RAW per-run data ===")
for i, (label, lang, msg) in enumerate(payloads):
    print(f"\n[{i+1}] {label} ({lang}):")
    for run in range(3):
        print(f"  run {run+1}: base in={base_ins[run] if run < len(base_ins) else '?'} out={base_outs[run] if run < len(base_outs) else '?'} | pordee in={pordee_ins[run] if run < len(pordee_ins) else '?'} out={pordee_outs[run] if run < len(pordee_outs) else '?'}")
