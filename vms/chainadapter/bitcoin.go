// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// BitcoinAdapter implements SPV verification for Bitcoin
type BitcoinAdapter struct {
	mu             sync.RWMutex
	config         *ChainConfig
	headerChain    map[uint64]*BitcoinHeader // Height -> Header
	headerByHash   map[[32]byte]*BitcoinHeader
	latestHeight   uint64
	checkpoints    []BitcoinCheckpoint
	initialized    bool
}

// BitcoinHeader represents a Bitcoin block header (80 bytes)
type BitcoinHeader struct {
	Version       int32    `json:"version"`
	PrevBlock     [32]byte `json:"prevBlock"`
	MerkleRoot    [32]byte `json:"merkleRoot"`
	Timestamp     uint32   `json:"timestamp"`
	Bits          uint32   `json:"bits"` // Difficulty target
	Nonce         uint32   `json:"nonce"`

	// Computed fields
	Hash          [32]byte `json:"hash"`
	Height        uint64   `json:"height"`
	ChainWork     *big.Int `json:"chainWork"`
}

// BitcoinCheckpoint represents a known checkpoint
type BitcoinCheckpoint struct {
	Height    uint64   `json:"height"`
	Hash      [32]byte `json:"hash"`
	Timestamp uint32   `json:"timestamp"`
}

// Bitcoin consensus parameters
const (
	BitcoinMaxTarget          = "00000000FFFF0000000000000000000000000000000000000000000000000000"
	BitcoinTargetTimespan     = 14 * 24 * 60 * 60 // 2 weeks in seconds
	BitcoinTargetSpacing      = 10 * 60           // 10 minutes
	BitcoinDifficultyInterval = 2016              // Blocks per difficulty adjustment
	BitcoinMinConfirmations   = 6
)

// NewBitcoinAdapter creates a new Bitcoin SPV adapter
func NewBitcoinAdapter() *BitcoinAdapter {
	return &BitcoinAdapter{
		headerChain:  make(map[uint64]*BitcoinHeader),
		headerByHash: make(map[[32]byte]*BitcoinHeader),
		checkpoints:  defaultBitcoinCheckpoints(),
	}
}

// ChainID returns the Bitcoin chain ID
func (a *BitcoinAdapter) ChainID() ChainID {
	return idBitcoin
}

// ChainName returns "Bitcoin"
func (a *BitcoinAdapter) ChainName() string {
	return "Bitcoin"
}

// VerificationMode returns ModeSPV
func (a *BitcoinAdapter) VerificationMode() VerificationMode {
	return ModeSPV
}

// Initialize initializes the adapter
func (a *BitcoinAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	a.config = config

	// Load checkpoint headers
	for _, cp := range a.checkpoints {
		a.headerChain[cp.Height] = &BitcoinHeader{
			Hash:      cp.Hash,
			Height:    cp.Height,
			Timestamp: cp.Timestamp,
		}
		a.headerByHash[cp.Hash] = a.headerChain[cp.Height]
		if cp.Height > a.latestHeight {
			a.latestHeight = cp.Height
		}
	}

	a.initialized = true
	return nil
}

// VerifyBlockHeader verifies a Bitcoin block header
func (a *BitcoinAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != idBitcoin {
		return ErrChainNotSupported
	}

	// Decode Bitcoin header from ExtraData
	btcHeader, err := decodeBitcoinHeader(header.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to decode Bitcoin header: %w", err)
	}

	// Verify proof of work
	if !a.verifyProofOfWork(btcHeader) {
		return fmt.Errorf("invalid proof of work")
	}

	// Verify header connects to known chain
	if err := a.verifyHeaderConnection(btcHeader); err != nil {
		return fmt.Errorf("header connection failed: %w", err)
	}

	// Store the header
	a.mu.Lock()
	a.headerChain[btcHeader.Height] = btcHeader
	a.headerByHash[btcHeader.Hash] = btcHeader
	if btcHeader.Height > a.latestHeight {
		a.latestHeight = btcHeader.Height
	}
	a.mu.Unlock()

	return nil
}

// VerifyTransaction verifies a Bitcoin transaction inclusion proof
func (a *BitcoinAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	if proof.ChainID != idBitcoin {
		return ErrChainNotSupported
	}

	// Get the block header
	a.mu.RLock()
	header, ok := a.headerByHash[proof.BlockHash]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	// Verify confirmations
	a.mu.RLock()
	confirmations := a.latestHeight - header.Height
	a.mu.RUnlock()

	if confirmations < a.config.RequiredConfirmations {
		return ErrInsufficientConf
	}

	// Verify Merkle proof
	if !verifyBitcoinMerkleProof(proof.TxHash, header.MerkleRoot, proof.MerkleProof, proof.TxIndex) {
		return ErrInvalidMerkleProof
	}

	return nil
}

