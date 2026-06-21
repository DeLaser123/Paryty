"""Quick test: NVIDIA API directly, then via proxy."""
import os
import sys
import urllib.request
import json
import ssl
import time

NVIDIA_URL = "https://integrate.api.nvidia.com/v1/chat/completions"
NVIDIA_KEY = os.environ.get("NVIDIA_API_KEY")
if not NVIDIA_KEY:
    print("Error: NVIDIA_API_KEY environment variable not set")
    sys.exit(1)

payload = json.dumps({
    "model": "moonshotai/kimi-k2.6",
    "messages": [{"role": "user", "content": "hi"}],
    "max_tokens": 10,
    "stream": False
}).encode()

print("Testing NVIDIA API directly...")
print(f"URL: {NVIDIA_URL}")
start = time.time()

req = urllib.request.Request(NVIDIA_URL, data=payload, method="POST")
req.add_header("Authorization", f"Bearer {NVIDIA_KEY}")
req.add_header("Content-Type", "application/json")

ctx = ssl.create_default_context()
try:
    with urllib.request.urlopen(req, context=ctx, timeout=120) as resp:
        result = json.loads(resp.read())
        elapsed = time.time() - start
        print(f"SUCCESS ({elapsed:.1f}s)")
        print(json.dumps(result, indent=2))
except Exception as e:
    elapsed = time.time() - start
    print(f"FAILED ({elapsed:.1f}s): {e}")

print()
print("Testing via proxy...")
PROXY_URL = "http://127.0.0.1:8765/v1/chat/completions"
proxy_payload = json.dumps({
    "model": "kimi-k2.6",
    "messages": [{"role": "user", "content": "hi"}],
    "max_tokens": 10,
    "stream": False
}).encode()

start = time.time()
req2 = urllib.request.Request(PROXY_URL, data=proxy_payload, method="POST")
req2.add_header("Content-Type", "application/json")
try:
    with urllib.request.urlopen(req2, timeout=120) as resp:
        result = json.loads(resp.read())
        elapsed = time.time() - start
        print(f"SUCCESS ({elapsed:.1f}s)")
        print(json.dumps(result, indent=2))
except Exception as e:
    elapsed = time.time() - start
    print(f"FAILED ({elapsed:.1f}s): {e}")
