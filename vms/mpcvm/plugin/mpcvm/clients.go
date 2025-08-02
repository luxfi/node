// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
)

// XChainClient interface for interacting with X-Chain
type XChainClient interface {
	GetUTXOs(ctx context.Context, addrs []string) ([]*lux.UTXO, error)
	IssueTx(ctx context.Context, tx []byte) (ids.ID, error)
	GetTx(ctx context.Context, txID ids.ID) ([]byte, error)
	GetBlockByHeight(ctx context.Context, height uint64) ([]byte, error)
	GetBalance(address ids.ShortID, assetID ids.ID) (uint64, error)
}

// PChainClient interface for interacting with P-Chain
type PChainClient interface {
	GetHeight(ctx context.Context) (uint64, error)
	// TODO: Fix GetValidatorOutput type
	// GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*api.GetValidatorOutput, error)
	GetCurrentValidators(ctx context.Context, subnetID ids.ID) ([]interface{}, error)
}

// CChainClient interface for interacting with C-Chain
type CChainClient interface {
	GetHeight(ctx context.Context) (uint64, error)
	GetBlockByNumber(ctx context.Context, blockNumber uint64) (interface{}, error)
	GetTransactionReceipt(ctx context.Context, txHash string) (interface{}, error)
}

// TeleportConfig contains teleport-specific configuration
type TeleportConfig struct {
	MaxTransferAmount    uint64 `json:"maxTransferAmount"`
	MinTransferAmount    uint64 `json:"minTransferAmount"`
	TransferFeePercent   uint64 `json:"transferFeePercent"`
	SettlementBatchSize  int    `json:"settlementBatchSize"`
	SettlementTimeout    int64  `json:"settlementTimeout"`
	MaxPendingTransfers  int    `json:"maxPendingTransfers"`
	ProofValidationDelay int64  `json:"proofValidationDelay"`
}