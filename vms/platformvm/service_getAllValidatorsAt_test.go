// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build skip

package platformvm

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	validators "github.com/luxfi/validators"

	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"

	pchainapi "github.com/luxfi/node/vms/platformvm/api"
)

// TestGetAllValidatorsAt tests the GetAllValidatorsAt RPC endpoint
func TestGetAllValidatorsAt(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	genesis := genesistest.New(t, genesistest.Config{})

	args := GetAllValidatorsAtArgs{}
	response := GetAllValidatorsAtReply{}

	service.vm.rt.Lock.Lock()
	lastAccepted := service.vm.manager.LastAccepted()
	lastAcceptedBlk, err := service.vm.manager.GetBlock(lastAccepted)
	require.NoError(err)
	service.vm.rt.Lock.Unlock()

	// Test at genesis height
	args.Height = pchainapi.Height(lastAcceptedBlk.Height())
	require.NoError(service.GetAllValidatorsAt(&http.Request{}, &args, &response))

	// Should have at least the primary network
	require.NotNil(response.ValidatorSets)
	require.Len(primarySet(t, response), len(genesis.Validators))

	// Test with proposed height
	args.Height = pchainapi.Height(pchainapi.ProposedHeight)
	require.NoError(service.GetAllValidatorsAt(context.WithValue(context.Background(), struct{}{}, "test"), &args, &response))
	require.NotNil(primarySet(t, response))

	// Verify each validator set has proper structure
	for _, set := range response.ValidatorSets {
		require.NotNil(set.Validators, "validator set for net %s should not be nil", set.ChainID)
		for _, validator := range set.Validators {
			require.NotZero(validator.Weight, "validator weight should not be zero")
		}
	}
}

// primarySet is the primary network's entry, which the reply carries as one
// element of a list rather than as a map lookup.
func primarySet(t *testing.T, reply GetAllValidatorsAtReply) []*validators.GetValidatorOutput {
	t.Helper()
	for _, set := range reply.ValidatorSets {
		if set.ChainID == constants.PrimaryNetworkID {
			return set.Validators
		}
	}
	require.FailNow(t, "the primary network is missing from the reply")
	return nil
}
