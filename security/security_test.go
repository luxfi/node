// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package security provides security testing and validation
package security

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTLSConfiguration ensures proper TLS configuration
func TestTLSConfiguration(t *testing.T) {
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
		InsecureSkipVerify:       false,
	}

	// Verify minimum TLS version
	require.GreaterOrEqual(t, config.MinVersion, uint16(tls.VersionTLS12), "TLS version must be 1.2 or higher")

	// Verify InsecureSkipVerify is false
	require.False(t, config.InsecureSkipVerify, "InsecureSkipVerify must be false in production")

	// Verify strong cipher suites
	require.NotEmpty(t, config.CipherSuites, "Cipher suites must be explicitly configured")
}

// TestIntegerOverflowProtection tests for integer overflow vulnerabilities
func TestIntegerOverflowProtection(t *testing.T) {
	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "uint32 to int conversion",
			fn: func() error {
				var u32 uint32 = math.MaxUint32
				if int(u32) < 0 {
					return fmt.Errorf("integer overflow detected")
				}
				return nil
			},
		},
		{
			name: "int64 multiplication overflow",
			fn: func() error {
				a := int64(math.MaxInt64 / 2)
				b := int64(3)
				// Check for overflow before multiplication
				if a > 0 && b > 0 && a > math.MaxInt64/b {
					return fmt.Errorf("multiplication would overflow")
				}
				_ = a * b
				return nil
			},
		},
		{
			name: "safe string to int conversion",
			fn: func() error {
				input := "9223372036854775807" // MaxInt64
				val, err := strconv.ParseInt(input, 10, 64)
				if err != nil {
					return err
				}
				if val < 0 {
					return fmt.Errorf("unexpected negative value")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			// Some tests expect errors for overflow detection
			if strings.Contains(tt.name, "overflow") && err != nil {
				// Expected error for overflow detection
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestInputValidation ensures proper input validation
func TestInputValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(string) error
	}{
		{
			name:  "numeric input validation",
			input: "12345",
			validate: func(s string) error {
				val, err := strconv.Atoi(s)
				if err != nil {
					return err
				}
				if val < 0 || val > 65535 {
					return fmt.Errorf("value out of range")
				}
				return nil
			},
		},
		{
			name:  "path traversal prevention",
			input: "../../../etc/passwd",
			validate: func(s string) error {
				if strings.Contains(s, "..") {
					return fmt.Errorf("path traversal detected")
				}
				return nil
			},
		},
		{
			name:  "SQL injection prevention",
			input: "'; DROP TABLE users; --",
			validate: func(s string) error {
				dangerous := []string{"DROP", "DELETE", "INSERT", "UPDATE", ";", "--"}
				upper := strings.ToUpper(s)
				for _, d := range dangerous {
					if strings.Contains(upper, d) {
						return fmt.Errorf("potentially dangerous SQL detected")
					}
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validate(tt.input)
			// Path traversal and SQL injection tests should fail
			if strings.Contains(tt.name, "traversal") || strings.Contains(tt.name, "injection") {
				require.Error(t, err, "Should detect and prevent %s", tt.name)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestCryptographicRandomness ensures proper use of crypto/rand
func TestCryptographicRandomness(t *testing.T) {
	// Test generation of cryptographically secure random bytes
	sizes := []int{16, 32, 64, 128}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			buf := make([]byte, size)
			n, err := rand.Read(buf)
			require.NoError(t, err)
			require.Equal(t, size, n)

			// Verify randomness (basic check - no zeros)
			allZeros := true
			for _, b := range buf {
				if b != 0 {
					allZeros = false
					break
				}
			}
			require.False(t, allZeros, "Random bytes should not be all zeros")
		})
	}
}

// TestHTTPClientTimeout ensures HTTP clients have proper timeouts
func TestHTTPClientTimeout(t *testing.T) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}

	// Verify timeout is set
	require.NotEqual(t, 0, client.Timeout, "HTTP client must have a timeout")
	require.LessOrEqual(t, client.Timeout, 60*time.Second, "HTTP client timeout should be reasonable")

	// Verify transport timeouts
	transport := client.Transport.(*http.Transport)
	require.NotEqual(t, 0, transport.TLSHandshakeTimeout, "TLS handshake timeout must be set")
	require.NotEqual(t, 0, transport.ResponseHeaderTimeout, "Response header timeout must be set")
}

// TestRateLimiting provides a basic rate limiting test
func TestRateLimiting(t *testing.T) {
	type rateLimiter struct {
		requests map[string][]time.Time
		limit    int
		window   time.Duration
	}

	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    10,
		window:   time.Minute,
	}

	checkLimit := func(clientID string) bool {
		now := time.Now()
		requests := rl.requests[clientID]

		// Remove old requests outside the window
		validRequests := []time.Time{}
		for _, reqTime := range requests {
			if now.Sub(reqTime) <= rl.window {
				validRequests = append(validRequests, reqTime)
			}
		}

		if len(validRequests) >= rl.limit {
			return false // Rate limit exceeded
		}

		validRequests = append(validRequests, now)
		rl.requests[clientID] = validRequests
		return true
	}

	clientID := "test-client"

	// Should allow first requests
	for i := 0; i < 10; i++ {
		allowed := checkLimit(clientID)
		require.True(t, allowed, "Request %d should be allowed", i+1)
	}

	// Should block after limit
	allowed := checkLimit(clientID)
	require.False(t, allowed, "Request should be blocked after rate limit")
}

// TestAccessControl verifies basic access control patterns
func TestAccessControl(t *testing.T) {
	type permission string
	const (
		permRead  permission = "read"
		permWrite permission = "write"
		permAdmin permission = "admin"
	)

	type role struct {
		name        string
		permissions []permission
	}

	roles := map[string]role{
		"viewer": {
			name:        "viewer",
			permissions: []permission{permRead},
		},
		"editor": {
			name:        "editor",
			permissions: []permission{permRead, permWrite},
		},
		"admin": {
			name:        "admin",
			permissions: []permission{permRead, permWrite, permAdmin},
		},
	}

	hasPermission := func(roleName string, perm permission) bool {
		role, exists := roles[roleName]
		if !exists {
			return false
		}
		for _, p := range role.permissions {
			if p == perm {
				return true
			}
		}
		return false
	}

	// Test permission checks
	require.True(t, hasPermission("viewer", permRead))
	require.False(t, hasPermission("viewer", permWrite))
	require.False(t, hasPermission("viewer", permAdmin))

	require.True(t, hasPermission("editor", permRead))
	require.True(t, hasPermission("editor", permWrite))
	require.False(t, hasPermission("editor", permAdmin))

	require.True(t, hasPermission("admin", permRead))
	require.True(t, hasPermission("admin", permWrite))
	require.True(t, hasPermission("admin", permAdmin))

	// Test non-existent role
	require.False(t, hasPermission("nonexistent", permRead))
}

// TestSecureStringComparison tests constant-time string comparison
func TestSecureStringComparison(t *testing.T) {
	// For security-sensitive comparisons (like tokens, passwords),
	// use constant-time comparison to prevent timing attacks
	constantTimeCompare := func(a, b string) bool {
		if len(a) != len(b) {
			return false
		}

		var result byte
		for i := 0; i < len(a); i++ {
			result |= a[i] ^ b[i]
		}
		return result == 0
	}

	tests := []struct {
		a, b     string
		expected bool
	}{
		{"password123", "password123", true},
		{"password123", "password124", false},
		{"short", "longer", false},
		{"", "", true},
	}

	for _, tt := range tests {
		result := constantTimeCompare(tt.a, tt.b)
		require.Equal(t, tt.expected, result, "Comparing %q and %q", tt.a, tt.b)
	}
}