#!/usr/bin/env python3
"""Test that talos-proxy forwards connections to the Talos API."""

import argparse
import socket
import ssl
import struct
import sys


def test_proxy(proxy_host, proxy_port, target):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(5)

    print(f"Connecting to proxy at {proxy_host}:{proxy_port}")
    try:
        sock.connect((proxy_host, proxy_port))
    except (ConnectionRefusedError, OSError) as e:
        print(f"FAIL: could not connect to proxy: {e}")
        return False

    print(f"Sending target header: {target}")
    header = struct.pack(">I", len(target)) + target.encode()
    sock.sendall(header)

    # The Talos API uses mTLS, so it waits for a TLS ClientHello before
    # sending anything back. Wrap the socket in TLS to trigger the handshake.
    print("Starting TLS handshake with target...")
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    try:
        tls_sock = ctx.wrap_socket(sock, server_hostname=target.split(":")[0])
        print(f"OK: TLS handshake succeeded (protocol={tls_sock.version()})")
        tls_sock.close()
        return True
    except ssl.SSLError as e:
        # A certificate error still means the proxy forwarded traffic and
        # the Talos API responded with a TLS ServerHello.
        if "certificate required" in str(e) or "certificate_required" in str(e):
            print(f"OK: Talos API responded (client certificate required, as expected)")
            sock.close()
            return True
        print(f"FAIL: TLS error: {e}")
        sock.close()
        return False
    except socket.timeout:
        print("FAIL: no response from target (timed out)")
        sock.close()
        return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--proxy",
        default="127.0.0.1:50000",
        help="proxy address (default: 127.0.0.1:50000)",
    )
    parser.add_argument(
        "target",
        help="target Talos API address (e.g. 10.200.0.5:50000)",
    )
    args = parser.parse_args()

    host, _, port = args.proxy.rpartition(":")
    success = test_proxy(host, int(port), args.target)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
