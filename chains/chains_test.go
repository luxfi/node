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

	// Start bootstrapping
	chain.AddChain(chainID)
	bootstrapping := chains.Bootstrapping()
	require.Contains(bootstrapping, netID)

	// Finish bootstrapping
	chain.Bootstrapped(chainID)
	require.Empty(chains.Bootstrapping())
}
