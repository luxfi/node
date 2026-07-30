// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"
)

// Every consensus key getConsensusConfig reads must also be a REGISTERED flag.
//
// These are two different surfaces and it is possible to ship one without the
// other: config/spec/flags.go is a catalog, config/flags.go is what actually
// calls fs.Duration/fs.Int on the pflag.FlagSet. A key wired into
// getConsensusConfig but never registered reads fine from a config file and
// dies on the command line with
//
//	luxd: failed to parse config: unknown flag: --<key>
//
// which is a hard boot failure, not a warning. That shipped once:
// --consensus-convergence-settle-window was added to keys.go, wired into
// getConsensusConfig and listed in the spec, but not registered — the unit test
// passed because it set the viper key directly, and the gap only surfaced when
// the built image crash-looped on startup.
func TestConsensusKeysAreRegisteredFlags(t *testing.T) {
	fs := BuildFlagSet()

	for _, key := range []string{
		ConsensusSampleSizeKey,
		ConsensusPreferenceQuorumSizeKey,
		ConsensusConfidenceQuorumSizeKey,
		ConsensusQuorumSizeKey,
		ConsensusCommitThresholdKey,
		ConsensusConcurrentRepollsKey,
		ConsensusOptimalProcessingKey,
		ConsensusMaxProcessingKey,
		ConsensusMaxTimeProcessingKey,
		ConsensusConvergenceSettleWindowKey,
	} {
		if fs.Lookup(key) == nil {
			t.Errorf("--%s is read by getConsensusConfig but is NOT a registered flag; luxd would exit with %q", key, "unknown flag: --"+key)
		}
	}
}

// The settle window must survive the whole path: flag -> parse -> Parameters.
// Setting the viper key directly (as the sibling test does) cannot catch a
// missing registration, so parse a real argv here.
func TestConvergenceSettleWindowParsesFromArgv(t *testing.T) {
	fs := BuildFlagSet()
	if err := fs.Parse([]string{"--consensus-convergence-settle-window=2s"}); err != nil {
		t.Fatalf("parsing the flag failed: %v", err)
	}
	f := fs.Lookup(ConsensusConvergenceSettleWindowKey)
	if f == nil {
		t.Fatal("flag not registered")
	}
	if got := f.Value.String(); got != "2s" {
		t.Errorf("value = %q, want %q", got, "2s")
	}
}
