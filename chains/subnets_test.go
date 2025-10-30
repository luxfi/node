// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/constants"
)

func TestNewSubnets(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(err)

	subnet, ok := subnets.GetOrCreate(constants.PrimaryNetworkID)
	require.False(ok)
	require.Equal(config[constants.PrimaryNetworkID], subnet.Config())
}

func TestNewSubnetsNoPrimaryNetworkConfig(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]subnets.Config{}

	_, err := NewSubnets(ids.EmptyNodeID, config)
	require.ErrorIs(err, ErrNoPrimaryNetworkConfig)
}

func TestSubnetsGetOrCreate(t *testing.T) {
	testNetID := ids.GenerateTestID()

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
					netID: testNetID,
					want:  true,
				},
				{
					netID: testNetID,
				},
			},
		},
		{
			name: "adding unique subnets succeeds",
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
			config := map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
			}
			subnets, err := NewSubnets(ids.EmptyNodeID, config)
			require.NoError(err)

			for _, arg := range tt.args {
				_, got := subnets.GetOrCreate(arg.netID)
				require.Equal(arg.want, got)
			}
		})
	}
}

func TestSubnetConfigs(t *testing.T) {
	testNetID := ids.GenerateTestID()

	tests := []struct {
		name   string
		config map[ids.ID]subnets.Config
		netID  ids.ID
		want   subnets.Config
	}{
		{
			name: "default to primary network config",
			config: map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
			},
			netID: testNetID,
			want:  subnets.Config{},
		},
		{
			name: "use net config",
			config: map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
				testNetID: {
					ValidatorOnly: true,
				},
			},
			netID: testNetID,
			want: subnets.Config{
				ValidatorOnly: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			subnets, err := NewSubnets(ids.EmptyNodeID, tt.config)
			require.NoError(err)

			subnet, ok := subnets.GetOrCreate(tt.netID)
			require.True(ok)

			require.Equal(tt.want, subnet.Config())
		})
	}
}

func TestSubnetsBootstrapping(t *testing.T) {
	require := require.New(t)

	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(err)

	netID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()

	subnet, ok := subnets.GetOrCreate(netID)
	require.True(ok)

	// Start bootstrapping
	subnet.AddChain(chainID)
	bootstrapping := subnets.Bootstrapping()
	require.Contains(bootstrapping, netID)

	// Finish bootstrapping
	subnet.Bootstrapped(chainID)
	require.Empty(subnets.Bootstrapping())
}
