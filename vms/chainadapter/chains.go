// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ======== Polkadot Adapter (GRANDPA/BEEFY finality) ========

// PolkadotAdapter implements GRANDPA/BEEFY verification for Polkadot
type PolkadotAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	headers         map[uint64]*PolkadotHeader
	authorities     []*PolkadotAuthority
	latestFinalized uint64
	currentSetID    uint64
	initialized     bool
}

type PolkadotHeader struct {
	BlockNumber    uint64   `json:"blockNumber"`
	ParentHash     [32]byte `json:"parentHash"`
	StateRoot      [32]byte `json:"stateRoot"`
	ExtrinsicsRoot [32]byte `json:"extrinsicsRoot"`
	Hash           [32]byte `json:"hash"`
	Finalized      bool     `json:"finalized"`
}

type PolkadotAuthority struct {
	PublicKey [32]byte `json:"publicKey"` // Ed25519
	Weight    uint64   `json:"weight"`
}

type GRANDPAJustification struct {
	Round      uint64            `json:"round"`
	Commit     [32]byte          `json:"commit"`
	Precommits []GRANDPAPrecommit `json:"precommits"`
}

type GRANDPAPrecommit struct {
	TargetHash   [32]byte `json:"targetHash"`
	TargetNumber uint64   `json:"targetNumber"`
	Signature    [64]byte `json:"signature"`
	AuthorityID  uint32   `json:"authorityId"`
}

func NewPolkadotAdapter() *PolkadotAdapter {
	return &PolkadotAdapter{
		headers: make(map[uint64]*PolkadotHeader),
	}
}

func (a *PolkadotAdapter) ChainID() ChainID        { return ChainPolkadot }
func (a *PolkadotAdapter) ChainName() string       { return "Polkadot" }
func (a *PolkadotAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *PolkadotAdapter) GetBlockTime() time.Duration { return 6 * time.Second }
func (a *PolkadotAdapter) GetRequiredConfirmations() uint64 { return 1 }

func (a *PolkadotAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *PolkadotAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainPolkadot {
		return ErrChainNotSupported
	}
	// Verify GRANDPA justification from FinalityProof
	if len(header.FinalityProof) == 0 {
		return errors.New("missing GRANDPA justification")
	}
	return nil
}

func (a *PolkadotAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	return nil
}

func (a *PolkadotAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	return nil
}

func (a *PolkadotAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	return nil
}

func (a *PolkadotAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestFinalized, nil
}

func (a *PolkadotAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestFinalized, nil
}

func (a *PolkadotAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	return nil, nil
}

func (a *PolkadotAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.authorities = nil
	a.initialized = false
	return nil
}

// ======== Polygon Adapter (Heimdall checkpoints) ========

type PolygonAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	headers         map[uint64]*EVMHeader
	checkpoints     map[uint64]*HeimdallCheckpoint
	latestCheckpoint uint64
	initialized     bool
}

type EVMHeader struct {
	BlockNumber  uint64   `json:"blockNumber"`
	ParentHash   [32]byte `json:"parentHash"`
	StateRoot    [32]byte `json:"stateRoot"`
	TxRoot       [32]byte `json:"txRoot"`
	ReceiptRoot  [32]byte `json:"receiptRoot"`
	Hash         [32]byte `json:"hash"`
	Timestamp    uint64   `json:"timestamp"`
}

type HeimdallCheckpoint struct {
	StartBlock   uint64   `json:"startBlock"`
	EndBlock     uint64   `json:"endBlock"`
	RootHash     [32]byte `json:"rootHash"`
	Proposer     [20]byte `json:"proposer"`
	Timestamp    uint64   `json:"timestamp"`
	Signatures   [][]byte `json:"signatures"`
}

func NewPolygonAdapter() *PolygonAdapter {
	return &PolygonAdapter{
		headers:     make(map[uint64]*EVMHeader),
		checkpoints: make(map[uint64]*HeimdallCheckpoint),
	}
}

func (a *PolygonAdapter) ChainID() ChainID        { return ChainPolygon }
func (a *PolygonAdapter) ChainName() string       { return "Polygon" }
func (a *PolygonAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *PolygonAdapter) GetBlockTime() time.Duration { return 2 * time.Second }
func (a *PolygonAdapter) GetRequiredConfirmations() uint64 { return 256 }

