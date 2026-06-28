// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// proposervm_wrap_test.go — pins the single policy gate that decides whether a
// linear chain.ChainVM is re-wrapped in proposervm for single-proposer-per-height
// block production (the consensus-safety fix for the equivocation crash). The
// gate is a pure function so the policy is verifiable without standing up a chain.
package chains

import (
	"testing"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
)

func TestShouldWrapInProposerVM(t *testing.T) {
	// A native chain ID that is NOT the P-Chain (e.g. the C-Chain): first 31
	// bytes zero, last byte the chain letter. Any non-platform ID exercises the
	// chainID condition; this mirrors how native chain IDs are shaped.
	cChainID := ids.ID{}
	cChainID[ids.IDLen-1] = 'C'
	xChainID := ids.ID{}
	xChainID[ids.IDLen-1] = 'X'

	tests := []struct {
		name             string
		k                int
		chainID          ids.ID
		innerIsDAGNative bool
		want             bool
		why              string
	}{
		{
			name:             "C-Chain devnet K=4",
			k:                4, // LocalBFTParams
			chainID:          cChainID,
			innerIsDAGNative: false,
			want:             true,
			why:              "multi-validator EVM, not the P-Chain, not DAG — the chain that crashed; must be wrapped",
		},
		{
			name:             "C-Chain mainnet K=21",
			k:                21, // MainnetParams
			chainID:          cChainID,
			innerIsDAGNative: false,
			want:             true,
			why:              "large multi-validator EVM is wrapped exactly as avalanchego wraps it",
		},
		{
			name:             "P-Chain is excluded even at K>1",
			k:                4,
			chainID:          constants.PlatformChainID,
			innerIsDAGNative: false,
			want:             false,
			why:              "P-Chain validator state is published mid-create; its windower would be empty — keep newPChainHeightVM",
		},
		{
			name:             "X-Chain (DAG-native) is excluded",
			k:                4,
			chainID:          xChainID,
			innerIsDAGNative: true,
			want:             false,
			why:              "linearized DAG VM uses a push-notification bridge that does not compose with proposervm's window",
		},
		{
			name:             "single-node K=1 is not wrapped",
			k:                1, // SingleValidatorParams
			chainID:          cChainID,
			innerIsDAGNative: false,
			want:             false,
			why:              "one validator is trivially the sole proposer; no equivocation to prevent",
		},
		{
			name:             "K=1 P-Chain is not wrapped",
			k:                1,
			chainID:          constants.PlatformChainID,
			innerIsDAGNative: false,
			want:             false,
			why:              "K==1 short-circuits regardless of chain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldWrapInProposerVM(tt.k, tt.chainID, tt.innerIsDAGNative)
			if got != tt.want {
				t.Fatalf("shouldWrapInProposerVM(k=%d, chainID=%s, dag=%v) = %v, want %v — %s",
					tt.k, tt.chainID, tt.innerIsDAGNative, got, tt.want, tt.why)
			}
		})
	}
}

// TestShouldWrapInProposerVM_FlowsFromSelectedParams proves the K the gate reads
// is the one selectConsensusParams produces per network, so the wrap decision is
// consistent with the BFT committee actually chosen: every sybil-protected
// network yields K>1 (C-Chain wrapped), and single-node yields K==1 (not wrapped).
func TestShouldWrapInProposerVM_FlowsFromSelectedParams(t *testing.T) {
	cChainID := ids.ID{}
	cChainID[ids.IDLen-1] = 'C'

	cases := []struct {
		name            string
		sybilProtection bool
		networkID       uint32
		wantWrapCChain  bool
	}{
		{"single-node dev", false, constants.LocalID, false},
		{"devnet sybil", true, constants.DevnetID, true},
		{"localnet sybil", true, constants.LocalID, true},
		{"mainnet", true, constants.MainnetID, true},
		{"testnet", true, constants.TestnetID, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := selectConsensusParams(c.sybilProtection, c.networkID)
			got := shouldWrapInProposerVM(params.K, cChainID, false)
			if got != c.wantWrapCChain {
				t.Fatalf("network %s (sybil=%v): K=%d → wrap=%v, want %v",
					c.name, c.sybilProtection, params.K, got, c.wantWrapCChain)
			}
			// The P-Chain is never wrapped, whatever the committee.
			if shouldWrapInProposerVM(params.K, constants.PlatformChainID, false) {
				t.Fatalf("network %s: P-Chain must never be wrapped (K=%d)", c.name, params.K)
			}
		})
	}
}
