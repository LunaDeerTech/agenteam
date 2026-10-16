#!/usr/bin/env python3
"""Explicitly authorized single-recipient SMTP diagnosis on an owned persistent Central.

No SMTP socket is opened here: delivery always uses the authenticated App API.
Secrets remain in memory; only allowlisted configuration/job facts are reported.
The send ledger is created before dispatch and prevents accidental new-key reruns.
"""

import argparse
import base64
import datetime
import hashlib
import http.client
import http.cookies
import json
import os
from pathlib import Path
import socket
import ssl
import stat
import subprocess
import time
import urllib.parse
import uuid


ORIGIN = "https://154.12.86.233:18443"
LOCAL_ADDRESS = "127.0.0.1:5256"
TERMINAL = {"sent", "failed", "cancelled", "unknown"}


def private_json(path):
    path = Path(path)
    st = path.lstat()
    if not stat.S_ISREG(st.st_mode) or stat.S_IMODE(st.st_mode) != 0o600 or st.st_uid != os.getuid():
        raise RuntimeError("private_file_required")
    return json.loads(path.read_text())


def save_private(path, value, exclusive=False):
    # A send scope/ledger is an operator-owned local guard, not an authorization
    # API. The actual external recipient must already be authorized by the user.
    parent = Path(path).parent.lstat()
    if not stat.S_ISDIR(parent.st_mode) or stat.S_IMODE(parent.st_mode) != 0o700 or parent.st_uid != os.getuid():
        raise RuntimeError("private_directory_required")
    flags = os.O_WRONLY | os.O_CREAT | (os.O_EXCL if exclusive else os.O_TRUNC)
    fd = os.open(path, flags | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream)


def safe_job(value):
    return {key: value[key] for key in (
        "job_id", "kind", "phase", "attempts", "version", "reason",
        "attempt_channel", "attempt_result", "created_at", "accepted_at",
    ) if key in value}


class PersistentConnection(http.client.HTTPSConnection):
    def __init__(self, manifest):
        self.manifest = manifest
        origin = urllib.parse.urlsplit(manifest["origin"])
        trust = ssl.create_default_context(cafile=manifest["certificate_ca_file"])
        trust.minimum_version = ssl.TLSVersion.TLSv1_2
        super().__init__(origin.hostname, origin.port, timeout=15, context=trust)

    def connect(self):
        raw = socket.create_connection(("127.0.0.1", 5256), self.timeout)
        try:
            self.sock = self._context.wrap_socket(raw, server_hostname=self.host)
            certificate = self.sock.getpeercert(binary_form=True)
            public = subprocess.run(
                ["openssl", "x509", "-inform", "DER", "-pubkey", "-noout"],
                input=certificate, capture_output=True, check=True,
            ).stdout
            public = subprocess.run(
                ["openssl", "pkey", "-pubin", "-outform", "DER"],
                input=public, capture_output=True, check=True,
            ).stdout
            spki = base64.b64encode(hashlib.sha256(public).digest()).decode()
            if spki != self.manifest["certificate_spki"]:
                raise RuntimeError("owned_spki_mismatch")
        except Exception:
            raw.close()
            if self.sock is not None:
                self.sock.close()
            raise


