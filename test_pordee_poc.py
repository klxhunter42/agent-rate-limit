#!/usr/bin/env python3
"""Pordee optimizer POC test - Thai vs English payload investigation."""
import json
import urllib.request
import urllib.error
import time

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"
MAX_TOKENS = 512

PAYLOADS = [
    {
        "label": "Thai-only short",
        "lang": "Thai",
        "messages": [
            {"role": "user", "content": "อธิบาย Kubernetes Ingress ให้หน่อย"}
        ]
    },
    {
        "label": "Thai verbose redundant",
        "lang": "Thai",
        "messages": [
            {"role": "system", "content": (
                "คุณเป็นผู้ช่วย AI ที่มีความรู้ด้าน DevOps และ Kubernetes "
                "คุณต้องตอบคำถามด้วยความรู้ความสามารถอย่างเต็มที่ "
                "คุณต้องให้คำตอบที่ละเอียดและครบถ้วน "
                "คุณต้องอธิบายให้เข้าใจง่ายและชัดเจน "
                "คุณต้องใช้ภาษาที่เหมาะสมและเป็นทางการ "
                "คุณต้องให้ตัวอย่างประกอบเพื่อความเข้าใจ "
                "คุณต้องสรุปประเด็นสำคัญในตอนท้าย "
                "คุณต้องตรวจสอบความถูกต้องของข้อมูลก่อนตอบ "
                "คุณต้องจัดระเบียบคำตอบให้เป็นหมวดหมู่ "
                "คุณต้องใช้คำศัพท์เฉพาะทางที่ถูกต้อง "
                "คุณต้องอ้างอิงแหล่งข้อมูลที่เชื่อถือได้ "
                "คุณต้องให้คำแนะนำเพิ่มเติมสำหรับการศึกษาเพิ่มเติม "
                "คุณต้องพิจารณามุมมองหลายด้าน "
                "คุณต้องเปรียบเทียบข้อดีข้อเสีย "
                "คุณต้องแบ่งปันประสบการณ์จริง "
                "คุณต้องเน้นข้อควรระวังและ best practices "
                "คุณต้องแนะนำเครื่องมือที่เกี่ยวข้อง "
                "คุณต้องอธิบายข้อจำกัดและข้อควรพิจารณา "
                "คุณต้องให้ troubleshooting tips "
                "คุณต้องสรุปขั้นตอนการใช้งานจริง "
                "คุณต้องแนะนำการตั้งค่าเริ่มต้นที่เหมาะสม "
                "คุณต้องอธิบายความแตกต่างจากทางเลือกอื่น "
                "คุณต้องให้ข้อมูลเกี่ยวกับ performance และ scalability "
                "คุณต้องแนะนำวิธี monitoring และ debugging"
            )},
            {"role": "user", "content": "ช่วยอธิบาย Kubernetes Ingress ให้หน่อย อยากรู้ว่ามันคืออะไร ทำงานยังไง ต่างจาก Service ยังไง และควรใช้เมื่อไหร่ ขอรายละเอียดเยอะๆ เต็มที่เลยนะครับ อยากเข้าใจทุกแง่มุมเลย"}
        ]
    },
    {
        "label": "English short",
        "lang": "English",
        "messages": [
            {"role": "user", "content": "Explain Kubernetes Ingress in simple terms"}
        ]
    },
    {
        "label": "Mixed Thai+English",
        "lang": "Mixed",
        "messages": [
            {"role": "user", "content": "Help me debug pod api-gw ที่ CrashLoopBackOff ตลอดเลย log ขึ้น connection refused to upstream:8443 ควรเช็คอะไรบ้าง"}
        ]
    },
    {
        "label": "Thai code gen",
        "lang": "Thai",
        "messages": [
            {"role": "user", "content": "เขียน Go HTTP server ให้หน่อย ที่มี health check endpoint และสามารถรองรับ graceful shutdown ได้"}
        ]
    },
]


def create_profile(name, pordee_enabled):
    overrides = {
        "semantic_dedup": False,
        "chunker": False,
        "sketch": False,
        "textcomp": False,
        "caveman": False,
        "pordee": pordee_enabled,
        "toolcomp": False,
        "toolfilter": False,
    }
    payload = {
        "name": name,
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": overrides,
    }
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        f"{BASE}/v1/profiles",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req)
        body = json.loads(resp.read())
        print(f"Profile '{name}' created/updated: ok")
        return name
    except urllib.error.HTTPError as e:
        print(f"Profile '{name}' error: {e.code} {e.read().decode()[:200]}")
        return name  # name-based, so return it anyway


def send_messages(profile_name, messages):
    payload = {
        "model": MODEL,
        "max_tokens": MAX_TOKENS,
        "messages": messages,
    }
    data = json.dumps(payload).encode()
    headers = {
        "Content-Type": "application/json",
        "x-api-key": API_KEY,
        "X-Profile": profile_name,
    }
    req = urllib.request.Request(
        f"{BASE}/v1/messages",
        data=data,
        headers=headers,
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=120)
        body = json.loads(resp.read())
        usage = body.get("usage", {})
        content_blocks = body.get("content", [])
        text = ""
        for b in content_blocks:
            if b.get("type") == "text":
                text += b.get("text", "")
        return {
            "input": usage.get("input_tokens", 0),
            "output": usage.get("output_tokens", 0),
            "cache_read": usage.get("cache_read_input_tokens", 0),
            "content": text[:150],
            "ok": True,
        }
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        print(f"  ERROR: {e.code} {err[:300]}")
        return {"input": 0, "output": 0, "cache_read": 0, "content": f"ERROR {e.code}", "ok": False}


