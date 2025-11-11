// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/metric"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/state"
)

func TestServiceGetProposedHeight(t *testing.T) {
	require := require.New(t)

	// Create a proposervm with mocked core VM  using existing test setup
	activationTime := time.Unix(0, 0)
	durangoTime := time.Unix(0, 0)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Set up validator state to return a known height
	currentPChainHeight := uint64(100)
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return currentPChainHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return currentPChainHeight, nil
	}

	// Create the service
	service := &Service{vm: proVM}

	// Create test request
	req := httptest.NewRequest("POST", "/", nil)
	args := &GetProposedHeightArgs{}
	reply := &GetProposedHeightReply{}

	// Call GetProposedHeight
	err := service.GetProposedHeight(req, args, reply)
	require.NoError(err)

	// The proposed height should be >= the current P-Chain height
	require.GreaterOrEqual(reply.ProposedHeight, currentPChainHeight)
}

func TestNewHTTPHandler(t *testing.T) {
	require := require.New(t)

	// Use the existing test setup
	activationTime := time.Unix(0, 0)
	durangoTime := time.Unix(0, 0)
	_, valState, proVM, db := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Initialize state if not already done
	if proVM.State == nil {
		baseState, err := state.NewMetered(db, "test", metric.NewRegistry())
		require.NoError(err)
		proVM.State = baseState
	}
	if proVM.Windower == nil {
		proVM.Windower = proposer.New(valState, proVM.ctx.NetworkID, proVM.ctx.ChainID)
	}

	// Test creating the HTTP handler
	handler, err := NewHTTPHandler(proVM)
	require.NoError(err)
	require.NotNil(handler)
}

func TestCreateHandlersIncludesProposerVM(t *testing.T) {
	require := require.New(t)

	// Use the existing test setup
	activationTime := time.Unix(0, 0)
	durangoTime := time.Unix(0, 0)
	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Get the handlers
	handlers, err := proVM.CreateHandlers(context.Background())
	require.NoError(err)
	require.NotNil(handlers)

	// Check that the proposervm handler is registered
	proposerHandler, ok := handlers["/proposervm"]
	require.True(ok, "proposervm handler should be registered")
	require.NotNil(proposerHandler)
}
