// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package chainadapter provides adapters for verifying data from external blockchains.
// This enables the RelayVM and OracleVM to verify cross-chain messages and oracle feeds
// from major blockchains including Bitcoin, Ethereum, Solana, Cosmos, and others.
package chainadapter

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/ids"
)

// ChainID represents a unique blockchain identifier. Values are taxonomic
// (1=Bitcoin family root, 2=Ethereum, ...) and stable; they are NOT EIP-155
// chain IDs. The canonical id<->name binding lives in DefaultChainSeed.
type ChainID uint32

// VerificationMode indicates how chain data is verified
type VerificationMode uint8

const (
	// ModeSPV uses Simple Payment Verification (Bitcoin-style PoW)
	ModeSPV VerificationMode = iota
	// ModeLightClient uses light client proofs (Ethereum sync committee, IBC)
	ModeLightClient
	// ModeVoteAttestation uses validator vote attestations (Solana)
	ModeVoteAttestation
	// ModeZKProof uses ZK validity proofs (zkSync, Starknet, Scroll)
	ModeZKProof
	// ModeThresholdCert uses threshold certificate attestations (XRPL, Axelar)
	ModeThresholdCert
	// ModeBFT uses BFT consensus signatures (Tendermint, HotStuff, pBFT)
	ModeBFT
	// ModeDAG uses DAG-based consensus (IOTA, Kaspa, Hedera Hashgraph)
	ModeDAG
	// ModeDPoS uses Delegated Proof of Stake (EOS, TRON, Lisk)
	ModeDPoS
	// ModePoA uses Proof of Authority (Ronin, VeChain, private chains)
	ModePoA
	// ModeOptimistic uses optimistic rollup fraud proofs (Arbitrum, Optimism)
	ModeOptimistic
	// ModeSCP uses Stellar Consensus Protocol (Stellar)
	ModeSCP
	// ModePBA uses Pure Byzantine Agreement (Algorand)
	ModePBA
	// ModeChainKey uses Chain Key cryptography (Internet Computer)
	ModeChainKey
	// ModeRingCT uses Ring Confidential Transactions (Monero privacy proofs)
	ModeRingCT
	// ModeGRANDPA uses GRANDPA finality (Polkadot parachains)
	ModeGRANDPA
)

// ChainType categorizes chains by their fundamental architecture
type ChainType uint8

const (
	// ChainTypeEVM is for EVM-compatible chains (Ethereum, Polygon, BSC, L2s)
	ChainTypeEVM ChainType = iota
	// ChainTypeUTXO is for UTXO-based chains (Bitcoin, Litecoin, Dogecoin)
	ChainTypeUTXO
	// ChainTypeAccount is for native account-model chains (Solana, NEAR, Aptos)
	ChainTypeAccount
	// ChainTypeCosmosSDK is for Cosmos SDK chains (Cosmos, Osmosis, Injective)
	ChainTypeCosmosSDK
	// ChainTypeSubstrate is for Polkadot parachains (Moonbeam, Acala, Phala)
	ChainTypeSubstrate
	// ChainTypeDAG is for DAG-based chains (IOTA, Kaspa, Hedera)
	ChainTypeDAG
	// ChainTypeMoveVM is for Move-based chains (Aptos, Sui)
	ChainTypeMoveVM
	// ChainTypeTVM is for TON Virtual Machine (TON)
	ChainTypeTVM
	// ChainTypeFVM is for Filecoin Virtual Machine
	ChainTypeFVM
	// ChainTypeStellar is for Stellar Consensus Protocol chains
	ChainTypeStellar
	// ChainTypeRipple is for XRP Ledger
	ChainTypeRipple
	// ChainTypeCardano is for Cardano (extended UTXO)
	ChainTypeCardano
	// ChainTypeAlgorand is for Algorand
	ChainTypeAlgorand
	// ChainTypeTezos is for Tezos
	ChainTypeTezos
	// ChainTypeICP is for Internet Computer
	ChainTypeICP
	// ChainTypePrivacy is for privacy chains (Monero, Zcash)
	ChainTypePrivacy
)

// String returns the string representation of ChainType
func (ct ChainType) String() string {
	switch ct {
	case ChainTypeEVM:
		return "EVM"
	case ChainTypeUTXO:
		return "UTXO"
	case ChainTypeAccount:
		return "Account"
	case ChainTypeCosmosSDK:
		return "CosmosSDK"
	case ChainTypeSubstrate:
		return "Substrate"
	case ChainTypeDAG:
		return "DAG"
	case ChainTypeMoveVM:
		return "MoveVM"
	case ChainTypeTVM:
		return "TVM"
	case ChainTypeFVM:
		return "FVM"
	case ChainTypeStellar:
		return "Stellar"
	case ChainTypeRipple:
		return "Ripple"
	case ChainTypeCardano:
		return "Cardano"
	case ChainTypeAlgorand:
		return "Algorand"
	case ChainTypeTezos:
		return "Tezos"
	case ChainTypeICP:
		return "ICP"
	case ChainTypePrivacy:
		return "Privacy"
	default:
		return "Unknown"
	}
}

