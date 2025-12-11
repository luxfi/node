// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bvm

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
)

// TestTeleportBridgeIntegration tests the full Teleport flow with BridgeVM
func TestTeleportBridgeIntegration(t *testing.T) {
	require := require.New(t)

	// Create source and destination chain IDs
	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()

	// Create a transfer payload
	transferPayload := warp.NewTransferPayload(
		assetID,
		uint64(1000000000000), // 1000 LUX
		[]byte("0x1234567890abcdef1234567890abcdef12345678"),
		[]byte("0xfedcba0987654321fedcba0987654321fedcba09"),
		uint64(100000000), // 0.1 LUX fee
		[]byte("bridge transfer via teleport"),
	)

	// Serialize the payload
	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Create Teleport message
	teleportMsg := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		sourceChain,
		destChain,
		12345, // nonce
		payloadBytes,
	)

	require.Equal(warp.TeleportVersion, teleportMsg.Version)
	require.Equal(warp.TeleportTransfer, teleportMsg.MessageType)
	require.Equal(sourceChain, teleportMsg.SourceChainID)
	require.Equal(destChain, teleportMsg.DestChainID)
	require.False(teleportMsg.Encrypted)

	// Validate the message
	err = teleportMsg.Validate()
	require.NoError(err)

	// Convert to Warp message
	networkID := uint32(96369) // Lux mainnet
	warpMsg, err := teleportMsg.ToWarpMessage(networkID)
	require.NoError(err)
	require.Equal(networkID, warpMsg.NetworkID)
	require.Equal(sourceChain, warpMsg.SourceChainID)
}

