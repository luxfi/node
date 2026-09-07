// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestChainsDefaultsEmpty pins the consent contract: a node runs no
// consent-requiring chain unless its operator names one. The default has to be
// empty rather than "everything in genesis", because the chains behind it cost
// an operator something validating the primary network did not ask for — a
// matcher to sit beside, an HSM, a GPU, a custody role — and a default that
// opts every validator into those is a default nobody chose.
func TestChainsDefaultsEmpty(t *testing.T) {
	require := require.New(t)

	fs := BuildFlagSet()
	flag := fs.Lookup(ChainsKey)
	require.NotNil(flag, "chains flag must be registered")

	v := viper.New()
	require.NoError(v.BindPFlags(fs))
	require.Empty(v.GetStringSlice(ChainsKey), "chains must read empty by default")

	v.Set(ChainsKey, []string{"D", "B", "M"})
	require.Equal([]string{"D", "B", "M"}, v.GetStringSlice(ChainsKey))
}

// TestChainsTakesAListNotABool is the regression lock on the shape. The opt-in
// was a bool named for ONE chain, which answered the question for that chain
// and left the next one to add a second flag, a second config field and a
// second branch that could disagree with the first. One list, any number of
// chains, one rule reading it.
func TestChainsTakesAListNotABool(t *testing.T) {
	fs := BuildFlagSet()
	flag := fs.Lookup(ChainsKey)
	require.NotNil(t, flag)
	require.Equal(t, "stringSlice", flag.Value.Type(),
		"consent must be a list of chains, not a per-chain boolean")

	var missing *pflag.Flag
	require.Equal(t, missing, fs.Lookup("dex-validator"),
		"the per-chain boolean must be gone, not shadowed by the list")
}
