#!/usr/bin/env python3
"""One direct system-DNS/TLS/EHLO probe; never AUTH, MAIL, RCPT or DATA.

This checks transport only. It is not an application delivery or inbox test.
"""

import argparse
import hashlib
import ipaddress
import json
import os
import socket
import ssl
import time
import urllib.parse


def reply(stream):
    size = 0
    code = None
    for _ in range(100):
        line = stream.readline(4097)
        size += len(line)
        if len(line) > 4096 or size > 32768 or len(line) < 5 or not line.endswith(b"\r\n"):
            raise ValueError("invalid_smtp_reply")
        if not line[:3].isdigit() or line[3:4] not in (b"-", b" "):
            raise ValueError("invalid_smtp_reply")
        if code is not None and code != int(line[:3]):
            raise ValueError("inconsistent_smtp_reply")
        code = int(line[:3])
        if line[3:4] == b" ":
            return code
    raise ValueError("smtp_reply_limit")


def probe(host, port, selected_address=None):
    result = {"host": host, "port": port, "authenticated": False, "mail_sent": False, "stage": "dns"}
    try:
        addresses = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
        result["resolved_addresses"] = list(dict.fromkeys(item[4][0] for item in addresses))
        if not addresses or any(not ipaddress.ip_address(address).is_global for address in result["resolved_addresses"]):
            raise ValueError("non_public_dns_answer")
        # One address, one connection; no alternate-address retries or scanning.
        if selected_address is not None:
            matching = [item for item in addresses if item[4][0] == selected_address]
            if not matching:
                raise ValueError("selected_address_not_in_current_dns")
            address = matching[0]
        else:
            address = next((item for item in addresses if item[0] == socket.AF_INET), addresses[0])
        result["selected_address"] = address[4][0]
        result["stage"] = "tcp"
        with socket.socket(address[0], address[1], address[2]) as raw:
            raw.settimeout(5)
            raw.connect(address[4])
            raw.settimeout(10)
            result["stage"] = "tls"
            trust = ssl.create_default_context()
            trust.minimum_version = ssl.TLSVersion.TLSv1_2
            with trust.wrap_socket(raw, server_hostname=host) as tls:
                result["tls"] = tls.version()
                result["certificate_sha256"] = hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest()
                result["certificate_hostname_verified"] = True
                result["stage"] = "greeting"
                with tls.makefile("rb") as stream:
                    result["greeting_code"] = reply(stream)
                    if result["greeting_code"] != 220:
                        raise ValueError("greeting_rejected")
                    result["stage"] = "ehlo"
                    tls.sendall(b"EHLO agenteam-diagnostic.invalid\r\n")
                    result["ehlo_code"] = reply(stream)
                    if result["ehlo_code"] != 250:
                        raise ValueError("ehlo_rejected")
                    tls.sendall(b"QUIT\r\n")
                    result["quit_code"] = reply(stream)
                    result["stage"] = "complete"
    except Exception as error:
        result["error_type"] = type(error).__name__
        if isinstance(error, OSError):
            result["errno"] = error.errno
        if isinstance(error, ssl.SSLCertVerificationError):
            result["certificate_verify_code"] = error.verify_code
    return result


def bounded_smtp_reply(tls):
    deadline = time.monotonic() + 10
    total, code, lines = 0, None, []
    for _ in range(100):
        line = bytearray()
        while not line.endswith(b"\r\n"):
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError()
            tls.settimeout(remaining)
            part = tls.recv(1)
            if not part:
                return "eof", None, []
            line.extend(part)
            total += 1
            if len(line) > 4096 or total > 32768:
                return "oversized", None, []
        if line.startswith(b"HTTP/"):
            return "http", None, []
        if len(line) < 5 or not line[:3].isdigit() or line[3:4] not in (b"-", b" "):
            return "non_smtp", None, []
        if code is not None and code != int(line[:3]):
            return "invalid_smtp", None, []
        code = int(line[:3])
        lines.append(bytes(line[4:-2]))
        if line[3:4] == b" ":
            return "smtp", code, lines
    return "oversized", None, []


