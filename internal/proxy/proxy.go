// Package proxy implements an HTTP CONNECT proxy that establishes tunnels
// to TCP targets after validating them against CIDR and port allowlists.
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// defaultHeaderReadTimeout is the maximum time to wait for the CONNECT request.
	defaultHeaderReadTimeout = 5 * time.Second

	// DefaultDialTimeout is the default timeout used when dialing a target if none is specified.
	DefaultDialTimeout = 5 * time.Second
)

// Server is an HTTP CONNECT proxy that dials a target and performs
// bidirectional byte forwarding after the tunnel is established.
type Server struct {
	dialTimeout  time.Duration
	allowedCIDRs []*net.IPNet
	allowedPorts []uint16
	activeConns  atomic.Int64
	logger       *zap.Logger
}

// NewServer creates a new proxy Server with the given options.
func NewServer(
	dialTimeout time.Duration, allowedCIDRs []*net.IPNet, allowedPorts []uint16, logger *zap.Logger,
) *Server {
	if dialTimeout == 0 {
		dialTimeout = DefaultDialTimeout
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Server{
		dialTimeout:  dialTimeout,
		allowedCIDRs: allowedCIDRs,
		allowedPorts: allowedPorts,
		logger:       logger,
	}
}

// Serve accepts connections from the listener and handles each one in a goroutine.
// It blocks until the context is cancelled or the listener is closed.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	s.logger.Info("proxy server starting",
		zap.String("address", listener.Addr().String()),
	)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	var waitGroup sync.WaitGroup

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.logger.Info("proxy server shutting down, waiting for active connections to drain")
				waitGroup.Wait()

				return nil
			default:
				return fmt.Errorf("accepting connection: %w", err)
			}
		}

		waitGroup.Go(func() {
			s.handleConnection(ctx, conn)
		})
	}
}

// ActiveConnections returns the number of currently active proxy connections.
func (s *Server) ActiveConnections() int64 {
	return s.activeConns.Load()
}

// handleConnection processes a single proxied connection.
func (s *Server) handleConnection(ctx context.Context, clientConn net.Conn) {
	s.activeConns.Add(1)
	defer s.activeConns.Add(-1)
	defer func() { _ = clientConn.Close() }()

	remoteAddr := clientConn.RemoteAddr().String()

	clientReader, targetAddr, ok := s.readAndValidateConnect(clientConn, remoteAddr)
	if !ok {
		return
	}

	s.logger.Info("proxying connection",
		zap.String("remote", remoteAddr),
		zap.String("target", targetAddr),
	)

	dialer := net.Dialer{Timeout: s.dialTimeout}

	targetConn, err := dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		s.logger.Warn("failed to dial target",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.Error(err),
		)

		writeStatus(clientConn, "502 Bad Gateway")

		return
	}
	defer func() { _ = targetConn.Close() }()

	err = WriteConnectEstablished(clientConn)
	if err != nil {
		s.logger.Warn("failed to write 200 response",
			zap.String("remote", remoteAddr),
			zap.Error(err),
		)

		return
	}

	// Propagate shutdown into the tunnel: closing both conns unblocks io.Copy so Serve can drain.
	stop := make(chan struct{})
	defer close(stop)

	go func() {
		select {
		case <-ctx.Done():
			_ = clientConn.Close()
			_ = targetConn.Close()
		case <-stop:
		}
	}()

	s.bidirectionalCopy(clientReader, clientConn, targetConn, remoteAddr, targetAddr)

	s.logger.Info("connection closed",
		zap.String("remote", remoteAddr),
		zap.String("target", targetAddr),
	)
}

// readAndValidateConnect reads the CONNECT request and validates the target
// against CIDR and port allowlists. On failure, it writes the appropriate
// HTTP status back to the client and returns ok=false. On success, the
// returned bufio.Reader should be used as the client-side read source to
// avoid losing any bytes buffered past the request headers.
func (s *Server) readAndValidateConnect(
	clientConn net.Conn, remoteAddr string,
) (*bufio.Reader, string, bool) {
	err := clientConn.SetReadDeadline(time.Now().Add(defaultHeaderReadTimeout))
	if err != nil {
		s.logger.Error("failed to set read deadline",
			zap.String("remote", remoteAddr),
			zap.Error(err),
		)

		return nil, "", false
	}

	clientReader := bufio.NewReader(clientConn)

	targetAddr, err := ReadConnectRequest(clientReader)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Warn("failed to read CONNECT request",
				zap.String("remote", remoteAddr),
				zap.Error(err),
			)
		}

		writeStatus(clientConn, "400 Bad Request")

		return nil, "", false
	}

	err = clientConn.SetReadDeadline(time.Time{})
	if err != nil {
		s.logger.Error("failed to clear read deadline",
			zap.String("remote", remoteAddr),
			zap.Error(err),
		)

		writeStatus(clientConn, "500 Internal Server Error")

		return nil, "", false
	}

	if !s.checkAllowlists(clientConn, remoteAddr, targetAddr) {
		return nil, "", false
	}

	return clientReader, targetAddr, true
}

// checkAllowlists runs CIDR + port allowlist checks, writing 403 and returning
// false if either fails.
func (s *Server) checkAllowlists(clientConn net.Conn, remoteAddr, targetAddr string) bool {
	err := ValidateCIDR(targetAddr, s.allowedCIDRs)
	if err != nil {
		s.logger.Warn("target address denied by CIDR policy",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.Error(err),
		)

		writeStatus(clientConn, "403 Forbidden")

		return false
	}

	err = ValidatePort(targetAddr, s.allowedPorts)
	if err != nil {
		s.logger.Warn("target address denied by port policy",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.Error(err),
		)

		writeStatus(clientConn, "403 Forbidden")

		return false
	}

	return true
}

// writeStatus writes a minimal HTTP/1.1 response with the given status line.
func writeStatus(w io.Writer, status string) {
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %s\r\n\r\n", status)
}

// bidirectionalCopy performs bidirectional byte forwarding between client and target,
// propagating half-close in each direction. clientReader carries any bytes that were
// buffered past the CONNECT request headers; clientConn is used for CloseWrite.
func (s *Server) bidirectionalCopy(
	clientReader io.Reader,
	clientConn net.Conn,
	targetConn net.Conn,
	remoteAddr string,
	targetAddr string,
) {
	var copyWg sync.WaitGroup

	copyWg.Go(func() {
		s.copyAndCloseWrite(targetConn, clientReader, "client->target", remoteAddr, targetAddr)
	})

	copyWg.Go(func() {
		s.copyAndCloseWrite(clientConn, targetConn, "target->client", remoteAddr, targetAddr)
	})

	copyWg.Wait()
}

// copyAndCloseWrite copies from src to dst, then signals half-close on dst.
func (s *Server) copyAndCloseWrite(
	dst net.Conn,
	src io.Reader,
	direction string,
	remoteAddr string,
	targetAddr string,
) {
	s.logger.Debug("starting copy",
		zap.String("direction", direction),
		zap.String("remote", remoteAddr),
		zap.String("target", targetAddr),
	)

	_, err := io.Copy(dst, src)

	if tc, ok := dst.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}

	if err != nil {
		s.logger.Debug(direction+" copy ended",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.Error(err),
		)
	}
}
