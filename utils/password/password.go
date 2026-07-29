// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"errors"
	"fmt"

	"github.com/nbutton23/zxcvbn-go"
)

// Strength is the strength of a password
type Strength int

const (
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

	// maxCheckedPassLen limits the length of the password that should be
	// strength checked to avoid DoS.
	maxCheckedPassLen = 50
)

var (
	ErrEmptyPassword = errors.New("empty password")
	ErrPassMaxLength = fmt.Errorf("password exceeds maximum length of %d chars", maxPassLen)
	ErrWeakPassword  = errors.New("password is too weak")
)

// SufficientlyStrong returns true if [password] has strength greater than or
// equal to [minimumStrength], scored by zxcvbn. This is the only
// implementation: a build tag must not decide how strong a password has to be.
func SufficientlyStrong(password string, minimumStrength Strength) bool {
	if len(password) > maxCheckedPassLen {
		password = password[:maxCheckedPassLen]
	}
	return zxcvbn.PasswordStrength(password, nil).Score >= int(minimumStrength)
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
