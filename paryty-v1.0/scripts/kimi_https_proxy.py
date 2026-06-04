"""
Qoder Kimi HTTPS Proxy (debug version)
Listens on 127.0.0.1:443, forwards to NVIDIA API.
"""
import http.server
import json
import ssl
import socketserver
import urllib.request
import os
import sys
import datetime

LISTEN_PORT = 443
NVIDIA_BASE = "https://integrate.api.nvidia.com/v1"
NVIDIA_KEY = "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"
CERT_DIR = os.path.dirname(os.path.abspath(__file__))
CERT_FILE = os.path.join(CERT_DIR, "moonshot_cert.pem")
KEY_FILE = os.path.join(CERT_DIR, "moonshot_key.pem")
LOG_FILE = os.path.join(CERT_DIR, "proxy.log")

MODEL_MAP = {
    "kimi-k2.6": "moonshotai/kimi-k2.6",
    "kimi-k2": "moonshotai/kimi-k2.6",
    "moonshot-v1-8k": "moonshotai/kimi-k2.6",
    "moonshot-v1-32k": "moonshotai/kimi-k2.6",
    "moonshot-v1-128k": "moonshotai/kimi-k2.6",
    "moonshot-v1-auto": "moonshotai/kimi-k2.6",
}


def log(msg):
    ts = datetime.datetime.now().strftime("%H:%M:%S")
    line = f"[{ts}] {msg}"
    print(line, flush=True)
    with open(LOG_FILE, "a", encoding="utf-8") as f:
        f.write(line + "\n")


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self._forward()

    def do_POST(self):
        self._forward()

    def _forward(self):
        log(f">> {self.command} {self.path}")

        # Read body
        clen = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(clen) if clen > 0 else b""
        log(f"   Body: {clen} bytes")

        # Rewrite model for POST
        if self.command == "POST" and body:
            try:
                data = json.loads(body)
                old = data.get("model", "")
                new = MODEL_MAP.get(old, old)
                if new != old:
                    log(f"   Model: {old} -> {new}")
                data["model"] = new
                body = json.dumps(data).encode()
            except Exception as e:
                log(f"   JSON error: {e}")

        # Build target (strip /v1 prefix since NVIDIA_BASE has it)
        path = self.path
        for pfx in ("/v1", "/V1"):
            if path.startswith(pfx):
                path = path[len(pfx):]
                break
        target = f"{NVIDIA_BASE}{path}"
        log(f"   -> {target}")

        # Forward
        req = urllib.request.Request(target, data=body or None, method=self.command)
        req.add_header("Authorization", f"Bearer {NVIDIA_KEY}")
        req.add_header("Content-Type", self.headers.get("Content-Type", "application/json"))

        ctx = ssl.create_default_context()
        try:
            with urllib.request.urlopen(req, context=ctx, timeout=300) as resp:
                resp_body = resp.read()
                log(f"   <- {resp.status} ({len(resp_body)} bytes)")
                self.send_response(resp.status)
                ct = resp.headers.get("Content-Type", "application/json")
                self.send_header("Content-Type", ct)
                self.send_header("Content-Length", str(len(resp_body)))
                self.end_headers()
                self.wfile.write(resp_body)
        except urllib.error.HTTPError as e:
            err = e.read()
            log(f"   <- {e.code} (error)")
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(err)))
            self.end_headers()
            self.wfile.write(err)
        except Exception as e:
            log(f"   !! {e}")
            err = json.dumps({"error": {"message": str(e), "type": "proxy_error"}}).encode()
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(err)))
            self.end_headers()
            self.wfile.write(err)

    def log_message(self, fmt, *args):
        pass


class Threaded(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def main():
    # Clear log
    with open(LOG_FILE, "w") as f:
        f.write("")

    server = Threaded(("127.0.0.1", LISTEN_PORT), Handler)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(CERT_FILE, KEY_FILE)
    server.socket = ctx.wrap_socket(server.socket, server_side=True)

    log(f"HTTPS Proxy on https://127.0.0.1:{LISTEN_PORT}")
    log(f"Forwarding to {NVIDIA_BASE}")
    log("Ready.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        log("Stopped.")
        server.shutdown()


if __name__ == "__main__":
    main()
