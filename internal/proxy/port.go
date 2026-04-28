package proxy

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

// ValidatePort checks whether the target address port is in the allowed ports list.
// If allowedPorts is nil or empty, all ports are allowed.
func ValidatePort(addr string, allowedPorts []uint16) error {
	if len(allowedPorts) == 0 {
		return nil
	}

	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidAddress, addr)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("%w: invalid port %s", ErrInvalidAddress, portStr)
	}

	if slices.Contains(allowedPorts, uint16(port)) {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrPortDenied, addr)
}

// ParsePorts parses a comma-separated list of port numbers into uint16 values.
// Returns nil if the input is empty.
func ParsePorts(raw string) ([]uint16, error) {
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

		ports = append(ports, uint16(port))
	}

	return ports, nil
}
