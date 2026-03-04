#!/usr/bin/env python3
"""Local TCP proxy that injects the talos-proxy binary header.

Listens locally and forwards connections through talos-proxy to a target
Talos node, prepending the binary header that talos-proxy expects.

The Talos API uses mTLS and the server certificate is issued for the node's
real IP. To make talosctl's TLS verification pass, listen on the target IP
by adding a loopback alias first:

    sudo ifconfig lo0 alias 10.200.0.8          # macOS
    sudo ip addr add 10.200.0.8/32 dev lo       # Linux

Usage:
    # With port-forward running: kubectl port-forward deploy/talos-proxy 50000
    python3 scripts/talos-proxy-client.py --listen 10.200.0.8:50001 --target 10.200.0.8:50000

    # Then use talosctl against the target IP:
    talosctl --talosconfig talosconfig.yaml --endpoints 10.200.0.8:50001 --nodes 10.200.0.8 version

    # Clean up when done:
    sudo ifconfig lo0 -alias 10.200.0.8         # macOS
    sudo ip addr del 10.200.0.8/32 dev lo       # Linux
"""

import argparse
import socket
import struct
import sys
import threading


def forward(src, dst):
    try:
        while True:
            data = src.recv(4096)
            if not data:
                break
            dst.sendall(data)
    except OSError:
        pass
    finally:
        try:
            dst.shutdown(socket.SHUT_WR)
        except OSError:
            pass


def handle_client(client_sock, proxy_host, proxy_port, target):
    proxy_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    proxy_sock.settimeout(5)

    try:
        proxy_sock.connect((proxy_host, proxy_port))
    except OSError as e:
        print(f"  Failed to connect to proxy: {e}")
        client_sock.close()
        return

    header = struct.pack(">I", len(target)) + target.encode()
    proxy_sock.sendall(header)
    proxy_sock.settimeout(None)

    t1 = threading.Thread(target=forward, args=(client_sock, proxy_sock), daemon=True)
    t2 = threading.Thread(target=forward, args=(proxy_sock, client_sock), daemon=True)
    t1.start()
    t2.start()
    t1.join()
    t2.join()

    client_sock.close()
    proxy_sock.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--listen", default="127.0.0.1:50001", help="local listen address (default: 127.0.0.1:50001)")
    parser.add_argument("--proxy", default="127.0.0.1:50000", help="talos-proxy address (default: 127.0.0.1:50000)")
    parser.add_argument("--target", required=True, help="target Talos node address (e.g. 10.200.0.8:50000)")
    args = parser.parse_args()

    listen_host, _, listen_port = args.listen.rpartition(":")
    proxy_host, _, proxy_port = args.proxy.rpartition(":")

    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind((listen_host, int(listen_port)))
    server.listen(16)

    print(f"Listening on {args.listen}")
    print(f"Forwarding via {args.proxy} -> {args.target}")
    print(f"\nUse: talosctl --endpoints {listen_host} --talosconfig <path> --nodes <node_IP> version")

    try:
        while True:
            client_sock, addr = server.accept()
            print(f"  Connection from {addr[0]}:{addr[1]}")
            threading.Thread(
                target=handle_client,
                args=(client_sock, proxy_host, int(proxy_port), args.target),
                daemon=True,
            ).start()
    except KeyboardInterrupt:
        print("\nStopped")
        server.close()
        sys.exit(0)


if __name__ == "__main__":
    main()