// VerifyMessage verifies a cross-chain message from Bitcoin
func (a *BitcoinAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	if msg.SourceChain != idBitcoin {
		return ErrChainNotSupported
	}

	// For Bitcoin, we verify the transaction containing the message
	proof := &TxInclusionProof{
		ChainID:     idBitcoin,
		BlockNumber: msg.SourceBlock,
		TxHash:      msg.SourceTxHash,
		MerkleProof: nil, // Would be extracted from SourceProof
	}

	// Decode the proof
	if len(msg.SourceProof) > 0 {
		// Parse merkle proof from SourceProof bytes
		// Format: [txIndex:4][proof_count:4][proof_1:32]...[proof_n:32]
		if len(msg.SourceProof) >= 8 {
			proof.TxIndex = binary.LittleEndian.Uint32(msg.SourceProof[0:4])
			proofCount := binary.LittleEndian.Uint32(msg.SourceProof[4:8])
			if uint32(len(msg.SourceProof)) >= 8+proofCount*32 {
				proof.MerkleProof = make([][]byte, proofCount)
				for i := uint32(0); i < proofCount; i++ {
					proof.MerkleProof[i] = msg.SourceProof[8+i*32 : 8+(i+1)*32]
				}
			}
		}
	}

	return a.VerifyTransaction(ctx, proof)
}

// VerifyEvent verifies a Bitcoin event (not applicable for Bitcoin)
func (a *BitcoinAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	// Bitcoin doesn't have events in the Ethereum sense
	// We could verify OP_RETURN outputs here
	return errors.New("Bitcoin does not support event verification")
}

// GetLatestFinalizedBlock returns the latest finalized block
func (a *BitcoinAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.latestHeight < a.config.RequiredConfirmations {
		return 0, nil
	}
	return a.latestHeight - a.config.RequiredConfirmations, nil
}

// GetRequiredConfirmations returns required confirmations
func (a *BitcoinAdapter) GetRequiredConfirmations() uint64 {
	if a.config != nil {
		return a.config.RequiredConfirmations
	}
	return BitcoinMinConfirmations
}

// GetBlockTime returns average block time
func (a *BitcoinAdapter) GetBlockTime() time.Duration {
	return 10 * time.Minute
}

// IsFinalized checks if a block is finalized
func (a *BitcoinAdapter) IsFinalized(ctx context.Context, blockNumber uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if blockNumber > a.latestHeight {
		return false, nil
	}
	return a.latestHeight-blockNumber >= a.config.RequiredConfirmations, nil
}

// GetValidatorSet returns nil for Bitcoin (PoW chain)
func (a *BitcoinAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	// Bitcoin uses PoW, not PoS
	return nil, nil
}

// Close closes the adapter
func (a *BitcoinAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headerChain = nil
	a.headerByHash = nil
	a.initialized = false
	return nil
}

// ======== Bitcoin-specific verification functions ========

// verifyProofOfWork verifies the header meets the difficulty target
func (a *BitcoinAdapter) verifyProofOfWork(header *BitcoinHeader) bool {
	// Calculate target from bits
	target := bitsToTarget(header.Bits)

	// Hash must be less than target
	hashBigInt := new(big.Int).SetBytes(header.Hash[:])
	return hashBigInt.Cmp(target) < 0
}

// verifyHeaderConnection verifies the header connects to the chain
func (a *BitcoinAdapter) verifyHeaderConnection(header *BitcoinHeader) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if parent exists
	parent, ok := a.headerByHash[header.PrevBlock]
	if !ok {
		// Check if it connects to a checkpoint
		for _, cp := range a.checkpoints {
			if header.PrevBlock == cp.Hash {
				return nil
			}
		}
		return fmt.Errorf("parent header not found: %s", hex.EncodeToString(header.PrevBlock[:]))
	}

	// Verify height is correct
	if header.Height != parent.Height+1 {
		return fmt.Errorf("invalid height: expected %d, got %d", parent.Height+1, header.Height)
	}

	// Verify timestamp is greater than parent (allowing for clock drift)
	if header.Timestamp <= parent.Timestamp-7200 { // Allow 2 hour drift
		return fmt.Errorf("timestamp too old")
	}

	return nil
}

// doubleSHA256 computes double SHA256 (Bitcoin hash)
func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// decodeBitcoinHeader decodes a Bitcoin header from 80 bytes
func decodeBitcoinHeader(data []byte) (*BitcoinHeader, error) {
	if len(data) < 80 {
		return nil, fmt.Errorf("header too short: %d bytes", len(data))
	}

	header := &BitcoinHeader{}

	// Version (4 bytes, little-endian)
	header.Version = int32(binary.LittleEndian.Uint32(data[0:4]))

	// Previous block hash (32 bytes, internal byte order)
	copy(header.PrevBlock[:], data[4:36])

	// Merkle root (32 bytes)
	copy(header.MerkleRoot[:], data[36:68])

	// Timestamp (4 bytes, little-endian)
	header.Timestamp = binary.LittleEndian.Uint32(data[68:72])

	// Bits (4 bytes, little-endian)
	header.Bits = binary.LittleEndian.Uint32(data[72:76])

	// Nonce (4 bytes, little-endian)
	header.Nonce = binary.LittleEndian.Uint32(data[76:80])

	// Compute hash
	header.Hash = doubleSHA256(data[0:80])

	return header, nil
}

