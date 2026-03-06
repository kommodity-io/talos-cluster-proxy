package proxy

import (
	"fmt"
	"net"
	"strings"
)

// ValidateCIDR checks whether the target address is within one of the allowed CIDRs.
// If allowedCIDRs is nil or empty, all addresses are allowed.
func ValidateCIDR(addr string, allowedCIDRs []*net.IPNet) error {
	if len(allowedCIDRs) == 0 {
		return nil
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidAddress, addr)
	}

	targetIP := net.ParseIP(host)
	if targetIP == nil {
		return fmt.Errorf("%w: cannot parse IP from %s", ErrCIDRDenied, host)
	}

	for _, cidr := range allowedCIDRs {
		if cidr.Contains(targetIP) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrCIDRDenied, addr)
}

// ParseCIDRs parses a comma-separated list of CIDR strings into net.IPNet values.
// Returns nil if the input is empty.
func ParseCIDRs(raw string) ([]*net.IPNet, error) {
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
