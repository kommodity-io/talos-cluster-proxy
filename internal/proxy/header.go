package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// maxAddressLength is the maximum allowed length for a target address string.
const maxAddressLength = 253 + 1 + 5 // max DNS name (253) + colon + port (max 65535 = 5 digits)

// ReadTargetAddress reads the protocol header from conn and returns the target address.
// The header format is: 4 bytes big-endian uint32 (address length) followed by the address string.
func ReadTargetAddress(conn io.Reader) (string, error) {
	var length uint32

	err := binary.Read(conn, binary.BigEndian, &length)
	if err != nil {
		return "", fmt.Errorf("reading address length: %w", err)
	}

	if length == 0 || length > maxAddressLength {
		return "", fmt.Errorf("%w: length %d exceeds max %d", ErrHeaderTooLarge, length, maxAddressLength)
	}

	buf := make([]byte, length)

	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return "", fmt.Errorf("reading address bytes: %w", err)
	}

	addr := string(buf)

	err = validateAddress(addr)
	if err != nil {
		return "", err
	}

	return addr, nil
}

// WriteTargetAddress writes the protocol header to conn for the given target address.
// The header format is: 4 bytes big-endian uint32 (address length) followed by the address string.
func WriteTargetAddress(conn io.Writer, addr string) error {
	err := validateAddress(addr)
	if err != nil {
		return err
	}

	if len(addr) > maxAddressLength {
		return fmt.Errorf("%w: length %d exceeds max %d", ErrHeaderTooLarge, len(addr), maxAddressLength)
	}

	length := uint32(len(addr)) //nolint:gosec // bounds checked above

	err = binary.Write(conn, binary.BigEndian, length)
	if err != nil {
		return fmt.Errorf("writing address length: %w", err)
	}

	_, err = conn.Write([]byte(addr))
	if err != nil {
		return fmt.Errorf("writing address bytes: %w", err)
	}

	return nil
}

// validateAddress checks that addr is a valid host:port pair.
func validateAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidAddress, addr)
	}

	if host == "" || port == "" {
		return fmt.Errorf("%w: %s", ErrInvalidAddress, addr)
	}

	return nil
}
