// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"context"
	"fmt"
	"math/big"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/common/hexutil"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/rpc"
)

// EthAPI provides an Ethereum RPC API
type EthAPI struct {
	backend *MinimalEthBackend
}

// NewEthAPI creates a new Ethereum RPC API
func NewEthAPI(backend *MinimalEthBackend) *EthAPI {
	return &EthAPI{backend: backend}
}

// ChainId returns the chain ID
func (api *EthAPI) ChainId() (*hexutil.Big, error) {
	return (*hexutil.Big)(api.backend.chainConfig.ChainID), nil
}

// BlockNumber returns the current block number
func (api *EthAPI) BlockNumber() (hexutil.Uint64, error) {
	header := api.backend.blockchain.CurrentBlock()
	return hexutil.Uint64(header.Number.Uint64()), nil
}

// GetBlockByNumber returns the block for the given block number
func (api *EthAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	var block *types.Block
	if number == rpc.LatestBlockNumber {
		block = api.backend.blockchain.GetBlock(api.backend.blockchain.CurrentBlock().Hash(), api.backend.blockchain.CurrentBlock().Number.Uint64())
	} else if number == rpc.EarliestBlockNumber {
		block = api.backend.blockchain.GetBlockByNumber(0)
	} else if number == rpc.PendingBlockNumber {
		// Return current block for pending
		block = api.backend.blockchain.GetBlock(api.backend.blockchain.CurrentBlock().Hash(), api.backend.blockchain.CurrentBlock().Number.Uint64())
	} else {
		block = api.backend.blockchain.GetBlockByNumber(uint64(number))
	}

	if block == nil {
		return nil, nil
	}

	return api.rpcMarshalBlock(block, true, fullTx)
}

// GetBlockByHash returns the block for the given block hash
func (api *EthAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]interface{}, error) {
	block := api.backend.blockchain.GetBlockByHash(hash)
	if block == nil {
		return nil, nil
	}
	return api.rpcMarshalBlock(block, true, fullTx)
}

// GetBalance returns the balance of an account
func (api *EthAPI) GetBalance(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Big, error) {
	state, err := api.backend.blockchain.StateAt(api.backend.blockchain.CurrentBlock().Root)
	if err != nil {
		return nil, err
	}
	balance := state.GetBalance(address)
	return (*hexutil.Big)(balance.ToBig()), nil
}

// GetCode returns the code of a contract
func (api *EthAPI) GetCode(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	state, err := api.backend.blockchain.StateAt(api.backend.blockchain.CurrentBlock().Root)
	if err != nil {
		return nil, err
	}
	return state.GetCode(address), nil
}

// GetTransactionCount returns the nonce of an account
func (api *EthAPI) GetTransactionCount(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Uint64, error) {
	state, err := api.backend.blockchain.StateAt(api.backend.blockchain.CurrentBlock().Root)
	if err != nil {
		return nil, err
	}
	nonce := state.GetNonce(address)
	return (*hexutil.Uint64)(&nonce), nil
}

// GasPrice returns the current gas price
func (api *EthAPI) GasPrice(ctx context.Context) (*hexutil.Big, error) {
	// Return a default gas price
	return (*hexutil.Big)(big.NewInt(1000000000)), nil // 1 gwei
}

// EstimateGas estimates the gas needed for a transaction
func (api *EthAPI) EstimateGas(ctx context.Context, args TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	// Return a default gas estimate
	return hexutil.Uint64(21000), nil
}

// SendRawTransaction sends a raw transaction
func (api *EthAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}

	errs := api.backend.txPool.Add([]*types.Transaction{tx}, true)
	if errs[0] != nil {
		return common.Hash{}, errs[0]
	}

	return tx.Hash(), nil
}

// GetTransactionByHash returns a transaction by hash
func (api *EthAPI) GetTransactionByHash(ctx context.Context, hash common.Hash) (*RPCTransaction, error) {
	// Check transaction pool first
	if tx := api.backend.txPool.Get(hash); tx != nil {
		return newRPCTransaction(tx, common.Hash{}, 0, 0, nil), nil
	}

	// Check blockchain
	tx, blockHash, blockNumber, index := rawdb.ReadCanonicalTransaction(api.backend.chainDb, hash)
	if tx == nil {
		return nil, nil
	}

	// Get block for base fee
	block := api.backend.blockchain.GetBlock(blockHash, blockNumber)
	var baseFee *big.Int
	if block != nil {
		baseFee = block.BaseFee()
	}

	return newRPCTransaction(tx, blockHash, blockNumber, index, baseFee), nil
}

