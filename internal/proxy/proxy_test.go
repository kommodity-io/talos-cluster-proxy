package proxy_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kommodity-io/talos-proxy/internal/proxy"
)

// startEchoServer starts a TCP server that echoes back any received data.
func startEchoServer(t *testing.T) net.Listener {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("failed to start echo server: %v", err)
	}

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close() //nolint:errcheck // test helper
				_, _ = io.Copy(conn, conn)
			}(conn)
		}
	}()

	return listener
}

// startHalfCloseServer starts a TCP server that reads all data, then writes a
// response and closes. Used to test half-close propagation.
func startHalfCloseServer(t *testing.T, response []byte) net.Listener {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("failed to start half-close server: %v", err)
	}

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close() //nolint:errcheck // test helper

				// Read until the client signals EOF.
				_, _ = io.ReadAll(conn)

				// Write the response after the client half-closed.
				_, _ = conn.Write(response)
			}(conn)
		}
	}()

	return listener
}

func startProxy(t *testing.T, allowedCIDRs []*net.IPNet) (net.Listener, *proxy.Server) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}

	server := proxy.NewServer(5*time.Second, allowedCIDRs, nil, nil)

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() { _ = server.Serve(t.Context(), listener) }()

	return listener, server
}

func dialProxy(t *testing.T, proxyAddr string, targetAddr string) net.Conn {
	t.Helper()

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", proxyAddr)
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}

	err = proxy.WriteTargetAddress(conn, targetAddr)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("failed to write target address: %v", err)
	}

	return conn
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	proxyListener, _ := startProxy(t, nil)

	conn := dialProxy(t, proxyListener.Addr().String(), echo.Addr().String())
	defer func() { _ = conn.Close() }()

	payload := []byte("hello talos-proxy")

	_, err := conn.Write(payload)
	if err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	buf := make([]byte, len(payload))

	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("expected %q, got %q", payload, buf)
	}
}

func TestLargePayload(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	proxyListener, _ := startProxy(t, nil)

	conn := dialProxy(t, proxyListener.Addr().String(), echo.Addr().String())
	defer func() { _ = conn.Close() }()

	// Send 1MB of data.
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	var readWg sync.WaitGroup

	readWg.Add(1)

	var readBuf []byte

	var readErr error

	go func() {
		defer readWg.Done()

		readBuf, readErr = io.ReadAll(conn)
	}()

	_, err := conn.Write(payload)
	if err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	// Signal we're done writing so the echo server sends back data.
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}

	readWg.Wait()

	if readErr != nil {
		t.Fatalf("failed to read response: %v", readErr)
	}

	if !bytes.Equal(readBuf, payload) {
		t.Fatalf("payload mismatch: sent %d bytes, received %d bytes", len(payload), len(readBuf))
	}
}

func TestInvalidHeaderTooLarge(t *testing.T) {
	t.Parallel()

	proxyListener, _ := startProxy(t, nil)

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Write a length that exceeds the maximum.
	err = binary.Write(conn, binary.BigEndian, uint32(100000))
	if err != nil {
		t.Fatalf("failed to write header: %v", err)
	}

	// The proxy should close the connection. Trying to read should return EOF or error.
	buf := make([]byte, 1)

	err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}

	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected error reading from proxy after invalid header")
	}
}

func TestInvalidHeaderMissingPort(t *testing.T) {
	t.Parallel()

	proxyListener, _ := startProxy(t, nil)

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Write a valid-length header but with an address missing the port.
	addr := "10.200.0.5"

	err = binary.Write(conn, binary.BigEndian, uint32(len(addr))) //nolint:gosec // test constant
	if err != nil {
		t.Fatalf("failed to write length: %v", err)
	}

	_, err = conn.Write([]byte(addr))
	if err != nil {
		t.Fatalf("failed to write address: %v", err)
	}

	// The proxy should close the connection.
	buf := make([]byte, 1)

	err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}

	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected error reading from proxy after invalid address")
	}
}

