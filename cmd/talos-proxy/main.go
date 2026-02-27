// Package main is the entrypoint for the talos-proxy binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kommodity-io/talos-proxy/internal/proxy"
)

const (
	defaultListenAddress = ":50000"
	defaultDialTimeout   = 5 * time.Second
)

func main() {
	err := run()
	if err != nil {
		logger, _ := zap.NewProduction()
		logger.Error("fatal error", zap.Error(err))
		os.Exit(1)
	}
}

func run() error {
	listenAddr := flag.String("listen-address", defaultListenAddress, "address to listen on (host:port)")
	dialTimeout := flag.Duration("dial-timeout", defaultDialTimeout, "timeout for dialing target addresses")
	allowedCIDRs := flag.String("allowed-cidrs", "", "comma-separated list of allowed target CIDRs (empty = allow all)")
	allowedPorts := flag.String("allowed-ports", "", "comma-separated list of allowed target ports (empty = allow all)")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")

	flag.Parse()

	level, err := zap.ParseAtomicLevel(*logLevel)
	if err != nil {
		return fmt.Errorf("parsing log level: %w", err)
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = level

	logger, err := cfg.Build()
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}
	defer logger.Sync()

	cidrs, err := parseCIDRs(*allowedCIDRs)
	if err != nil {
		return fmt.Errorf("parsing allowed CIDRs: %w", err)
	}

	ports, err := parsePorts(*allowedPorts)
	if err != nil {
		return fmt.Errorf("parsing allowed ports: %w", err)
	}

	server := proxy.NewServer(*dialTimeout, cidrs, ports, logger)

	listenConfig := net.ListenConfig{}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	listener, err := listenConfig.Listen(ctx, "tcp", *listenAddr)
	if err != nil {
		return fmt.Errorf("creating listener on %s: %w", *listenAddr, err)
	}

	logger.Info("talos-proxy starting",
		zap.String("listen-address", *listenAddr),
		zap.Duration("dial-timeout", *dialTimeout),
		zap.String("allowed-cidrs", *allowedCIDRs),
		zap.String("allowed-ports", *allowedPorts),
	)

	err = server.Serve(ctx, listener)
	if err != nil {
		return fmt.Errorf("server exited: %w", err)
	}

	logger.Info("talos-proxy stopped")

	return nil
}

// parseCIDRs parses a comma-separated list of CIDR strings into net.IPNet values.
// Returns nil if the input is empty.
func parseCIDRs(raw string) ([]*net.IPNet, error) {
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	cidrs := make([]*net.IPNet, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		_, cidr, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", part, err)
		}

		cidrs = append(cidrs, cidr)
	}

	return cidrs, nil
}

// parsePorts parses a comma-separated list of port numbers into uint16 values.
// Returns nil if the input is empty.
func parsePorts(raw string) ([]uint16, error) {
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	ports := make([]uint16, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		port, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", part, err)
		}

		ports = append(ports, uint16(port)) //nolint:gosec // bounds checked by ParseUint with bitSize 16
	}

	return ports, nil
}