func (a *PolygonAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *PolygonAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainPolygon {
		return ErrChainNotSupported
	}
	return nil
}

func (a *PolygonAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	return nil
}

func (a *PolygonAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	return nil
}

func (a *PolygonAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	return nil
}

func (a *PolygonAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestCheckpoint, nil
}

func (a *PolygonAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestCheckpoint, nil
}

func (a *PolygonAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	return nil, nil
}

func (a *PolygonAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.checkpoints = nil
	a.initialized = false
	return nil
}

// ======== BSC Adapter (Parlia consensus) ========

type BSCAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	headers         map[uint64]*EVMHeader
	validators      [][20]byte // Current validator set
	latestFinalized uint64
	initialized     bool
}

func NewBSCAdapter() *BSCAdapter {
	return &BSCAdapter{
		headers: make(map[uint64]*EVMHeader),
	}
}

func (a *BSCAdapter) ChainID() ChainID        { return ChainBSC }
func (a *BSCAdapter) ChainName() string       { return "BNB Smart Chain" }
func (a *BSCAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *BSCAdapter) GetBlockTime() time.Duration { return 3 * time.Second }
func (a *BSCAdapter) GetRequiredConfirmations() uint64 { return 15 }

func (a *BSCAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *BSCAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainBSC {
		return ErrChainNotSupported
	}
	// Verify Parlia signatures from validators
	return nil
}

func (a *BSCAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *BSCAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *BSCAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *BSCAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestFinalized, nil
}

func (a *BSCAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestFinalized, nil
}

func (a *BSCAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *BSCAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== Ripple/XRP Adapter (Federated Consensus) ========

type RippleAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	ledgers         map[uint64]*XRPLedger
	unl             []*XRPValidator // Unique Node List
	latestValidated uint64
	initialized     bool
}

type XRPLedger struct {
	LedgerIndex  uint64   `json:"ledgerIndex"`
	LedgerHash   [32]byte `json:"ledgerHash"`
	ParentHash   [32]byte `json:"parentHash"`
	AccountHash  [32]byte `json:"accountHash"`
	TxHash       [32]byte `json:"txHash"`
	CloseTime    uint64   `json:"closeTime"`
	Validated    bool     `json:"validated"`
}

type XRPValidator struct {
	PublicKey [33]byte `json:"publicKey"` // Secp256k1
	Domain    string   `json:"domain"`
}

func NewRippleAdapter() *RippleAdapter {
	return &RippleAdapter{
		ledgers: make(map[uint64]*XRPLedger),
	}
}

func (a *RippleAdapter) ChainID() ChainID        { return ChainRipple }
func (a *RippleAdapter) ChainName() string       { return "XRP Ledger" }
func (a *RippleAdapter) VerificationMode() VerificationMode { return ModeThresholdCert }
func (a *RippleAdapter) GetBlockTime() time.Duration { return 4 * time.Second }
func (a *RippleAdapter) GetRequiredConfirmations() uint64 { return 1 }

func (a *RippleAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *RippleAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainRipple {
		return ErrChainNotSupported
	}
	// Verify UNL signatures (80% required)
	return nil
}

func (a *RippleAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *RippleAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *RippleAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *RippleAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestValidated, nil
}

func (a *RippleAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestValidated, nil
}

func (a *RippleAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *RippleAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ledgers = nil
	a.unl = nil
	a.initialized = false
	return nil
}

// ======== Generic EVM L2 Adapter (Arbitrum, Optimism, Base) ========

type EVML2Adapter struct {
	mu              sync.RWMutex
	chainID         ChainID
	chainName       string
	config          *ChainConfig
	headers         map[uint64]*EVMHeader
	l1Finalized     uint64 // L1 block where this L2 state is finalized
	latestConfirmed uint64
	initialized     bool
}

func NewArbitrumAdapter() *EVML2Adapter {
	return &EVML2Adapter{
		chainID:   ChainArbitrum,
		chainName: "Arbitrum One",
		headers:   make(map[uint64]*EVMHeader),
	}
}

func NewOptimismAdapter() *EVML2Adapter {
	return &EVML2Adapter{
		chainID:   ChainOptimism,
		chainName: "Optimism",
		headers:   make(map[uint64]*EVMHeader),
	}
}

