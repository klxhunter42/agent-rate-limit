#!/usr/bin/env python3
"""POC: Text Compression optimizer stage deep test."""
import json
import urllib.request
import urllib.error
import time

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"
MAX_TOKENS = 256

ALL_OFF = {
    "semantic_dedup": False, "chunker": False, "sketch": False,
    "textcomp": False, "caveman": False, "pordee": False,
    "toolcomp": False, "toolfilter": False
}

TEXTCOMP_ONLY = {
    "semantic_dedup": False, "chunker": False, "sketch": False,
    "caveman": False, "pordee": False, "toolcomp": False, "toolfilter": False
}

def api(method, path, body=None, headers=None):
    hdrs = {"Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        err_body = e.read().decode()
        print(f"  HTTP {e.code}: {err_body[:300]}")
        return None

def create_profile(name, overrides):
    body = {
        "name": name,
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": overrides
    }
    resp = api("POST", "/v1/profiles", body)
    if resp:
        print(f"  Profile '{name}' created: {resp.get('id', resp.get('name', 'ok'))}")
    else:
        print(f"  Profile '{name}' may already exist, continuing...")
    return resp

def send_message(profile_name, system, messages):
    body = {
        "model": MODEL,
        "max_tokens": MAX_TOKENS,
        "messages": messages
    }
    if system:
        body["system"] = system
    hdrs = {
        "X-Profile": profile_name,
        "Authorization": f"Bearer {API_KEY}"
    }
    resp = api("POST", "/v1/messages", body, hdrs)
    if resp and "usage" in resp:
        return {
            "input_tokens": resp["usage"].get("input_tokens", "N/A"),
            "output_tokens": resp["usage"].get("output_tokens", "N/A")
        }
    return {"input_tokens": "ERR", "output_tokens": "ERR"}

# --- Payloads (Anthropic format: system is top-level, messages are user/assistant only) ---
PAYLOADS = [
    {
        "name": "Verbose system prompt (whitespace)",
        "system": """You are a helpful assistant.   You should always be polite and respectful.



You should provide detailed   and   thorough   answers to all questions.


When the user asks about code, you should explain each line carefully.




When the user asks about infrastructure, you should consider cost,    reliability,    and    scalability.



Always use best practices.    Always follow security guidelines.    Always test your code.






Remember to be concise but complete.    Remember to be accurate.    Remember to be helpful.




Good luck!""",
        "messages": [
            {"role": "user", "content": "What is Kubernetes?"}
        ]
    },
    {
        "name": "Large JSON config block",
        "system": "You are a config validator.",
        "messages": [
            {"role": "user", "content": """Validate this Kubernetes deployment config:

```json
{
    "apiVersion": "apps/v1",
    "kind": "Deployment",
    "metadata": {
        "name": "my-app",
        "namespace": "production",
        "labels": {
            "app": "my-app",
            "version": "v2.1.0",
            "team": "platform",
            "env": "production"
        },
        "annotations": {
            "deployed-by": "argo-cd",
            "rollout-strategy": "canary"
        }
    },
    "spec": {
        "replicas": 3,
        "selector": {
            "matchLabels": {
                "app": "my-app"
            }
        },
        "template": {
            "metadata": {
                "labels": {
                    "app": "my-app",
                    "version": "v2.1.0"
                }
            },
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "my-registry/my-app:v2.1.0",
                        "ports": [{"containerPort": 8080}],
                        "resources": {
                            "requests": {"cpu": "250m", "memory": "256Mi"},
                            "limits": {"cpu": "500m", "memory": "512Mi"}
                        },
                        "env": [
                            {"name": "LOG_LEVEL", "value": "info"},
                            {"name": "DB_HOST", "valueFrom": {"secretKeyRef": {"name": "db-secret", "key": "host"}}}
                        ]
                    }
                ]
            }
        }
    }
}
```

Is this config valid?"""}
        ]
    },
    {
        "name": "Code diff with empty lines",
        "system": "You are a code reviewer.",
        "messages": [
            {"role": "user", "content": """Review this diff:

```diff
--- a/main.go
+++ b/main.go
@@ -1,10 +1,15 @@
 package main


 import (
     "fmt"


     "net/http"


     "os"
 )


 func main() {


-    fmt.Println("hello")
+    port := os.Getenv("PORT")
+    if port == "" {
+        port = "8080"
+    }
+
+    http.HandleFunc("/", handler)
+    fmt.Printf("Listening on %s\\n", port)
+    http.ListenAndServe(":"+port, nil)
 }


+func handler(w http.ResponseWriter, r *http.Request) {
+    fmt.Fprintf(w, "Hello, World!")
+}
+
```

What changed?"""}
        ]
    },
    {
        "name": "Clean prompt (control)",
        "system": "You are a helpful assistant.",
        "messages": [
            {"role": "user", "content": "Explain what a rate limiter is in 2 sentences."}
        ]
    },
    {
        "name": "Mixed: repeated blank lines",
        "system": """You are an expert DevOps engineer.


You specialize in Kubernetes, Terraform, and CI/CD pipelines.




Always follow security best practices.


Always consider cost optimization.



Prefer simple solutions over complex ones.""",
        "messages": [
            {"role": "user", "content": """I need to set up a CI/CD pipeline.


We use GitHub Actions.


Deploy to Kubernetes.




The app is a Node.js microservice.



It uses Docker for containerization.


Can you help me get started?"""}
        ]
    }
]

def main():
    print("=" * 60)
    print("Text Compression POC Test")
    print("=" * 60)

    # Create profiles
    print("\n[1] Creating profiles...")
    create_profile("opt-textcomp-deep", TEXTCOMP_ONLY)
    create_profile("opt-baseline-tc", ALL_OFF)

    # Run tests
    results = []
    for i, payload in enumerate(PAYLOADS, 1):
        print(f"\n[{i+1}] Testing: {payload['name']}")

        print(f"  Baseline...")
        base = send_message("opt-baseline-tc", payload.get("system"), payload["messages"])
        time.sleep(1)

        print(f"  TextComp...")
        tc = send_message("opt-textcomp-deep", payload.get("system"), payload["messages"])
        time.sleep(1)

        results.append({
            "#": i,
            "Payload": payload["name"],
            "base_in": base["input_tokens"],
            "tc_in": tc["input_tokens"],
            "base_out": base["output_tokens"],
            "tc_out": tc["output_tokens"]
        })
        print(f"  Base: in={base['input_tokens']} out={base['output_tokens']} | TC: in={tc['input_tokens']} out={tc['output_tokens']}")

    # Print table
    print("\n" + "=" * 130)
    hdr = f"| {'#':>1} | {'Payload Type':<35} | {'Base In':>8} | {'TC In':>8} | {'In Saved':>11} | {'Base Out':>8} | {'TC Out':>8} | {'Out Saved':>11} | {'Verdict':<12} |"
    sep = f"|{'-'*3}|{'-'*37}|{'-'*10}|{'-'*10}|{'-'*13}|{'-'*10}|{'-'*10}|{'-'*13}|{'-'*14}|"
    print(hdr)
    print(sep)

    for r in results:
        bi, ti = r["base_in"], r["tc_in"]
        bo, to_ = r["base_out"], r["tc_out"]

        def calc_saved(b, t):
            if isinstance(b, (int, float)) and isinstance(t, (int, float)) and b > 0:
                pct = ((b - t) / b) * 100
                return f"{int(b-t)} ({pct:.1f}%)"
            return "N/A"

        def verdict(b, t):
            if isinstance(b, (int, float)) and isinstance(t, (int, float)) and b > 0:
                diff = b - t
                if diff > 50: return "STRONG SAVE"
                if diff > 10: return "MODERATE"
                if diff > 0: return "MINIMAL"
                if diff == 0: return "NEUTRAL"
                return "OVERHEAD"
            return "UNKNOWN"

        in_saved = calc_saved(bi, ti)
        out_saved = calc_saved(bo, to_)
        v = verdict(bi, ti)

        print(f"| {r['#']:>1} | {r['Payload']:<35} | {str(bi):>8} | {str(ti):>8} | {in_saved:>11} | {str(bo):>8} | {str(to_):>8} | {out_saved:>11} | {v:<12} |")

    print("=" * 130)

if __name__ == "__main__":
    main()
