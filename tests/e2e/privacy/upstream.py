import json, os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DUMP_DIR = os.environ.get("UPSTREAM_DUMP_DIR", "/tmp/privacy-e2e/upstream")
os.makedirs(DUMP_DIR, exist_ok=True)
COUNTER = os.path.join(DUMP_DIR, "counter")

def next_index():
    n = 1
    if os.path.exists(COUNTER):
        n = int(open(COUNTER).read().strip() or "0") + 1
    open(COUNTER, "w").write(str(n))
    return n

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass

    def _json(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.endswith("/models"):
            self._json({"object": "list", "data": [{"id": "bf-privacy-e2e", "object": "model"}]})
        else:
            self.send_response(404); self.send_header("Content-Length", "0"); self.end_headers()

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n)
        idx = next_index()
        with open(os.path.join(DUMP_DIR, "req_%03d.json" % idx), "wb") as f:
            f.write(raw)
        try: req = json.loads(raw)
        except Exception: req = {}
        text = ""
        for m in reversed(req.get("messages") or []):
            if m.get("role") == "user" and isinstance(m.get("content"), str):
                text = m["content"]; break

        if req.get("stream"):
            # 把上游看到的文本（含占位符）从中间切成两半，分两个 chunk 发回，
            # 用来验证真实进程里的跨 chunk 还原。
            mid = len(text) // 2
            parts = [text[:mid], text[mid:]]
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "close")  # 不关连接网关的 SSE reader 会一直等 EOF
            self.close_connection = True
            self.end_headers()
            for p in parts:
                chunk = {"id": "chatcmpl-e2e", "object": "chat.completion.chunk",
                         "model": req.get("model", "bf-privacy-e2e"),
                         "choices": [{"index": 0, "delta": {"content": p}, "finish_reason": None}]}
                self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
                self.wfile.flush()
            done = {"id": "chatcmpl-e2e", "object": "chat.completion.chunk",
                    "model": req.get("model", "bf-privacy-e2e"),
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]}
            self.wfile.write(("data: " + json.dumps(done) + "\n\n").encode())
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
            return

        self._json({
            "id": "chatcmpl-e2e", "object": "chat.completion",
            "model": req.get("model", "bf-privacy-e2e"),
            "choices": [{"index": 0, "finish_reason": "stop",
                         "message": {"role": "assistant", "content": "我看到的：" + text}}],
            "usage": {"prompt_tokens": 9, "completion_tokens": 5, "total_tokens": 14},
        })

ThreadingHTTPServer(("127.0.0.1", int(os.environ.get("UPSTREAM_PORT", "19090"))), H).serve_forever()