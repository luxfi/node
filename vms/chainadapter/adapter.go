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

// Chain identifiers for supported blockchains
const (
	// Major L1s (1-20)
	ChainBitcoin   ChainID = 1
	ChainEthereum  ChainID = 2
	ChainSolana    ChainID = 3
	ChainCosmos    ChainID = 4
	ChainPolkadot  ChainID = 5
	ChainPolygon   ChainID = 6
	ChainBSC       ChainID = 7
	ChainRipple    ChainID = 8
	ChainAvalanche ChainID = 9
	ChainArbitrum  ChainID = 10
	ChainOptimism  ChainID = 11
	ChainBase      ChainID = 12
	ChainTron      ChainID = 13
	ChainCardano   ChainID = 14
	ChainNear      ChainID = 15
	ChainAptos     ChainID = 16
	ChainSui       ChainID = 17
	ChainTON       ChainID = 18
	ChainStellar   ChainID = 19
	ChainAlgorand  ChainID = 20

	// EVM L1s (21-40)
	ChainFantom    ChainID = 21
	ChainCronos    ChainID = 22
	ChainGnosis    ChainID = 23
	ChainCelo      ChainID = 24
	ChainMoonbeam  ChainID = 25
	ChainMoonriver ChainID = 26
	ChainAstar     ChainID = 27
	ChainMetis     ChainID = 28
	ChainBoba      ChainID = 29
	ChainAurora    ChainID = 30
	ChainKlaytn    ChainID = 31
	ChainFuse      ChainID = 32
	ChainEvmos     ChainID = 33
	ChainKava      ChainID = 34
	ChainOKX       ChainID = 35
	ChainPulse     ChainID = 36
	ChainCore      ChainID = 37
	ChainFlare     ChainID = 38
	ChainSongbird  ChainID = 39
	ChainRON       ChainID = 40 // Ronin

	// EVM L2s and Rollups (41-70)
	ChainZkSync     ChainID = 41
	ChainStarknet   ChainID = 42
	ChainScroll     ChainID = 43
	ChainLinea      ChainID = 44
	ChainMantle     ChainID = 45
	ChainZora       ChainID = 46
	ChainMode       ChainID = 47
	ChainBlast      ChainID = 48
	ChainManta      ChainID = 49
	ChainPolygonZk  ChainID = 50
	ChainLoopring   ChainID = 51
	ChainImmutableX ChainID = 52
	ChaindYdX       ChainID = 53
	ChainApechain   ChainID = 54
	ChainWorldchain ChainID = 55
	ChainTaiko      ChainID = 56
	ChainFrax       ChainID = 57
	ChainRedstone   ChainID = 58
	ChainLisk       ChainID = 59
	ChainBob        ChainID = 60
	ChainCyber      ChainID = 61
	ChainMint       ChainID = 62
	ChainKroma      ChainID = 63
	ChainOpBNB      ChainID = 64
	ChainXLayer     ChainID = 65
	ChainZircuit    ChainID = 66

	// Cosmos Ecosystem (71-100)
	ChainOsmosis     ChainID = 71
	ChainInjective   ChainID = 72
	ChainSei         ChainID = 73
	ChainCelestia    ChainID = 74
	ChainThorchain   ChainID = 75
	ChainAkash       ChainID = 76
	ChainJuno        ChainID = 77
	ChainStargaze    ChainID = 78
	ChainSecret      ChainID = 79
	ChainAxelar      ChainID = 80
	ChainStride      ChainID = 81
	ChainNeutron     ChainID = 82
	ChainNoble       ChainID = 83
	ChainMars        ChainID = 84
	ChainPersistence ChainID = 85
	ChainFetchAI     ChainID = 86
	ChainBand        ChainID = 87
	ChainRegen       ChainID = 88
	ChainSommelier   ChainID = 89
	ChainUmee        ChainID = 90
	ChainCanto       ChainID = 91
	ChainDymension   ChainID = 92
	ChainSaga        ChainID = 93

	// DAG-based and Unique Consensus (101-120)
	ChainHedera    ChainID = 101
	ChainIOTA      ChainID = 102
	ChainKaspa     ChainID = 103
	ChainFilecoin  ChainID = 104
	ChainICP       ChainID = 105 // Internet Computer
	ChainFlow      ChainID = 106
	ChainMina      ChainID = 107
	ChainMultiversX ChainID = 108 // Elrond
	ChainHarmony   ChainID = 109
	ChainZilliqa   ChainID = 110
	ChainVechain   ChainID = 111
	ChainTheta     ChainID = 112
	ChainEOS       ChainID = 113
	ChainWAX       ChainID = 114
	ChainTezos     ChainID = 115
	ChainNEO       ChainID = 116
	ChainQtum      ChainID = 117
	ChainWaves     ChainID = 118
	ChainOntology  ChainID = 119
	ChainRavencoin ChainID = 120

	// Bitcoin Forks and PoW Chains (121-140)
	ChainLitecoin    ChainID = 121
	ChainBitcoinCash ChainID = 122
	ChainDogecoin    ChainID = 123
	ChainZcash       ChainID = 124
	ChainMonero      ChainID = 125
	ChainDash        ChainID = 126
	ChainDecred      ChainID = 127
	ChainDigiByte    ChainID = 128
	ChainSiacoin     ChainID = 129
	ChainHorizen     ChainID = 130
	ChainErgo        ChainID = 131
	ChainFiro        ChainID = 132
	ChainKomodo      ChainID = 133
	ChainPivx        ChainID = 134
	ChainBSV         ChainID = 135 // Bitcoin SV
	ChainEtherClassic ChainID = 136
	ChainFlux        ChainID = 137
	ChainHandshake   ChainID = 138
	ChainNervos      ChainID = 139
	ChainConflux     ChainID = 140

	// Polkadot Parachains (141-160)
	ChainAcala     ChainID = 141
	ChainPhala     ChainID = 142
	ChainBifrost   ChainID = 143
	ChainParallel  ChainID = 144
	ChainClover    ChainID = 145
	ChainCentrifuge ChainID = 146
	ChainInterlay  ChainID = 147
	ChainHydra     ChainID = 148
	ChainNodle     ChainID = 149
	ChainEfinity   ChainID = 150
	ChainMangata   ChainID = 151
	ChainZeitgeist ChainID = 152
	ChainKusama    ChainID = 153
	ChainPolimec   ChainID = 154

	// Gaming and NFT Chains (161-180)
	ChainWemix     ChainID = 161
	ChainOasys     ChainID = 162
	ChainBeam      ChainID = 163
	ChainXai       ChainID = 164
	ChainSaakuru   ChainID = 165
	ChainViction   ChainID = 166 // Tomo
	ChainPlaydapp  ChainID = 167
	ChainTreasure  ChainID = 168
	ChainSkale     ChainID = 169
	ChainLoom      ChainID = 170
	ChainEnjin     ChainID = 171

	// DeFi and Finance Chains (181-200)
	ChainUnichain  ChainID = 181
	ChainSwell     ChainID = 182
	ChainEtherfi   ChainID = 183
	ChainInk       ChainID = 184
	ChainMorph     ChainID = 185
	ChainRari      ChainID = 186
	ChainShape     ChainID = 187
	ChainAbstract  ChainID = 188
	ChainSoneium   ChainID = 189
	ChainAILayer   ChainID = 190
	ChainHyperliquid ChainID = 191
	ChainBerachain ChainID = 192
	ChainMonad     ChainID = 193
	ChainMegaETH   ChainID = 194

	// Lux ecosystem
	ChainLux       ChainID = 1000 // Self-reference
)

// ChainID represents a unique blockchain identifier
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
