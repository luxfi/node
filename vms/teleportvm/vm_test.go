// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	_ "github.com/luxfi/crypto/threshold/bls" // Register BLS threshold scheme
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
)

const (
	MinTeleportBond = 1_000_000 * 1e9 // 1M LUX
)

// =========================================================================
// Factory / Init
// =========================================================================

func TestVMID(t *testing.T) {
	require := require.New(t)
	require.NotEqual(ids.Empty, VMID)
	require.Equal(ids.ID{'t', 'e', 'l', 'e', 'p', 'o', 'r', 't', 'v', 'm'}, VMID)
}

func TestFactoryNew(t *testing.T) {
	require := require.New(t)
	factory := &Factory{}
	vm, err := factory.New(log.NewNoOpLogger())
	require.NoError(err)
	require.NotNil(vm)
	require.IsType(&VM{}, vm)
}

func TestVMInitialize(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	require.True(vm.running)

	// Verify version
	ver, err := vm.Version(context.Background())
	require.NoError(err)
	require.Equal("v1.0.0", ver)

	// Verify health
	health, err := vm.HealthCheck(context.Background())
	require.NoError(err)
	require.True(health.Healthy)
}

// =========================================================================
// Bridge / Signer Set (from bridgevm tests)
// =========================================================================

func TestSignerSetOptInRegistration(t *testing.T) {
	require := require.New(t)

	vm := &VM{
		config: Config{
			MaxSigners:     100,
			ThresholdRatio: 0.67,
		},
		signerSet: &SignerSet{
			Signers:      make([]*SignerInfo, 0, 100),
			Waitlist:     make([]ids.NodeID, 0),
			CurrentEpoch: 0,
			SetFrozen:    false,
			ThresholdT:   0,
		},
	}

	for i := 0; i < 10; i++ {
		nodeID := ids.GenerateTestNodeID()
		input := &RegisterValidatorInput{NodeID: nodeID.String()}
		result, err := vm.RegisterValidator(input)
		require.NoError(err)
		require.True(result.Success)
		require.True(result.Registered)
		require.False(result.Waitlisted)
		require.False(result.ReshareNeeded)
		require.Equal(uint64(0), result.CurrentEpoch)
		require.Equal(i, result.SignerIndex)
	}

	require.Equal(10, len(vm.signerSet.Signers))
	require.Equal(uint64(0), vm.signerSet.CurrentEpoch)
	require.Equal(6, vm.signerSet.ThresholdT) // floor(10 * 0.67) = 6
}

func TestSignerSetFreezeAtMax(t *testing.T) {
	require := require.New(t)

	vm := &VM{
		config: Config{
			MaxSigners:     5,
			ThresholdRatio: 0.67,
		},
		signerSet: &SignerSet{
			Signers:      make([]*SignerInfo, 0, 5),
			Waitlist:     make([]ids.NodeID, 0),
			CurrentEpoch: 0,
		},
	}

	for i := 0; i < 5; i++ {
		nodeID := ids.GenerateTestNodeID()
		result, err := vm.RegisterValidator(&RegisterValidatorInput{NodeID: nodeID.String()})
		require.NoError(err)
		require.True(result.Registered)
	}

	require.True(vm.signerSet.SetFrozen)

	// Next goes to waitlist
	nodeID := ids.GenerateTestNodeID()
	result, err := vm.RegisterValidator(&RegisterValidatorInput{NodeID: nodeID.String()})
	require.NoError(err)
	require.True(result.Waitlisted)
	require.False(result.Registered)
}

func TestRemoveSignerTriggersReshare(t *testing.T) {
	require := require.New(t)

	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	vm := &VM{
		config: Config{MaxSigners: 100, ThresholdRatio: 0.67},
		signerSet: &SignerSet{
			Signers: []*SignerInfo{
				{NodeID: nodeID1, SlotIndex: 0, Active: true},
				{NodeID: nodeID2, SlotIndex: 1, Active: true},
				{NodeID: nodeID3, SlotIndex: 2, Active: true},
			},
			Waitlist:     make([]ids.NodeID, 0),
			CurrentEpoch: 0,
			ThresholdT:   2,
		},
		log: log.NewNoOpLogger(),
	}

	result, err := vm.RemoveSigner(nodeID2, nil)
	require.NoError(err)
	require.True(result.Success)
	require.Equal(uint64(1), result.NewEpoch)
	require.Equal(2, result.ActiveSigners)
}

func TestSlashSignerPartial(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	initialBond := uint64(150_000_000 * 1e9)

	vm := &VM{
		config: Config{MaxSigners: 100, ThresholdRatio: 0.67},
		signerSet: &SignerSet{
			Signers: []*SignerInfo{
				{NodeID: nodeID, SlotIndex: 0, Active: true, BondAmount: initialBond},
			},
			ThresholdT: 1,
		},
	}

	result, err := vm.SlashSigner(&SlashSignerInput{
		NodeID: nodeID, Reason: "failed to sign", SlashPercent: 10, Evidence: []byte("proof"),
	})
	require.NoError(err)
	require.True(result.Success)
	require.Equal(initialBond/10, result.SlashedAmount)
	require.False(result.RemovedFromSet)
}

func TestSlashSignerRemoval(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	initialBond := uint64(110_000_000 * 1e9)

	vm := &VM{
		config: Config{MaxSigners: 100, ThresholdRatio: 0.67},
		signerSet: &SignerSet{
			Signers: []*SignerInfo{
				{NodeID: nodeID, SlotIndex: 0, Active: true, BondAmount: initialBond},
			},
			ThresholdT: 1,
		},
	}

	result, err := vm.SlashSigner(&SlashSignerInput{
		NodeID: nodeID, Reason: "double signing", SlashPercent: 20, Evidence: []byte("proof"),
	})
	require.NoError(err)
	require.True(result.RemovedFromSet)
	require.Equal(uint64(1), vm.signerSet.CurrentEpoch)
	require.Empty(vm.signerSet.Signers)
}

