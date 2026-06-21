"""
Qoder ↔ NVIDIA NIM Proxy for Kimi K2.6
========================================
Sits between Qoder's Kimi BYOK integration and NVIDIA's OpenAI-compatible API.
Rewrites model names so Qoder's "kimi-k2.6" becomes NVIDIA's "moonshotai/kimi-k2.6".

Usage:
  1. Run: python kimi_proxy.py
  2. In Qoder: Add Kimi BYOK with:
      - API Key: YOUR_NVIDIA_API_KEY (set NVIDIA_API_KEY env var)
     - Base URL override (if supported): http://localhost:8765/v1

If Qoder doesn't allow URL override for Kimi, see README instructions below.
"""

import http.server
import json
import os
import urllib.request
import ssl
import sys

LISTEN_PORT = 8765
NVIDIA_BASE = "https://integrate.api.nvidia.com/v1"
NVIDIA_KEY = os.environ.get("NVIDIA_API_KEY", "YOUR_NVIDIA_API_KEY")

# Map Qoder model names -> NVIDIA model names
MODEL_MAP = {
    "kimi-k2.6": "moonshotai/kimi-k2.6",
    "moonshot-v1-8k": "moonshotai/kimi-k2.6",
    "moonshot-v1-32k": "moonshotai/kimi-k2.6",
    "moonshot-v1-128k": "moonshotai/kimi-k2.6",
}


class ProxyHandler(http.server.BaseHTTPRequestHandler):
    def _target_url(self):
        """Strip /v1 prefix from path since NVIDIA_BASE already includes it."""
        path = self.path
        for prefix in ["/v1", "/V1"]:
            if path.startswith(prefix):
                path = path[len(prefix):]
        return f"{NVIDIA_BASE}{path}"

    def do_GET(self):
        """Handle GET requests (e.g. /v1/models)"""
        target_url = self._target_url()
        req = urllib.request.Request(target_url, method="GET")
        req.add_header("Authorization", f"Bearer {NVIDIA_KEY}")
        req.add_header("Accept", "application/json")
        try:
            ctx = ssl.create_default_context()
            with urllib.request.urlopen(req, context=ctx) as resp:
                body = resp.read()
                self.send_response(resp.status)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                # Rewrite model list to show Qoder-compatible names
                data = json.loads(body)
                # Inject our mapped models at the top
                injected = [
                    {"id": "kimi-k2.6", "object": "model", "created": 1700000000, "owned_by": "moonshot"},
                ]
                if "data" in data:
                    data["data"] = injected + data["data"]
                self.wfile.write(json.dumps(data).encode())
        except Exception as e:
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())

    def do_POST(self):
        """Handle POST requests (chat/completions) — rewrite model name & forward"""
        content_len = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_len)

        try:
            payload = json.loads(body) if body else {}
        except json.JSONDecodeError:
            payload = {}

        # Rewrite model name
        original_model = payload.get("model", "")
        mapped_model = MODEL_MAP.get(original_model, original_model)
        payload["model"] = mapped_model

        target_url = self._target_url()
        is_stream = payload.get("stream", False)

        req = urllib.request.Request(
            target_url,
            data=json.dumps(payload).encode(),
            method="POST",
        )
        req.add_header("Authorization", f"Bearer {NVIDIA_KEY}")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "text/event-stream" if is_stream else "application/json")

        try:
            ctx = ssl.create_default_context()
            with urllib.request.urlopen(req, context=ctx) as resp:
                resp_body = resp.read()
                self.send_response(resp.status)
                if is_stream:
                    self.send_header("Content-Type", "text/event-stream")
                else:
                    self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(resp_body)
        except urllib.error.HTTPError as e:
            err_body = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(err_body)
        except Exception as e:
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())

    def log_message(self, format, *args):
        print(f"[proxy] {args[0]}")


def main():
    server = http.server.HTTPServer(("127.0.0.1", LISTEN_PORT), ProxyHandler)
    print(f"Kimi proxy running on http://127.0.0.1:{LISTEN_PORT}")
    print(f"Forwarding to: {NVIDIA_BASE}")
    print(f"Model mapping: {MODEL_MAP}")
    print()
    print("In Qoder, configure Kimi BYOK with:")
    print(f"  API Key:  {NVIDIA_KEY[:20]}...")
    print(f"  Base URL: http://127.0.0.1:{LISTEN_PORT}/v1")
    print()
    print("Press Ctrl+C to stop.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down.")
        server.shutdown()


if __name__ == "__main__":
    main()