def main():
    print("=" * 80)
    print("PORDEE OPTIMIZER POC TEST")
    print("=" * 80)

    # Create profiles
    print("\n--- Creating profiles ---")
    base_profile = create_profile("pordee-test-base", False)
    pordee_profile = create_profile("pordee-test-only", True)
    print(f"Base profile: {base_profile}")
    print(f"Pordee profile: {pordee_profile}")

    # Run tests
    results = []
    print("\n--- Running payload tests ---")

    for i, p in enumerate(PAYLOADS):
        print(f"\n[{i+1}/5] {p['label']} ({p['lang']})")

        # Baseline
        print(f"  Baseline...", end=" ", flush=True)
        base = send_messages(base_profile, p["messages"])
        print(f"in={base['input']} out={base['output']} cache_read={base['cache_read']}")
        time.sleep(0.3)

        # Pordee
        print(f"  Pordee...", end=" ", flush=True)
        pordee = send_messages(pordee_profile, p["messages"])
        print(f"in={pordee['input']} out={pordee['output']} cache_read={pordee['cache_read']}")
        time.sleep(0.3)

        in_delta = pordee["input"] - base["input"]
        in_pct = (in_delta / base["input"] * 100) if base["input"] > 0 else 0
        out_delta = pordee["output"] - base["output"]
        out_pct = (out_delta / base["output"] * 100) if base["output"] > 0 else 0

        if not base["ok"] or not pordee["ok"]:
            verdict = "ERROR"
        elif in_pct < -5:
            verdict = "SAVINGS"
        elif in_pct > 5:
            verdict = "OVERHEAD"
        else:
            verdict = "NO CHANGE"

        results.append({
            "num": i + 1,
            "lang": p["lang"],
            "label": p["label"],
            "base_in": base["input"],
            "pordee_in": pordee["input"],
            "in_delta": in_pct,
            "base_out": base["output"],
            "pordee_out": pordee["output"],
            "out_delta": out_pct,
            "base_cache": base["cache_read"],
            "pordee_cache": pordee["cache_read"],
            "verdict": verdict,
            "base_text": base["content"],
            "pordee_text": pordee["content"],
        })

    # Print table
    print("\n" + "=" * 80)
    print("RESULTS TABLE")
    print("=" * 80)
    print(f"| # | Language | Payload               | Base In | Pordee In | In D    | Base Out | Pordee Out | Out D   | Verdict     |")
    print(f"|---|----------|-----------------------|---------|-----------|---------|----------|------------|---------|-------------|")
    for r in results:
        print(f"| {r['num']} | {r['lang']:8s} | {r['label']:21s} | {r['base_in']:7d} | {r['pordee_in']:9d} | {r['in_delta']:+6.1f}% | {r['base_out']:8d} | {r['pordee_out']:10d} | {r['out_delta']:+6.1f}% | {r['verdict']:11s} |")

    # Cache tokens
    print(f"\n--- Cache tokens ---")
    for r in results:
        print(f"  [{r['num']}] {r['label']}: base_cache={r['base_cache']}, pordee_cache={r['pordee_cache']}")

    # Response quality spot check
    print(f"\n--- Response preview (first 120 chars) ---")
    for r in results:
        print(f"  [{r['num']}] BASE:   {r['base_text'][:120]}")
        print(f"       PORDEE: {r['pordee_text'][:120]}")
        print()

    # Analysis
    print("=" * 80)
    print("ANALYSIS")
    print("=" * 80)

    thai_results = [r for r in results if r["lang"] == "Thai"]
    mixed_results = [r for r in results if r["lang"] == "Mixed"]
    eng_results = [r for r in results if r["lang"] == "English"]

    if thai_results:
        avg_thai_in = sum(r["in_delta"] for r in thai_results) / len(thai_results)
        print(f"Thai-only payloads avg input delta: {avg_thai_in:+.1f}%")
    if mixed_results:
        avg_mixed_in = sum(r["in_delta"] for r in mixed_results) / len(mixed_results)
        print(f"Mixed Thai+English avg input delta: {avg_mixed_in:+.1f}%")
    if eng_results:
        avg_eng_in = sum(r["in_delta"] for r in eng_results) / len(eng_results)
        print(f"English-only avg input delta: {avg_eng_in:+.1f}%")

    any_savings = any(r["in_delta"] < -5 for r in results)
    any_overhead = any(r["in_delta"] > 5 for r in results)
    print(f"\nAny payload with >5% input savings: {any_savings}")
    print(f"Any payload with >5% input overhead: {any_overhead}")

    # Verdict by payload type
    print(f"\n--- Verdict summary ---")
    for r in results:
        print(f"  [{r['num']}] {r['label']} ({r['lang']}): In={r['in_delta']:+.1f}%, Out={r['out_delta']:+.1f}% -> {r['verdict']}")


if __name__ == "__main__":
    main()