func TestCIDRDenied(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	// Allow only 192.168.0.0/16 — the echo server will be on 127.0.0.1 which is outside.
	_, cidr, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatalf("failed to parse CIDR: %v", err)
	}

	proxyListener, _ := startProxy(t, []*net.IPNet{cidr})

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	err = proxy.WriteTargetAddress(conn, echo.Addr().String())
	if err != nil {
		t.Fatalf("failed to write target address: %v", err)
	}

	// Write some payload.
	_, err = conn.Write([]byte("should not arrive"))
	if err != nil {
		// May fail immediately, that's ok.
		return
	}

	// The proxy should close the connection.
	buf := make([]byte, 1)

	err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}

	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected error reading from proxy when CIDR is denied")
	}
}

func TestCIDRAllowed(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	// Allow 127.0.0.0/8 — the echo server is on 127.0.0.1.
	_, cidr, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatalf("failed to parse CIDR: %v", err)
	}

	proxyListener, _ := startProxy(t, []*net.IPNet{cidr})

	conn := dialProxy(t, proxyListener.Addr().String(), echo.Addr().String())
	defer func() { _ = conn.Close() }()

	payload := []byte("cidr allowed")

	_, err = conn.Write(payload)
	if err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	buf := make([]byte, len(payload))

	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("expected %q, got %q", payload, buf)
	}
}

func TestDialTimeout(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test setup
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	server := proxy.NewServer(100*time.Millisecond, nil, nil, nil)

	go func() { _ = server.Serve(t.Context(), listener) }()

	// Grab a port with nothing listening on it.
	unusedListener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test setup
	if err != nil {
		t.Fatalf("failed to create unused listener: %v", err)
	}

	unusedAddr := unusedListener.Addr().String()
	_ = unusedListener.Close()

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	err = proxy.WriteTargetAddress(conn, unusedAddr)
	if err != nil {
		t.Fatalf("failed to write target address: %v", err)
	}

	// The proxy should close the connection after failing to dial.
	buf := make([]byte, 1)

	err = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}

	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected error reading from proxy after dial failure")
	}
}

func TestHalfClosePropagation(t *testing.T) {
	t.Parallel()

	response := []byte("server-response-after-half-close")

	halfCloseServer := startHalfCloseServer(t, response)
	defer func() { _ = halfCloseServer.Close() }()

	proxyListener, _ := startProxy(t, nil)

	conn := dialProxy(t, proxyListener.Addr().String(), halfCloseServer.Addr().String())
	defer func() { _ = conn.Close() }()

	payload := []byte("client-data")

	_, err := conn.Write(payload)
	if err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	// Half-close the write side — this should propagate through the proxy.
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatal("expected TCP connection")
	}

	err = tcpConn.CloseWrite()
	if err != nil {
		t.Fatalf("failed to close write: %v", err)
	}

	// Read the response that the server sends after detecting our half-close.
	result, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !bytes.Equal(result, response) {
		t.Fatalf("expected %q, got %q", response, result)
	}
}

func TestHeaderReadWriteRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		addr string
	}{
		{name: "ipv4", addr: "10.200.0.5:50000"},
		{name: "ipv6", addr: "[::1]:50000"},
		{name: "hostname", addr: "node1.cluster.local:50000"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			err := proxy.WriteTargetAddress(&buf, testCase.addr)
			if err != nil {
				t.Fatalf("WriteTargetAddress failed: %v", err)
			}

			addr, err := proxy.ReadTargetAddress(&buf)
			if err != nil {
				t.Fatalf("ReadTargetAddress failed: %v", err)
			}

			if addr != testCase.addr {
				t.Fatalf("expected %q, got %q", testCase.addr, addr)
			}
		})
	}
}

func TestReadTargetAddressErrors(t *testing.T) {
	t.Parallel()

	t.Run("empty reader", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		_, err := proxy.ReadTargetAddress(&buf)
		if err == nil {
			t.Fatal("expected error for empty reader")
		}
	})

	t.Run("zero length", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		err := binary.Write(&buf, binary.BigEndian, uint32(0))
		if err != nil {
			t.Fatalf("failed to write zero length: %v", err)
		}

		_, err = proxy.ReadTargetAddress(&buf)
		if err == nil {
			t.Fatal("expected error for zero length")
		}

		if !errors.Is(err, proxy.ErrHeaderTooLarge) {
			t.Fatalf("expected ErrHeaderTooLarge, got %v", err)
		}
	})
}