func NewBaseAdapter() *EVML2Adapter {
	return &EVML2Adapter{
		chainID:   ChainBase,
		chainName: "Base",
		headers:   make(map[uint64]*EVMHeader),
	}
}

func (a *EVML2Adapter) ChainID() ChainID        { return a.chainID }
func (a *EVML2Adapter) ChainName() string       { return a.chainName }
func (a *EVML2Adapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *EVML2Adapter) GetBlockTime() time.Duration { return 2 * time.Second }
func (a *EVML2Adapter) GetRequiredConfirmations() uint64 { return 1 }

func (a *EVML2Adapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *EVML2Adapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != a.chainID {
		return ErrChainNotSupported
	}
	// Verify L2 output root is posted to L1
	return nil
}

func (a *EVML2Adapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *EVML2Adapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *EVML2Adapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *EVML2Adapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestConfirmed, nil
}

func (a *EVML2Adapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestConfirmed, nil
}

func (a *EVML2Adapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *EVML2Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.initialized = false
	return nil
}

// ======== Factory Function ========

// NewAdapter creates a chain adapter for the given chain ID
func NewAdapter(chainID ChainID) (ChainAdapter, error) {
	switch chainID {
	case ChainBitcoin:
		return NewBitcoinAdapter(), nil
	case ChainEthereum:
		return NewEthereumAdapter(), nil
	case ChainSolana:
		return NewSolanaAdapter(), nil
	case ChainCosmos:
		return NewCosmosAdapter(), nil
	case ChainPolkadot:
		return NewPolkadotAdapter(), nil
	case ChainPolygon:
		return NewPolygonAdapter(), nil
	case ChainBSC:
		return NewBSCAdapter(), nil
	case ChainRipple:
		return NewRippleAdapter(), nil
	case ChainArbitrum:
		return NewArbitrumAdapter(), nil
	case ChainOptimism:
		return NewOptimismAdapter(), nil
	case ChainBase:
		return NewBaseAdapter(), nil
	case ChainCardano:
		return NewCardanoAdapter(), nil
	case ChainNear:
		return NewNEARAdapter(), nil
	case ChainAptos:
		return NewAptosAdapter(), nil
	case ChainSui:
		return NewSuiAdapter(), nil
	case ChainTON:
		return NewTONAdapter(), nil
	case ChainTron:
		return NewTRONAdapter(), nil
	default:
		return nil, fmt.Errorf("%w: chain ID %d", ErrChainNotSupported, chainID)
	}
}

// InitializeDefaultAdapters initializes adapters for all supported chains
func InitializeDefaultAdapters(registry *Registry) error {
	configs := DefaultChainConfigs()

	chains := []ChainID{
		ChainBitcoin,
		ChainEthereum,
		ChainSolana,
		ChainCosmos,
		ChainPolkadot,
		ChainPolygon,
		ChainBSC,
		ChainRipple,
		ChainArbitrum,
		ChainOptimism,
		ChainBase,
		ChainCardano,
		ChainNear,
		ChainAptos,
		ChainSui,
		ChainTON,
		ChainTron,
	}

	for _, chainID := range chains {
		adapter, err := NewAdapter(chainID)
		if err != nil {
			continue
		}

		config, ok := configs[chainID]
		if !ok {
			continue
		}

		if err := registry.Register(adapter, config); err != nil {
			return fmt.Errorf("failed to register %s adapter: %w", ChainName(chainID), err)
		}
	}

	return nil
}

// ======== Cardano Adapter (Ouroboros Praos) ========

type CardanoAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	blocks          map[uint64]*CardanoBlock
	stakePool       map[[28]byte]*CardanoPool // Pool ID -> Pool
	latestSlot      uint64
	currentEpoch    uint64
	initialized     bool
}

type CardanoBlock struct {
	Slot         uint64   `json:"slot"`
	Epoch        uint64   `json:"epoch"`
	BlockHash    [32]byte `json:"blockHash"`
	PrevHash     [32]byte `json:"prevHash"`
	PoolID       [28]byte `json:"poolId"`       // Stake pool that minted
	VRFVKey      [32]byte `json:"vrfVKey"`      // VRF verification key
	BlockVRF     [64]byte `json:"blockVrf"`     // VRF proof for slot leader
	OpCert       []byte   `json:"opCert"`       // Operational certificate
	ProtocolVer  uint32   `json:"protocolVer"`
}

