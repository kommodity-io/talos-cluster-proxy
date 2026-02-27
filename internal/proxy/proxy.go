// Package proxy implements a TCP proxy that reads a target address header
// from each incoming connection, dials the target, and performs bidirectional
// byte forwarding.
package proxy

import (
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
	// defaultHeaderReadTimeout is the maximum time to wait for the address header.
	defaultHeaderReadTimeout = 5 * time.Second

	// defaultDialTimeout is the maximum time to wait when dialing the target.
	defaultDialTimeout = 5 * time.Second
)

// Server is a TCP proxy that reads a target address header from each incoming
// connection, dials the target, and performs bidirectional byte forwarding.
type Server struct {
	dialTimeout  time.Duration
	allowedCIDRs []*net.IPNet
	allowedPorts []uint16
	activeConns  atomic.Int64
	logger       *zap.Logger
}

// NewServer creates a new proxy Server with the given options.
func NewServer(dialTimeout time.Duration, allowedCIDRs []*net.IPNet, allowedPorts []uint16, logger *zap.Logger) *Server {
	if dialTimeout == 0 {
		dialTimeout = defaultDialTimeout
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

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.logger.Info("proxy server shutting down")

				return nil
			default:
				return fmt.Errorf("accepting connection: %w", err)
			}
		}

		go s.handleConnection(ctx, conn)
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

	targetAddr, err := s.readAndValidateHeader(clientConn, remoteAddr)
	if err != nil {
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
			zap.String("error", err.Error()),
		)

		return
	}
	defer func() { _ = targetConn.Close() }()

	s.bidirectionalCopy(clientConn, targetConn, remoteAddr, targetAddr)

	s.logger.Info("connection closed",
		zap.String("remote", remoteAddr),
		zap.String("target", targetAddr),
	)
}

// readAndValidateHeader reads the protocol header and validates the target against CIDRs.
// Logs warnings/errors and returns an error if the connection should be dropped.
func (s *Server) readAndValidateHeader(clientConn net.Conn, remoteAddr string) (string, error) {
	err := clientConn.SetReadDeadline(time.Now().Add(defaultHeaderReadTimeout))
	if err != nil {
		s.logger.Error("failed to set read deadline",
			zap.String("remote", remoteAddr),
			zap.String("error", err.Error()),
		)

		return "", fmt.Errorf("setting header read deadline: %w", err)
	}

	targetAddr, err := ReadTargetAddress(clientConn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Warn("failed to read target address",
				zap.String("remote", remoteAddr),
				zap.String("error", err.Error()),
			)
		}

		return "", err
	}

	err = clientConn.SetReadDeadline(time.Time{})
	if err != nil {
		s.logger.Error("failed to clear read deadline",
			zap.String("remote", remoteAddr),
			zap.String("error", err.Error()),
		)

		return "", fmt.Errorf("clearing header read deadline: %w", err)
	}

	err = ValidateCIDR(targetAddr, s.allowedCIDRs)
	if err != nil {
		s.logger.Warn("target address denied by CIDR policy",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.String("error", err.Error()),
		)

		return "", err
	}

	err = ValidatePort(targetAddr, s.allowedPorts)
	if err != nil {
		s.logger.Warn("target address denied by port policy",
			zap.String("remote", remoteAddr),
			zap.String("target", targetAddr),
			zap.String("error", err.Error()),
		)

		return "", err
	}

	return targetAddr, nil
}

// bidirectionalCopy performs bidirectional byte forwarding between client and target,
// propagating half-close in each direction.
func (s *Server) bidirectionalCopy(
	clientConn net.Conn,
	targetConn net.Conn,
	remoteAddr string,
	targetAddr string,
) {
	var copyWg sync.WaitGroup

	copyWg.Add(1)

	go func() {
		defer copyWg.Done()

		s.copyAndCloseWrite(targetConn, clientConn, "client->target", remoteAddr, targetAddr)
	}()

	s.copyAndCloseWrite(clientConn, targetConn, "target->client", remoteAddr, targetAddr)

	copyWg.Wait()
}

// copyAndCloseWrite copies from src to dst, then signals half-close on dst.
func (s *Server) copyAndCloseWrite(
	dst net.Conn,
	src net.Conn,
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
			zap.String("error", err.Error()),
		)
	}
}
