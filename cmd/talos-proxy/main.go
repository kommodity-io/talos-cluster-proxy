// Package main is the entrypoint for the talos-proxy binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kommodity-io/talos-proxy/internal/proxy"
)

const (
	defaultListenPort  = 50000
	defaultDialTimeout = 5 * time.Second
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
	listenPort := flag.Int("listen-port", defaultListenPort, "port to listen on")
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
	defer logger.Sync() //nolint:errcheck // best-effort flush

	cidrs, err := proxy.ParseCIDRs(*allowedCIDRs)
	if err != nil {
		return fmt.Errorf("parsing allowed CIDRs: %w", err)
	}

	ports, err := proxy.ParsePorts(*allowedPorts)
	if err != nil {
		return fmt.Errorf("parsing allowed ports: %w", err)
	}

	server := proxy.NewServer(*dialTimeout, cidrs, ports, logger)

	listenConfig := net.ListenConfig{}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	listenAddr := fmt.Sprintf(":%d", *listenPort)

	listener, err := listenConfig.Listen(ctx, "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("creating listener on %s: %w", listenAddr, err)
	}

	logger.Info("talos-proxy starting",
		zap.String("listen-address", listenAddr),
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
