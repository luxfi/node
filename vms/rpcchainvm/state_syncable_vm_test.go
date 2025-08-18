// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blocktest"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms/rpcchainvm/grpcutils"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
	"github.com/luxfi/node/vms/rpcchainvm/runtime/subprocess"
)

var (
	_ block.ChainVM         = &StateSyncEnabledMock{}
	_ block.StateSyncableVM = &StateSyncEnabledMock{}

	preSummaryHeight = uint64(1789)
	SummaryHeight    = uint64(2022)

	// a summary to be returned in some UTs
	mockedSummary = &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: SummaryHeight,
		BytesV:  []byte("summary"),
	}

	// last accepted blocks data before and after summary is accepted
	preSummaryBlk = &blocktest.Block{
		Decidable: consensustest.Decidable{
			IDV:     ids.ID{'f', 'i', 'r', 's', 't', 'B', 'l', 'K'},
			StatusV: choices.Accepted,
		},
		HeightV: preSummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	summaryBlk = &blocktest.Block{
		Decidable: consensustest.Decidable{
			IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'B', 'l', 'K'},
			StatusV: choices.Accepted,
		},
		HeightV: SummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	// a fictitious error unrelated to state sync
	errBrokenConnectionOrSomething = errors.New("brokenConnectionOrSomething")
	errNothingToParse              = errors.New("nil summary bytes. Nothing to parse")
)

type StateSyncEnabledMock struct {
	blockmock.ChainVM
	blockmock.StateSyncableVM
}

func stateSyncEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "stateSyncEnabledTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		callCount := 0
		ssVM.StateSyncEnabledF = func(context.Context) (bool, error) {
			callCount++
			switch callCount {
			case 1:
				return false, block.ErrStateSyncableVMNotImplemented
			case 2:
				return false, nil
			case 3:
				return true, nil
			case 4:
				return false, errBrokenConnectionOrSomething
			}
			return false, nil
		}
	}

	return ssVM
}

func getOngoingSyncStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getOngoingSyncStateSummaryTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		callCount := 0
		ssVM.StateSyncGetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
			callCount++
			switch callCount {
			case 1:
				return nil, block.ErrStateSyncableVMNotImplemented
			case 2:
				return mockedSummary, nil
			case 3:
				return nil, errBrokenConnectionOrSomething
			}
			return nil, nil
		}
	}

	return ssVM
}

func getLastStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getLastStateSummaryTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		callCount := 0
		ssVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
			callCount++
			switch callCount {
			case 1:
				return nil, block.ErrStateSyncableVMNotImplemented
			case 2:
				return mockedSummary, nil
			case 3:
				return nil, errBrokenConnectionOrSomething
			}
			return nil, nil
		}
	}

	return ssVM
}

func parseStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "parseStateSummaryTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		callCount := 0
		ssVM.ParseStateSummaryF = func(context.Context, []byte) (block.StateSummary, error) {
			callCount++
			switch callCount {
			case 1:
				return nil, block.ErrStateSyncableVMNotImplemented
			case 2:
				return mockedSummary, nil
			case 3:
				return nil, errNothingToParse
			case 4:
				return nil, errBrokenConnectionOrSomething
			}
			return nil, nil
		}
	}

	return ssVM
}

func getStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getStateSummaryTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		callCount := 0
		ssVM.GetStateSummaryF = func(context.Context, uint64) (block.StateSummary, error) {
			callCount++
			switch callCount {
			case 1:
				return nil, block.ErrStateSyncableVMNotImplemented
			case 2:
				return mockedSummary, nil
			case 3:
				return nil, errBrokenConnectionOrSomething
			}
			return nil, nil
		}
	}

	return ssVM
}

func acceptStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "acceptStateSummaryTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		ssVM.GetStateSummaryF = func(context.Context, uint64) (block.StateSummary, error) {
			return mockedSummary, nil
		}
		
		parseCallCount := 0
		ssVM.ParseStateSummaryF = func(context.Context, []byte) (block.StateSummary, error) {
			parseCallCount++
			switch parseCallCount {
			case 1:
				// setup summary to be accepted before returning it
				mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
					return block.StateSyncStatic, nil
				}
				return mockedSummary, nil
			case 2:
				// setup summary to be skipped before returning it
				mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
					return block.StateSyncSkipped, nil
				}
				return mockedSummary, nil
			case 3:
				// setup summary to fail accept
				mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
					return block.StateSyncSkipped, errBrokenConnectionOrSomething
				}
				return mockedSummary, nil
			}
			return nil, nil
		}
	}

	return ssVM
}

