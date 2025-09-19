// Package security provides security validation and protection utilities
package security

import (
	"crypto/subtle"
	"fmt"
	"math"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

// InputValidator provides methods for validating and sanitizing user input
type InputValidator struct {
	// MaxStringLength is the maximum allowed string length
	MaxStringLength int
	// AllowedPathPrefixes are the allowed path prefixes for file operations
	AllowedPathPrefixes []string
}

// NewInputValidator creates a new input validator with default settings
func NewInputValidator() *InputValidator {
	return &InputValidator{
		MaxStringLength:     10000,
		AllowedPathPrefixes: []string{"/tmp", "/var/tmp"},
	}
}

// ValidateStringLength checks if a string length is within acceptable bounds
func (v *InputValidator) ValidateStringLength(s string) error {
	if len(s) > v.MaxStringLength {
		return fmt.Errorf("string length %d exceeds maximum allowed %d", len(s), v.MaxStringLength)
	}
	return nil
}

// ValidateFilePath validates a file path for security issues
func (v *InputValidator) ValidateFilePath(path string) error {
	// Clean the path to resolve . and .. elements
	cleanPath := filepath.Clean(path)

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal detected in path: %s", path)
	}

	// Ensure the path is absolute
	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("path must be absolute: %s", cleanPath)
	}

	// Check if path is within allowed prefixes
	allowed := false
	for _, prefix := range v.AllowedPathPrefixes {
		if strings.HasPrefix(cleanPath, prefix) {
			allowed = true
			break
		}
	}

	if !allowed && len(v.AllowedPathPrefixes) > 0 {
		return fmt.Errorf("path %s is not within allowed prefixes", cleanPath)
	}

	return nil
}

// ValidateIPAddress validates an IP address or hostname
func (v *InputValidator) ValidateIPAddress(addr string) error {
	// Check if it's a hostname:port combination and extract the host
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		addr = host
	}

	// First check if it looks like an IPv4 address and validate properly
	if strings.Count(addr, ".") == 3 && !strings.Contains(addr, ":") {
		parts := strings.Split(addr, ".")
		allNumeric := true
		for _, part := range parts {
			if part == "" {
				allNumeric = false
				break
			}
			for _, ch := range part {
				if ch < '0' || ch > '9' {
					allNumeric = false
					break
				}
			}
			if !allNumeric {
				break
			}
		}

		// Only validate as IPv4 if all parts are numeric
		if allNumeric {
			for _, part := range parts {
				num := 0
				if _, err := fmt.Sscanf(part, "%d", &num); err != nil || num < 0 || num > 255 {
					return fmt.Errorf("invalid IPv4 address: %s", addr)
				}
			}
		}
	}

	// Check if it's a valid IP address
	ip := net.ParseIP(addr)
	if ip != nil {
		return nil
	}

	// Basic hostname validation
	if len(addr) > 253 {
		return fmt.Errorf("hostname too long: %d characters", len(addr))
	}

	// Check for valid hostname pattern
	hostnameRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !hostnameRegex.MatchString(addr) {
		return fmt.Errorf("invalid hostname or IP address: %s", addr)
	}

	return nil
}

// ValidatePort validates a network port number
func (v *InputValidator) ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be between 1 and 65535)", port)
	}
	return nil
}

// SafeIntConversion safely converts between integer types checking for overflow
func SafeIntConversion(value int64, bitSize int) (int64, error) {
	switch bitSize {
	case 8:
		if value < math.MinInt8 || value > math.MaxInt8 {
			return 0, fmt.Errorf("value %d overflows int8", value)
		}
	case 16:
		if value < math.MinInt16 || value > math.MaxInt16 {
			return 0, fmt.Errorf("value %d overflows int16", value)
		}
	case 32:
		if value < math.MinInt32 || value > math.MaxInt32 {
			return 0, fmt.Errorf("value %d overflows int32", value)
		}
	case 64:
		// Already int64, no overflow possible
	default:
		return 0, fmt.Errorf("unsupported bit size: %d", bitSize)
	}
	return value, nil
}

// SafeUintConversion safely converts to unsigned integer checking for overflow
func SafeUintConversion(value int64, bitSize int) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("cannot convert negative value %d to unsigned", value)
	}

	uvalue := uint64(value)
	switch bitSize {
	case 8:
		if uvalue > math.MaxUint8 {
			return 0, fmt.Errorf("value %d overflows uint8", value)
		}
	case 16:
		if uvalue > math.MaxUint16 {
			return 0, fmt.Errorf("value %d overflows uint16", value)
		}
	case 32:
		if uvalue > math.MaxUint32 {
			return 0, fmt.Errorf("value %d overflows uint32", value)
		}
	case 64:
		// Already uint64, no overflow possible from positive int64
	default:
		return 0, fmt.Errorf("unsupported bit size: %d", bitSize)
	}
	return uvalue, nil
}

// ConstantTimeCompare performs constant-time comparison of two strings
// Returns true if strings are equal, false otherwise
func ConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	aBytes := []byte(a)
	bBytes := []byte(b)
	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1
}

// SanitizeSQL performs basic SQL injection prevention
// This is a basic implementation - use prepared statements for production
func SanitizeSQL(input string) string {
	// Remove or escape potentially dangerous characters
	replacer := strings.NewReplacer(
		";", "",
		"--", "",
		"/*", "",
		"*/", "",
		"xp_", "",
		"sp_", "",
		"'", "''",
		"\"", "\"\"",
	)
	return replacer.Replace(input)
}

// ValidateAlphanumeric checks if a string contains only alphanumeric characters
func ValidateAlphanumeric(s string) error {
	alphanumeric := regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	if !alphanumeric.MatchString(s) {
		return fmt.Errorf("string contains non-alphanumeric characters")
	}
	return nil
}

// ValidateEmail performs basic email validation
func ValidateEmail(email string) error {
	// Basic email regex pattern
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("invalid email format")
	}
	if len(email) > 254 { // RFC 5321
		return fmt.Errorf("email address too long")
	}
	return nil
}

// ValidateURL performs basic URL validation
func ValidateURL(url string) error {
	// Check for common URL injection patterns
	dangerous := []string{
		"javascript:",
		"data:",
		"vbscript:",
		"file:",
		"<script",
		"onclick",
		"onerror",
	}

	lowerURL := strings.ToLower(url)
	for _, pattern := range dangerous {
		if strings.Contains(lowerURL, pattern) {
			return fmt.Errorf("potentially dangerous URL pattern detected: %s", pattern)
		}
	}

	// Basic URL format check
	if !strings.HasPrefix(lowerURL, "http://") && !strings.HasPrefix(lowerURL, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}

	return nil
}

// RateLimiter provides basic rate limiting functionality
type RateLimiter struct {
	requests map[string][]int64
	limit    int
	window   int64 // in seconds
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, windowSeconds int64) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]int64),
		limit:    limit,
		window:   windowSeconds,
	}
}

// Allow checks if a request should be allowed based on rate limits
func (r *RateLimiter) Allow(clientID string, now int64) bool {
	// Clean old requests
	validRequests := []int64{}
	for _, reqTime := range r.requests[clientID] {
		if now-reqTime <= r.window {
			validRequests = append(validRequests, reqTime)
		}
	}

	if len(validRequests) >= r.limit {
		r.requests[clientID] = validRequests
		return false
	}

	validRequests = append(validRequests, now)
	r.requests[clientID] = validRequests
	return true
}