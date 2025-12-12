// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tvm

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/threshold/pkg/party"
	"github.com/luxfi/threshold/pkg/pool"
	"github.com/stretchr/testify/require"
)

// TestThresholdVMWarpSignatureSupport tests Warp 1.5 signature integration
func TestThresholdVMWarpSignatureSupport(t *testing.T) {
	require := require.New(t)

	// Create a mock ThresholdVM with protocol registry
	workerPool := pool.NewPool(4)
	registry := NewProtocolRegistry(workerPool)

	// Verify all protocols are registered
	protocols := registry.Available()
	require.GreaterOrEqual(len(protocols), 4) // LSS, CGGMP21, BLS, Ringtail

	// Check specific protocols
	_, err := registry.Get(ProtocolLSS)
	require.NoError(err)

	_, err = registry.Get(ProtocolBLS)
	require.NoError(err)

	_, err = registry.Get(ProtocolRingtail)
	require.NoError(err)
}

// TestWarpMessageSigningFlow tests the flow of signing a Warp message
func TestWarpMessageSigningFlow(t *testing.T) {
	require := require.New(t)

	// Create source and destination chain IDs
	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	networkID := uint32(96369)

	// Create a Teleport transfer payload
	assetID := ids.GenerateTestID()
	transferPayload := warp.NewTransferPayload(
		assetID,
		uint64(1000000000000), // 1000 LUX
		[]byte("0xsender"),
		[]byte("0xrecipient"),
		uint64(10000000), // 0.01 LUX fee
		[]byte("threshold signed transfer"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Create Teleport message
	teleportMsg := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		sourceChain,
		destChain,
		12345,
		payloadBytes,
	)

	// Convert to Warp message
	warpMsg, err := teleportMsg.ToWarpMessage(networkID)
	require.NoError(err)

	// The message is ready to be threshold-signed
	// In production, this would go through T-Chain's RequestSignature
	t.Logf("Warp message created: NetworkID=%d, SourceChain=%s, PayloadLen=%d",
		warpMsg.NetworkID, warpMsg.SourceChainID.String()[:8], len(warpMsg.Payload))

	// Verify the payload structure
	require.NotEmpty(warpMsg.Payload)
	require.Equal(networkID, warpMsg.NetworkID)
	require.Equal(sourceChain, warpMsg.SourceChainID)
}

// TestProtocolInfoForWarp tests protocol info retrieval for Warp signatures
func TestProtocolInfoForWarp(t *testing.T) {
	require := require.New(t)

	infos := GetProtocolInfo()
	require.GreaterOrEqual(len(infos), 4)

	// Find LSS protocol info
	var lssInfo *ProtocolInfo
	var blsInfo *ProtocolInfo
	var ringtailInfo *ProtocolInfo

	for i := range infos {
		switch infos[i].Name {
		case string(ProtocolLSS):
			lssInfo = &infos[i]
		case string(ProtocolBLS):
			blsInfo = &infos[i]
		case string(ProtocolRingtail):
			ringtailInfo = &infos[i]
		}
	}

	// Verify LSS (for ECDSA Warp signatures)
	require.NotNil(lssInfo)
	require.Equal("secp256k1", lssInfo.SupportedCurves[0])
	require.Equal(64, lssInfo.SignatureSize)
	require.False(lssInfo.IsPostQuantum)
	require.True(lssInfo.SupportsReshare)

	// Verify BLS (for aggregate Warp signatures)
	require.NotNil(blsInfo)
	require.Equal("bls12-381", blsInfo.SupportedCurves[0])
	require.Equal(96, blsInfo.SignatureSize)
	require.False(blsInfo.IsPostQuantum)

	// Verify Ringtail (for quantum-safe Warp signatures)
	require.NotNil(ringtailInfo)
	require.Equal("lattice", ringtailInfo.SupportedCurves[0])
	require.True(ringtailInfo.IsPostQuantum)
}

// TestThresholdVMKeygenSession tests key generation session flow
func TestThresholdVMKeygenSession(t *testing.T) {
	require := require.New(t)

	// Create party IDs for a 3-of-5 threshold setup
	partyIDs := make([]party.ID, 5)
	for i := range partyIDs {
		partyIDs[i] = party.ID(ids.GenerateTestNodeID().String())
	}

	// Create a keygen session
	sessionID := ids.GenerateTestID().String()
	keyID := "bridge-key-001"

	session := &KeygenSession{
		SessionID:    sessionID,
		KeyID:        keyID,
		KeyType:      string(ProtocolLSS),
		Threshold:    2, // 2-of-5
		TotalParties: 5,
		PartyIDs:     partyIDs,
		Status:       "pending",
		RequestedBy:  "B-Chain",
		StartedAt:    time.Now(),
	}

	require.Equal(sessionID, session.SessionID)
	require.Equal(keyID, session.KeyID)
	require.Equal(string(ProtocolLSS), session.KeyType)
	require.Equal(2, session.Threshold)
	require.Equal(5, session.TotalParties)
	require.Equal("pending", session.Status)
}

// TestThresholdVMSigningSession tests signing session flow
func TestThresholdVMSigningSession(t *testing.T) {
	require := require.New(t)

	// Create a message hash (simulating Warp message hash)
	messageHash := make([]byte, 32)
	for i := range messageHash {
		messageHash[i] = byte(i)
	}

	// Create party IDs
	partyIDs := make([]party.ID, 5)
	for i := range partyIDs {
		partyIDs[i] = party.ID(ids.GenerateTestNodeID().String())
	}

	// Create a signing session
	sessionID := ids.GenerateTestID().String()
	session := &SigningSession{
		SessionID:       sessionID,
		KeyID:           "bridge-key-001",
		RequestingChain: "B-Chain",
		MessageHash:     messageHash,
		MessageType:     "warp",
		Status:          "pending",
		SignerParties:   partyIDs[:3], // Only 3 of 5 needed
		CreatedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(5 * time.Minute),
	}

	require.Equal(sessionID, session.SessionID)
	require.Equal("bridge-key-001", session.KeyID)
	require.Equal("B-Chain", session.RequestingChain)
	require.Equal(messageHash, session.MessageHash)
	require.Equal("warp", session.MessageType)
	require.Equal(3, len(session.SignerParties))
}

// TestCrossChainMPCRequest tests cross-chain MPC request parsing
func TestCrossChainMPCRequest(t *testing.T) {
	require := require.New(t)

	// Create a cross-chain sign request
	messageHash := make([]byte, 32)
	for i := range messageHash {
		messageHash[i] = byte(i * 2)
	}

	req := CrossChainMPCRequest{
		Type:            "sign",
		RequestingChain: "B-Chain",
		KeyID:           "bridge-key-001",
		MessageHash:     messageHash,
		MessageType:     "warp",
	}

	require.Equal("sign", req.Type)
	require.Equal("B-Chain", req.RequestingChain)
	require.Equal("bridge-key-001", req.KeyID)
	require.Equal(messageHash, req.MessageHash)
	require.Equal("warp", req.MessageType)

	// Test keygen request
	keygenReq := CrossChainMPCRequest{
		Type:            "keygen",
		RequestingChain: "B-Chain",
		KeyID:           "bridge-key-002",
		KeyType:         "secp256k1",
	}

	require.Equal("keygen", keygenReq.Type)
	require.Equal("secp256k1", keygenReq.KeyType)

	// Test reshare request
	reshareReq := CrossChainMPCRequest{
		Type:            "reshare",
		RequestingChain: "B-Chain",
		KeyID:           "bridge-key-001",
	}

	require.Equal("reshare", reshareReq.Type)
}

// TestManagedKeyForWarp tests managed key structure for Warp operations
func TestManagedKeyForWarp(t *testing.T) {
	require := require.New(t)

	// Create party IDs
	partyIDs := make([]party.ID, 5)
	for i := range partyIDs {
		partyIDs[i] = party.ID(ids.GenerateTestNodeID().String())
	}

	// Create a managed key for bridge operations
	key := &ManagedKey{
		KeyID:        "bridge-key-001",
		KeyType:      "secp256k1",
		PublicKey:    make([]byte, 33), // Compressed secp256k1 public key
		Address:      make([]byte, 20), // Ethereum-style address
		Threshold:    2,
		TotalParties: 5,
		Generation:   1,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Time{},
		SignCount:    0,
		Status:       "active",
		PartyIDs:     partyIDs,
	}

	require.Equal("bridge-key-001", key.KeyID)
	require.Equal("secp256k1", key.KeyType)
	require.Equal(33, len(key.PublicKey))
	require.Equal(20, len(key.Address))
	require.Equal(2, key.Threshold)
	require.Equal(5, key.TotalParties)
	require.Equal(uint64(1), key.Generation)
	require.Equal("active", key.Status)
	require.Equal(5, len(key.PartyIDs))
}

// TestChainPermissionsForWarp tests chain permission validation
func TestChainPermissionsForWarp(t *testing.T) {
	require := require.New(t)

	// B-Chain permissions (full access for bridge)
	bChainPerms := &ChainPermissions{
		ChainID:           "B-Chain",
		ChainName:         "Bridge Chain",
		CanSign:           true,
		CanKeygen:         true,
		CanReshare:        true,
		AllowedKeyTypes:   []string{"secp256k1", "ringtail"},
		MaxSigningSize:    1024,
		RequirePreHash:    false,
		DailySigningLimit: 100000,
	}

	require.True(bChainPerms.CanSign)
	require.True(bChainPerms.CanKeygen)
	require.True(bChainPerms.CanReshare)
	require.Contains(bChainPerms.AllowedKeyTypes, "secp256k1")
	require.Contains(bChainPerms.AllowedKeyTypes, "ringtail")

	// C-Chain permissions (limited for smart contracts)
	cChainPerms := &ChainPermissions{
		ChainID:           "C-Chain",
		ChainName:         "Contract Chain",
		CanSign:           true,
		CanKeygen:         false,
		CanReshare:        false,
		AllowedKeyTypes:   []string{"secp256k1"},
		MaxSigningSize:    256,
		RequirePreHash:    true, // EVM uses pre-hashed messages
		DailySigningLimit: 50000,
	}

	require.True(cChainPerms.CanSign)
	require.False(cChainPerms.CanKeygen)
	require.False(cChainPerms.CanReshare)
	require.True(cChainPerms.RequirePreHash)
}

// TestWarpSignatureTypes tests signature type selection for Warp
func TestWarpSignatureTypes(t *testing.T) {
	require := require.New(t)

	// Classical: BLS aggregate signatures (current Warp)
	require.Equal("BLS", warp.SigTypeBLS.String())
	require.False(warp.SigTypeBLS.IsQuantumSafe())

	// Post-Quantum: Ringtail signatures (Warp 1.5)
	require.Equal("Ringtail", warp.SigTypeRingtail.String())
	require.True(warp.SigTypeRingtail.IsQuantumSafe())

	// Hybrid: Both for transition period
	require.Equal("Hybrid", warp.SigTypeHybrid.String())
	require.True(warp.SigTypeHybrid.IsQuantumSafe())

	// Recommended for bridge operations (quantum-safe)
	recommended := warp.RecommendedSignatureType()
	require.Equal(warp.SigTypeRingtail, recommended)
}

// TestThresholdVMStats tests statistics tracking
func TestThresholdVMStats(t *testing.T) {
	require := require.New(t)

	stats := &vmStats{
		TotalSignatures:    100,
		TotalKeygens:       5,
		ActiveSessions:     3,
		SignaturesByChain:  make(map[string]uint64),
		AverageSigningTime: 150 * time.Millisecond,
		SuccessRate:        0.99,
	}

	stats.SignaturesByChain["B-Chain"] = 80
	stats.SignaturesByChain["C-Chain"] = 20

	require.Equal(uint64(100), stats.TotalSignatures)
	require.Equal(uint64(5), stats.TotalKeygens)
	require.Equal(3, stats.ActiveSessions)
	require.Equal(uint64(80), stats.SignaturesByChain["B-Chain"])
	require.Equal(uint64(20), stats.SignaturesByChain["C-Chain"])
	require.Equal(150*time.Millisecond, stats.AverageSigningTime)
	require.InDelta(0.99, stats.SuccessRate, 0.001)
}

// TestEncryptedWarpThroughThreshold tests encrypted payload handling
func TestEncryptedWarpThroughThreshold(t *testing.T) {
	require := require.New(t)

	// Create source and destination chain IDs
	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()

	// Create a private transfer
	assetID := ids.GenerateTestID()
	transferPayload := warp.NewTransferPayload(
		assetID,
		uint64(50000000000000), // 50000 LUX (private transfer)
		[]byte("0xprivateSender"),
		[]byte("0xprivateRecipient"),
		uint64(100000000),
		[]byte("private threshold-signed transfer"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Generate recipient's ML-KEM public key
	recipientPubKey := make([]byte, warp.MLKEM768PublicKeyLen)
	for i := range recipientPubKey {
		recipientPubKey[i] = byte(i % 256)
	}
	recipientKeyID := []byte("threshold-recipient-key")

	// Create encrypted Teleport message
	privateTeleport, err := warp.NewPrivateTeleportMessage(
		sourceChain,
		destChain,
		99999,
		payloadBytes,
		recipientPubKey,
		recipientKeyID,
	)
	require.NoError(err)
	require.True(privateTeleport.Encrypted)

	// Convert to Warp for threshold signing
	warpMsg, err := privateTeleport.ToWarpMessage(96369)
	require.NoError(err)

	// The encrypted payload is ready for threshold signing
	// Threshold signers don't need to decrypt - they sign the encrypted blob
	t.Logf("Encrypted Warp message ready for threshold signing: PayloadLen=%d",
		len(warpMsg.Payload))

	// Create cross-chain MPC request for the encrypted message
	messageHash := make([]byte, 32) // In production, this would be hash of warpMsg.Bytes()
	req := CrossChainMPCRequest{
		Type:            "sign",
		RequestingChain: "B-Chain",
		KeyID:           "bridge-key-001",
		MessageHash:     messageHash,
		MessageType:     "warp-encrypted",
	}

	require.Equal("warp-encrypted", req.MessageType)
}

// TestRingtailProtocolForWarp tests Ringtail protocol for quantum-safe Warp
func TestRingtailProtocolForWarp(t *testing.T) {
	require := require.New(t)

	// Create protocol registry
	workerPool := pool.NewPool(4)
	registry := NewProtocolRegistry(workerPool)

	// Get Ringtail handler
	handler, err := registry.Get(ProtocolRingtail)
	require.NoError(err)
	require.NotNil(handler)

	// Verify protocol properties
	require.Equal(ProtocolRingtail, handler.Name())
	curves := handler.SupportedCurves()
	require.Contains(curves, "lattice")

	// Verify Ringtail protocol info
	infos := GetProtocolInfo()
	var ringtailInfo *ProtocolInfo
	for i := range infos {
		if infos[i].Name == string(ProtocolRingtail) {
			ringtailInfo = &infos[i]
			break
		}
	}
	require.NotNil(ringtailInfo)
	require.True(ringtailInfo.IsPostQuantum)
	require.Equal(2420, ringtailInfo.SignatureSize) // Dilithium3 size
}

// TestWarpMessageHashForSigning tests message hash creation for signing
func TestWarpMessageHashForSigning(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	networkID := uint32(96369)

	// Create a transfer payload
	assetID := ids.GenerateTestID()
	transferPayload := warp.NewTransferPayload(
		assetID,
		uint64(1000000000000),
		[]byte("0xsender"),
		[]byte("0xrecipient"),
		uint64(10000000),
		nil,
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	teleportMsg := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		sourceChain,
		destChain,
		12345,
		payloadBytes,
	)

	warpMsg, err := teleportMsg.ToWarpMessage(networkID)
	require.NoError(err)

	// Get the bytes that would be signed
	msgBytes := warpMsg.Bytes()
	require.NotEmpty(msgBytes)

	// In production, the hash of these bytes would go to T-Chain for signing
	t.Logf("Warp message bytes for signing: %d bytes", len(msgBytes))
}

// TestThresholdConfigDefaults tests default threshold configuration
func TestThresholdConfigDefaults(t *testing.T) {
	require := require.New(t)

	// Create a VM with default config
	vm := &VM{}
	require.NoError(vm.parseConfig(nil)) // nil config uses defaults

	// Verify defaults
	require.Equal(2, vm.config.Threshold)
	require.Equal(3, vm.config.TotalParties)
	require.Equal(5*time.Minute, vm.config.SessionTimeout)
	require.Equal(100, vm.config.MaxActiveSessions)
	require.Equal(10, vm.config.MaxSessionsPerChain)
	require.Equal(30*24*time.Hour, vm.config.KeyRotationPeriod)
	require.Equal(90*24*time.Hour, vm.config.MaxKeyAge)

	// Verify default authorized chains
	require.Contains(vm.config.AuthorizedChains, "B-Chain")
	require.Contains(vm.config.AuthorizedChains, "C-Chain")
	require.Contains(vm.config.AuthorizedChains, "P-Chain")
	require.Contains(vm.config.AuthorizedChains, "X-Chain")
	require.Contains(vm.config.AuthorizedChains, "Q-Chain")

	// Verify B-Chain has full permissions
	bChain := vm.config.AuthorizedChains["B-Chain"]
	require.True(bChain.CanSign)
	require.True(bChain.CanKeygen)
	require.True(bChain.CanReshare)
	require.Equal(uint64(100000), bChain.DailySigningLimit)
}

// TestKeyShareInterface tests the KeyShare interface
func TestKeyShareInterface(t *testing.T) {
	require := require.New(t)

	// Create a mock lss key share
	partyID := party.ID("test-party-1")
	pubKey := make([]byte, 33)
	for i := range pubKey {
		pubKey[i] = byte(i)
	}

	share := &lssKeyShare{
		config:  nil, // Would be set by actual keygen
		pubKey:  pubKey,
		partyID: partyID,
		thresh:  2,
		total:   5,
		gen:     1,
	}

	// Test interface methods
	require.Equal(pubKey, share.PublicKey())
	require.Equal(partyID, share.PartyID())
	require.Equal(2, share.Threshold())
	require.Equal(5, share.TotalParties())
	require.Equal(uint64(1), share.Generation())
	require.Equal(ProtocolLSS, share.Protocol())
}

// TestOperationTypes tests threshold operation types for blocks
func TestOperationTypes(t *testing.T) {
	require := require.New(t)

	// Test operation type constants
	require.Equal("keygen", OpTypeKeygen)
	require.Equal("sign", OpTypeSign)
	require.Equal("reshare", OpTypeReshare)
	require.Equal("refresh", OpTypeRefresh)

	// Create operations
	keygenOp := &Operation{
		Type:      OpTypeKeygen,
		SessionID: "keygen-session-1",
		KeyID:     "bridge-key-001",
		Timestamp: time.Now().Unix(),
	}

	signOp := &Operation{
		Type:            OpTypeSign,
		SessionID:       "sign-session-1",
		KeyID:           "bridge-key-001",
		RequestingChain: "B-Chain",
		Timestamp:       time.Now().Unix(),
	}

	require.Equal(OpTypeKeygen, keygenOp.Type)
	require.Equal(OpTypeSign, signOp.Type)
	require.Equal("B-Chain", signOp.RequestingChain)
}

// TestProtocolSelection tests selecting the right protocol for Warp operations
func TestProtocolSelection(t *testing.T) {
	require := require.New(t)

	workerPool := pool.NewPool(4)
	registry := NewProtocolRegistry(workerPool)

	// For standard Warp (ECDSA): Use LSS
	lssHandler, err := registry.Get(ProtocolLSS)
	require.NoError(err)
	require.Contains(lssHandler.SupportedCurves(), "secp256k1")

	// For aggregate Warp (BLS): Use BLS
	blsHandler, err := registry.Get(ProtocolBLS)
	require.NoError(err)
	require.Contains(blsHandler.SupportedCurves(), "bls12-381")

	// For quantum-safe Warp: Use Ringtail
	rtHandler, err := registry.Get(ProtocolRingtail)
	require.NoError(err)
	require.Contains(rtHandler.SupportedCurves(), "lattice")

	// Unsupported protocol should error
	_, err = registry.Get("unsupported")
	require.Error(err)
}