func lastAcceptedBlockPostStateSummaryAcceptTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "lastAcceptedBlockPostStateSummaryAcceptTestKey"

	// create mock
	ssVM := &StateSyncEnabledMock{}

	if loadExpectations {
		ssVM.InitializeF = func(
			context.Context, interface{}, interface{}, interface{},
			interface{}, interface{}, interface{}, interface{},
		) error {
			return nil
		}
		
		lastAcceptedCallCount := 0
		ssVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
			lastAcceptedCallCount++
			if lastAcceptedCallCount <= 1 {
				return preSummaryBlk.ID(), nil
			}
			return summaryBlk.ID(), nil
		}
		
		ssVM.GetBlockF = func(context.Context, ids.ID) (block.Block, error) {
			if lastAcceptedCallCount <= 1 {
				return preSummaryBlk, nil
			}
			return summaryBlk, nil
		}
		
		ssVM.ParseStateSummaryF = func(context.Context, []byte) (block.StateSummary, error) {
			// setup summary to be accepted before returning it
			mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
				return block.StateSyncStatic, nil
			}
			return mockedSummary, nil
		}
		
		ssVM.SetStateF = func(context.Context, uint8) error {
			return nil
		}
	}

	return ssVM
}

func buildClientHelper(require *require.Assertions, testKey string) *VMClient {
	process := helperProcess(testKey)

	log := logging.NewLogger(
		testKey,
		logging.NewWrappedCore(
			logging.Info,
			originalStderr,
			logging.Colors.ConsoleEncoder(),
		),
	)

	listener, err := grpcutils.NewListener()
	require.NoError(err)

	status, stopper, err := subprocess.Bootstrap(
		context.Background(),
		listener,
		process,
		&subprocess.Config{
			Stderr:           originalStderr,
			Stdout:           io.Discard,
			Log:              logging.NoLog{},
			HandshakeTimeout: runtime.DefaultHandshakeTimeout,
		},
	)
	require.NoError(err)

	clientConn, err := grpcutils.Dial(status.Addr)
	require.NoError(err)

	return NewClient(clientConn, stopper, status.Pid, nil, metric.NewPrefixGatherer())
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)
	testKey := stateSyncEnabledTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// test state sync not implemented
	// Note that enabled == false is returned rather than
	// common.ErrStateSyncableVMNotImplemented
	enabled, err := vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// test state sync disabled
	enabled, err = vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// test state sync enabled
	enabled, err = vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	// test a non-special error.
	_, err = vm.StateSyncEnabled(context.Background())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getOngoingSyncStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// test unimplemented case; this is just a guard
	_, err := vm.GetOngoingSyncStateSummary(context.Background())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	_, err = vm.GetOngoingSyncStateSummary(context.Background())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetLastStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getLastStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// test unimplemented case; this is just a guard
	_, err := vm.GetLastStateSummary(context.Background())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	_, err = vm.GetLastStateSummary(context.Background())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := parseStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// test unimplemented case; this is just a guard
	_, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful parsing
	summary, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test parsing nil summary
	_, err = vm.ParseStateSummary(context.Background(), nil)
	require.Error(err) //nolint:forbidigo // currently returns grpc errors

	// test a non-special error.
	_, err = vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// test unimplemented case; this is just a guard
	_, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	_, err = vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestAcceptStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := acceptStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// retrieve the summary first
	summary, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.NoError(err)

	// test status Summary
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

	// test skipped Summary
	status, err = summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)

	// test a non-special error.
	_, err = summary.Accept(context.Background())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

// Show that LastAccepted call returns the right answer after a StateSummary
// is accepted AND engine state moves to bootstrapping
func TestLastAcceptedBlockPostStateSummaryAccept(t *testing.T) {
	require := require.New(t)
	testKey := lastAcceptedBlockPostStateSummaryAcceptTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	// Step 1: initialize VM and check initial LastAcceptedBlock
	ctx := consensustest.Context(t, consensustest.CChainID)

	require.NoError(vm.Initialize(context.Background(), ctx, prefixdb.New([]byte{}, memdb.New()), nil, nil, nil, nil, nil))

	blkID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(preSummaryBlk.ID(), blkID)

	lastBlk, err := vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(preSummaryBlk.Height(), lastBlk.Height())

	// Step 2: pick a state summary to an higher height and accept it
	summary, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.NoError(err)

	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

	// State Sync accept does not duly update LastAccepted block information
	// since state sync can complete asynchronously
	blkID, err = vm.LastAccepted(context.Background())
	require.NoError(err)

	lastBlk, err = vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(preSummaryBlk.Height(), lastBlk.Height())

	// Setting state to bootstrapping duly update last accepted block
	require.NoError(vm.SetState(context.Background(), consensus.Bootstrapping))

	blkID, err = vm.LastAccepted(context.Background())
	require.NoError(err)

	lastBlk, err = vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(summary.Height(), lastBlk.Height())
}