func TestValidateCIDR(t *testing.T) {
	t.Parallel()

	_, allowed, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{allowed}

	t.Run("allowed", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidateCIDR("10.200.0.5:50000", cidrs)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("denied", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidateCIDR("192.168.1.1:50000", cidrs)
		if !errors.Is(err, proxy.ErrCIDRDenied) {
			t.Fatalf("expected ErrCIDRDenied, got %v", err)
		}
	})

	t.Run("nil cidrs allows all", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidateCIDR("192.168.1.1:50000", nil)
		if err != nil {
			t.Fatalf("expected no error with nil CIDRs, got %v", err)
		}
	})
}

func TestPortAllowed(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	// Parse the echo server port and allow only that port.
	_, portStr, _ := net.SplitHostPort(echo.Addr().String())

	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}

	port := parseTestPort(t, portStr)
	server := proxy.NewServer(5*time.Second, nil, []uint16{port}, nil)

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() { _ = server.Serve(t.Context(), listener) }()

	conn := dialProxy(t, listener.Addr().String(), echo.Addr().String())
	defer func() { _ = conn.Close() }()

	payload := []byte("port allowed")

	_, err = conn.Write(payload)
	if err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	buf := make([]byte, len(payload))

	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("expected %q, got %q", payload, buf)
	}
}

func TestPortDenied(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	// Allow only port 1 — the echo server will be on a different port.
	listener, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test helper
	if err != nil {
		t.Fatalf("failed to start proxy listener: %v", err)
	}

	server := proxy.NewServer(5*time.Second, nil, []uint16{1}, nil)

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() { _ = server.Serve(t.Context(), listener) }()

	dialer := net.Dialer{Timeout: 2 * time.Second}

	conn, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	err = proxy.WriteTargetAddress(conn, echo.Addr().String())
	if err != nil {
		t.Fatalf("failed to write target address: %v", err)
	}

	// The proxy should close the connection.
	buf := make([]byte, 1)

	err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}

	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected error reading from proxy when port is denied")
	}
}

func TestValidatePort(t *testing.T) {
	t.Parallel()

	ports := []uint16{50000, 443}

	t.Run("allowed", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidatePort("10.200.0.5:50000", ports)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("denied", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidatePort("10.200.0.5:8080", ports)
		if !errors.Is(err, proxy.ErrPortDenied) {
			t.Fatalf("expected ErrPortDenied, got %v", err)
		}
	})

	t.Run("nil ports allows all", func(t *testing.T) {
		t.Parallel()

		err := proxy.ValidatePort("10.200.0.5:8080", nil)
		if err != nil {
			t.Fatalf("expected no error with nil ports, got %v", err)
		}
	})
}

func parseTestPort(t *testing.T, portStr string) uint16 {
	t.Helper()

	var port uint16

	_, err := fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", portStr, err)
	}

	return port
}

func TestActiveConnections(t *testing.T) {
	t.Parallel()

	echo := startEchoServer(t)
	defer func() { _ = echo.Close() }()

	proxyListener, server := startProxy(t, nil)

	if server.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active connections, got %d", server.ActiveConnections())
	}

	conn := dialProxy(t, proxyListener.Addr().String(), echo.Addr().String())

	// Give the proxy goroutine time to increment the counter.
	time.Sleep(100 * time.Millisecond)

	if server.ActiveConnections() != 1 {
		t.Fatalf("expected 1 active connection, got %d", server.ActiveConnections())
	}

	_ = conn.Close()

	// Give the proxy goroutine time to decrement the counter.
	time.Sleep(100 * time.Millisecond)

	if server.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active connections after close, got %d", server.ActiveConnections())
	}
}
