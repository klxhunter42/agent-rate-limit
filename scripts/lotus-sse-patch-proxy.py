#!/usr/bin/env python3
"""Thin proxy that patches Lotus SSE streaming events to be Claude Code compatible.

Lotus /v1/messages returns Anthropic-format responses but the streaming `message_start`
event is missing `usage` (and `role`) in the message object, which causes:
  "Cannot set properties of undefined (setting 'output_tokens')"

This proxy injects the missing fields in SSE events before relaying to the client.
"""
import json
import sys
import http.server
from http.client import HTTPSConnection

UPSTREAM_HOST = "llm.internal/custom"
UPSTREAM_PATH_PREFIX = "/llm"
API_KEY = "REDACTED"
LISTEN_PORT = 8999


def patch_message_start(data):
    msg = data.get("message", {})
    if "usage" not in msg:
        msg["usage"] = {"input_tokens": 0, "output_tokens": 0}
    if "role" not in msg:
        msg["role"] = "assistant"
    if "type" not in msg:
        msg["type"] = "message"
    if "stop_reason" not in msg:
        msg["stop_reason"] = None
    data["message"] = msg
    return data


def patch_event(event_type, raw_data):
    try:
        data = json.loads(raw_data)
    except (json.JSONDecodeError, TypeError):
        return raw_data
    if event_type == "message_start":
        data = patch_message_start(data)
    return json.dumps(data, ensure_ascii=False)


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        path = self.path
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

        upstream_path = UPSTREAM_PATH_PREFIX + path
        headers = {
            "Content-Type": "application/json",
            "Authorization": "Bearer " + API_KEY,
            "anthropic-version": self.headers.get("anthropic-version", "2023-06-01"),
        }
        if self.headers.get("anthropic-beta"):
            headers["anthropic-beta"] = self.headers.get("anthropic-beta")

        conn = HTTPSConnection(UPSTREAM_HOST, timeout=120)
        conn.request("POST", upstream_path, json.dumps(body), headers)
        resp = conn.getresponse()

        is_stream = body.get("stream", False)

        if not is_stream:
            data = resp.read()
            conn.close()
            self.send_response(resp.status)
            for h in ["Content-Type"]:
                v = resp.getheader(h)
                if v:
                    self.send_header(h, v)
            self.end_headers()
            self.wfile.write(data)
            return

        # Streaming: patch SSE events
        self.send_response(resp.status)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()

        buf = ""
        current_event = None
        try:
            while True:
                chunk = resp.read(4096)
                if not chunk:
                    break
                buf += chunk.decode("utf-8", errors="replace")

                while "\n" in buf:
                    line, buf = buf.split("\n", 1)
                    line = line.rstrip("\r")

                    if line.startswith("event: "):
                        current_event = line[7:].strip()
                    elif line.startswith("data: "):
                        raw_data = line[6:]
                        if current_event in ("message_start", "message_delta", "content_block_start",
                                             "content_block_delta", "content_block_stop", "message_stop"):
                            raw_data = patch_event(current_event, raw_data)
                        line = "data: " + raw_data
                    elif line == "":
                        current_event = None

                    self.wfile.write((line + "\n").encode())
                    self.wfile.flush()
        except Exception:
            pass
        finally:
            if buf.strip():
                self.wfile.write(buf.encode())
                self.wfile.flush()
            conn.close()

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
    print(f"SSE-patch proxy on http://127.0.0.1:{LISTEN_PORT} -> {UPSTREAM_HOST}{UPSTREAM_PATH_PREFIX}")
    server.serve_forever()