// GetTransactionReceipt returns the receipt of a transaction
func (api *EthAPI) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	// First get the transaction to get block info
	tx, blockHash, blockNumber, index := rawdb.ReadCanonicalTransaction(api.backend.chainDb, hash)
	if tx == nil {
		return nil, nil
	}

	// Get the receipts for the block
	block := api.backend.blockchain.GetBlock(blockHash, blockNumber)
	if block == nil {
		return nil, nil
	}
	
	receipts := rawdb.ReadReceipts(api.backend.chainDb, blockHash, blockNumber, block.Time(), api.backend.chainConfig)
	if len(receipts) <= int(index) {
		return nil, nil
	}
	
	receipt := receipts[index]

	// Get sender
	var signer types.Signer
	if tx.Protected() {
		signer = types.LatestSignerForChainID(tx.ChainId())
	} else {
		signer = types.HomesteadSigner{}
	}
	from, _ := types.Sender(signer, tx)

	fields := map[string]interface{}{
		"blockHash":         blockHash,
		"blockNumber":       hexutil.Uint64(blockNumber),
		"transactionHash":   hash,
		"transactionIndex":  hexutil.Uint64(index),
		"from":              from,
		"to":                tx.To(),
		"gasUsed":           hexutil.Uint64(receipt.GasUsed),
		"cumulativeGasUsed": hexutil.Uint64(receipt.CumulativeGasUsed),
		"contractAddress":   nil,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
		"status":            hexutil.Uint64(receipt.Status),
	}

	if receipt.ContractAddress != (common.Address{}) {
		fields["contractAddress"] = receipt.ContractAddress
	}

	return fields, nil
}

// rpcMarshalBlock converts a block to the RPC representation
func (api *EthAPI) rpcMarshalBlock(block *types.Block, inclTx bool, fullTx bool) (map[string]interface{}, error) {
	fields := map[string]interface{}{
		"number":           (*hexutil.Big)(block.Number()),
		"hash":             block.Hash(),
		"parentHash":       block.ParentHash(),
		"nonce":            block.Nonce(),
		"mixHash":          block.MixDigest(),
		"sha3Uncles":       block.UncleHash(),
		"logsBloom":        block.Bloom(),
		"stateRoot":        block.Root(),
		"miner":            block.Coinbase(),
		"difficulty":       (*hexutil.Big)(block.Difficulty()),
		"extraData":        hexutil.Bytes(block.Extra()),
		"size":             hexutil.Uint64(block.Size()),
		"gasLimit":         hexutil.Uint64(block.GasLimit()),
		"gasUsed":          hexutil.Uint64(block.GasUsed()),
		"timestamp":        hexutil.Uint64(block.Time()),
		"transactionsRoot": block.TxHash(),
		"receiptsRoot":     block.ReceiptHash(),
	}

	if inclTx {
		if fullTx {
			txs := make([]*RPCTransaction, len(block.Transactions()))
			for i, tx := range block.Transactions() {
				txs[i] = newRPCTransaction(tx, block.Hash(), block.Number().Uint64(), uint64(i), block.BaseFee())
			}
			fields["transactions"] = txs
		} else {
			txs := make([]common.Hash, len(block.Transactions()))
			for i, tx := range block.Transactions() {
				txs[i] = tx.Hash()
			}
			fields["transactions"] = txs
		}
	}

	fields["uncles"] = block.Uncles()

	if block.BaseFee() != nil {
		fields["baseFeePerGas"] = (*hexutil.Big)(block.BaseFee())
	}

	return fields, nil
}

// TransactionArgs represents the arguments for a transaction
type TransactionArgs struct {
	From     *common.Address  `json:"from"`
	To       *common.Address  `json:"to"`
	Gas      *hexutil.Uint64  `json:"gas"`
	GasPrice *hexutil.Big     `json:"gasPrice"`
	Value    *hexutil.Big     `json:"value"`
	Nonce    *hexutil.Uint64  `json:"nonce"`
	Data     *hexutil.Bytes   `json:"data"`
	Input    *hexutil.Bytes   `json:"input"`
}

