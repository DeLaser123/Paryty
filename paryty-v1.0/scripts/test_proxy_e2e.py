"""Test the HTTPS proxy end-to-end: request to api.moonshot.cn routed through our proxy."""
import urllib.request
import json
import ssl
import time

TEST_URL = "https://api.moonshot.cn/v1/chat/completions"
CERT_FILE = r"d:\__Projects\Paryty\paryty-v1.0\scripts\moonshot_cert.pem"

payload = json.dumps({
    "model": "kimi-k2.6",
    "messages": [{"role": "user", "content": "Say hello in 3 words"}],
    "max_tokens": 30,
    "stream": False
}).encode()

print(f"Testing: {TEST_URL}")
print(f"With model: kimi-k2.6 (should be rewritten to moonshotai/kimi-k2.6)")
print()

# Trust our self-signed cert
ctx = ssl.create_default_context()
ctx.load_verify_locations(CERT_FILE)

req = urllib.request.Request(TEST_URL, data=payload, method="POST")
req.add_header("Content-Type", "application/json")
req.add_header("Authorization", "Bearer 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")

start = time.time()
try:
    with urllib.request.urlopen(req, context=ctx, timeout=300) as resp:
        result = json.loads(resp.read())
        elapsed = time.time() - start
        print(f"SUCCESS ({elapsed:.1f}s)")
        print(f"Model used: {result.get('model', 'unknown')}")
        choice = result.get('choices', [{}])[0]
        msg = choice.get('message', {}).get('content', '(no content)')
        print(f"Response: {msg}")
except Exception as e:
    elapsed = time.time() - start
    print(f"FAILED ({elapsed:.1f}s): {e}")
