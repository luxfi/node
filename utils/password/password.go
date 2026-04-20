//go:build !zxcvbn

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"errors"
	"fmt"
)

// Strength is the strength of a password
type Strength int

const (
	// The scoring mechanism of the zxcvbn package is defined as follows:
	// 0 # too guessable: risky password. (guesses < 10^3)
	// 1 # very guessable: protection from throttled online attacks. (guesses < 10^6)
	// 2 # somewhat guessable: protection from unthrottled online attacks. (guesses < 10^8)
	// 3 # safely unguessable: moderate protection from offline slow-hash scenario. (guesses < 10^10)
	// 4 # very unguessable: strong protection from offline slow-hash scenario. (guesses >= 10^10)

	// VeryWeak password
	VeryWeak = 0
	// Weak password
	Weak = 1
	// Fair password
	Fair = 2
	// Strong password
	Strong = 3
	// VeryStrong password
	VeryStrong = 4

	// OK password is the recommended minimum strength for API calls
	OK = Fair

	// maxPassLen is the maximum allowed password length
	maxPassLen = 1024

)

var (
	ErrEmptyPassword = errors.New("empty password")
	ErrPassMaxLength = fmt.Errorf("password exceeds maximum length of %d chars", maxPassLen)
	ErrWeakPassword  = errors.New("password is too weak")
)

// SufficientlyStrong returns true if [password] meets basic length requirements.
// Build with -tags=zxcvbn for full password strength analysis.
func SufficientlyStrong(password string, minimumStrength Strength) bool {
	// VeryWeak (0) accepts any password - this is the minimum bar
	if minimumStrength == VeryWeak {
		return true
	}
	// Higher strength levels require longer passwords
	// Weak(1)=12, Fair(2)=20, Strong(3)=28, VeryStrong(4)=36
	// This is more conservative than zxcvbn but provides basic protection
	minLen := 4 + int(minimumStrength)*8
	return len(password) >= minLen
}

// IsValid returns nil if [password] is a reasonable length and has strength
// greater than or equal to [minimumStrength]
func IsValid(password string, minimumStrength Strength) error {
	switch {
	case len(password) == 0:
		return ErrEmptyPassword
	case len(password) > maxPassLen:
		return ErrPassMaxLength
	case !SufficientlyStrong(password, minimumStrength):
		return ErrWeakPassword
	default:
		return nil
	}
}
