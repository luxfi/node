// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSufficientlyStrong(t *testing.T) {
	tests := []struct {
		password string
		expected Strength
	}{
		{
			password: "",
			expected: VeryWeak,
		},
		{
			password: "a",
			expected: VeryWeak,
		},
		{
			password: "password",
			expected: VeryWeak,
		},
		{
			password: "thisisareallylongandpresumablyverystrongpassword",
			expected: VeryStrong,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s-%d", test.password, test.expected), func(t *testing.T) {
			require.True(t, SufficientlyStrong(test.password, test.expected))
		})
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		password    string
		expected    Strength
		expectedErr error
	}{
		{
			password:    "",
			expected:    VeryWeak,
			expectedErr: ErrEmptyPassword,
		},
		{
			password: "a",
			expected: VeryWeak,
		},
		{
			password: "password",
			expected: VeryWeak,
		},
		{
			password: "thisisareallylongandpresumablyverystrongpassword",
			expected: VeryStrong,
		},
		{
			password: string(make([]byte, maxPassLen)),
			expected: VeryWeak,
		},
		{
			password:    string(make([]byte, maxPassLen+1)),
			expected:    VeryWeak,
			expectedErr: ErrPassMaxLength,
		},
		{
			password:    "password",
			expected:    Weak,
			expectedErr: ErrWeakPassword,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s-%d", test.password, test.expected), func(t *testing.T) {
			err := IsValid(test.password, test.expected)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

// TestIsValidRejectsLongButGuessable pins that strength is scored, not measured
// in characters. Every password here clears the 20-character bar the length-only
// fallback applied at OK (Fair), and each is one of the first guesses a cracker
// makes. The fallback shipped in the default build, so these were accepted by
// keystore.CreateUser and auth.ChangePassword.
func TestIsValidRejectsLongButGuessable(t *testing.T) {
	for _, pw := range []string{
		"aaaaaaaaaaaaaaaaaaaa",
		"password1234password",
		"11111111111111111111",
		"qwertyuiopqwertyuiop",
		"passwordpasswordpassword",
	} {
		t.Run(pw, func(t *testing.T) {
			require.GreaterOrEqual(t, len(pw), 20)
			require.ErrorIs(t, IsValid(pw, OK), ErrWeakPassword)
			require.False(t, SufficientlyStrong(pw, OK))
		})
	}
}
