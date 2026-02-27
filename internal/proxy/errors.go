package proxy

import "errors"

var (
	// ErrHeaderTooLarge is returned when the address header exceeds the maximum allowed size.
	ErrHeaderTooLarge = errors.New("address header exceeds maximum size")

	// ErrInvalidAddress is returned when the target address is not a valid host:port pair.
	ErrInvalidAddress = errors.New("invalid target address: must be host:port")

	// ErrCIDRDenied is returned when the target IP is not within any of the allowed CIDRs.
	ErrCIDRDenied = errors.New("target address not in allowed CIDRs")
)