// bitsToTarget converts compact bits format to target
func bitsToTarget(bits uint32) *big.Int {
	// Extract mantissa and exponent
	mantissa := bits & 0x007fffff
	exponent := uint(bits >> 24)

	// Calculate target
	target := new(big.Int).SetUint64(uint64(mantissa))

	if exponent <= 3 {
		target.Rsh(target, uint(3-exponent)*8)
	} else {
		target.Lsh(target, uint(exponent-3)*8)
	}

	return target
}

// verifyBitcoinMerkleProof verifies a Bitcoin Merkle proof
func verifyBitcoinMerkleProof(txHash, merkleRoot [32]byte, proof [][]byte, txIndex uint32) bool {
	hash := txHash

	for i, sibling := range proof {
		var combined []byte
		if (txIndex>>uint(i))&1 == 0 {
			// Hash is on the left
			combined = append(hash[:], sibling...)
		} else {
			// Hash is on the right
			combined = append(sibling, hash[:]...)
		}
		hash = doubleSHA256(combined)
	}

	return bytes.Equal(hash[:], merkleRoot[:])
}

// defaultBitcoinCheckpoints returns known Bitcoin checkpoints
func defaultBitcoinCheckpoints() []BitcoinCheckpoint {
	// These are actual Bitcoin mainnet checkpoints
	return []BitcoinCheckpoint{
		{Height: 0, Hash: hexTo32("000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"), Timestamp: 1231006505},
		{Height: 11111, Hash: hexTo32("0000000069e244f73d78e8fd29ba2fd2ed618bd6fa2ee92559f542fdb26e7c1d"), Timestamp: 1231006505},
		{Height: 33333, Hash: hexTo32("000000002dd5588a74784eaa7ab0507a18ad16a236e7b1ce69f00d7ddfb5d0a6"), Timestamp: 1260716069},
		{Height: 74000, Hash: hexTo32("0000000000573993a3c9e41ce34471c079dcf5f52a0e824a81e7f953b8661a20"), Timestamp: 1282927016},
		{Height: 105000, Hash: hexTo32("00000000000291ce28027faea320c8d2b054b2e0fe44a773f3eefb151d6bdc97"), Timestamp: 1302304875},
		{Height: 134444, Hash: hexTo32("00000000000005b12ffd4cd315cd34ffd4a594f430ac814c91184a0d42d2b0fe"), Timestamp: 1316573866},
		{Height: 168000, Hash: hexTo32("000000000000099e61ea72015e79632f216fe6cb33d7899acb35b75c8303b763"), Timestamp: 1330489014},
		{Height: 193000, Hash: hexTo32("000000000000059f452a5f7340de6682a977387c17010ff6e6c3bd83ca8b1317"), Timestamp: 1346201616},
		{Height: 210000, Hash: hexTo32("000000000000048b95347e83192f69cf0366076336c639f9b7228e9ba171342e"), Timestamp: 1354116278},
		{Height: 250000, Hash: hexTo32("000000000000003887df1f29024b06fc2200b55f8af8f35453d7be294df2d214"), Timestamp: 1375533383},
		{Height: 295000, Hash: hexTo32("00000000000000004d9b4ef50f0f9d686fd69db2e03af35a100370c64632a983"), Timestamp: 1397080064},
		{Height: 478558, Hash: hexTo32("0000000000000000011865af4122fe3b144e2cbeea86142e8ff2fb4107352d43"), Timestamp: 1501611161},
		{Height: 504031, Hash: hexTo32("0000000000000000001c8018d9cb3b742ef25114f27563e3fc4a1902167f9893"), Timestamp: 1516499168},
		{Height: 630000, Hash: hexTo32("000000000000000000024bead8df69990852c202db0e0097c1a12ea637d7e96d"), Timestamp: 1589225023},
		{Height: 700000, Hash: hexTo32("0000000000000000000590fc0f3eba193a278534220b2b37e9849e1a770ca959"), Timestamp: 1631006505},
		{Height: 800000, Hash: hexTo32("00000000000000000002a7c4c1e48d76c5a37902165a270156b7a8d72728a054"), Timestamp: 1690568305},
	}
}

// hexTo32 converts a hex string to a 32-byte array (with byte reversal for Bitcoin)
func hexTo32(s string) [32]byte {
	var result [32]byte
	b, _ := hex.DecodeString(s)
	// Reverse for Bitcoin's internal byte order
	for i := 0; i < len(b)/2; i++ {
		b[i], b[len(b)-1-i] = b[len(b)-1-i], b[i]
	}
	copy(result[:], b)
	return result
}
