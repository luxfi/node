// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInputValidator(t *testing.T) {
	v := NewInputValidator()

	t.Run("ValidateStringLength", func(t *testing.T) {
		// Valid length
		require.NoError(t, v.ValidateStringLength("short string"))

		// Too long
		longString := strings.Repeat("a", v.MaxStringLength+1)
		require.Error(t, v.ValidateStringLength(longString))
	})

	t.Run("ValidateFilePath", func(t *testing.T) {
		// Use platform-specific paths
		if runtime.GOOS == "windows" {
			v.AllowedPathPrefixes = []string{"C:\\tmp", "C:\\var\\tmp"}

			// Valid paths
			require.NoError(t, v.ValidateFilePath("C:\\tmp\\test.txt"))
			require.NoError(t, v.ValidateFilePath("C:\\var\\tmp\\data.log"))

			// Path traversal attempts
			require.Error(t, v.ValidateFilePath("C:\\tmp\\..\\etc\\passwd"))
			require.Error(t, v.ValidateFilePath("..\\..\\etc\\passwd"))

			// Not absolute
			require.Error(t, v.ValidateFilePath("relative\\path.txt"))

			// Outside allowed prefixes
			require.Error(t, v.ValidateFilePath("C:\\etc\\passwd"))
			require.Error(t, v.ValidateFilePath("C:\\Users\\user\\file.txt"))
		} else {
			v.AllowedPathPrefixes = []string{"/tmp", "/var/tmp"}

			// Valid paths
			require.NoError(t, v.ValidateFilePath("/tmp/test.txt"))
			require.NoError(t, v.ValidateFilePath("/var/tmp/data.log"))

			// Path traversal attempts
			require.Error(t, v.ValidateFilePath("/tmp/../etc/passwd"))
			require.Error(t, v.ValidateFilePath("../../etc/passwd"))

			// Not absolute
			require.Error(t, v.ValidateFilePath("relative/path.txt"))

			// Outside allowed prefixes
			require.Error(t, v.ValidateFilePath("/etc/passwd"))
			require.Error(t, v.ValidateFilePath("/home/user/file.txt"))
		}
	})

	t.Run("ValidateIPAddress", func(t *testing.T) {
		// Valid IPs
		require.NoError(t, v.ValidateIPAddress("192.168.1.1"))
		require.NoError(t, v.ValidateIPAddress("10.0.0.1"))
		require.NoError(t, v.ValidateIPAddress("::1"))
		require.NoError(t, v.ValidateIPAddress("2001:db8::1"))

		// Valid hostnames
		require.NoError(t, v.ValidateIPAddress("example.com"))
		require.NoError(t, v.ValidateIPAddress("sub.domain.example.com"))
		require.NoError(t, v.ValidateIPAddress("localhost"))

		// With port
		require.NoError(t, v.ValidateIPAddress("192.168.1.1:8080"))
		require.NoError(t, v.ValidateIPAddress("example.com:443"))

		// Invalid
		require.Error(t, v.ValidateIPAddress("not_a_valid-host!"))
		require.Error(t, v.ValidateIPAddress("192.168.1.256")) // Invalid octet
		require.Error(t, v.ValidateIPAddress(strings.Repeat("a", 254))) // Too long
	})

	t.Run("ValidatePort", func(t *testing.T) {
		// Valid ports
		require.NoError(t, v.ValidatePort(80))
		require.NoError(t, v.ValidatePort(443))
		require.NoError(t, v.ValidatePort(8080))
		require.NoError(t, v.ValidatePort(65535))

		// Invalid ports
		require.Error(t, v.ValidatePort(0))
		require.Error(t, v.ValidatePort(-1))
		require.Error(t, v.ValidatePort(65536))
		require.Error(t, v.ValidatePort(100000))
	})
}

