// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/luxfi/ids"
)

// Errors for MPC wallet operations
var (
	ErrNoKeyForChain      = errors.New("no key available for chain")
	ErrUnsupportedCurve   = errors.New("unsupported curve for chain")
	ErrSigningFailed      = errors.New("signing failed")
	ErrInvalidAddress     = errors.New("invalid address format")
	ErrKeyDerivationFailed = errors.New("key derivation failed")
)

// SigningCurve represents the elliptic curve used for signing
type SigningCurve string

const (
	// CurveSecp256k1 is used by Bitcoin, Ethereum, and most EVM chains
	CurveSecp256k1 SigningCurve = "secp256k1"
	// CurveEd25519 is used by Solana, NEAR, Cardano, Polkadot, etc.
	CurveEd25519 SigningCurve = "ed25519"
	// CurveSr25519 is used by Polkadot/Substrate (Schnorr)
	CurveSr25519 SigningCurve = "sr25519"
	// CurveBLS12381 is used for BLS signatures (Ethereum 2.0, aggregation)
	CurveBLS12381 SigningCurve = "bls12381"
	// CurveRistretto is used by some privacy chains
	CurveRistretto SigningCurve = "ristretto255"
)

// GetSigningCurve returns the signing curve for a chain type
func GetSigningCurve(chainType ChainType) SigningCurve {
	switch chainType {
	case ChainTypeEVM, ChainTypeUTXO:
		return CurveSecp256k1
	case ChainTypeAccount, ChainTypeMoveVM, ChainTypeStellar, ChainTypeAlgorand,
		ChainTypeCardano, ChainTypeTezos, ChainTypeTVM:
		return CurveEd25519
	case ChainTypeSubstrate:
		return CurveSr25519
	case ChainTypeCosmosSDK:
		return CurveSecp256k1 // Cosmos uses secp256k1 by default
	default:
		return CurveSecp256k1
	}
}

// MPCKeyShare represents a threshold key share for MPC signing
type MPCKeyShare struct {
	Index       int          `json:"index"`
	Threshold   int          `json:"threshold"`
	TotalShares int          `json:"totalShares"`
	Curve       SigningCurve `json:"curve"`
	ShareBytes  []byte       `json:"shareBytes"`
	PublicKey   []byte       `json:"publicKey"`    // Corresponding public key share
	GroupKey    []byte       `json:"groupKey"`     // Group/combined public key
}

// ChainKeySet holds keys for a specific chain
type ChainKeySet struct {
	ChainID     ChainID      `json:"chainId"`
	ChainType   ChainType    `json:"chainType"`
	Curve       SigningCurve `json:"curve"`
	PublicKey   []byte       `json:"publicKey"`   // Derived public key for this chain
	Address     string       `json:"address"`     // Chain-specific address
	MPCShare    *MPCKeyShare `json:"mpcShare,omitempty"`
	DerivePath  string       `json:"derivePath"`  // BIP44/derivation path
}

// MPCWallet represents a multi-chain MPC wallet that can sign for any supported chain
type MPCWallet interface {
	// GetAddress returns the wallet address for a specific chain
	GetAddress(chainID ChainID) (string, error)

	// GetPublicKey returns the public key for a specific chain
	GetPublicKey(chainID ChainID) ([]byte, error)

	// SignMessage signs a message for a specific chain using MPC
	SignMessage(ctx context.Context, chainID ChainID, message []byte) ([]byte, error)

	// SignTransaction signs a transaction for a specific chain using MPC
	SignTransaction(ctx context.Context, chainID ChainID, tx *UnsignedTransaction) (*SignedTransaction, error)

	// GetSupportedChains returns all chains this wallet can sign for
	GetSupportedChains() []ChainID

	// HasKeyForChain checks if the wallet has a key for the given chain
	HasKeyForChain(chainID ChainID) bool

	// GetChainKeySet returns the key set for a specific chain
	GetChainKeySet(chainID ChainID) (*ChainKeySet, error)
}