// AddressFormat describes how addresses are formatted on this chain
type AddressFormat uint8

const (
	// AddressFormatHex is for hex addresses (0x... for EVM, no prefix for others)
	AddressFormatHex AddressFormat = iota
	// AddressFormatBase58 is for Base58 addresses (Bitcoin, Solana)
	AddressFormatBase58
	// AddressFormatBech32 is for Bech32 addresses (bc1..., cosmos1...)
	AddressFormatBech32
	// AddressFormatSS58 is for SS58 addresses (Polkadot)
	AddressFormatSS58
	// AddressFormatCustom is for chain-specific formats
	AddressFormatCustom
)

// Errors
var (
	ErrChainNotSupported     = errors.New("chain not supported")
	ErrInvalidProof          = errors.New("invalid proof")
	ErrBlockNotFinalized     = errors.New("block not finalized")
	ErrInsufficientConf      = errors.New("insufficient confirmations")
	ErrHeaderNotFound        = errors.New("header not found")
	ErrInvalidSignature      = errors.New("invalid signature")
	ErrStaleData             = errors.New("data is stale")
	ErrQuorumNotMet          = errors.New("quorum not met")
	ErrInvalidMerkleProof    = errors.New("invalid merkle proof")
	ErrValidatorSetMismatch  = errors.New("validator set mismatch")
)

// ChainAdapter is the interface that all chain adapters must implement
type ChainAdapter interface {
	// ChainID returns the chain identifier
	ChainID() ChainID

	// ChainName returns the human-readable chain name
	ChainName() string

	// VerificationMode returns the primary verification mode for this chain
	VerificationMode() VerificationMode

	// VerifyBlockHeader verifies a block header from this chain
	VerifyBlockHeader(ctx context.Context, header *BlockHeader) error

	// VerifyTransaction verifies a transaction inclusion proof
	VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error

	// VerifyMessage verifies a cross-chain message from this chain
	VerifyMessage(ctx context.Context, msg *CrossChainMessage) error

	// VerifyEvent verifies an event/log from this chain
	VerifyEvent(ctx context.Context, event *ChainEvent) error

	// GetLatestFinalizedBlock returns the latest finalized block number
	GetLatestFinalizedBlock(ctx context.Context) (uint64, error)

	// GetRequiredConfirmations returns required confirmations for finality
	GetRequiredConfirmations() uint64

	// GetBlockTime returns the average block time
	GetBlockTime() time.Duration

	// IsFinalized checks if a block is considered finalized
	IsFinalized(ctx context.Context, blockNumber uint64) (bool, error)

	// GetValidatorSet returns the current validator set (if applicable)
	GetValidatorSet(ctx context.Context) (*ValidatorSet, error)

	// Initialize initializes the adapter with configuration
	Initialize(config *ChainConfig) error

	// Close cleans up adapter resources
	Close() error
}

// BlockHeader represents a block header from any chain
type BlockHeader struct {
	ChainID     ChainID   `json:"chainId"`
	BlockNumber uint64    `json:"blockNumber"`
	BlockHash   [32]byte  `json:"blockHash"`
	ParentHash  [32]byte  `json:"parentHash"`
	StateRoot   [32]byte  `json:"stateRoot"`
	TxRoot      [32]byte  `json:"txRoot"`
	ReceiptRoot [32]byte  `json:"receiptRoot"`
	Timestamp   int64     `json:"timestamp"`

	// Chain-specific fields stored as raw bytes
	ExtraData   []byte    `json:"extraData"`

	// Proof of finality (varies by chain)
	FinalityProof []byte  `json:"finalityProof"`
}

// TxInclusionProof proves a transaction was included in a block
type TxInclusionProof struct {
	ChainID     ChainID  `json:"chainId"`
	BlockNumber uint64   `json:"blockNumber"`
	BlockHash   [32]byte `json:"blockHash"`
	TxHash      [32]byte `json:"txHash"`
	TxIndex     uint32   `json:"txIndex"`

	// Merkle proof path
	MerkleProof [][]byte `json:"merkleProof"`

	// Transaction data (may be nil if only proving inclusion)
	TxData      []byte   `json:"txData,omitempty"`
}

// CrossChainMessage represents a message to be relayed between chains
type CrossChainMessage struct {
	ID            ids.ID   `json:"id"`
	SourceChain   ChainID  `json:"sourceChain"`
	DestChain     ChainID  `json:"destChain"`
	Sender        []byte   `json:"sender"`
	Recipient     []byte   `json:"recipient"`
	Nonce         uint64   `json:"nonce"`
	Payload       []byte   `json:"payload"`

	// Source chain proof
	SourceBlock   uint64   `json:"sourceBlock"`
	SourceTxHash  [32]byte `json:"sourceTxHash"`
	SourceProof   []byte   `json:"sourceProof"`

	// Timestamp and expiry
	Timestamp     int64    `json:"timestamp"`
	ExpiryTime    int64    `json:"expiryTime"`
}

