"""Bounded protocol observation for the explicitly authorized DEV endpoint.

This is not a Model Runtime or domain acceptance test. It preserves urllib's
HTTP_PROXY/HTTPS_PROXY/NO_PROXY handling and validates TLS using system roots.
Only status, dimensions and bounded shape facts are printed; no URL, key or
vector escapes. Shape observations are not a Runtime acceptance result.
"""
import json
import os
import ssl
import time
import urllib.parse
import urllib.request

base = os.environ.get("DEV_EMBEDDING_BASE_URL", "")
key = os.environ.get("DEV_EMBEDDING_API_KEY", "")
model = os.environ.get("DEV_EMBEDDING_MODEL", "")
if not base or not key or not model or urllib.parse.urlsplit(base).scheme != "https":
    raise SystemExit("authorized embedding environment unavailable")
body = json.dumps({"model": model, "input": ["agenteam M3 embedding dimension check"], "encoding_format": "float"}).encode()
request = urllib.request.Request(base.rstrip("/") + "/embeddings", data=body, method="POST", headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context()))
started = time.monotonic()
try:
    with opener.open(request, timeout=25) as response:
        status = response.status
        raw = response.read(8 * 1024 * 1024 + 1)
        if len(raw) > 8 * 1024 * 1024:
            raise ValueError("response_bound")
        payload = json.loads(raw)
        rows = payload.get("data", [])
        if len(rows) != 1 or not isinstance(rows[0].get("embedding"), list):
            raise ValueError("response_shape")
        facts = {"ok": True, "http_status": status, "dimensions": len(rows[0]["embedding"]), "items": len(rows), "tls_verification": True, "default_proxy_environment_preserved": True, "duration_seconds": round(time.monotonic() - started, 3)}
        if os.environ.get("AGENTEAM_M3_EMBEDDING_SHAPE") == "1":
            usage = payload.get("usage")
            usage_keys = set(usage) if isinstance(usage, dict) else set()
            facts["shape"] = {
                "envelope_fields_expected": set(payload) <= {"object", "data", "model", "usage"},
                "object_is_list": payload.get("object") == "list",
                "model_is_nonempty_string": isinstance(payload.get("model"), str) and bool(payload["model"]),
                "row_fields_expected": set(rows[0]) == {"object", "index", "embedding"},
                "row_object_is_embedding": rows[0].get("object") == "embedding",
                "row_index_is_zero": rows[0].get("index") == 0,
                "usage_is_object": isinstance(usage, dict),
                "usage_fields_expected": usage_keys <= {"prompt_tokens", "total_tokens"},
                "usage_extra_fields_count": len(usage_keys - {"prompt_tokens", "total_tokens"}),
                "completion_tokens_present": "completion_tokens" in usage_keys,
                "completion_tokens_is_integer_zero": isinstance(usage, dict) and type(usage.get("completion_tokens")) is int and usage["completion_tokens"] == 0,
                "response_bytes": len(raw),
            }
        print(json.dumps(facts))
except Exception as error:
    print(json.dumps({"ok": False, "error_type": type(error).__name__, "http_status": getattr(error, "code", None), "tls_verification": True, "default_proxy_environment_preserved": True, "duration_seconds": round(time.monotonic() - started, 3)}))
    raise SystemExit(1)