class Client:
    def __init__(self, manifest):
        self.manifest = manifest
        self.cookies = {}
        self.csrf = None

    def request(self, method, path, body=None, key=None, csrf=None):
        if not path.startswith(("/api/v1/", "/__instance__/identity")):
            raise RuntimeError("unexpected_path")
        headers = {"Origin": ORIGIN, "Sec-Fetch-Site": "same-origin"}
        if self.cookies:
            headers["Cookie"] = "; ".join(name + "=" + value for name, value in self.cookies.items())
        encoded = None
        if body is not None:
            encoded = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
            headers["X-CSRF-Token"] = csrf if csrf is not None else self.csrf
            headers["Idempotency-Key"] = key or str(uuid.uuid4())
        connection = PersistentConnection(self.manifest)
        try:
            connection.request(method, path, encoded, headers)
            response = connection.getresponse()
            raw = response.read(1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise RuntimeError("response_too_large")
            for name, value in response.getheaders():
                if name.lower() != "set-cookie":
                    continue
                parsed = http.cookies.SimpleCookie(value)
                for cookie in parsed.values():
                    if cookie.key not in ("__Host-agenteam_session", "__Host-agenteam_browser"):
                        raise RuntimeError("unexpected_cookie")
                    if not cookie["secure"] or not cookie["httponly"] or cookie["domain"] or cookie["path"] != "/":
                        raise RuntimeError("unsafe_cookie")
                    if cookie.value:
                        self.cookies[cookie.key] = cookie.value
                    else:
                        self.cookies.pop(cookie.key, None)
            value = json.loads(raw) if raw else {}
            if response.status >= 400:
                raise RuntimeError("http_" + str(response.status) + "_" + str(value.get("code", "unclassified")))
            return response.status, value
        finally:
            connection.close()

    def login(self):
        _, bootstrap = self.request("GET", "/api/v1/auth/bootstrap")
        status, _ = self.request("POST", "/api/v1/sessions/login", private_json(self.manifest["credentials_file"]), csrf=bootstrap["csrf_token"])
        if status != 200:
            raise RuntimeError("unexpected_login_status")
        _, session = self.request("GET", "/api/v1/session")
        self.csrf = session["csrf_token"]
        if session["user"]["role"] != "admin":
            raise RuntimeError("current_admin_required")


def owned_runtime(filename):
    root = Path(filename).absolute().parent
    info = root.lstat()
    if root.resolve() != root or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid():
        raise RuntimeError("private_runtime_directory_required")
    manifest = private_json(filename)
    if manifest["origin"] != ORIGIN or manifest["local_tls_address"] != LOCAL_ADDRESS:
        raise RuntimeError("unexpected_persistent_instance")
    for key in ("certificate_ca_file", "certificate_file", "credentials_file"):
        path = Path(manifest[key])
        if not path.is_relative_to(root) or path.resolve() != path:
            raise RuntimeError("runtime_material_outside_owned_directory")
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.getuid():
            raise RuntimeError("private_runtime_material_required")
    record = private_json(root / "central-process.json")
    fields = (Path("/proc") / str(record["pid"]) / "stat").read_text().split(") ", 1)[1].split()
    if fields[0] in ("Z", "X") or int(fields[19]) != record["start_ticks"]:
        raise RuntimeError("central_process_changed")
    if (Path("/proc") / str(record["pid"]) / "exe").resolve() != Path(manifest["central_binary"]).resolve():
        raise RuntimeError("central_executable_changed")
    if record["source_revision"] != manifest["source_revision"] or record["binary_sha256"] != manifest["central_binary_sha256"]:
        raise RuntimeError("central_source_changed")
    if hashlib.sha256(Path(manifest["central_binary"]).read_bytes()).hexdigest() != manifest["central_binary_sha256"]:
        raise RuntimeError("central_binary_changed")
    return manifest


def run(args):
    manifest = owned_runtime(args.runtime)
    client = Client(manifest)
    _, identity = client.request("GET", "/__instance__/identity")
    if identity != {"nonce": manifest["nonce"], "source_revision": manifest["source_revision"]}:
        raise RuntimeError("persistent_identity_changed")
    result = {"origin": ORIGIN, "source_revision": manifest["source_revision"],
              "owned_tls_spki_nonce_verified": True, "sent_request": False}
    try:
        client.login()
        _, settings = client.request("GET", "/api/v1/system/smtp")
        result["smtp"] = {key: settings[key] for key in (
            "version", "configured", "host", "port", "encryption", "credential_present",
            "auto_retry_count", "retry_interval_seconds",
        )}
        _, jobs = client.request("GET", "/api/v1/system/mail-jobs/management?limit=25")
        result["recent_jobs"] = [safe_job(job) for job in jobs["items"]]
        if args.action == "inspect":
            return result
        if not args.ledger:
            raise RuntimeError("private_ledger_required")
        if args.action == "send":
            if not args.scope:
                raise RuntimeError("private_authorization_scope_required")
            scope = private_json(args.scope)
            if set(scope) != {"operation", "origin", "runtime_file", "recipient"} or scope["operation"] != "smtp_test_once" or scope["origin"] != ORIGIN or scope["runtime_file"] != args.runtime:
                raise RuntimeError("send_scope_mismatch")
            if not settings["configured"] or not settings["credential_present"]:
                raise RuntimeError("smtp_not_configured")
            # A pending test's recipient is deliberately absent from public DTOs.
            # Pause for owner correlation rather than risk a duplicate delivery.
            if any(job["kind"] == "test" and job["phase"] not in TERMINAL for job in jobs["items"]):
                raise RuntimeError("existing_test_requires_owner_review")
            operation = {"key": str(uuid.uuid4()), "recipient": scope["recipient"],
                         "origin": ORIGIN, "phase": "prepared", "runtime_file": args.runtime}
            save_private(args.ledger, operation, exclusive=True)
            operation["phase"] = "dispatched"
            save_private(args.ledger, operation)
            status, receipt = client.request("POST", "/api/v1/system/smtp/test", {"recipient": scope["recipient"]}, key=operation["key"])
            if status != 202 or set(receipt) != {"job_id"}:
                raise RuntimeError("unexpected_test_receipt")
            uuid.UUID(receipt["job_id"])
            operation.update(phase="accepted", job_id=receipt["job_id"], http_status=status)
            save_private(args.ledger, operation)
            result.update(sent_request=True, accepted_http_status=status, job_id=receipt["job_id"])
        else:
            operation = private_json(args.ledger)
            if operation["origin"] != ORIGIN or operation["runtime_file"] != args.runtime:
                raise RuntimeError("ledger_instance_mismatch")
        deadline = time.monotonic() + args.wait_seconds
        previous = None
        while True:
            _, job = client.request("GET", "/api/v1/system/mail-jobs/" + operation["job_id"] + "/management")
            result["test_job"] = safe_job(job)
            if result["test_job"] != previous:
                print(json.dumps({"observed_job": result["test_job"]}), flush=True)
                previous = result["test_job"]
            if job["phase"] in TERMINAL or time.monotonic() >= deadline:
                break
            time.sleep(2)
        operation["last_observation"] = result["test_job"]
        save_private(args.ledger, operation)
        return result
    finally:
        if client.csrf is not None:
            status, _ = client.request("POST", "/api/v1/sessions/logout", {})
            result["diagnostic_session_logout_status"] = status


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("inspect", "send", "job"))
    parser.add_argument("--runtime", required=True)
    parser.add_argument("--scope", help="0600 exact recipient/runtime authorization, never a public endpoint")
    parser.add_argument("--ledger", help="0600 exclusive original-operation ledger; job mode is read-only")
    parser.add_argument("--wait-seconds", type=int, default=45, choices=range(1, 601))
    options = parser.parse_args()
    try:
        print(json.dumps(run(options), ensure_ascii=False))
    except Exception as error:
        print(json.dumps({"failed": str(error) if type(error) is RuntimeError else type(error).__name__}))
        raise SystemExit(1)