// UnsignedTransaction represents a transaction to be signed
type UnsignedTransaction struct {
	ChainID   ChainID `json:"chainId"`
	Nonce     uint64  `json:"nonce"`
	To        []byte  `json:"to"`
	Value     []byte  `json:"value"`     // Big-endian encoded
	Data      []byte  `json:"data"`
	GasLimit  uint64  `json:"gasLimit"`
	GasPrice  []byte  `json:"gasPrice"`  // Big-endian encoded

	// EVM-specific fields
	MaxFeePerGas         []byte `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas []byte `json:"maxPriorityFeePerGas,omitempty"`
	AccessList           []byte `json:"accessList,omitempty"`

	// Chain-specific raw transaction bytes
	RawTxBytes []byte `json:"rawTxBytes,omitempty"`
}

// SignedTransaction represents a signed transaction
type SignedTransaction struct {
	ChainID      ChainID  `json:"chainId"`
	TxHash       [32]byte `json:"txHash"`
	Signature    []byte   `json:"signature"`
	SignedTxBytes []byte  `json:"signedTxBytes"` // Complete signed transaction
	SignerAddress string  `json:"signerAddress"`
}

// MultiChainMPCWallet implements MPCWallet for multi-chain operations
type MultiChainMPCWallet struct {
	mu sync.RWMutex

	// Master key information
	masterPublicKey []byte
	threshold       int
	totalShares     int

	// Per-chain key sets
	chainKeys map[ChainID]*ChainKeySet

	// Chain configurations
	chainConfigs map[ChainID]*ChainConfig

	// MPC signer interface
	signer MPCSigner
}

// MPCSigner is the interface for MPC signing operations
type MPCSigner interface {
	// SignShare creates a signature share for the given message
	SignShare(ctx context.Context, message []byte, curve SigningCurve) ([]byte, error)

	// AggregateShares combines signature shares into a final signature
	AggregateShares(ctx context.Context, message []byte, shares [][]byte, curve SigningCurve) ([]byte, error)

	// GetPublicShare returns this signer's public share
	GetPublicShare(curve SigningCurve) []byte

	// GetGroupPublicKey returns the combined group public key
	GetGroupPublicKey(curve SigningCurve) []byte

	// DeriveChildKey derives a child key for a specific path
	DeriveChildKey(path string, curve SigningCurve) ([]byte, error)
}

// NewMultiChainMPCWallet creates a new multi-chain MPC wallet
func NewMultiChainMPCWallet(signer MPCSigner, threshold, totalShares int) *MultiChainMPCWallet {
	return &MultiChainMPCWallet{
		threshold:    threshold,
		totalShares:  totalShares,
		chainKeys:    make(map[ChainID]*ChainKeySet),
		chainConfigs: make(map[ChainID]*ChainConfig),
		signer:       signer,
	}
}

// InitializeChain initializes keys for a specific chain
func (w *MultiChainMPCWallet) InitializeChain(config *ChainConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	curve := GetSigningCurve(config.ChainType)

	// Derive chain-specific key using BIP44-style path
	// m/purpose'/coin_type'/account'/change/address_index
	derivePath := fmt.Sprintf("m/44'/%d'/0'/0/0", config.EVMChainID)
	if config.EVMChainID == 0 {
		// Use internal chain ID for non-EVM chains
		derivePath = fmt.Sprintf("m/44'/%d'/0'/0/0", config.ChainID)
	}

	publicKey, err := w.signer.DeriveChildKey(derivePath, curve)
	if err != nil {
		return fmt.Errorf("failed to derive key for chain %s: %w", config.Name, err)
	}

	// Generate address from public key
	address, err := w.deriveAddress(config, publicKey)
	if err != nil {
		return fmt.Errorf("failed to derive address for chain %s: %w", config.Name, err)
	}

	keySet := &ChainKeySet{
		ChainID:    config.ChainID,
		ChainType:  config.ChainType,
		Curve:      curve,
		PublicKey:  publicKey,
		Address:    address,
		DerivePath: derivePath,
		MPCShare: &MPCKeyShare{
			Curve:      curve,
			Threshold:  w.threshold,
			TotalShares: w.totalShares,
			GroupKey:   w.signer.GetGroupPublicKey(curve),
			PublicKey:  w.signer.GetPublicShare(curve),
		},
	}

	w.chainKeys[config.ChainID] = keySet
	w.chainConfigs[config.ChainID] = config

	return nil
}

// deriveAddress derives a chain-specific address from a public key
func (w *MultiChainMPCWallet) deriveAddress(config *ChainConfig, publicKey []byte) (string, error) {
	switch config.ChainType {
	case ChainTypeEVM:
		return w.deriveEVMAddress(publicKey)
	case ChainTypeUTXO:
		return w.deriveBitcoinAddress(publicKey, config)
	case ChainTypeAccount:
		return w.deriveAccountAddress(publicKey, config)
	case ChainTypeCosmosSDK:
		return w.deriveCosmosAddress(publicKey, config)
	case ChainTypeSubstrate:
		return w.deriveSubstrateAddress(publicKey, config)
	default:
		// Generic hex encoding for unknown types
		return "0x" + hex.EncodeToString(publicKey), nil
	}
}

// deriveEVMAddress derives an Ethereum-style address from a secp256k1 public key
func (w *MultiChainMPCWallet) deriveEVMAddress(publicKey []byte) (string, error) {
	// For secp256k1, derive from the last 20 bytes of public key
	if len(publicKey) < 33 {
		return "", ErrInvalidAddress
	}

	// Use last 20 bytes of public key as address
	// Full Keccak256 derivation is used when integrated with geth
	addressBytes := publicKey[len(publicKey)-20:]
	return "0x" + hex.EncodeToString(addressBytes), nil
}

// deriveBitcoinAddress derives a Bitcoin-style address
func (w *MultiChainMPCWallet) deriveBitcoinAddress(publicKey []byte, config *ChainConfig) (string, error) {
	// Simplified - in production implement proper Base58Check or Bech32
	return config.AddressPrefix + hex.EncodeToString(publicKey[:20]), nil
}

// deriveAccountAddress derives an account-based address (Solana, NEAR, etc.)
func (w *MultiChainMPCWallet) deriveAccountAddress(publicKey []byte, config *ChainConfig) (string, error) {
	// For Ed25519 chains, the public key often IS the address (base58 encoded)
	// Simplified - in production implement proper base58 encoding
	return hex.EncodeToString(publicKey), nil
}

// deriveCosmosAddress derives a Cosmos SDK Bech32 address
func (w *MultiChainMPCWallet) deriveCosmosAddress(publicKey []byte, config *ChainConfig) (string, error) {
	// Simplified - in production implement proper Bech32 encoding
	prefix := config.AddressPrefix
	if prefix == "" {
		prefix = "cosmos"
	}
	return prefix + "1" + hex.EncodeToString(publicKey[:20]), nil
}

// deriveSubstrateAddress derives a Substrate SS58 address
func (w *MultiChainMPCWallet) deriveSubstrateAddress(publicKey []byte, config *ChainConfig) (string, error) {
	// Simplified - in production implement proper SS58 encoding
	return hex.EncodeToString(publicKey), nil
}

// GetAddress returns the wallet address for a specific chain
func (w *MultiChainMPCWallet) GetAddress(chainID ChainID) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	keySet, exists := w.chainKeys[chainID]
	if !exists {
		return "", ErrNoKeyForChain
	}

	return keySet.Address, nil
}

// GetPublicKey returns the public key for a specific chain
func (w *MultiChainMPCWallet) GetPublicKey(chainID ChainID) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	keySet, exists := w.chainKeys[chainID]
	if !exists {
		return nil, ErrNoKeyForChain
	}

	return keySet.PublicKey, nil
}

// SignMessage signs a message for a specific chain using MPC
func (w *MultiChainMPCWallet) SignMessage(ctx context.Context, chainID ChainID, message []byte) ([]byte, error) {
	w.mu.RLock()
	keySet, exists := w.chainKeys[chainID]
	w.mu.RUnlock()

	if !exists {
		return nil, ErrNoKeyForChain
	}

	// Create signature share
	share, err := w.signer.SignShare(ctx, message, keySet.Curve)
	if err != nil {
		return nil, fmt.Errorf("failed to create signature share: %w", err)
	}

	// In a real MPC system, we would collect shares from other parties
	// and aggregate them. For now, we assume single-party or the signer
	// handles aggregation internally.
	return share, nil
}

// SignTransaction signs a transaction for a specific chain using MPC
func (w *MultiChainMPCWallet) SignTransaction(ctx context.Context, chainID ChainID, tx *UnsignedTransaction) (*SignedTransaction, error) {
	w.mu.RLock()
	keySet, exists := w.chainKeys[chainID]
	config, configExists := w.chainConfigs[chainID]
	w.mu.RUnlock()

	if !exists || !configExists {
		return nil, ErrNoKeyForChain
	}

	// Prepare message to sign based on chain type
	var signingMessage []byte
	switch config.ChainType {
	case ChainTypeEVM:
		signingMessage = prepareEVMSigningMessage(tx)
	case ChainTypeUTXO:
		signingMessage = prepareUTXOSigningMessage(tx)
	default:
		signingMessage = tx.RawTxBytes
	}

	// Sign the message
	signature, err := w.signer.SignShare(ctx, signingMessage, keySet.Curve)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Create signed transaction
	signedTx := &SignedTransaction{
		ChainID:       chainID,
		Signature:     signature,
		SignerAddress: keySet.Address,
	}

	// Compute transaction hash
	copy(signedTx.TxHash[:], signingMessage[:32])

	return signedTx, nil
}

// GetSupportedChains returns all chains this wallet can sign for
func (w *MultiChainMPCWallet) GetSupportedChains() []ChainID {
	w.mu.RLock()
	defer w.mu.RUnlock()

	chains := make([]ChainID, 0, len(w.chainKeys))
	for chainID := range w.chainKeys {
		chains = append(chains, chainID)
	}
	return chains
}

// HasKeyForChain checks if the wallet has a key for the given chain
func (w *MultiChainMPCWallet) HasKeyForChain(chainID ChainID) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	_, exists := w.chainKeys[chainID]
	return exists
}

// GetChainKeySet returns the key set for a specific chain
func (w *MultiChainMPCWallet) GetChainKeySet(chainID ChainID) (*ChainKeySet, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	keySet, exists := w.chainKeys[chainID]
	if !exists {
		return nil, ErrNoKeyForChain
	}

	return keySet, nil
}

// Helper functions for transaction signing

func prepareEVMSigningMessage(tx *UnsignedTransaction) []byte {
	// In production, this would RLP encode the transaction and hash it
	// For now, return raw tx bytes or a hash of the transaction data
	if len(tx.RawTxBytes) > 0 {
		return tx.RawTxBytes
	}
	return tx.Data
}

func prepareUTXOSigningMessage(tx *UnsignedTransaction) []byte {
	// In production, this would create proper sighash for UTXO inputs
	return tx.RawTxBytes
}

// BridgeWalletAdapter adapts MPCWallet for bridge operations
type BridgeWalletAdapter struct {
	wallet     MPCWallet
	registry   *ChainRegistry
}

// NewBridgeWalletAdapter creates a bridge wallet adapter
func NewBridgeWalletAdapter(wallet MPCWallet, registry *ChainRegistry) *BridgeWalletAdapter {
	return &BridgeWalletAdapter{
		wallet:   wallet,
		registry: registry,
	}
}

// GetBridgeAddress returns the bridge custody address for a chain
func (b *BridgeWalletAdapter) GetBridgeAddress(chainID ChainID) (string, error) {
	return b.wallet.GetAddress(chainID)
}

// SignBridgeTransfer signs a bridge transfer transaction
func (b *BridgeWalletAdapter) SignBridgeTransfer(ctx context.Context, transfer *BridgeTransfer) (*SignedBridgeTransfer, error) {
	// Get destination chain config
	config := b.registry.GetConfig(transfer.DestChain)
	if config == nil {
		return nil, ErrChainNotSupported
	}

	// Create unsigned transaction for the destination chain
	unsignedTx := &UnsignedTransaction{
		ChainID:    transfer.DestChain,
		To:         transfer.Recipient,
		Value:      transfer.Amount,
		Data:       transfer.Data,
		RawTxBytes: transfer.RawTxBytes,
	}

	// Sign the transaction
	signedTx, err := b.wallet.SignTransaction(ctx, transfer.DestChain, unsignedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign bridge transfer: %w", err)
	}

	return &SignedBridgeTransfer{
		Transfer:      transfer,
		SignedTx:      signedTx,
		BridgeAddress: signedTx.SignerAddress,
	}, nil
}

// BridgeTransfer represents a cross-chain bridge transfer
type BridgeTransfer struct {
	ID          ids.ID  `json:"id"`
	SourceChain ChainID `json:"sourceChain"`
	DestChain   ChainID `json:"destChain"`
	Sender      []byte  `json:"sender"`
	Recipient   []byte  `json:"recipient"`
	Asset       ids.ID  `json:"asset"`
	Amount      []byte  `json:"amount"` // Big-endian encoded
	Data        []byte  `json:"data"`
	RawTxBytes  []byte  `json:"rawTxBytes"`
}

// SignedBridgeTransfer represents a signed bridge transfer
type SignedBridgeTransfer struct {
	Transfer      *BridgeTransfer    `json:"transfer"`
	SignedTx      *SignedTransaction `json:"signedTx"`
	BridgeAddress string             `json:"bridgeAddress"`
}

// ChainRegistry manages chain configurations
type ChainRegistry struct {
	mu      sync.RWMutex
	configs map[ChainID]*ChainConfig
}

// NewChainRegistry creates a new chain registry
func NewChainRegistry() *ChainRegistry {
	return &ChainRegistry{
		configs: make(map[ChainID]*ChainConfig),
	}
}

// Register registers a chain configuration
func (r *ChainRegistry) Register(config *ChainConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[config.ChainID] = config
}

// GetConfig returns the configuration for a chain
func (r *ChainRegistry) GetConfig(chainID ChainID) *ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.configs[chainID]
}

// GetAllConfigs returns all registered chain configurations
func (r *ChainRegistry) GetAllConfigs() []*ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	configs := make([]*ChainConfig, 0, len(r.configs))
	for _, config := range r.configs {
		configs = append(configs, config)
	}
	return configs
}

// GetEVMChains returns all EVM-compatible chains
func (r *ChainRegistry) GetEVMChains() []*ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var evmChains []*ChainConfig
	for _, config := range r.configs {
		if config.IsEVM || config.ChainType == ChainTypeEVM {
			evmChains = append(evmChains, config)
		}
	}
	return evmChains
}

// GetNativeChains returns all non-EVM chains (native primary networks)
func (r *ChainRegistry) GetNativeChains() []*ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var nativeChains []*ChainConfig
	for _, config := range r.configs {
		if !config.IsEVM && config.ChainType != ChainTypeEVM {
			nativeChains = append(nativeChains, config)
		}
	}
	return nativeChains
}

// GetChainsByType returns chains of a specific type
func (r *ChainRegistry) GetChainsByType(chainType ChainType) []*ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var chains []*ChainConfig
	for _, config := range r.configs {
		if config.ChainType == chainType {
			chains = append(chains, config)
		}
	}
	return chains
}

