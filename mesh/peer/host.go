// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"net"
	"strings"
)

var (
	ErrInvalidHost         = errors.New("invalid host")
	ErrIPLiteralNotAllowed = errors.New("IP literals are not allowed")
)

// UnsignedHost represents a hostname-based endpoint claim.
type UnsignedHost struct {
	Host      string
	Port      uint16
	Timestamp uint64
}

// CanonicalHost normalizes and validates a hostname.
// It rejects IP literals, trims whitespace and trailing dots, and lowercases.
func CanonicalHost(host string) (string, error) {
	canonical := strings.TrimSpace(host)
	if canonical == "" {
		return "", ErrInvalidHost
	}

	// Strip IPv6 brackets if present.
	if strings.HasPrefix(canonical, "[") && strings.HasSuffix(canonical, "]") {
		canonical = strings.TrimPrefix(canonical, "[")
		canonical = strings.TrimSuffix(canonical, "]")
	}

	// Reject IP literals (IPv4 or IPv6).
	if ip := net.ParseIP(canonical); ip != nil {
		return "", ErrIPLiteralNotAllowed
	}

	// Trim trailing dot for FQDNs.
	canonical = strings.TrimSuffix(canonical, ".")
	if canonical == "" || canonical == "." {
		return "", ErrInvalidHost
	}

	labels := strings.Split(canonical, ".")
	for _, label := range labels {
		if label == "" {
			return "", ErrInvalidHost
		}
		if len(label) > 63 {
			return "", ErrInvalidHost
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidHost
		}
		for i := 0; i < len(label); i++ {
			ch := label[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
				continue
			}
			return "", ErrInvalidHost
		}
	}

	return strings.ToLower(canonical), nil
}
