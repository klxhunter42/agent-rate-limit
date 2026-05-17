#!/usr/bin/env python3
"""Check raw response format from gateway."""
import json
import urllib.request

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"

body = {
    "model": "glm-5.1",
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Say hello"}],
}
headers = {
    "Content-Type": "application/json",
    "Authorization": f"Bearer {API_KEY}",
    "X-Profile": "opt-cav-base",
}

req = urllib.request.Request(f"{BASE}/v1/messages", data=json.dumps(body).encode(), headers=headers, method="POST")
with urllib.request.urlopen(req, timeout=60) as resp:
    raw = resp.read().decode()
    parsed = json.loads(raw)
    print("Keys:", list(parsed.keys()))
    print("Usage:", json.dumps(parsed.get("usage", {}), indent=2))
    if "content" in parsed:
        print("Content type:", type(parsed["content"]))
        print("Content:", json.dumps(parsed["content"][:300] if isinstance(parsed["content"], str) else parsed["content"], indent=2, ensure_ascii=False))
    # Print full but truncated
    print("\nFull response (truncated):")
    print(json.dumps(parsed, indent=2, ensure_ascii=False)[:1500])
