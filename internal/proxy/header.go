package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

// ReadConnectRequest reads an HTTP CONNECT request from br and returns the target address.
func ReadConnectRequest(br *bufio.Reader) (string, error) {
	req, err := http.ReadRequest(br)
	if err != nil {
		return "", fmt.Errorf("reading HTTP request: %w", err)
	}

	if req.Method != http.MethodConnect {
		return "", fmt.Errorf("%w: expected CONNECT, got %s", ErrInvalidMethod, req.Method)
	}

	host, port, err := net.SplitHostPort(req.Host)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("%w: %s", ErrInvalidAddress, req.Host)
	}

	return req.Host, nil
}

// WriteConnectEstablished writes a 200 Connection established response to w.
func WriteConnectEstablished(w io.Writer) error {
	_, err := fmt.Fprintf(w, "HTTP/1.1 200 Connection established\r\n\r\n")
	if err != nil {
		return fmt.Errorf("writing CONNECT response: %w", err)
	}

	return nil
}