// TestTeleportBridgeRequestConversion tests converting Teleport messages to BridgeRequests
func TestTeleportBridgeRequestConversion(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()
	sourceTxID := ids.GenerateTestID()

	// Create transfer payload
	sender := []byte("0x1234567890abcdef")
	recipient := []byte("0xfedcba0987654321")
	amount := uint64(5000000000000) // 5000 LUX
	fee := uint64(50000000)         // 0.05 LUX

	transferPayload := warp.NewTransferPayload(
		assetID,
		amount,
		sender,
		recipient,
		fee,
		[]byte("cross-chain bridge"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Create Teleport message
	nonce := uint64(999)
	teleportMsg := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		sourceChain,
		destChain,
		nonce,
		payloadBytes,
	)

	// Convert Teleport to BridgeRequest
	bridgeRequest := teleportToBridgeRequest(teleportMsg, sourceTxID)
	require.NotNil(bridgeRequest)

	// Verify BridgeRequest fields
	require.Equal(sourceChain.String(), bridgeRequest.SourceChain)
	require.Equal(destChain.String(), bridgeRequest.DestChain)
	require.Equal(sourceTxID, bridgeRequest.SourceTxID)
	require.Equal("pending", bridgeRequest.Status)
}

// teleportToBridgeRequest converts a Teleport message to a BridgeRequest
// This is the integration point between Teleport protocol and BridgeVM
func teleportToBridgeRequest(msg *warp.TeleportMessage, sourceTxID ids.ID) *BridgeRequest {
	if msg.MessageType != warp.TeleportTransfer {
		return nil
	}

	// Parse the transfer payload
	payload, err := warp.ParseTransferPayload(msg.Payload)
	if err != nil {
		return nil
	}

	return &BridgeRequest{
		ID:            ids.GenerateTestID(),
		SourceChain:   msg.SourceChainID.String(),
		DestChain:     msg.DestChainID.String(),
		Asset:         payload.AssetID,
		Amount:        payload.Amount,
		Recipient:     payload.Recipient,
		SourceTxID:    sourceTxID,
		Confirmations: 0,
		Status:        "pending",
		MPCSignatures: [][]byte{},
		CreatedAt:     time.Now(),
	}
}

// TestPrivateTeleportBridgeTransfer tests encrypted bridge transfers
func TestPrivateTeleportBridgeTransfer(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()

	// Create transfer payload
	amount := uint64(10000000000000) // 10000 LUX (private transfer)
	sender := []byte("0xsecretSender12345678")
	recipient := []byte("0xsecretRecipient87654321")

	transferPayload := warp.NewTransferPayload(
		assetID,
		amount,
		sender,
		recipient,
		uint64(100000000), // fee
		[]byte("private bridge transfer"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Generate recipient's ML-KEM public key (placeholder)
	recipientPubKey := make([]byte, warp.MLKEM768PublicKeyLen)
	for i := range recipientPubKey {
		recipientPubKey[i] = byte(i % 256)
	}
	recipientKeyID := []byte("bridge-recipient-key-001")

	// Create encrypted Teleport message
	privateTeleport, err := warp.NewPrivateTeleportMessage(
		sourceChain,
		destChain,
		42, // nonce
		payloadBytes,
		recipientPubKey,
		recipientKeyID,
	)
	require.NoError(err)
	require.NotNil(privateTeleport)

	// Verify it's encrypted
	require.True(privateTeleport.Encrypted)
	require.Equal(warp.TeleportPrivate, privateTeleport.MessageType)
	require.NotEqual(payloadBytes, privateTeleport.Payload) // Should be different (encrypted)

	// Validate the message
	err = privateTeleport.Validate()
	require.NoError(err)

	// Simulate recipient decryption
	// In real scenario, recipient would use their ML-KEM private key
	recipientPrivKey := make([]byte, 2400) // ML-KEM-768 private key size
	for i := range recipientPrivKey {
		recipientPrivKey[i] = byte(i % 256)
	}

	decryptedPayload, err := privateTeleport.DecryptPayload(recipientPrivKey)
	require.NoError(err)
	require.Equal(payloadBytes, decryptedPayload)

	// Parse decrypted payload
	parsedPayload, err := warp.ParseTransferPayload(decryptedPayload)
	require.NoError(err)
	require.Equal(assetID, parsedPayload.AssetID)
	require.Equal(amount, parsedPayload.Amount)
	require.Equal(sender, parsedPayload.Sender)
	require.Equal(recipient, parsedPayload.Recipient)
}

// TestTeleportLockUnlockFlow tests the lock/unlock bridge mechanism
func TestTeleportLockUnlockFlow(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()

	// Step 1: Create a LOCK message on source chain
	lockPayload := warp.NewTransferPayload(
		assetID,
		uint64(2000000000000), // 2000 LUX to lock
		[]byte("0xsourceSender"),
		[]byte("0xdestRecipient"),
		uint64(10000000),
		[]byte("lock for bridge"),
	)

	lockBytes, err := lockPayload.Bytes()
	require.NoError(err)

	lockMsg := warp.NewTeleportMessage(
		warp.TeleportLock,
		sourceChain,
		destChain,
		1, // nonce
		lockBytes,
	)

	require.Equal(warp.TeleportLock, lockMsg.MessageType)
	err = lockMsg.Validate()
	require.NoError(err)

	// Step 2: Convert to Warp message for signing
	warpLockMsg, err := lockMsg.ToWarpMessage(96369)
	require.NoError(err)
	require.NotNil(warpLockMsg)

	// Step 3: Create matching UNLOCK message on destination chain
	// (In production, this would be created after verifying the lock)
	unlockMsg := warp.NewTeleportMessage(
		warp.TeleportUnlock,
		destChain,   // Now originating from dest
		sourceChain, // For reference
		1,           // Same nonce for correlation
		lockBytes,   // Same payload
	)

	require.Equal(warp.TeleportUnlock, unlockMsg.MessageType)
	err = unlockMsg.Validate()
	require.NoError(err)

	// Verify the lock and unlock are properly correlated
	require.Equal(lockMsg.Nonce, unlockMsg.Nonce)
}

// TestTeleportGovernanceMessage tests cross-chain governance
func TestTeleportGovernanceMessage(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID() // P-Chain
	destChain := ids.GenerateTestID()   // Target subnet

	// Create governance payload (e.g., validator set update)
	governancePayload := []byte(`{
		"action": "update_validator_set",
		"epoch": 42,
		"validators": ["NodeID-1", "NodeID-2", "NodeID-3"],
		"weights": [1000, 1000, 1000]
	}`)

	govMsg := warp.NewTeleportMessage(
		warp.TeleportGovernance,
		sourceChain,
		destChain,
		100, // governance nonce
		governancePayload,
	)

	require.Equal(warp.TeleportGovernance, govMsg.MessageType)
	err := govMsg.Validate()
	require.NoError(err)

	// Convert to Warp for cross-chain delivery
	warpGovMsg, err := govMsg.ToWarpMessage(96369)
	require.NoError(err)
	require.NotNil(warpGovMsg)
}

// TestTeleportAttestationMessage tests oracle/attestation messages
func TestTeleportAttestationMessage(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.GenerateTestID() // A-Chain (attestation)
	destChain := ids.GenerateTestID()   // Target chain needing price feed

	// Create attestation payload
	attestPayload := &warp.TeleportAttestPayload{
		AttestationType: 1, // Price feed
		Timestamp:       uint64(time.Now().Unix()),
		Data:            []byte(`{"asset":"LUX/USD","price":"25.50","source":"oracle-1"}`),
		AttesterID:      ids.GenerateTestNodeID(),
	}

	// Serialize attestation
	attestBytes, err := warp.Codec.Marshal(warp.CodecVersion, attestPayload)
	require.NoError(err)

	attestMsg := warp.NewTeleportMessage(
		warp.TeleportAttest,
		sourceChain,
		destChain,
		uint64(time.Now().UnixNano()), //nolint:gosec // Use timestamp as nonce for attestations
		attestBytes,
	)

	require.Equal(warp.TeleportAttest, attestMsg.MessageType)
	err = attestMsg.Validate()
	require.NoError(err)

	// Convert to Warp
	warpAttestMsg, err := attestMsg.ToWarpMessage(96369)
	require.NoError(err)
	require.NotNil(warpAttestMsg)

	// Verify we can deserialize the attestation
	parsedAttest := &warp.TeleportAttestPayload{}
	_, err = warp.Codec.Unmarshal(attestMsg.Payload, parsedAttest)
	require.NoError(err)
	require.Equal(attestPayload.AttestationType, parsedAttest.AttestationType)
	require.Equal(attestPayload.Timestamp, parsedAttest.Timestamp)
}

// TestTeleportSwapMessage tests atomic cross-chain swaps
func TestTeleportSwapMessage(t *testing.T) {
	require := require.New(t)

	chainA := ids.GenerateTestID()
	chainB := ids.GenerateTestID()
	assetA := ids.GenerateTestID()
	assetB := ids.GenerateTestID()

	// Swap payload: swap assetA on chainA for assetB on chainB
	swapPayload := struct {
		OfferAsset    ids.ID `serialize:"true"`
		OfferAmount   uint64 `serialize:"true"`
		WantAsset     ids.ID `serialize:"true"`
		WantAmount    uint64 `serialize:"true"`
		Maker         []byte `serialize:"true"`
		Taker         []byte `serialize:"true"`
		ExpiryTime    int64  `serialize:"true"`
		HashLock      []byte `serialize:"true"`
	}{
		OfferAsset:  assetA,
		OfferAmount: 1000000000000, // 1000 assetA
		WantAsset:   assetB,
		WantAmount:  500000000000, // 500 assetB
		Maker:       []byte("0xmakerAddress"),
		Taker:       []byte("0xtakerAddress"),
		ExpiryTime:  time.Now().Add(24 * time.Hour).Unix(),
		HashLock:    []byte("sha256hashlock1234567890123456"),
	}

	swapBytes, err := warp.Codec.Marshal(warp.CodecVersion, &swapPayload)
	require.NoError(err)

	swapMsg := warp.NewTeleportMessage(
		warp.TeleportSwap,
		chainA,
		chainB,
		uint64(time.Now().UnixNano()),
		swapBytes,
	)

	require.Equal(warp.TeleportSwap, swapMsg.MessageType)
	err = swapMsg.Validate()
	require.NoError(err)
}

// TestSignatureTypeForBridge tests selecting appropriate signature type
func TestSignatureTypeForBridge(t *testing.T) {
	require := require.New(t)

	// Recommended should be Ringtail (quantum-safe)
	recommended := warp.RecommendedSignatureType()
	require.Equal(warp.SigTypeRingtail, recommended)

	// Check quantum safety
	require.False(warp.SigTypeBLS.IsQuantumSafe())
	require.True(warp.SigTypeRingtail.IsQuantumSafe())
	require.True(warp.SigTypeHybrid.IsQuantumSafe())

	// String representations
	require.Equal("BLS", warp.SigTypeBLS.String())
	require.Equal("Ringtail", warp.SigTypeRingtail.String())
	require.Equal("Hybrid", warp.SigTypeHybrid.String())
}

// TestTeleportMessageCodecRoundTrip tests full serialization roundtrip
func TestTeleportMessageCodecRoundTrip(t *testing.T) {
	require := require.New(t)

	original := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		54321,
		[]byte("test payload for codec roundtrip"),
	)

	// Encode
	encoded, err := warp.Codec.Marshal(warp.CodecVersion, original)
	require.NoError(err)

	// Decode
	decoded := &warp.TeleportMessage{}
	_, err = warp.Codec.Unmarshal(encoded, decoded)
	require.NoError(err)

	// Verify
	require.Equal(original.Version, decoded.Version)
	require.Equal(original.MessageType, decoded.MessageType)
	require.Equal(original.SourceChainID, decoded.SourceChainID)
	require.Equal(original.DestChainID, decoded.DestChainID)
	require.Equal(original.Nonce, decoded.Nonce)
	require.Equal(original.Payload, decoded.Payload)
	require.Equal(original.Encrypted, decoded.Encrypted)
}

// TestBridgeSignerSetWithTeleport tests signer set management for Teleport
func TestBridgeSignerSetWithTeleport(t *testing.T) {
	require := require.New(t)

	vm := &VM{
		config: BridgeConfig{
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

	// Register 10 signers for the bridge
	for i := 0; i < 10; i++ {
		nodeID := ids.GenerateTestNodeID()
		result, err := vm.RegisterValidator(&RegisterValidatorInput{
			NodeID:     nodeID.String(),
			BondAmount: "100000000000000000", // 100M LUX bond
		})
		require.NoError(err)
		require.True(result.Success)
		require.False(result.ReshareNeeded) // LP-333: No reshare on opt-in
	}

	// Verify signer set
	require.Equal(10, len(vm.signerSet.Signers))
	require.Equal(6, vm.signerSet.ThresholdT) // 10 * 0.67 = 6

	// Create a Teleport message that this signer set would sign
	sourceChain := ids.GenerateTestID()
	destChain := ids.GenerateTestID()
	assetID := ids.GenerateTestID()

	transferPayload := warp.NewTransferPayload(
		assetID,
		uint64(50000000000000), // 50000 LUX
		[]byte("0xbridge_sender"),
		[]byte("0xbridge_recipient"),
		uint64(500000000), // 0.5 LUX fee
		[]byte("signed by bridge signers"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	teleportMsg := warp.NewTeleportMessage(
		warp.TeleportTransfer,
		sourceChain,
		destChain,
		uint64(vm.signerSet.CurrentEpoch),
		payloadBytes,
	)

	// Convert to Warp for signing
	warpMsg, err := teleportMsg.ToWarpMessage(96369)
	require.NoError(err)
	require.NotNil(warpMsg)

	// In production, the signer set would threshold-sign this message
	// Here we just verify the message is ready for signing
	require.NotEmpty(warpMsg.Payload)
}

// TestFullBridgeFlowWithTeleport tests the complete bridge flow
func TestFullBridgeFlowWithTeleport(t *testing.T) {
	require := require.New(t)

	// Setup chains
	cChain := ids.GenerateTestID()   // C-Chain (EVM source)
	xChain := ids.GenerateTestID()   // X-Chain (UTXO destination)
	luxAsset := ids.GenerateTestID() // LUX asset ID

	// Step 1: User initiates bridge on C-Chain
	amount := uint64(100000000000000) // 100,000 LUX
	sender := []byte("0xC_CHAIN_USER_ADDRESS_HERE__")
	recipient := []byte("X_CHAIN_USER_ADDRESS_")
	fee := uint64(1000000000) // 1 LUX fee

	transferPayload := warp.NewTransferPayload(
		luxAsset,
		amount,
		sender,
		recipient,
		fee,
		[]byte("C-Chain to X-Chain bridge"),
	)

	payloadBytes, err := transferPayload.Bytes()
	require.NoError(err)

	// Step 2: Create Teleport LOCK message
	lockNonce := uint64(time.Now().UnixNano()) //nolint:gosec // safe for test nonce
	lockMsg := warp.NewTeleportMessage(
		warp.TeleportLock,
		cChain,
		xChain,
		lockNonce,
		payloadBytes,
	)

	err = lockMsg.Validate()
	require.NoError(err)

	// Step 3: Convert to Warp message
	warpLockMsg, err := lockMsg.ToWarpMessage(96369)
	require.NoError(err)

	// Step 4: (Simulated) B-Chain signers validate and sign
	// In production: threshold signature via MPC
	t.Logf("Warp message ready for signing: NetworkID=%d, SourceChain=%s",
		warpLockMsg.NetworkID, warpLockMsg.SourceChainID)

	// Step 5: After signature, create UNLOCK on destination
	unlockMsg := warp.NewTeleportMessage(
		warp.TeleportUnlock,
		xChain,  // Now originates from X-Chain
		cChain,  // References C-Chain
		lockNonce, // Same nonce for correlation
		payloadBytes,
	)

	err = unlockMsg.Validate()
	require.NoError(err)

	// Step 6: Verify the bridge flow is complete
	require.Equal(lockMsg.Nonce, unlockMsg.Nonce)
	require.Equal(warp.TeleportLock, lockMsg.MessageType)
	require.Equal(warp.TeleportUnlock, unlockMsg.MessageType)

	t.Logf("Bridge flow complete: LOCK(%s->%s) -> UNLOCK(%s->%s)",
		cChain.String()[:8], xChain.String()[:8],
		xChain.String()[:8], cChain.String()[:8])
}