// =========================================================================
// Relay (from relayvm tests)
// =========================================================================

func TestOpenChannelAndSendMessage(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()

	channel, err := vm.OpenChannel(sourceChain, destChain, "ordered", "1.0")
	require.NoError(err)
	require.NotNil(channel)
	require.Equal("open", channel.State)

	// Retrieve channel
	retrieved, err := vm.GetChannel(channel.ID)
	require.NoError(err)
	require.Equal(channel.ID, retrieved.ID)

	// Send message
	msg, err := vm.SendMessage(channel.ID, []byte(`{"test": true}`), []byte("sender"), []byte("receiver"), time.Now().Add(time.Hour).Unix())
	require.NoError(err)
	require.NotEqual(ids.Empty, msg.ID)
	require.Equal(uint64(0), msg.Sequence)
	require.Equal(MessagePending, msg.State)

	// Receive message
	receipt, err := vm.ReceiveMessage(msg.ID, []byte("proof"), 100)
	require.NoError(err)
	require.True(receipt.Success)

	retrieved2, err := vm.GetMessage(msg.ID)
	require.NoError(err)
	require.Equal(MessageVerified, retrieved2.State)

	// Close channel
	err = vm.CloseChannel(channel.ID)
	require.NoError(err)

	closed, err := vm.GetChannel(channel.ID)
	require.NoError(err)
	require.Equal("closed", closed.State)
}

func TestCreateVerifiedMessage(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()

	channel, err := vm.OpenChannel(sourceChain, destChain, "ordered", "1.0")
	require.NoError(err)

	msg, err := vm.SendMessage(channel.ID, []byte(`{"test": true}`), []byte("s"), []byte("r"), time.Now().Add(time.Hour).Unix())
	require.NoError(err)

	_, err = vm.ReceiveMessage(msg.ID, []byte("proof"), 100)
	require.NoError(err)

	retrieved, _ := vm.GetMessage(msg.ID)
	verifiedMsg, err := vm.CreateVerifiedMessage(retrieved)
	require.NoError(err)
	require.NotNil(verifiedMsg)
	require.Equal(sourceChain, verifiedMsg.SrcDomain)
	require.Equal(destChain, verifiedMsg.DstDomain)
}

// =========================================================================
// Oracle (from oraclevm tests)
// =========================================================================

func TestRegisterFeedAndSubmitObservation(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	operatorID := ids.GenerateTestNodeID()
	feed := &Feed{
		ID:        ids.GenerateTestID(),
		Name:      "test-feed",
		Operators: []ids.NodeID{operatorID},
	}

	err := vm.RegisterFeed(feed)
	require.NoError(err)

	retrieved, err := vm.GetFeed(feed.ID)
	require.NoError(err)
	require.Equal("active", retrieved.Status)

	// Duplicate fails
	err = vm.RegisterFeed(feed)
	require.Error(err)

	// Submit observation
	obs := &Observation{
		FeedID:     feed.ID,
		Value:      []byte(`{"price": 100.50}`),
		Timestamp:  time.Now(),
		OperatorID: operatorID,
		Signature:  []byte("test-sig"),
	}
	err = vm.SubmitObservation(obs)
	require.NoError(err)
	require.Len(vm.pendingObs[feed.ID], 1)
}

// =========================================================================
// Block Management
// =========================================================================

func TestBuildAndAcceptBlock(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	blk, err := vm.BuildBlock(context.Background())
	require.NoError(err)
	require.NotNil(blk)
	require.Equal(uint64(1), blk.Height())

	lastAccepted, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(lastAccepted, blk.Parent())

	// Accept
	err = blk.Accept(context.Background())
	require.NoError(err)

	newLastAccepted, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(blk.ID(), newLastAccepted)
}

func TestCreateHandlers(t *testing.T) {
	require := require.New(t)
	vm := setupTestVM(t)
	defer vm.Shutdown(context.Background())

	handlers, err := vm.CreateHandlers(context.Background())
	require.NoError(err)
	require.Contains(handlers, "/rpc")
}

// =========================================================================
// Helpers
// =========================================================================

func setupTestVM(t *testing.T) *VM {
	t.Helper()

	vm := &VM{
		pendingBridges: make(map[ids.ID]*BridgeRequest),
		chainClients:   make(map[string]ChainClient),
		channels:       make(map[ids.ID]*Channel),
		messages:       make(map[ids.ID]*Message),
		pendingMsgs:    make(map[ids.ID][]*Message),
		sequences:      make(map[ids.ID]uint64),
		feeds:          make(map[ids.ID]*Feed),
		feedsByName:    make(map[string]ids.ID),
		pendingObs:     make(map[ids.ID][]*Observation),
		values:         make(map[ids.ID]map[uint64]*AggregatedValue),
		pendingBlocks:  make(map[ids.ID]*Block),
	}

	genesis := &Genesis{
		Timestamp: time.Now().Unix(),
		Message:   "teleportvm test genesis",
	}
	genesisBytes, _ := json.Marshal(genesis)

	config := DefaultConfig()
	configBytes, _ := json.Marshal(config)

	toEngine := make(chan vmcore.Message, 10)

	init := vmcore.Init{
		Runtime: &runtime.Runtime{
			ChainID: ids.GenerateTestID(),
			Log:     log.NewNoOpLogger(),
		},
		DB:       memdb.New(),
		Genesis:  genesisBytes,
		Config:   configBytes,
		ToEngine: toEngine,
	}

	err := vm.Initialize(context.Background(), init)
	require.NoError(t, err)

	return vm
}
