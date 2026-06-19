// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestDexValidatorFlagDefaultsOff pins the runtime opt-in contract for the
// D-Chain DEX: the dex-validator flag exists, defaults to false (most nodes do
// NOT run the DEX even though the DEXVM factory is always linked), and flips to
// true when set. Participation is a runtime decision, not a compile-time one.
func TestDexValidatorFlagDefaultsOff(t *testing.T) {
	require := require.New(t)

	// The flag must be registered in the canonical flag set.
	fs := BuildFlagSet()
	flag := fs.Lookup(DexValidatorKey)
	require.NotNil(flag, "dex-validator flag must be registered")
	require.Equal("false", flag.DefValue, "dex-validator must default to false")

	// Default (unset) reads as false.
	v := viper.New()
	require.NoError(v.BindPFlags(fs))
	require.False(v.GetBool(DexValidatorKey), "dex-validator must read false by default")

	// Explicit opt-in flips it.
	v.Set(DexValidatorKey, true)
	require.True(v.GetBool(DexValidatorKey), "dex-validator must read true when set")
}