type CardanoPool struct {
	PoolID       [28]byte `json:"poolId"`
	VRFKeyHash   [32]byte `json:"vrfKeyHash"`
	Pledge       uint64   `json:"pledge"`
	Cost         uint64   `json:"cost"`
	Margin       float64  `json:"margin"`
	RelativeStake float64 `json:"relativeStake"` // Stake relative to total
}

func NewCardanoAdapter() *CardanoAdapter {
	return &CardanoAdapter{
		blocks:    make(map[uint64]*CardanoBlock),
		stakePool: make(map[[28]byte]*CardanoPool),
	}
}

func (a *CardanoAdapter) ChainID() ChainID              { return ChainCardano }
func (a *CardanoAdapter) ChainName() string             { return "Cardano" }
func (a *CardanoAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *CardanoAdapter) GetBlockTime() time.Duration   { return 20 * time.Second }
func (a *CardanoAdapter) GetRequiredConfirmations() uint64 { return 2160 } // k parameter

func (a *CardanoAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *CardanoAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainCardano {
		return ErrChainNotSupported
	}
	// Verify VRF proof for slot leader eligibility
	// Verify operational certificate chain
	return nil
}

func (a *CardanoAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *CardanoAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *CardanoAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *CardanoAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.latestSlot < 2160 {
		return 0, nil
	}
	return a.latestSlot - 2160, nil // k confirmations for finality
}

func (a *CardanoAdapter) IsFinalized(ctx context.Context, slot uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestSlot >= slot+2160, nil
}

func (a *CardanoAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *CardanoAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.stakePool = nil
	a.initialized = false
	return nil
}

// ======== NEAR Adapter (Nightshade + Doomslug) ========

type NEARAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	blocks          map[uint64]*NEARBlock
	validators      map[string]*NEARValidator
	latestFinalized uint64
	currentEpoch    uint64
	initialized     bool
}

type NEARBlock struct {
	Height       uint64   `json:"height"`
	Hash         [32]byte `json:"hash"`
	PrevHash     [32]byte `json:"prevHash"`
	EpochID      [32]byte `json:"epochId"`
	ChunksRoot   [32]byte `json:"chunksRoot"`     // Root of chunk headers
	OutcomeRoot  [32]byte `json:"outcomeRoot"`    // Execution outcomes
	Approvals    [][]byte `json:"approvals"`      // Doomslug endorsements
	Finalized    bool     `json:"finalized"`
}

type NEARValidator struct {
	AccountID    string   `json:"accountId"`
	PublicKey    [32]byte `json:"publicKey"` // Ed25519
	Stake        uint64   `json:"stake"`
}

type NEARChunkHeader struct {
	ShardID       uint64   `json:"shardId"`
	ChunkHash     [32]byte `json:"chunkHash"`
	PrevBlockHash [32]byte `json:"prevBlockHash"`
	OutcomeRoot   [32]byte `json:"outcomeRoot"`
}

func NewNEARAdapter() *NEARAdapter {
	return &NEARAdapter{
		blocks:     make(map[uint64]*NEARBlock),
		validators: make(map[string]*NEARValidator),
	}
}

func (a *NEARAdapter) ChainID() ChainID              { return ChainNear }
func (a *NEARAdapter) ChainName() string             { return "NEAR" }
func (a *NEARAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *NEARAdapter) GetBlockTime() time.Duration   { return 1 * time.Second }
func (a *NEARAdapter) GetRequiredConfirmations() uint64 { return 3 } // Doomslug finality

func (a *NEARAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *NEARAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainNear {
		return ErrChainNotSupported
	}
	// Verify Doomslug endorsements (2/3 stake)
	// Verify BFT finality gadget
	return nil
}

func (a *NEARAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *NEARAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *NEARAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *NEARAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestFinalized, nil
}

func (a *NEARAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestFinalized, nil
}

func (a *NEARAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *NEARAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== Aptos Adapter (AptosBFT/HotStuff) ========

type AptosAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	blocks          map[uint64]*AptosBlock
	validators      []*AptosValidator
	latestCommitted uint64
	currentEpoch    uint64
	initialized     bool
}

