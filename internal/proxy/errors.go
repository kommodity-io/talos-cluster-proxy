package proxy

import "errors"

var (
	// ErrInvalidMethod is returned when the request uses a method other than CONNECT.
	ErrInvalidMethod = errors.New("invalid HTTP method: expected CONNECT")

	// ErrInvalidAddress is returned when the target address is not a valid host:port pair.
	ErrInvalidAddress = errors.New("invalid target address: must be host:port")

	// ErrHostnameNotAllowed is returned when the target host is a DNS name rather than an IP literal.
	ErrHostnameNotAllowed = errors.New("invalid target address: must be an IP literal, not a hostname")

	// ErrCIDRDenied is returned when the target IP is not within any of the allowed CIDRs.
	ErrCIDRDenied = errors.New("target address not in allowed CIDRs")

	// ErrPortDenied is returned when the target port is not in the allowed ports list.
	ErrPortDenied = errors.New("target port not in allowed ports")
)
