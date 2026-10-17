#!/usr/bin/env python3
"""Check public preview ownership through a temporary anonymous FRP relay.

The environment proxy cannot validate the preview's private TLS authority.
This relay only exposes identity/release JSON. Its upstream independently
verifies the original CA, IP and exact leaf. The public hop uses HTTP; it does
not prove end-to-end TLS on the application's original HTTPS URL.
"""

import argparse
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import ssl
import subprocess
import threading
import time
import uuid


def process(pid):
    try:
        fields = Path(f"/proc/{pid}/stat").read_text().split(") ", 1)[1].split()
    except FileNotFoundError:
        return None
    return {"pid": pid, "state": fields[0], "parent_pid": int(fields[1]), "start_ticks": int(fields[19])}


def children(parent):
    records = []
    for entry in Path("/proc").iterdir():
        if entry.name.isdecimal():
            record = process(int(entry.name))
            if record and record["parent_pid"] == parent:
                records.append(record)
    return records


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime", type=Path, required=True)
    parser.add_argument("--frontend-directory", type=Path, required=True)
    parser.add_argument("--output-directory", type=Path, required=True)
    parser.add_argument("--remote-port", type=int, required=True)
    args = parser.parse_args()
    assert 1024 <= args.remote_port <= 65535 and args.remote_port != 18443
    os.umask(0o077)
    args.output_directory.mkdir(mode=0o700)
    assert args.output_directory.resolve() == args.output_directory
    runtime = json.loads(args.runtime.read_text())
    assert runtime["origin"] == "https://154.12.86.233:18443"
    assert runtime["local_tls_address"] == "127.0.0.1:5256"
    leaf = ssl.PEM_cert_to_DER_cert(Path(runtime["certificate_file"]).read_text())
    context = ssl.create_default_context(cafile=runtime["certificate_ca_file"])
    expected_release = {
        "source_revision": runtime["source_revision"],
        "backend_sha256": runtime["central_binary_sha256"],
        "frontend_index_sha256": hashlib.sha256(
            (args.frontend_directory / "dist/index.html").read_bytes()
        ).hexdigest(),
        "nonce": runtime["nonce"],
    }
    assert json.loads(
        (args.frontend_directory / "dist/__instance__/release.json").read_text()
    ) == expected_release

    def upstream(path):
        with socket.create_connection(("127.0.0.1", 5256), timeout=8) as raw:
            with context.wrap_socket(raw, server_hostname="154.12.86.233") as tls:
                assert tls.getpeercert(binary_form=True) == leaf, "leaf_mismatch"
                tls.sendall((
                    f"GET {path} HTTP/1.1\r\n"
                    "Host: 154.12.86.233:18443\r\nConnection: close\r\n\r\n"
                ).encode())
                response = http.client.HTTPResponse(tls)
                response.begin()
                body = response.read(8193)
                assert response.status == 200 and len(body) <= 8192
                assert response.getheader("Set-Cookie") is None
                value = json.loads(body)
                assert value.get("nonce") == runtime["nonce"], "nonce_mismatch"
                if path.endswith("release.json"):
                    assert value == expected_release, "release_mismatch"
                return body

    identity = json.loads(upstream("/__instance__/identity"))
    upstream("/__instance__/release.json")
    token = uuid.uuid4().hex
    routes = {
        f"/{token}/identity": "/__instance__/identity",
        f"/{token}/release": "/__instance__/release.json",
    }
    result = {
        "revision": runtime["source_revision"],
        "backend_sha256": runtime["central_binary_sha256"],
        "frontend_index_sha256": expected_release["frontend_index_sha256"],
        "public_hop": "HTTP through temporary FRP port",
        "original_https_end_to_end_verified": False,
        "upstream_original_ca_ip_leaf_verified": True,
        "original_service_and_mapping_unchanged": True,
        "requests": [],
    }
    handler_failures = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def setup(self):
            super().setup()
            self.connection.settimeout(10)

        def log_message(self, *_args):
            pass

        def do_GET(self):
            if self.path not in routes:
                self.send_error(404)
                return
            try:
                body = upstream(routes[self.path])
            except Exception as error:
                handler_failures.append(type(error).__name__)
                self.send_error(502)
                return
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    server.timeout = 10
    relay_port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, name="public-identity-relay")
    thread.start()
    child = None
    frpc = None
    log_path = args.output_directory / "frp.log"
    failure = None
    try:
        name = "model-identity-" + token[:16]
        with log_path.open("wb") as log:
            child = subprocess.Popen([
                "python3", "/workspace/agenteam-onboarding/frp/run-preview.py",
                "--server-addr", "mcpc-terra.lunadeer.cn", "--server-port", "443",
                "--local-port", str(relay_port), "--remote-port", str(args.remote_port),
                "--name", name,
            ], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                start_new_session=True, close_fds=True)
        result["helper"] = process(child.pid)
        result["relay_port"] = relay_port
        result["remote_port"] = args.remote_port
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            assert child.poll() is None, "frp_helper_exited"
            log_text = log_path.read_text(errors="replace")
            if "login to server success" in log_text and f"[{name}] start proxy success" in log_text:
                break
            if "start error" in log_text or "start proxy error" in log_text:
                raise RuntimeError("frp_mapping_rejected")
            time.sleep(0.1)
        else:
            raise RuntimeError("frp_start_timeout")
        child_records = children(child.pid)
        assert len(child_records) == 1, "frpc_child_identity"
        frpc = child_records[0]
        executable = Path(f"/proc/{frpc['pid']}/exe").resolve()
        assert executable == Path("/workspace/toolchains/frp_0.71.1_linux_amd64/frpc")
        result["frpc"] = frpc
        result["frp_login_and_mapping_confirmed"] = True
        for kind, expected in (("identity", identity), ("release", expected_release)):
            body_path = args.output_directory / f"public-{kind}.json"
            response = subprocess.run([
                "curl", "--silent", "--show-error", "--max-time", "15",
                "--output", str(body_path), "--write-out", "%{http_code}",
                f"http://mcpc-terra.lunadeer.cn:{args.remote_port}/{token}/{kind}",
            ], capture_output=True, text=True, timeout=20)
            (args.output_directory / f"public-{kind}.stderr").write_text(response.stderr)
            facts = {"kind": kind, "curl_exit": response.returncode, "http_status": response.stdout}
            result["requests"].append(facts)
            assert response.returncode == 0 and response.stdout == "200", "public_request_failed"
            facts["matches_strict_local_body"] = json.loads(body_path.read_text()) == expected
            assert facts["matches_strict_local_body"], "public_body_mismatch"
        assert not handler_failures, "relay_upstream_failed"
        result["public_nonce_and_release_matched"] = True
    except BaseException as error:
        failure = error
        result["failure"] = str(error) if isinstance(error, (AssertionError, RuntimeError)) else type(error).__name__
    finally:
        if child is not None:
            if frpc is None:
                remaining = children(child.pid)
                if len(remaining) == 1:
                    frpc = remaining[0]
                    result["frpc"] = frpc
            if child.poll() is None:
                child.send_signal(signal.SIGTERM)
            try:
                result["helper_wait_exit"] = child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                result["helper_wait_timeout"] = True
                failure = failure or RuntimeError("helper_did_not_join")
            result["helper_absent"] = process(child.pid) is None
            result["frpc_absent"] = process(frpc["pid"]) is None if frpc else None
        server.shutdown()
        thread.join(timeout=12)
        server.server_close()
        result["relay_joined"] = not thread.is_alive()
        with socket.socket() as probe:
            probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            probe.bind(("127.0.0.1", relay_port))
        result["relay_port_released"] = True
        result["cleanup_complete"] = all(result.get(key) is True for key in (
            "helper_absent", "frpc_absent", "relay_joined", "relay_port_released",
        ))
        if not result["cleanup_complete"]:
            failure = failure or RuntimeError("owned_resources_not_joined")
        (args.output_directory / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))
    return 1 if failure else 0


if __name__ == "__main__":
    raise SystemExit(main())
