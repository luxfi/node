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

	"github.com/luxfi/ids"
)

// Bridge adapter errors
var (
	ErrBridgeNotInitialized   = errors.New("bridge adapter not initialized")
	ErrSourceChainUnsupported = errors.New("source chain not supported")
	ErrDestChainUnsupported   = errors.New("destination chain not supported")
	ErrInvalidBridgeProof     = errors.New("invalid bridge proof")
	ErrTransferAlreadyExists  = errors.New("transfer already exists")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrInsufficientLiquidity  = errors.New("insufficient bridge liquidity")
)

// BridgeStatus represents the status of a bridge transfer
type BridgeStatus uint8

const (
	BridgeStatusPending BridgeStatus = iota
	BridgeStatusConfirmed
	BridgeStatusSigned
	BridgeStatusRelayed
	BridgeStatusCompleted
	BridgeStatusFailed
)

// String returns the string representation of BridgeStatus
func (s BridgeStatus) String() string {
	switch s {
	case BridgeStatusPending:
		return "pending"
	case BridgeStatusConfirmed:
		return "confirmed"
	case BridgeStatusSigned:
		return "signed"
	case BridgeStatusRelayed:
		return "relayed"
	case BridgeStatusCompleted:
		return "completed"
	case BridgeStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// BridgeRequest represents a cross-chain bridge request
type BridgeRequest struct {
	// Request identification
	ID            ids.ID       `json:"id"`
	Nonce         uint64       `json:"nonce"`
	CreatedAt     time.Time    `json:"createdAt"`

	// Chain routing
	SourceChain   ChainID      `json:"sourceChain"`
	DestChain     ChainID      `json:"destChain"`

	// Transfer details
	Sender        []byte       `json:"sender"`
	Recipient     []byte       `json:"recipient"`
	Asset         ids.ID       `json:"asset"`
	Amount        []byte       `json:"amount"`      // Big-endian encoded

	// Source chain proof
	SourceTxHash  [32]byte     `json:"sourceTxHash"`
	SourceBlock   uint64       `json:"sourceBlock"`
	SourceProof   *TxInclusionProof `json:"sourceProof,omitempty"`

	// Status tracking
	Status        BridgeStatus `json:"status"`
	Confirmations uint32       `json:"confirmations"`

	// MPC signature
	Signature     []byte       `json:"signature,omitempty"`
	SignedAt      time.Time    `json:"signedAt,omitempty"`

	// Destination chain completion
	DestTxHash    [32]byte     `json:"destTxHash,omitempty"`
	DestBlock     uint64       `json:"destBlock,omitempty"`
	CompletedAt   time.Time    `json:"completedAt,omitempty"`
}

// Hash returns the hash of the bridge request for signing
func (r *BridgeRequest) Hash() [32]byte {
	h := sha256.New()
	h.Write(r.ID[:])
	binary.Write(h, binary.BigEndian, r.Nonce)
	binary.Write(h, binary.BigEndian, uint32(r.SourceChain))
	binary.Write(h, binary.BigEndian, uint32(r.DestChain))
	h.Write(r.Sender)
	h.Write(r.Recipient)
	h.Write(r.Asset[:])
	h.Write(r.Amount)
	h.Write(r.SourceTxHash[:])
	binary.Write(h, binary.BigEndian, r.SourceBlock)

	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

// BridgeAdapter provides cross-chain bridge functionality using chain adapters
type BridgeAdapter struct {
	mu sync.RWMutex

	// Chain adapters for source/destination verification
	adapters map[ChainID]ChainAdapter

	// Chain registry for configurations
	registry *ChainRegistry

	// MPC wallet for signing
	wallet MPCWallet

	// Pending and completed transfers
	pendingTransfers   map[ids.ID]*BridgeRequest
	completedTransfers map[ids.ID]*BridgeRequest

	// Nonce tracking per source chain
	nonces map[ChainID]uint64

	// Configuration
	minConfirmations   map[ChainID]uint32
	maxPendingTransfers int
}

// NewBridgeAdapter creates a new bridge adapter
func NewBridgeAdapter(registry *ChainRegistry, wallet MPCWallet) *BridgeAdapter {
	return &BridgeAdapter{
		adapters:           make(map[ChainID]ChainAdapter),
		registry:           registry,
		wallet:             wallet,
		pendingTransfers:   make(map[ids.ID]*BridgeRequest),
		completedTransfers: make(map[ids.ID]*BridgeRequest),
		nonces:             make(map[ChainID]uint64),
		minConfirmations:   make(map[ChainID]uint32),
		maxPendingTransfers: 10000,
	}
}

// RegisterAdapter registers a chain adapter for bridging
func (b *BridgeAdapter) RegisterAdapter(adapter ChainAdapter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	chainID := adapter.ChainID()

	// Get chain config to set minimum confirmations
	config := b.registry.GetConfig(chainID)
	if config != nil {
		b.minConfirmations[chainID] = uint32(config.RequiredConfirmations)
	} else {
		// Default minimum confirmations based on chain type
		b.minConfirmations[chainID] = 12
	}

	b.adapters[chainID] = adapter
	return nil
}

// GetAdapter returns the adapter for a chain
func (b *BridgeAdapter) GetAdapter(chainID ChainID) (ChainAdapter, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	adapter, exists := b.adapters[chainID]
	return adapter, exists
}

// InitiateBridge initiates a new bridge transfer
func (b *BridgeAdapter) InitiateBridge(ctx context.Context, req *BridgeRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if source and destination chains are supported
	if _, exists := b.adapters[req.SourceChain]; !exists {
		return ErrSourceChainUnsupported
	}
	if _, exists := b.adapters[req.DestChain]; !exists {
		return ErrDestChainUnsupported
	}

	// Check if transfer already exists
	if _, exists := b.pendingTransfers[req.ID]; exists {
		return ErrTransferAlreadyExists
	}

	// Check pending transfer limit
	if len(b.pendingTransfers) >= b.maxPendingTransfers {
		return errors.New("too many pending transfers")
	}

	// Assign nonce
	req.Nonce = b.nonces[req.SourceChain]
	b.nonces[req.SourceChain]++

	// Set initial status
	req.Status = BridgeStatusPending
	req.CreatedAt = time.Now()

	// Store pending transfer
	b.pendingTransfers[req.ID] = req

	return nil
}

// VerifySourceTransaction verifies the source transaction and updates confirmations
func (b *BridgeAdapter) VerifySourceTransaction(ctx context.Context, reqID ids.ID) error {
	b.mu.Lock()
	req, exists := b.pendingTransfers[reqID]
	if !exists {
		b.mu.Unlock()
		return ErrTransferNotFound
	}
	adapter := b.adapters[req.SourceChain]
	minConfs := b.minConfirmations[req.SourceChain]
	b.mu.Unlock()

	// Verify source transaction inclusion
	if req.SourceProof != nil {
		if err := adapter.VerifyTransaction(ctx, req.SourceProof); err != nil {
			return fmt.Errorf("source transaction verification failed: %w", err)
		}
	}

	// Get latest finalized block to calculate confirmations
	latestBlock, err := adapter.GetLatestFinalizedBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	confirmations := uint32(0)
	if latestBlock > req.SourceBlock {
		confirmations = uint32(latestBlock - req.SourceBlock)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Update confirmations
	req.Confirmations = confirmations

	// Update status if enough confirmations
	if confirmations >= minConfs && req.Status == BridgeStatusPending {
		req.Status = BridgeStatusConfirmed
	}

	return nil
}

// SignBridgeRequest creates MPC signature for a confirmed bridge request
func (b *BridgeAdapter) SignBridgeRequest(ctx context.Context, reqID ids.ID) error {
	b.mu.Lock()
	req, exists := b.pendingTransfers[reqID]
	if !exists {
		b.mu.Unlock()
		return ErrTransferNotFound
	}

	if req.Status != BridgeStatusConfirmed {
		b.mu.Unlock()
		return fmt.Errorf("request not confirmed: status is %s", req.Status.String())
	}
	b.mu.Unlock()

	// Create message hash for signing
	hash := req.Hash()

	// Sign using MPC wallet for the destination chain
	signature, err := b.wallet.SignMessage(ctx, req.DestChain, hash[:])
	if err != nil {
		return fmt.Errorf("failed to sign bridge request: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	req.Signature = signature
	req.SignedAt = time.Now()
	req.Status = BridgeStatusSigned

	return nil
}

// GetBridgeRequest returns a bridge request by ID
func (b *BridgeAdapter) GetBridgeRequest(reqID ids.ID) (*BridgeRequest, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if req, exists := b.pendingTransfers[reqID]; exists {
		return req, nil
	}
	if req, exists := b.completedTransfers[reqID]; exists {
		return req, nil
	}
	return nil, ErrTransferNotFound
}

// ConfirmDelivery confirms that a bridge transfer was delivered on the destination chain
func (b *BridgeAdapter) ConfirmDelivery(ctx context.Context, reqID ids.ID, destTxHash [32]byte, destBlock uint64) error {
	b.mu.Lock()
	req, exists := b.pendingTransfers[reqID]
	if !exists {
		b.mu.Unlock()
		return ErrTransferNotFound
	}

	adapter := b.adapters[req.DestChain]
	b.mu.Unlock()

	// Verify destination transaction is finalized
	finalized, err := adapter.IsFinalized(ctx, destBlock)
	if err != nil {
		return fmt.Errorf("failed to check finality: %w", err)
	}
	if !finalized {
		return errors.New("destination transaction not finalized")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Update request with completion info
	req.DestTxHash = destTxHash
	req.DestBlock = destBlock
	req.CompletedAt = time.Now()
	req.Status = BridgeStatusCompleted

	// Move to completed transfers
	delete(b.pendingTransfers, reqID)
	b.completedTransfers[reqID] = req

	return nil
}

// GetPendingTransfers returns all pending transfers
func (b *BridgeAdapter) GetPendingTransfers() []*BridgeRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()

	transfers := make([]*BridgeRequest, 0, len(b.pendingTransfers))
	for _, req := range b.pendingTransfers {
		transfers = append(transfers, req)
	}
	return transfers
}

// GetTransfersForChain returns pending transfers for a specific source or destination chain
func (b *BridgeAdapter) GetTransfersForChain(chainID ChainID, isSource bool) []*BridgeRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var transfers []*BridgeRequest
	for _, req := range b.pendingTransfers {
		if isSource && req.SourceChain == chainID {
			transfers = append(transfers, req)
		} else if !isSource && req.DestChain == chainID {
			transfers = append(transfers, req)
		}
	}
	return transfers
}

// BridgeRoute represents a supported bridge route between two chains
type BridgeRoute struct {
	SourceChain     ChainID       `json:"sourceChain"`
	DestChain       ChainID       `json:"destChain"`
	SourceConfig    *ChainConfig  `json:"sourceConfig"`
	DestConfig      *ChainConfig  `json:"destConfig"`
	MinAmount       []byte        `json:"minAmount"`
	MaxAmount       []byte        `json:"maxAmount"`
	EstimatedTime   time.Duration `json:"estimatedTime"`
	BridgeFee       uint64        `json:"bridgeFee"` // In basis points (1/100 of 1%)
	Enabled         bool          `json:"enabled"`
}

// GetSupportedRoutes returns all supported bridge routes
func (b *BridgeAdapter) GetSupportedRoutes() []*BridgeRoute {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var routes []*BridgeRoute

	// Generate routes for all adapter pairs
	for sourceID := range b.adapters {
		sourceConfig := b.registry.GetConfig(sourceID)
		for destID := range b.adapters {
			if sourceID == destID {
				continue
			}

			destConfig := b.registry.GetConfig(destID)

			// Calculate estimated time based on source confirmations + dest block time
			estimatedTime := time.Duration(b.minConfirmations[sourceID]) * sourceConfig.BlockTime
			if destConfig != nil {
				estimatedTime += destConfig.BlockTime * 2 // Add some buffer
			}

			routes = append(routes, &BridgeRoute{
				SourceChain:   sourceID,
				DestChain:     destID,
				SourceConfig:  sourceConfig,
				DestConfig:    destConfig,
				EstimatedTime: estimatedTime,
				BridgeFee:     30, // 0.3% default fee
				Enabled:       true,
			})
		}
	}

	return routes
}

// BridgeMetrics contains metrics for the bridge adapter
type BridgeMetrics struct {
	TotalPending     int                      `json:"totalPending"`
	TotalCompleted   int                      `json:"totalCompleted"`
	PendingByChain   map[ChainID]int          `json:"pendingByChain"`
	CompletedByRoute map[string]int           `json:"completedByRoute"` // "source->dest" format
	AverageTime      map[string]time.Duration `json:"averageTime"`
}

// GetMetrics returns bridge metrics
func (b *BridgeAdapter) GetMetrics() *BridgeMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()

	metrics := &BridgeMetrics{
		TotalPending:     len(b.pendingTransfers),
		TotalCompleted:   len(b.completedTransfers),
		PendingByChain:   make(map[ChainID]int),
		CompletedByRoute: make(map[string]int),
		AverageTime:      make(map[string]time.Duration),
	}

	// Count pending by chain
	for _, req := range b.pendingTransfers {
		metrics.PendingByChain[req.SourceChain]++
	}

	// Count completed by route
	routeTimes := make(map[string][]time.Duration)
	for _, req := range b.completedTransfers {
		route := fmt.Sprintf("%d->%d", req.SourceChain, req.DestChain)
		metrics.CompletedByRoute[route]++

		if !req.CompletedAt.IsZero() && !req.CreatedAt.IsZero() {
			duration := req.CompletedAt.Sub(req.CreatedAt)
			routeTimes[route] = append(routeTimes[route], duration)
		}
	}

	// Calculate average times
	for route, times := range routeTimes {
		if len(times) > 0 {
			var total time.Duration
			for _, t := range times {
				total += t
			}
			metrics.AverageTime[route] = total / time.Duration(len(times))
		}
	}

	return metrics
}

// CrossChainAsset represents an asset that can be bridged
type CrossChainAsset struct {
	AssetID        ids.ID            `json:"assetId"`
	Name           string            `json:"name"`
	Symbol         string            `json:"symbol"`
	Decimals       uint8             `json:"decimals"`
	NativeChain    ChainID           `json:"nativeChain"`      // Chain where asset is native
	WrappedAddresses map[ChainID][]byte `json:"wrappedAddresses"` // Wrapped addresses on other chains
}

// AssetRegistry tracks bridgeable assets across chains
type AssetRegistry struct {
	mu     sync.RWMutex
	assets map[ids.ID]*CrossChainAsset
}

// NewAssetRegistry creates a new asset registry
func NewAssetRegistry() *AssetRegistry {
	return &AssetRegistry{
		assets: make(map[ids.ID]*CrossChainAsset),
	}
}

// RegisterAsset registers a cross-chain asset
func (r *AssetRegistry) RegisterAsset(asset *CrossChainAsset) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assets[asset.AssetID] = asset
}

// GetAsset returns an asset by ID
func (r *AssetRegistry) GetAsset(assetID ids.ID) (*CrossChainAsset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	asset, exists := r.assets[assetID]
	return asset, exists
}

// GetWrappedAddress returns the wrapped address for an asset on a specific chain
func (r *AssetRegistry) GetWrappedAddress(assetID ids.ID, chainID ChainID) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	asset, exists := r.assets[assetID]
	if !exists {
		return nil, errors.New("asset not found")
	}

	if asset.NativeChain == chainID {
		return nil, nil // Native asset, no wrapped address
	}

	address, exists := asset.WrappedAddresses[chainID]
	if !exists {
		return nil, fmt.Errorf("no wrapped address for chain %d", chainID)
	}

	return address, nil
}

// GetAssetsByChain returns all assets that have addresses on a chain
func (r *AssetRegistry) GetAssetsByChain(chainID ChainID) []*CrossChainAsset {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var assets []*CrossChainAsset
	for _, asset := range r.assets {
		if asset.NativeChain == chainID {
			assets = append(assets, asset)
		} else if _, exists := asset.WrappedAddresses[chainID]; exists {
			assets = append(assets, asset)
		}
	}
	return assets
}