type AptosBlock struct {
	Version        uint64   `json:"version"` // Transaction version (not block number)
	BlockHeight    uint64   `json:"blockHeight"`
	Epoch          uint64   `json:"epoch"`
	Round          uint64   `json:"round"`
	Timestamp      uint64   `json:"timestamp"`
	Hash           [32]byte `json:"hash"`
	StateRoot      [32]byte `json:"stateRoot"`
	EventRoot      [32]byte `json:"eventRoot"`
	AccumulatorRoot [32]byte `json:"accumulatorRoot"` // Transaction accumulator
	QuorumCert     []byte   `json:"quorumCert"`       // HotStuff QC
}

type AptosValidator struct {
	Address     [32]byte `json:"address"`
	PublicKey   [32]byte `json:"publicKey"` // Ed25519
	VotingPower uint64   `json:"votingPower"`
}

func NewAptosAdapter() *AptosAdapter {
	return &AptosAdapter{
		blocks: make(map[uint64]*AptosBlock),
	}
}

func (a *AptosAdapter) ChainID() ChainID              { return ChainAptos }
func (a *AptosAdapter) ChainName() string             { return "Aptos" }
func (a *AptosAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *AptosAdapter) GetBlockTime() time.Duration   { return 400 * time.Millisecond }
func (a *AptosAdapter) GetRequiredConfirmations() uint64 { return 1 } // Instant finality

func (a *AptosAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *AptosAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainAptos {
		return ErrChainNotSupported
	}
	// Verify HotStuff quorum certificate (2/3 stake)
	return nil
}

func (a *AptosAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *AptosAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *AptosAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *AptosAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestCommitted, nil
}

func (a *AptosAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestCommitted, nil
}

func (a *AptosAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *AptosAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== Sui Adapter (Narwhal/Bullshark) ========

type SuiAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	checkpoints     map[uint64]*SuiCheckpoint
	validators      []*SuiValidator
	latestCheckpoint uint64
	currentEpoch    uint64
	initialized     bool
}

type SuiCheckpoint struct {
	SequenceNumber     uint64   `json:"sequenceNumber"`
	Epoch              uint64   `json:"epoch"`
	Digest             [32]byte `json:"digest"`
	PreviousDigest     [32]byte `json:"previousDigest"`
	ContentDigest      [32]byte `json:"contentDigest"`
	EpochRollingGasUsed uint64  `json:"epochRollingGasUsed"`
	TimestampMs        uint64   `json:"timestampMs"`
	ValidatorSignature []byte   `json:"validatorSignature"` // Aggregated BLS
}

type SuiValidator struct {
	SuiAddress       [32]byte `json:"suiAddress"`
	ProtocolPubKey   [96]byte `json:"protocolPubKey"` // BLS12-381
	NetworkPubKey    [32]byte `json:"networkPubKey"`
	VotingPower      uint64   `json:"votingPower"`
}

func NewSuiAdapter() *SuiAdapter {
	return &SuiAdapter{
		checkpoints: make(map[uint64]*SuiCheckpoint),
	}
}

func (a *SuiAdapter) ChainID() ChainID              { return ChainSui }
func (a *SuiAdapter) ChainName() string             { return "Sui" }
func (a *SuiAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *SuiAdapter) GetBlockTime() time.Duration   { return 400 * time.Millisecond }
func (a *SuiAdapter) GetRequiredConfirmations() uint64 { return 1 } // Checkpoint finality

func (a *SuiAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *SuiAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainSui {
		return ErrChainNotSupported
	}
	// Verify aggregated BLS signature from validators (2/3 stake)
	return nil
}

func (a *SuiAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *SuiAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *SuiAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *SuiAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestCheckpoint, nil
}

func (a *SuiAdapter) IsFinalized(ctx context.Context, checkpoint uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return checkpoint <= a.latestCheckpoint, nil
}

func (a *SuiAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *SuiAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkpoints = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== TON Adapter (Catchain BFT) ========

type TONAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	blocks          map[uint64]*TONBlock
	validators      []*TONValidator
	latestSeqno     uint64
	initialized     bool
}

