package proxy

import (
	"fmt"
	"net"
	"slices"
	"strconv"
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
