#!/usr/bin/env python3
"""One direct system-DNS/TLS/EHLO probe; never AUTH, MAIL, RCPT or DATA.

This checks transport only. It is not an application delivery or inbox test.
"""

import argparse
import hashlib
import ipaddress
import json
import socket
import ssl


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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, default=465, choices=[465])
    parser.add_argument("--address", help="Explicitly authorized alternate from this host's current DNS; still only one connection")
    args = parser.parse_args()
    print(json.dumps(probe(args.host, args.port, args.address)))