// ChainEvent represents an event/log from a chain
type ChainEvent struct {
	ChainID     ChainID  `json:"chainId"`
	BlockNumber uint64   `json:"blockNumber"`
	TxHash      [32]byte `json:"txHash"`
	LogIndex    uint32   `json:"logIndex"`

	// Event identifier (e.g., topic0 for Ethereum)
	EventID     [32]byte `json:"eventId"`

	// Event data
	Address     []byte   `json:"address"`
	Topics      [][]byte `json:"topics"`
	Data        []byte   `json:"data"`

	// Proof of inclusion
	Proof       []byte   `json:"proof"`
}

// ValidatorSet represents a validator set for PoS chains
type ValidatorSet struct {
	ChainID     ChainID        `json:"chainId"`
	Epoch       uint64         `json:"epoch"`
	Validators  []*Validator   `json:"validators"`
	TotalStake  uint64         `json:"totalStake"`
	Threshold   uint64         `json:"threshold"` // 2/3 stake required for finality
	ValidFrom   uint64         `json:"validFrom"` // Block number this set is valid from
	ValidUntil  uint64         `json:"validUntil"`
}

// Validator represents a single validator
type Validator struct {
	Address    []byte   `json:"address"`
	PublicKey  []byte   `json:"publicKey"`
	Stake      uint64   `json:"stake"`
	VotingPower uint64  `json:"votingPower"`
}

// ChainConfig contains configuration for a chain adapter
type ChainConfig struct {
	ChainID            ChainID       `json:"chainId"`
	Name               string        `json:"name"`
	NetworkID          uint64        `json:"networkId"`         // Internal network identifier
	EVMChainID         uint64        `json:"evmChainId"`        // EVM chain ID (0 for non-EVM)
	NativeSymbol       string        `json:"nativeSymbol"`      // e.g., "ETH", "BTC", "SOL"
	NativeDecimals     uint8         `json:"nativeDecimals"`    // e.g., 18 for ETH, 8 for BTC
	IsEVM              bool          `json:"isEvm"`             // True for EVM-compatible chains
	ChainType          ChainType     `json:"chainType"`         // Fundamental architecture type
	AddressFormat      AddressFormat `json:"addressFormat"`     // Address encoding format
	AddressPrefix      string        `json:"addressPrefix"`     // Address prefix (0x, bc1, cosmos1, etc.)
	RPCEndpoints       []string      `json:"rpcEndpoints"`
	WSEndpoints        []string      `json:"wsEndpoints,omitempty"`
	ExplorerURL        string        `json:"explorerUrl,omitempty"`

	// Finality parameters
	RequiredConfirmations uint64     `json:"requiredConfirmations"`
	FinalityMode         string      `json:"finalityMode"` // "probabilistic", "instant", "epoch"
	BlockTime            time.Duration `json:"blockTime"`

	// Verification parameters
	TrustThreshold       float64     `json:"trustThreshold"` // e.g., 0.67 for 2/3
	MaxClockDrift        time.Duration `json:"maxClockDrift"`
	StalenessThreshold   time.Duration `json:"stalenessThreshold"`

	// MPC/Bridge parameters
	SupportsMPC          bool        `json:"supportsMpc"`       // Supports MPC/TSS signing
	MPCCurve             string      `json:"mpcCurve"`          // secp256k1, ed25519, sr25519, bls12381
	NativeMultisig       bool        `json:"nativeMultisig"`    // Has native multisig support
	SupportsSmartContracts bool      `json:"supportsSmartContracts"`

	// Chain-specific config stored as raw bytes
	ExtraConfig          []byte      `json:"extraConfig,omitempty"`
}

// OracleDataPoint represents a price/data point from an oracle source
type OracleDataPoint struct {
	SourceChain  ChainID  `json:"sourceChain"`
	FeedID       [32]byte `json:"feedId"`
	Value        []byte   `json:"value"`        // Big-endian encoded value
	Decimals     uint8    `json:"decimals"`
	Timestamp    int64    `json:"timestamp"`
	BlockNumber  uint64   `json:"blockNumber"`

	// Source proof
	SourceProof  []byte   `json:"sourceProof"`

	// Aggregation metadata
	SourceCount  uint32   `json:"sourceCount"`
	Confidence   uint32   `json:"confidence"` // 0-10000 (basis points)
}

// ChainMetrics contains metrics for a chain adapter
type ChainMetrics struct {
	ChainID              ChainID
	LastVerifiedBlock    uint64
	TotalVerifications   uint64
	SuccessfulVerifications uint64
	FailedVerifications  uint64
	AverageLatency       time.Duration
	LastError            error
	LastErrorTime        time.Time
}
