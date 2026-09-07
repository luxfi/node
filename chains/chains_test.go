// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/nets"
)

func TestNewNets(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	}

	chains, err := NewNets(ids.EmptyNodeID, config)
	require.NoError(err)

	chain, ok := chains.GetOrCreate(constants.PrimaryNetworkID)
	require.False(ok)
	require.Equal(config[constants.PrimaryNetworkID], chain.Config())
}

func TestNewNetsNoPrimaryNetworkConfig(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]nets.Config{}

	_, err := NewNets(ids.EmptyNodeID, config)
	require.ErrorIs(err, ErrNoPrimaryNetworkConfig)
}

func TestNetsGetOrCreate(t *testing.T) {
	testChainID := ids.GenerateTestID()

	type args struct {
		netID ids.ID
		want  bool
	}

	tests := []struct {
		name string
		args []args
	}{
		{
			name: "adding duplicate net is a noop",
			args: []args{
				{
					netID: testChainID,
					want:  true,
				},
				{
					netID: testChainID,
				},
			},
		},
		{
			name: "adding unique chains succeeds",
			args: []args{
				{
					netID: ids.GenerateTestID(),
					want:  true,
				},
				{
					netID: ids.GenerateTestID(),
					want:  true,
				},
				{
					netID: ids.GenerateTestID(),
					want:  true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			config := map[ids.ID]nets.Config{
				constants.PrimaryNetworkID: {},
			}
			chains, err := NewNets(ids.EmptyNodeID, config)
			require.NoError(err)

			for _, arg := range tt.args {
				_, got := chains.GetOrCreate(arg.netID)
				require.Equal(arg.want, got)
			}
		})
	}
}

func TestNetConfigs(t *testing.T) {
	testChainID := ids.GenerateTestID()

	tests := []struct {
		name   string
		config map[ids.ID]nets.Config
		netID  ids.ID
		want   nets.Config
	}{
		{
			name: "default to primary network config",
			config: map[ids.ID]nets.Config{
				constants.PrimaryNetworkID: {},
			},
			netID: testChainID,
			want:  nets.Config{},
		},
		{
			name: "use net config",
			config: map[ids.ID]nets.Config{
				constants.PrimaryNetworkID: {},
				testChainID: {
					ValidatorOnly: true,
				},
			},
			netID: testChainID,
			want: nets.Config{
				ValidatorOnly: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			chains, err := NewNets(ids.EmptyNodeID, tt.config)
			require.NoError(err)

			chain, ok := chains.GetOrCreate(tt.netID)
			require.True(ok)

			require.Equal(tt.want, chain.Config())
		})
	}
}

func TestNetsBootstrapping(t *testing.T) {
	require := require.New(t)

	config := map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	}

	chains, err := NewNets(ids.EmptyNodeID, config)
	require.NoError(err)

	netID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()

	chain, ok := chains.GetOrCreate(netID)
	require.True(ok)

	// Start bootstrapping. What comes back is the CHAIN that is syncing, never
	// the net that holds it — this assertion used to demand netID, which is
	// how the phantom "11111111111111111111111111111111LpoYY" survived review.
	chain.AddChain(chainID)
	bootstrapping := chains.Bootstrapping()
	require.Equal([]ids.ID{chainID}, bootstrapping)
	require.NotContains(bootstrapping, netID)

	// Finish bootstrapping
	chain.Bootstrapped(chainID)
	require.Empty(chains.Bootstrapping())
}

// The "bootstrapped" health check publishes Nets.Bootstrapping() verbatim as
// its message. s.chains is keyed by NET id, so returning the key reported
// constants.PrimaryNetworkID (ids.Empty, cb58
// "11111111111111111111111111111111LpoYY") as though it were an unbootstrapped
// CHAIN. Operators saw a chain ID the chain manager denies exists, and every
// stuck chain on the net collapsed into that one phantom entry.
func TestNetsBootstrappingReportsChainsNotNets(t *testing.T) {
	require := require.New(t)

	chains, err := NewNets(ids.EmptyNodeID, map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	})
	require.NoError(err)

	primary, _ := chains.GetOrCreate(constants.PrimaryNetworkID)
	cChainID := ids.GenerateTestID()
	dChainID := ids.GenerateTestID()
	primary.AddChain(cChainID)
	primary.AddChain(dChainID)

	bootstrapping := chains.Bootstrapping()

	// The phantom: never the net's own ID.
	require.NotContains(bootstrapping, constants.PrimaryNetworkID)
	require.NotContains(
		bootstrapping,
		ids.Empty,
		"health check reported the primary NET id as an unbootstrapped chain",
	)
	// Both stuck chains must be individually nameable.
	require.ElementsMatch([]ids.ID{cChainID, dChainID}, bootstrapping)

	primary.Bootstrapped(cChainID)
	require.Equal([]ids.ID{dChainID}, chains.Bootstrapping())

	primary.Bootstrapped(dChainID)
	require.Empty(chains.Bootstrapping())
}
