package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

// ReadConnectRequest reads an HTTP CONNECT request from br and returns the target address.
// The target host must be an IP literal (IPv4 or IPv6); hostnames are rejected so that
// the CIDR allowlist check is authoritative and does not depend on DNS resolution.
func ReadConnectRequest(br *bufio.Reader) (string, error) {
	req, err := http.ReadRequest(br)
	if err != nil {
		return "", fmt.Errorf("reading HTTP request: %w", err)
	}
	defer req.Body.Close()

	if req.Method != http.MethodConnect {
		return "", fmt.Errorf("%w: expected CONNECT, got %s", ErrInvalidMethod, req.Method)
	}

	host, port, err := net.SplitHostPort(req.URL.Host)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("%w: %s", ErrInvalidAddress, req.URL.Host)
	}

	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("%w: %s", ErrHostnameNotAllowed, req.URL.Host)
	}

	return req.URL.Host, nil
}

// WriteConnectEstablished writes a 200 Connection established response to w.
func WriteConnectEstablished(w io.Writer) error {
	_, err := fmt.Fprintf(w, "HTTP/1.1 200 Connection established\r\n\r\n")
	if err != nil {
		return fmt.Errorf("writing CONNECT response: %w", err)
	}

	return nil
}
