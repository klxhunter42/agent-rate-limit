#!/usr/bin/env python3
"""Lightweight Anthropic-to-OpenAI format translator proxy."""
import json
import sys
import uuid
import http.server
from http.client import HTTPSConnection
from urllib.parse import urlparse

UPSTREAM_URL = "api-cpxis.lotuss.com"
UPSTREAM_PATH = "/llm/v1/chat/completions"
API_KEY = "devops.lotuss.Db90t8pjZE2kdn0uI8os9jldmLoH9s"
DEFAULT_MODEL = "default"
LISTEN_PORT = 8999


def anthropic_to_openai(body):
    messages = []
    if body.get("system"):
        s = body["system"]
        if isinstance(s, list):
            texts = [b.get("text", "") for b in s if b.get("type") == "text"]
            s = "\n".join(texts)
        messages.append({"role": "system", "content": s})
    for m in body.get("messages", []):
        content = m.get("content", "")
        if isinstance(content, list):
            texts = [b.get("text", "") for b in content if b.get("type") == "text"]
            content = "\n".join(texts)
        messages.append({"role": m.get("role", "user"), "content": content})
    mt = min(body.get("max_tokens", 4096), 16000)
    return {
        "model": DEFAULT_MODEL,  # Always override to upstream model name
        "messages": messages,
        "max_tokens": mt,
        "temperature": body.get("temperature", 0.7),
        "stream": False,
    }


def openai_to_anthropic(resp):
    choice = resp.get("choices", [{}])[0]
    msg = choice.get("message", {})
    usage = resp.get("usage", {})
    return {
        "id": resp.get("id", "msg_" + uuid.uuid4().hex[:24]),
        "type": "message",
        "role": "assistant",
        "content": [{"type": "text", "text": msg.get("content", "")}],
        "model": "claude-sonnet-4-20250514",  # Fake model name so Claude Code accepts it
        "stop_reason": "end_turn" if choice.get("finish_reason") == "stop" else choice.get("finish_reason"),
        "usage": {
            "input_tokens": usage.get("prompt_tokens", 0),
            "output_tokens": usage.get("completion_tokens", 0),
        },
    }


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        path = urlparse(self.path).path
        if not path.startswith("/v1/messages"):
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw)
        except Exception:
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": "invalid json"}).encode())
            return
        openai_body = anthropic_to_openai(body)
        conn = HTTPSConnection(UPSTREAM_URL, timeout=120)
        conn.request("POST", UPSTREAM_PATH,
                     json.dumps(openai_body),
                     {"Content-Type": "application/json",
                      "Authorization": "Bearer " + API_KEY})
        resp = conn.getresponse()
        data = resp.read()
        conn.close()
        if resp.status != 200:
            self.send_response(resp.status)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(data)
            return
        openai_resp = json.loads(data)
        anthro = openai_to_anthropic(openai_resp)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(anthro).encode())

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok"}).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, fmt, *args):
        print(fmt % args, flush=True)


if __name__ == "__main__":
    server = http.server.HTTPServer(("127.0.0.1", LISTEN_PORT), Handler)
    print("Proxy listening on http://127.0.0.1:" + str(LISTEN_PORT))
    server.serve_forever()