func TestSafeIntConversion(t *testing.T) {
	tests := []struct {
		name    string
		value   int64
		bitSize int
		wantErr bool
	}{
		{"int8 valid", 127, 8, false},
		{"int8 overflow", 128, 8, true},
		{"int8 underflow", -129, 8, true},
		{"int16 valid", 32767, 16, false},
		{"int16 overflow", 32768, 16, true},
		{"int32 valid", 2147483647, 32, false},
		{"int32 overflow", 2147483648, 32, true},
		{"int64 valid", 9223372036854775807, 64, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeIntConversion(tt.value, tt.bitSize)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSafeUintConversion(t *testing.T) {
	tests := []struct {
		name    string
		value   int64
		bitSize int
		wantErr bool
	}{
		{"uint8 valid", 255, 8, false},
		{"uint8 overflow", 256, 8, true},
		{"uint8 negative", -1, 8, true},
		{"uint16 valid", 65535, 16, false},
		{"uint16 overflow", 65536, 16, true},
		{"uint32 valid", 4294967295, 32, false},
		{"uint32 overflow", 4294967296, 32, true},
		{"uint64 valid", 9223372036854775807, 64, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafeUintConversion(tt.value, tt.bitSize)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConstantTimeCompare(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		{"password", "password", true},
		{"password", "Password", false},
		{"short", "longer", false},
		{"", "", true},
		{"abc", "def", false},
	}

	for _, tt := range tests {
		result := ConstantTimeCompare(tt.a, tt.b)
		require.Equal(t, tt.expected, result, "Comparing %q and %q", tt.a, tt.b)
	}
}

func TestSanitizeSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal input", "normal input"},
		{"input; DROP TABLE users", "input DROP TABLE users"},
		{"input--comment", "inputcomment"},
		{"input /* comment */", "input  comment "},
		{"input'quote", "input''quote"},
		{`input"doublequote`, `input""doublequote`},
	}

	for _, tt := range tests {
		result := SanitizeSQL(tt.input)
		require.Equal(t, tt.expected, result, "Sanitizing %q", tt.input)
	}
}

func TestValidateAlphanumeric(t *testing.T) {
	// Valid
	require.NoError(t, ValidateAlphanumeric("abc123"))
	require.NoError(t, ValidateAlphanumeric("ABC"))
	require.NoError(t, ValidateAlphanumeric("123"))

	// Invalid
	require.Error(t, ValidateAlphanumeric("abc-123"))
	require.Error(t, ValidateAlphanumeric("abc_123"))
	require.Error(t, ValidateAlphanumeric("abc 123"))
	require.Error(t, ValidateAlphanumeric("abc@123"))
}

func TestValidateEmail(t *testing.T) {
	// Valid
	require.NoError(t, ValidateEmail("user@example.com"))
	require.NoError(t, ValidateEmail("user.name@example.com"))
	require.NoError(t, ValidateEmail("user+tag@example.co.uk"))

	// Invalid
	require.Error(t, ValidateEmail("notanemail"))
	require.Error(t, ValidateEmail("@example.com"))
	require.Error(t, ValidateEmail("user@"))
	require.Error(t, ValidateEmail("user@.com"))
	require.Error(t, ValidateEmail(strings.Repeat("a", 250)+"@example.com"))
}

func TestValidateURL(t *testing.T) {
	// Valid
	require.NoError(t, ValidateURL("http://example.com"))
	require.NoError(t, ValidateURL("https://example.com"))
	require.NoError(t, ValidateURL("https://example.com/path"))

	// Invalid - dangerous patterns
	require.Error(t, ValidateURL("javascript:alert(1)"))
	require.Error(t, ValidateURL("data:text/html,<script>alert(1)</script>"))
	require.Error(t, ValidateURL("vbscript:msgbox"))
	require.Error(t, ValidateURL("file:///etc/passwd"))

	// Invalid - wrong protocol
	require.Error(t, ValidateURL("ftp://example.com"))
	require.Error(t, ValidateURL("example.com"))
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(3, 60) // 3 requests per 60 seconds
	clientID := "test-client"
	now := time.Now().Unix()

	// First 3 requests should be allowed
	require.True(t, rl.Allow(clientID, now))
	require.True(t, rl.Allow(clientID, now+1))
	require.True(t, rl.Allow(clientID, now+2))

	// 4th request should be blocked
	require.False(t, rl.Allow(clientID, now+3))

	// After window expires, should allow again
	require.True(t, rl.Allow(clientID, now+61))
}