def system_proxy_probe(host, port):
    """One explicit deployment-proxy probe, never SMTP AUTH/MAIL/RCPT/DATA."""
    result = {"host": host, "port": port, "authenticated": False, "mail_sent": False,
              "stage": "proxy_configuration"}
    try:
        proxy = urllib.parse.urlsplit(os.environ.get("HTTPS_PROXY") or os.environ.get("HTTP_PROXY", ""))
        # This diagnostic is deliberately bounded to the approved system proxy.
        if proxy.scheme != "http" or proxy.hostname != "proxy" or proxy.port != 8080 or proxy.username is not None:
            raise ValueError("unsupported_system_proxy")
        result.update(proxy_host=proxy.hostname, proxy_port=proxy.port, stage="proxy_tcp")
        with socket.create_connection((proxy.hostname, proxy.port), timeout=10) as raw:
            raw.settimeout(10)
            result["stage"] = "http_connect"
            target = host + ":" + str(port)
            if any(character in target for character in "\r\n"):
                raise ValueError("invalid_target")
            raw.sendall(("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n").encode("ascii"))
            header, deadline = bytearray(), time.monotonic() + 10
            while not header.endswith(b"\r\n\r\n"):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError()
                raw.settimeout(remaining)
                part = raw.recv(1)
                if not part:
                    raise EOFError()
                header.extend(part)
                if len(header) > 16384:
                    raise ValueError("oversized_proxy_response")
            result["connect_status"] = int(header.split(b"\r\n", 1)[0].split()[1])
            if result["connect_status"] != 200:
                return result
            result["stage"] = "tls"
            trust = ssl.create_default_context()
            trust.minimum_version = ssl.TLSVersion.TLSv1_2
            raw.settimeout(10)
            with trust.wrap_socket(raw, server_hostname=host) as tls:
                result.update(tls=tls.version(), certificate_hostname_verified=True,
                              certificate_sha256=hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest())
                result["leaf_issuer"] = [{"field": key, "value": value}
                                         for group in tls.getpeercert().get("issuer", ()) for key, value in group
                                         if key in ("organizationName", "commonName")]
                result["stage"] = "smtp_greeting"
                classification, code, _ = bounded_smtp_reply(tls)
                result.update(greeting_class=classification, greeting_code=code)
                if classification != "smtp" or code != 220:
                    return result
                result["stage"] = "ehlo"
                tls.sendall(b"EHLO agenteam-diagnostic.invalid\r\n")
                classification, code, lines = bounded_smtp_reply(tls)
                result.update(ehlo_class=classification, ehlo_code=code)
                if classification != "smtp" or code != 250:
                    return result
                capabilities = {b"AUTH", b"STARTTLS", b"SIZE", b"PIPELINING", b"8BITMIME", b"SMTPUTF8", b"DSN", b"CHUNKING"}
                result["capabilities"] = sorted({line.split()[0].decode("ascii") for line in lines if line.split() and line.split()[0] in capabilities})
                tls.sendall(b"QUIT\r\n")
                classification, code, _ = bounded_smtp_reply(tls)
                result.update(stage="complete", quit_class=classification, quit_code=code)
    except Exception as error:
        result["error_type"] = type(error).__name__
        if isinstance(error, OSError):
            result["errno"] = error.errno
        if isinstance(error, ssl.SSLCertVerificationError):
            result["certificate_verify_code"] = error.verify_code
        if isinstance(error, TimeoutError) and result["stage"] == "smtp_greeting":
            result["greeting_class"] = "timeout"
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, default=465, choices=[465])
    parser.add_argument("--address", help="Explicitly authorized alternate from this host's current DNS; still only one connection")
    parser.add_argument("--system-proxy", action="store_true", help="Explicitly authorized proxy:8080 CONNECT/TLS/greeting probe; no SMTP credentials")
    args = parser.parse_args()
    if args.system_proxy and args.address:
        parser.error("proxy and explicit address cannot be combined")
    print(json.dumps(system_proxy_probe(args.host, args.port) if args.system_proxy else probe(args.host, args.port, args.address)))