// RPCTransaction represents a transaction for RPC
type RPCTransaction struct {
	BlockHash        *common.Hash      `json:"blockHash"`
	BlockNumber      *hexutil.Big      `json:"blockNumber"`
	From             common.Address    `json:"from"`
	Gas              hexutil.Uint64    `json:"gas"`
	GasPrice         *hexutil.Big      `json:"gasPrice"`
	GasFeeCap        *hexutil.Big      `json:"maxFeePerGas,omitempty"`
	GasTipCap        *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	Hash             common.Hash       `json:"hash"`
	Input            hexutil.Bytes     `json:"input"`
	Nonce            hexutil.Uint64    `json:"nonce"`
	To               *common.Address   `json:"to"`
	TransactionIndex *hexutil.Uint64   `json:"transactionIndex"`
	Value            *hexutil.Big      `json:"value"`
	Type             hexutil.Uint64    `json:"type"`
	Accesses         *types.AccessList `json:"accessList,omitempty"`
	ChainID          *hexutil.Big      `json:"chainId,omitempty"`
	V                *hexutil.Big      `json:"v"`
	R                *hexutil.Big      `json:"r"`
	S                *hexutil.Big      `json:"s"`
}

// newRPCTransaction creates an RPCTransaction from a types.Transaction
func newRPCTransaction(tx *types.Transaction, blockHash common.Hash, blockNumber uint64, index uint64, baseFee *big.Int) *RPCTransaction {
	var signer types.Signer
	if tx.Protected() {
		signer = types.LatestSignerForChainID(tx.ChainId())
	} else {
		signer = types.HomesteadSigner{}
	}

	from, _ := types.Sender(signer, tx)
	v, r, s := tx.RawSignatureValues()

	result := &RPCTransaction{
		Type:     hexutil.Uint64(tx.Type()),
		From:     from,
		Gas:      hexutil.Uint64(tx.Gas()),
		GasPrice: (*hexutil.Big)(tx.GasPrice()),
		Hash:     tx.Hash(),
		Input:    hexutil.Bytes(tx.Data()),
		Nonce:    hexutil.Uint64(tx.Nonce()),
		To:       tx.To(),
		Value:    (*hexutil.Big)(tx.Value()),
		V:        (*hexutil.Big)(v),
		R:        (*hexutil.Big)(r),
		S:        (*hexutil.Big)(s),
	}

	if blockHash != (common.Hash{}) {
		result.BlockHash = &blockHash
		result.BlockNumber = (*hexutil.Big)(big.NewInt(int64(blockNumber)))
		result.TransactionIndex = (*hexutil.Uint64)(&index)
	}

	switch tx.Type() {
	case types.AccessListTxType:
		al := tx.AccessList()
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
	case types.DynamicFeeTxType:
		al := tx.AccessList()
		result.Accesses = &al
		result.ChainID = (*hexutil.Big)(tx.ChainId())
		result.GasFeeCap = (*hexutil.Big)(tx.GasFeeCap())
		result.GasTipCap = (*hexutil.Big)(tx.GasTipCap())
		result.GasPrice = (*hexutil.Big)(tx.GasPrice())
	}

	return result
}

// NetAPI provides network RPC API
type NetAPI struct {
	networkID uint64
}

// Version returns the network ID
func (api *NetAPI) Version() string {
	return fmt.Sprintf("%d", api.networkID)
}

// Listening returns whether the node is listening for network connections
func (api *NetAPI) Listening() bool {
	return false // We don't have p2p networking
}

// PeerCount returns the number of connected peers
func (api *NetAPI) PeerCount() hexutil.Uint {
	return hexutil.Uint(0) // We don't have p2p networking
}

// Web3API provides web3 RPC API
type Web3API struct{}

// ClientVersion returns the node client version
func (api *Web3API) ClientVersion() string {
	return "Lux/v1.0.0"
}

// Sha3 returns the Keccak-256 hash of the given data
func (api *Web3API) Sha3(input hexutil.Bytes) hexutil.Bytes {
	return crypto.Keccak256(input)
}