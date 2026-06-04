import urllib.request, json, ssl, time
CERT = r"d:\__Projects\Paryty\paryty-v1.0\scripts\moonshot_cert.pem"
ctx = ssl.create_default_context()
ctx.load_verify_locations(CERT)
payload = json.dumps({"model":"kimi-k2.6","messages":[{"role":"user","content":"Say hi"}],"max_tokens":10,"stream":False}).encode()
req = urllib.request.Request("https://api.moonshot.cn/v1/chat/completions", data=payload, method="POST")
req.add_header("Content-Type","application/json")
req.add_header("Authorization","Bearer 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")
t=time.time()
try:
    with urllib.request.urlopen(req, context=ctx, timeout=300) as r:
        res=json.loads(r.read())
        print(f"OK {time.time()-t:.1f}s model={res.get('model')} msg={res.get('choices',[{}])[0].get('message',{}).get('content','')}")
except Exception as e:
    print(f"FAIL {time.time()-t:.1f}s {e}")
