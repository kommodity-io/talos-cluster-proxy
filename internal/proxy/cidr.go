package proxy

import (
	"fmt"
	"net"
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