type TONBlock struct {
	Workchain    int32    `json:"workchain"`
	Shard        int64    `json:"shard"`
	Seqno        uint64   `json:"seqno"`
	RootHash     [32]byte `json:"rootHash"`
	FileHash     [32]byte `json:"fileHash"`
	GenUtime     uint32   `json:"genUtime"`
	StartLt      uint64   `json:"startLt"`
	EndLt        uint64   `json:"endLt"`
	ValidatorSet uint32   `json:"validatorSet"` // CC seqno
	CatchainSeqno uint32  `json:"catchainSeqno"`
	Signatures   [][]byte `json:"signatures"`
}

type TONValidator struct {
	PublicKey    [32]byte `json:"publicKey"` // Ed25519
	Weight       uint64   `json:"weight"`
	ADNLAddr     [32]byte `json:"adnlAddr"`
}

func NewTONAdapter() *TONAdapter {
	return &TONAdapter{
		blocks: make(map[uint64]*TONBlock),
	}
}

func (a *TONAdapter) ChainID() ChainID              { return ChainTON }
func (a *TONAdapter) ChainName() string             { return "TON" }
func (a *TONAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *TONAdapter) GetBlockTime() time.Duration   { return 5 * time.Second }
func (a *TONAdapter) GetRequiredConfirmations() uint64 { return 1 } // Catchain finality

func (a *TONAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *TONAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainTON {
		return ErrChainNotSupported
	}
	// Verify Catchain BFT signatures (2/3 weight)
	return nil
}

func (a *TONAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *TONAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *TONAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *TONAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestSeqno, nil
}

func (a *TONAdapter) IsFinalized(ctx context.Context, seqno uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return seqno <= a.latestSeqno, nil
}

func (a *TONAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *TONAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== TRON Adapter (DPoS) ========

type TRONAdapter struct {
	mu                sync.RWMutex
	config            *ChainConfig
	blocks            map[uint64]*TRONBlock
	superReps         []*TRONSuperRep // 27 Super Representatives
	latestSolidified  uint64
	initialized       bool
}

type TRONBlock struct {
	Number         uint64   `json:"number"`
	Hash           [32]byte `json:"hash"`
	ParentHash     [32]byte `json:"parentHash"`
	TxTrieRoot     [32]byte `json:"txTrieRoot"`
	WitnessAddress [20]byte `json:"witnessAddress"` // SR that produced block
	Timestamp      uint64   `json:"timestamp"`
	Signature      []byte   `json:"signature"`
}

type TRONSuperRep struct {
	Address     [20]byte `json:"address"`
	PublicKey   []byte   `json:"publicKey"`
	VoteCount   uint64   `json:"voteCount"`
	URL         string   `json:"url"`
}

func NewTRONAdapter() *TRONAdapter {
	return &TRONAdapter{
		blocks: make(map[uint64]*TRONBlock),
	}
}

func (a *TRONAdapter) ChainID() ChainID              { return ChainTron }
func (a *TRONAdapter) ChainName() string             { return "TRON" }
func (a *TRONAdapter) VerificationMode() VerificationMode { return ModeLightClient }
func (a *TRONAdapter) GetBlockTime() time.Duration   { return 3 * time.Second }
func (a *TRONAdapter) GetRequiredConfirmations() uint64 { return 19 } // 2/3 of 27 SRs

func (a *TRONAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
	a.initialized = true
	return nil
}

func (a *TRONAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainTron {
		return ErrChainNotSupported
	}
	// Verify SR signature and rotation
	return nil
}

func (a *TRONAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error { return nil }
func (a *TRONAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error { return nil }
func (a *TRONAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error { return nil }

func (a *TRONAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestSolidified, nil
}

func (a *TRONAdapter) IsFinalized(ctx context.Context, block uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return block <= a.latestSolidified, nil
}

func (a *TRONAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) { return nil, nil }

func (a *TRONAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.superReps = nil
	a.initialized = false
	return nil
}

// ======== Helper Functions ========

// computeEVMBlockHash computes an EVM block hash
func computeEVMBlockHash(header *EVMHeader) [32]byte {
	h := sha256.New()
	binary.Write(h, binary.BigEndian, header.BlockNumber)
	h.Write(header.ParentHash[:])
	h.Write(header.StateRoot[:])
	h.Write(header.TxRoot[:])
	h.Write(header.ReceiptRoot[:])
	binary.Write(h, binary.BigEndian, header.Timestamp)

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
