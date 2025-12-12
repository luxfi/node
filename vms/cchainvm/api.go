// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"

	"github.com/holiman/uint256"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/common/hexutil"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/tracing"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/rpc"
)

// Migrated state storage prefix - stores imported account state for direct query
var migratedStatePrefix = []byte("migrated-account-")

// migratedAccountKey returns the key for storing migrated account state
func migratedAccountKey(address common.Address) []byte {
	return append(migratedStatePrefix, address.Bytes()...)
}

// MigratedAccountState stores account state imported via migrate API
type MigratedAccountState struct {
	Balance  *big.Int          `json:"balance"`
	Nonce    uint64            `json:"nonce"`
	Code     []byte            `json:"code,omitempty"`
	Storage  map[string]string `json:"storage,omitempty"`
}

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
// If historical blocks are available, returns the highest available block number
func (api *EthAPI) BlockNumber() (hexutil.Uint64, error) {
	header := api.backend.blockchain.CurrentBlock()
	canonicalHeight := header.Number.Uint64()

	// Check if we have historical blocks beyond the canonical chain
	historicalHeight := api.getHighestHistoricalBlock()
	if historicalHeight > canonicalHeight {
		return hexutil.Uint64(historicalHeight), nil
	}

	return hexutil.Uint64(canonicalHeight), nil
}

// getHighestHistoricalBlock returns the highest block number in historical storage
func (api *EthAPI) getHighestHistoricalBlock() uint64 {
	// Check database for highest canonical hash
	// Start from a high number and work down to find the highest stored block
	// Use a binary search-like approach for efficiency
	high := uint64(2000000) // Start with 2M as upper bound
	low := uint64(0)

	// Quick check: see if we have block 1000000
	if hash := rawdb.ReadCanonicalHash(api.backend.chainDb, 1000000); hash != (common.Hash{}) {
		low = 1000000
	}
	if hash := rawdb.ReadCanonicalHash(api.backend.chainDb, 1100000); hash != (common.Hash{}) {
		low = 1100000
	}

	// Binary search to find highest block
	for low < high {
		mid := (low + high + 1) / 2
		hash := rawdb.ReadCanonicalHash(api.backend.chainDb, mid)
		if hash != (common.Hash{}) {
			low = mid
		} else {
			high = mid - 1
		}
	}

	return low
}

// GetBlockByNumber returns the block for the given block number
// Checks both canonical chain and historical blocks database
func (api *EthAPI) GetBlockByNumber(ctx context.Context, number rpc.BlockNumber, fullTx bool) (map[string]interface{}, error) {
	var block *types.Block
	if number == rpc.LatestBlockNumber {
		// For latest, check if we have historical blocks that are higher
		historicalHeight := api.getHighestHistoricalBlock()
		canonicalHeight := api.backend.blockchain.CurrentBlock().Number.Uint64()
		if historicalHeight > canonicalHeight {
			block = api.getHistoricalBlock(historicalHeight)
		} else {
			block = api.backend.blockchain.GetBlock(api.backend.blockchain.CurrentBlock().Hash(), canonicalHeight)
		}
	} else if number == rpc.EarliestBlockNumber {
		block = api.backend.blockchain.GetBlockByNumber(0)
		if block == nil {
			block = api.getHistoricalBlock(0)
		}
	} else if number == rpc.PendingBlockNumber {
		// Return current block for pending
		block = api.backend.blockchain.GetBlock(api.backend.blockchain.CurrentBlock().Hash(), api.backend.blockchain.CurrentBlock().Number.Uint64())
	} else {
		// Try canonical chain first
		block = api.backend.blockchain.GetBlockByNumber(uint64(number))
		// If not found, try historical blocks
		if block == nil {
			block = api.getHistoricalBlock(uint64(number))
		}
	}

	if block == nil {
		return nil, nil
	}

	return api.rpcMarshalBlock(block, true, fullTx)
}

// getHistoricalBlock retrieves a block from the database that may not be in the canonical chain
func (api *EthAPI) getHistoricalBlock(number uint64) *types.Block {
	// Read canonical hash for this block number
	hash := rawdb.ReadCanonicalHash(api.backend.chainDb, number)
	if hash == (common.Hash{}) {
		return nil
	}

	// Read header and body from database
	header := rawdb.ReadHeader(api.backend.chainDb, hash, number)
	if header == nil {
		return nil
	}

	body := rawdb.ReadBody(api.backend.chainDb, hash, number)
	if body == nil {
		// Return block with just header if no body
		return types.NewBlockWithHeader(header)
	}

	return types.NewBlockWithHeader(header).WithBody(*body)
}

// GetBlockByHash returns the block for the given block hash
// Checks both canonical chain and historical blocks database
func (api *EthAPI) GetBlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (map[string]interface{}, error) {
	// Try canonical chain first
	block := api.backend.blockchain.GetBlockByHash(hash)
	// If not found, try historical blocks
	if block == nil {
		block = api.getHistoricalBlockByHash(hash)
	}
	if block == nil {
		return nil, nil
	}
	return api.rpcMarshalBlock(block, true, fullTx)
}

// hashToNumberCache caches hash -> block number mappings for faster lookups
var hashToNumberCache = make(map[common.Hash]uint64)
var hashToNumberCacheBuilding = false
var hashToNumberCacheBuilt = false
var hashCacheMutex sync.RWMutex

// getHistoricalBlockByHash retrieves a block by hash from the database
// Uses the same approach as getHistoricalBlock (by number) since the hash->number
// index may be in a different database wrapper
func (api *EthAPI) getHistoricalBlockByHash(hash common.Hash) *types.Block {
	// Check cache first (read lock)
	hashCacheMutex.RLock()
	if num, ok := hashToNumberCache[hash]; ok {
		hashCacheMutex.RUnlock()
		return api.getHistoricalBlock(num)
	}
	cacheBuilt := hashToNumberCacheBuilt
	cacheBuilding := hashToNumberCacheBuilding
	hashCacheMutex.RUnlock()

	// If cache is built and hash not found, return nil
	if cacheBuilt {
		return nil
	}

	// If cache is already building, just wait for it rather than doing linear search
	if cacheBuilding {
		return nil
	}

	high := api.getHighestHistoricalBlock()
	if high == 0 {
		return nil
	}

	// Start building cache in background if not already building
	hashCacheMutex.Lock()
	if !hashToNumberCacheBuilding && !hashToNumberCacheBuilt {
		hashToNumberCacheBuilding = true
		hashCacheMutex.Unlock()
		
		// Build cache asynchronously
		go func() {
			fmt.Printf("Starting async hash index build for %d blocks...\n", high)
			for num := uint64(0); num <= high; num++ {
				block := api.getHistoricalBlock(num)
				if block != nil {
					hashCacheMutex.Lock()
					hashToNumberCache[block.Hash()] = num
					hashCacheMutex.Unlock()
				}
				// Print progress every 50000 blocks
				if num > 0 && num%50000 == 0 {
					fmt.Printf("Building hash index: %d/%d blocks\n", num, high)
				}
			}
			hashCacheMutex.Lock()
			hashToNumberCacheBuilt = true
			hashToNumberCacheBuilding = false
			hashCacheMutex.Unlock()
			fmt.Printf("Hash index complete: %d entries\n", len(hashToNumberCache))
		}()
	} else {
		hashCacheMutex.Unlock()
	}

	// Return nil for now - the cache is building in background
	// Subsequent calls will find the hash in cache once built
	return nil
}

// GetBalance returns the balance of an account
// First checks migrated state (from imported blocks), then falls back to canonical state
func (api *EthAPI) GetBalance(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (*hexutil.Big, error) {
	// Check migrated state first (imported via migrate_importBlocks)
	if api.backend.chainDb != nil {
		key := migratedAccountKey(address)
		if data, err := api.backend.chainDb.Get(key); err == nil && len(data) > 0 {
			var migrated MigratedAccountState
			if err := json.Unmarshal(data, &migrated); err == nil && migrated.Balance != nil {
				return (*hexutil.Big)(migrated.Balance), nil
			}
		}
	}

	// Fall back to canonical state
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

// SendTransaction sends a transaction (creates and signs if needed)
func (api *EthAPI) SendTransaction(ctx context.Context, args TransactionArgs) (common.Hash, error) {
	// For now, return an error as we don't have account management
	return common.Hash{}, fmt.Errorf("eth_sendTransaction not supported - use eth_sendRawTransaction")
}

// Call executes a message call immediately without creating a transaction
func (api *EthAPI) Call(ctx context.Context, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	// Return empty result for now
	// In production, would execute EVM call
	return hexutil.Bytes{}, nil
}

// GetStorageAt returns the value from a storage position at a given address
func (api *EthAPI) GetStorageAt(ctx context.Context, address common.Address, key string, blockNrOrHash rpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	state, err := api.backend.blockchain.StateAt(api.backend.blockchain.CurrentBlock().Root)
	if err != nil {
		return nil, err
	}
	
	// Parse the key
	keyHash := common.HexToHash(key)
	val := state.GetState(address, keyHash)
	return val[:], nil
}

// Accounts returns the list of accounts owned by the client
func (api *EthAPI) Accounts() []common.Address {
	// Return empty list as we don't manage accounts
	return []common.Address{}
}

// Sign signs data with the given account
func (api *EthAPI) Sign(addr common.Address, data hexutil.Bytes) (hexutil.Bytes, error) {
	return nil, fmt.Errorf("eth_sign not supported")
}

// SignTransaction signs a transaction
func (api *EthAPI) SignTransaction(args TransactionArgs) (hexutil.Bytes, error) {
	return nil, fmt.Errorf("eth_signTransaction not supported")
}

// GetLogs returns logs matching the filter criteria
func (api *EthAPI) GetLogs(ctx context.Context, criteria map[string]interface{}) ([]*types.Log, error) {
	// Return empty logs for now
	// In production, would query logs from blockchain
	return []*types.Log{}, nil
}

// Mining returns whether the client is actively mining
func (api *EthAPI) Mining() bool {
	return false
}

// Hashrate returns the client's hashrate
func (api *EthAPI) Hashrate() hexutil.Uint64 {
	return hexutil.Uint64(0)
}

// Syncing returns the syncing status
func (api *EthAPI) Syncing() (interface{}, error) {
	// Return false to indicate not syncing
	return false, nil
}

// GetUncleCountByBlockHash returns the number of uncles in a block by hash
func (api *EthAPI) GetUncleCountByBlockHash(ctx context.Context, hash common.Hash) *hexutil.Uint {
	block := api.backend.blockchain.GetBlockByHash(hash)
	if block == nil {
		return nil
	}
	count := hexutil.Uint(len(block.Uncles()))
	return &count
}

// GetUncleCountByBlockNumber returns the number of uncles in a block by number  
func (api *EthAPI) GetUncleCountByBlockNumber(ctx context.Context, number rpc.BlockNumber) *hexutil.Uint {
	var block *types.Block
	if number == rpc.LatestBlockNumber {
		block = api.backend.blockchain.GetBlock(api.backend.blockchain.CurrentBlock().Hash(), api.backend.blockchain.CurrentBlock().Number.Uint64())
	} else {
		block = api.backend.blockchain.GetBlockByNumber(uint64(number))
	}
	if block == nil {
		return nil
	}
	count := hexutil.Uint(len(block.Uncles()))
	return &count
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
		"nonce":            block.Header().Nonce,
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
	From     *common.Address `json:"from"`
	To       *common.Address `json:"to"`
	Gas      *hexutil.Uint64 `json:"gas"`
	GasPrice *hexutil.Big    `json:"gasPrice"`
	Value    *hexutil.Big    `json:"value"`
	Nonce    *hexutil.Uint64 `json:"nonce"`
	Data     *hexutil.Bytes  `json:"data"`
	Input    *hexutil.Bytes  `json:"input"`
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

// TxPoolAPI provides transaction pool RPC API
type TxPoolAPI struct {
	backend *MinimalEthBackend
}

// NewTxPoolAPI creates a new TxPool RPC API
func NewTxPoolAPI(backend *MinimalEthBackend) *TxPoolAPI {
	return &TxPoolAPI{backend: backend}
}

// Status returns the status of the transaction pool
func (api *TxPoolAPI) Status() map[string]hexutil.Uint {
	// Return mock status for now - in production would query actual tx pool
	return map[string]hexutil.Uint{
		"pending": hexutil.Uint(0),
		"queued":  hexutil.Uint(0),
	}
}

// Content returns the transactions in the pool
func (api *TxPoolAPI) Content() map[string]map[string]map[string]interface{} {
	// Return empty content for now
	return map[string]map[string]map[string]interface{}{
		"pending": {},
		"queued":  {},
	}
}

// Inspect returns a summary of the pool
func (api *TxPoolAPI) Inspect() map[string]map[string]map[string]string {
	return map[string]map[string]map[string]string{
		"pending": {},
		"queued":  {},
	}
}

// DebugAPI provides debug RPC API
type DebugAPI struct {
	backend *MinimalEthBackend
}

// NewDebugAPI creates a new Debug RPC API
func NewDebugAPI(backend *MinimalEthBackend) *DebugAPI {
	return &DebugAPI{backend: backend}
}

// TraceTransaction returns the trace of a transaction
func (api *DebugAPI) TraceTransaction(ctx context.Context, txHash common.Hash) (interface{}, error) {
	// Return a basic trace structure for now
	return map[string]interface{}{
		"gas":         hexutil.Uint64(21000),
		"returnValue": hexutil.Bytes{},
		"structLogs":  []interface{}{},
	}, nil
}

// TraceBlock returns traces for all transactions in a block
func (api *DebugAPI) TraceBlock(ctx context.Context, blockRlp hexutil.Bytes) (interface{}, error) {
	// Return empty traces for now
	return []interface{}{}, nil
}

// TraceBlockByNumber returns traces for all transactions in a block by number
func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber) (interface{}, error) {
	return []interface{}{}, nil
}

// TraceBlockByHash returns traces for all transactions in a block by hash
func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash) (interface{}, error) {
	return []interface{}{}, nil
}

// LuxAPI provides Lux-specific RPC API
type LuxAPI struct {
	vm *VM
}

// NewLuxAPI creates a new Lux RPC API
func NewLuxAPI(vm *VM) *LuxAPI {
	return &LuxAPI{vm: vm}
}

// ReplayStatus returns the status of database replay
func (api *LuxAPI) ReplayStatus() map[string]interface{} {
	// CRITICAL FIX: Check if vm and replayProgress are initialized
	if api.vm == nil || api.vm.replayProgress == nil {
		// Return a safe default status if not initialized
		return map[string]interface{}{
			"replaying": false,
			"state":     "not_initialized",
			"error":     "VM or replay progress not initialized",
		}
	}
	return api.vm.replayProgress.GetStatus()
}

// StartReplayRequest represents the request for starting database replay
type StartReplayRequest struct {
	SourcePath string `json:"sourcePath"`
	StartBlock uint64 `json:"startBlock"`
	EndBlock   uint64 `json:"endBlock"`
	BatchSize  uint64 `json:"batchSize"`
}

// ReplayStart starts the database replay process from the specified source
// This is the lux_replayStart RPC method
func (api *LuxAPI) ReplayStart(req StartReplayRequest) (map[string]interface{}, error) {
	// CRITICAL FIX: Check if vm is initialized
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	// CRITICAL FIX: Initialize replayProgress if it's nil
	if api.vm.replayProgress == nil {
		api.vm.replayProgress = NewReplayProgress()
	}

	// Check if replay is already in progress
	if api.vm.replayProgress.IsReplaying() {
		return nil, fmt.Errorf("replay already in progress")
	}

	// Use default source if not provided
	if req.SourcePath == "" {
		req.SourcePath = "/home/z/.lux-cli/runs/network_current/node1/chainData/dnmzhuf6poM6PUNQCe7MWWfBdTJEnddhHRNXz2x7H6qSmyBEJ/db/pebbledb"
	}

	// Check if source exists
	if _, err := os.Stat(req.SourcePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source path does not exist: %s", req.SourcePath)
	}

	// Set default values if not provided
	if req.EndBlock == 0 {
		req.EndBlock = 1100000
	}
	if req.BatchSize == 0 {
		req.BatchSize = 100
	}

	// Start replay in background goroutine
	go func() {
		api.vm.log.Info("Starting runtime replay via RPC",
			"source", req.SourcePath,
			"start", req.StartBlock,
			"end", req.EndBlock,
			"batch", req.BatchSize)

		// Update progress
		api.vm.replayProgress.Start(req.EndBlock - req.StartBlock)
		api.vm.replayProgress.UpdatePhase("Initializing replay")

		// Create the replay coordinator
		config := &DatabaseReplayConfig{
			SourcePath:          req.SourcePath,
			NetReplayEnabled: true,
			NetReplayStart:   req.StartBlock,
			NetReplayEnd:     req.EndBlock,
			NetReplayBatch:   req.BatchSize,
			CopyAllState:        true,
			DatabaseType:        "pebbledb",
		}

		// Run the actual replay
		if err := api.vm.RunReplay(config); err != nil {
			api.vm.log.Error("Replay failed", "error", err)
			api.vm.replayProgress.UpdatePhase(fmt.Sprintf("Failed: %v", err))
		} else {
			api.vm.replayProgress.UpdatePhase("Completed successfully")
			api.vm.log.Info("Replay completed successfully")
		}
	}()

	return map[string]interface{}{
		"started":     true,
		"sourcePath":  req.SourcePath,
		"startBlock":  req.StartBlock,
		"endBlock":    req.EndBlock,
		"batchSize":   req.BatchSize,
		"message":     "Replay started in background",
	}, nil
}

// ReloadBlockchain forces the blockchain to reload and recognize database changes
// This is useful after replay or direct database modifications
func (api *LuxAPI) ReloadBlockchain() (map[string]interface{}, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	// Ensure logger
	api.vm.ensureLogger()

	result := make(map[string]interface{})

	// Get current state before reload
	var beforeBlock uint64
	if api.vm.blockChain != nil {
		if currentBlock := api.vm.blockChain.CurrentBlock(); currentBlock != nil {
			beforeBlock = currentBlock.Number.Uint64()
			result["beforeBlockNumber"] = beforeBlock
			result["beforeBlockHash"] = currentBlock.Hash().Hex()
		}
	}

	// Get database head
	dbHeadHash := rawdb.ReadHeadBlockHash(api.vm.ethDB)
	dbHeadNumber := uint64(0)
	if dbHeadHash != (common.Hash{}) {
		if num, ok := rawdb.ReadHeaderNumber(api.vm.ethDB, dbHeadHash); ok {
			dbHeadNumber = num
		}
	}
	result["databaseHeadNumber"] = dbHeadNumber
	result["databaseHeadHash"] = dbHeadHash.Hex()

	// Perform reload
	err := api.vm.ReloadBlockchainState()
	if err != nil {
		result["success"] = false
		result["error"] = err.Error()
	} else {
		result["success"] = true
	}

	// Get state after reload
	if api.vm.blockChain != nil {
		if currentBlock := api.vm.blockChain.CurrentBlock(); currentBlock != nil {
			result["afterBlockNumber"] = currentBlock.Number.Uint64()
			result["afterBlockHash"] = currentBlock.Hash().Hex()
			result["blocksRecovered"] = currentBlock.Number.Uint64() - beforeBlock
		}
	}

	return result, nil
}

// VerifyBlockchain checks the integrity of the blockchain
func (api *LuxAPI) VerifyBlockchain() (map[string]interface{}, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	// Ensure logger
	api.vm.ensureLogger()

	result := make(map[string]interface{})

	// Run integrity check
	err := api.vm.VerifyBlockchainIntegrity()
	if err != nil {
		result["healthy"] = false
		result["error"] = err.Error()
	} else {
		result["healthy"] = true
	}

	// Add current state info
	if api.vm.blockChain != nil {
		if currentBlock := api.vm.blockChain.CurrentBlock(); currentBlock != nil {
			result["currentBlockNumber"] = currentBlock.Number.Uint64()
			result["currentBlockHash"] = currentBlock.Hash().Hex()
			result["stateRoot"] = currentBlock.Root.Hex()
		}

		if currentHeader := api.vm.blockChain.CurrentHeader(); currentHeader != nil {
			result["currentHeaderNumber"] = currentHeader.Number.Uint64()
			result["currentHeaderHash"] = currentHeader.Hash().Hex()
		}
	}

	// Add database head info
	dbHeadHash := rawdb.ReadHeadBlockHash(api.vm.ethDB)
	if dbHeadHash != (common.Hash{}) {
		result["databaseHeadHash"] = dbHeadHash.Hex()
		if num, ok := rawdb.ReadHeaderNumber(api.vm.ethDB, dbHeadHash); ok {
			result["databaseHeadNumber"] = num
		}
	}

	return result, nil
}

// SetHead forces the blockchain to set its head to a specific block number
// This is useful after importing blocks via migrate_importBlocks
// Accessible as lux_setHead
func (api *LuxAPI) SetHead(blockNumber uint64) (map[string]interface{}, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	api.vm.ensureLogger()
	result := make(map[string]interface{})

	// Get current state
	currentBlock := api.vm.blockChain.CurrentBlock()
	if currentBlock != nil {
		result["beforeBlockNumber"] = currentBlock.Number.Uint64()
		result["beforeBlockHash"] = currentBlock.Hash().Hex()
	}

	api.vm.log.Info("Setting blockchain head", "targetBlock", blockNumber)

	// Use SetHead to rewind/forward the chain
	if err := api.vm.blockChain.SetHead(blockNumber); err != nil {
		result["success"] = false
		result["error"] = fmt.Sprintf("SetHead failed: %v", err)
		return result, nil
	}

	// Get new state
	newBlock := api.vm.blockChain.CurrentBlock()
	if newBlock != nil {
		result["afterBlockNumber"] = newBlock.Number.Uint64()
		result["afterBlockHash"] = newBlock.Hash().Hex()
	}

	result["success"] = true
	result["targetBlock"] = blockNumber

	api.vm.log.Info("Blockchain head set successfully",
		"targetBlock", blockNumber,
		"newHead", newBlock.Number.Uint64(),
	)

	return result, nil
}

// Mine manually triggers block building and acceptance for dev mode
// This is useful when the consensus engine isn't running to auto-mine
// Accessible as lux_mine
func (api *LuxAPI) Mine() (map[string]interface{}, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	api.vm.ensureLogger()
	result := make(map[string]interface{})

	// Check pending transactions
	pending := api.vm.txPool.Pending(txpool.PendingFilter{})
	pendingCount := 0
	for _, batch := range pending {
		pendingCount += len(batch)
	}
	result["pendingTxs"] = pendingCount

	// Get current block before mining
	currentBlock := api.vm.blockChain.CurrentBlock()
	if currentBlock != nil {
		result["beforeBlockNumber"] = currentBlock.Number.Uint64()
		result["beforeBlockHash"] = currentBlock.Hash().Hex()
	}

	// Build a new block
	ctx := context.Background()
	blk, err := api.vm.BuildBlock(ctx)
	if err != nil {
		result["success"] = false
		result["error"] = fmt.Sprintf("failed to build block: %v", err)
		return result, nil
	}

	// Accept the block
	if err := blk.Accept(ctx); err != nil {
		result["success"] = false
		result["error"] = fmt.Sprintf("failed to accept block: %v", err)
		return result, nil
	}

	// Get new block state
	newBlock := api.vm.blockChain.CurrentBlock()
	if newBlock != nil {
		result["afterBlockNumber"] = newBlock.Number.Uint64()
		result["afterBlockHash"] = newBlock.Hash().Hex()
	}

	result["success"] = true
	result["blockID"] = blk.ID().String()
	api.vm.log.Info("Mined new block", "height", result["afterBlockNumber"], "id", blk.ID().String())

	return result, nil
}

// MigrateAPI provides migration RPC API for block import operations
type MigrateAPI struct {
	vm *VM
}

// NewMigrateAPI creates a new Migrate RPC API
func NewMigrateAPI(vm *VM) *MigrateAPI {
	return &MigrateAPI{vm: vm}
}

// ImportBlockEntry represents a single block to import
type ImportBlockEntry struct {
	Height       uint64                       `json:"height"`
	Hash         string                       `json:"hash"`
	Header       string                       `json:"header"`
	Body         string                       `json:"body"`
	Receipts     string                       `json:"receipts"`
	StateChanges map[string]*ImportAccountState `json:"stateChanges,omitempty"`
}

// ImportAccountState represents account state to import
type ImportAccountState struct {
	Balance  string            `json:"balance"`
	Nonce    uint64            `json:"nonce"`
	Code     string            `json:"code,omitempty"`
	Storage  map[string]string `json:"storage,omitempty"`
	CodeHash string            `json:"codeHash,omitempty"`
}

// ImportBlocksRequest represents the request for importing blocks
type ImportBlocksRequest struct {
	Blocks []ImportBlockEntry `json:"blocks"`
}

// ImportBlocksResponse represents the response from importing blocks
type ImportBlocksResponse struct {
	Imported      int      `json:"imported"`
	Failed        int      `json:"failed"`
	FirstHeight   uint64   `json:"firstHeight"`
	LastHeight    uint64   `json:"lastHeight"`
	Errors        []string `json:"errors,omitempty"`
	HeadBlockHash string   `json:"headBlockHash"`
}

// Helper functions for constructing database keys
var (
	headerPrefix = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
)

// encodeBlockNumberForKey encodes a block number as big endian uint64 for database keys
func encodeBlockNumberForKey(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerKey constructs the header key: headerPrefix + num (uint64 big endian) + hash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append(headerPrefix, encodeBlockNumberForKey(number)...), hash.Bytes()...)
}

// ImportBlocks imports an array of blocks into the C-Chain database
// Each block should have header, body, and receipts as RLP hex-encoded data
// This is the migrate_importBlocks RPC method
// Note: Accepts array directly (not wrapped in struct) for CLI compatibility
func (api *MigrateAPI) ImportBlocks(blocks []ImportBlockEntry) (*ImportBlocksResponse, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	if api.vm.ethDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("no blocks provided")
	}

	api.vm.ensureLogger()
	api.vm.log.Info("Starting block import via RPC", "count", len(blocks))

	response := &ImportBlocksResponse{
		Errors: []string{},
	}

	var firstHeight, lastHeight uint64
	firstSet := false

	for _, entry := range blocks {
		// Track height range
		if !firstSet {
			firstHeight = entry.Height
			firstSet = true
		}
		lastHeight = entry.Height

		// Decode hex strings to bytes
		headerBytes, err := decodeHex(entry.Header)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to decode header hex: %v", entry.Height, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		bodyBytes, err := decodeHex(entry.Body)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to decode body hex: %v", entry.Height, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		receiptsBytes, err := decodeHex(entry.Receipts)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to decode receipts hex: %v", entry.Height, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Decode block hash
		hashBytes, err := decodeHex(entry.Hash)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to decode hash: %v", entry.Height, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Write raw RLP directly to database keys (bypass types.Header decoding)
		// This avoids WithdrawalsHash compatibility issues with pre-Shanghai blocks
		hash := common.BytesToHash(hashBytes)
		number := entry.Height

		// Write header RLP manually
		// First write the hash -> number mapping
		rawdb.WriteHeaderNumber(api.vm.ethDB, hash, number)

		// Then write the header RLP data
		key := headerKey(number, hash)
		if err := api.vm.ethDB.Put(key, headerBytes); err != nil {
			errMsg := fmt.Sprintf("height %d: failed to write header: %v", entry.Height, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Write body RLP
		rawdb.WriteBodyRLP(api.vm.ethDB, hash, number, bodyBytes)

		// Write receipts RLP
		if len(receiptsBytes) > 0 {
			rawdb.WriteRawReceipts(api.vm.ethDB, hash, number, receiptsBytes)
		}

		// Write canonical hash
		rawdb.WriteCanonicalHash(api.vm.ethDB, hash, number)

		// Write transaction lookup index for eth_getTransactionByHash
		if len(bodyBytes) > 0 {
			var body types.Body
			if err := rlp.DecodeBytes(bodyBytes, &body); err == nil && len(body.Transactions) > 0 {
				txHashes := make([]common.Hash, len(body.Transactions))
				for i, tx := range body.Transactions {
					txHashes[i] = tx.Hash()
				}
				rawdb.WriteTxLookupEntries(api.vm.ethDB, number, txHashes)
			}
		}

		// Import state changes if provided
		if entry.StateChanges != nil && len(entry.StateChanges) > 0 {
			// Decode header to get state root
			var header types.Header
			if err := rlp.DecodeBytes(headerBytes, &header); err == nil {
				if err := api.importStateChanges(entry.StateChanges, &header); err != nil {
					api.vm.log.Warn("Failed to import state changes",
						"height", entry.Height,
						"error", err)
				}
			}
		}

		response.Imported++

		// Log progress periodically
		if response.Imported%1000 == 0 {
			api.vm.log.Info("Import progress", "imported", response.Imported, "current", entry.Height)
		}
	}

	// Update head pointers to the last successfully imported block
	if response.Imported > 0 {
		// Find the last successfully imported block
		for i := len(blocks) - 1; i >= 0; i-- {
			entry := blocks[i]
			hashBytes, err := decodeHex(entry.Hash)
			if err == nil {
				hash := common.BytesToHash(hashBytes)
				rawdb.WriteHeadBlockHash(api.vm.ethDB, hash)
				rawdb.WriteHeadHeaderHash(api.vm.ethDB, hash)
				response.HeadBlockHash = hash.Hex()
				break
			}
		}
	}

	response.FirstHeight = firstHeight
	response.LastHeight = lastHeight

	api.vm.log.Info("Block import complete",
		"imported", response.Imported,
		"failed", response.Failed,
		"firstHeight", firstHeight,
		"lastHeight", lastHeight,
	)

	return response, nil
}

// decodeHex decodes a hex string (with or without 0x prefix) to bytes
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return []byte{}, nil
	}
	return hex.DecodeString(s)
}

// importStateChanges imports state changes into both the state trie and a key-value store
// The key-value store allows direct balance queries without requiring proper state root matching
func (api *MigrateAPI) importStateChanges(stateChanges map[string]*ImportAccountState, header *types.Header) error {
	// Get state database from blockchain
	if api.vm.blockChain == nil {
		return fmt.Errorf("blockchain not initialized")
	}

	// Create state database at the block's state root
	stateDB, err := api.vm.blockChain.StateAt(header.Root)
	if err != nil {
		// If state doesn't exist yet, create from empty root
		stateDB, err = state.New(types.EmptyRootHash, api.vm.blockChain.StateCache())
		if err != nil {
			return fmt.Errorf("failed to create state DB: %w", err)
		}
	}

	// Apply each account's state changes to both state trie and key-value store
	storedCount := 0
	for addrHex, accountState := range stateChanges {
		addr := common.HexToAddress(addrHex)

		// Parse balance
		var balance *big.Int
		if accountState.Balance != "" {
			balance, _ = new(big.Int).SetString(accountState.Balance, 0)
		}
		if balance == nil {
			balance = big.NewInt(0)
		}

		// Set balance in state trie (convert big.Int to uint256)
		stateDB.SetBalance(addr, uint256.MustFromBig(balance), tracing.BalanceChangeUnspecified)

		// Set nonce
		stateDB.SetNonce(addr, accountState.Nonce, tracing.NonceChangeUnspecified)

		// Parse code
		var code []byte
		if accountState.Code != "" {
			code, _ = decodeHex(accountState.Code)
		}

		// Set code if provided
		if len(code) > 0 {
			stateDB.SetCode(addr, code, tracing.CodeChangeUnspecified)
		}

		// Set storage if provided
		if accountState.Storage != nil {
			for keyHex, valueHex := range accountState.Storage {
				key := common.HexToHash(keyHex)
				value := common.HexToHash(valueHex)
				stateDB.SetState(addr, key, value)
			}
		}

		// Also store in key-value store for direct queries (bypasses state root issues)
		migratedState := MigratedAccountState{
			Balance: balance,
			Nonce:   accountState.Nonce,
			Code:    code,
			Storage: accountState.Storage,
		}
		if data, err := json.Marshal(migratedState); err == nil {
			key := migratedAccountKey(addr)
			if err := api.vm.ethDB.Put(key, data); err != nil {
				api.vm.log.Warn("Failed to store migrated account", "address", addr.Hex(), "error", err)
			} else {
				storedCount++
			}
		}
	}

	api.vm.log.Info("Stored migrated state in key-value store", "accounts", storedCount, "block", header.Number.Uint64())

	// Commit the state changes (deleteEmptyObjects=true, snaps=false, lastProcessedChunk=true)
	newRoot, err := stateDB.Commit(header.Number.Uint64(), true, true)
	if err != nil {
		return fmt.Errorf("failed to commit state: %w", err)
	}

	// Write trie to database
	if err := api.vm.blockChain.StateCache().TrieDB().Commit(newRoot, false); err != nil {
		return fmt.Errorf("failed to commit trie: %w", err)
	}

	// Verify the state root matches (for validation)
	if newRoot != header.Root {
		api.vm.log.Warn("State root mismatch after import",
			"expected", header.Root.Hex(),
			"got", newRoot.Hex(),
			"height", header.Number.Uint64())
	}

	return nil
}

// SetGenesisRequest represents the request for setting genesis from imported block 0
type SetGenesisRequest struct {
	Height   uint64 `json:"height"`
	Hash     string `json:"hash"`
	Header   string `json:"header"`
	Body     string `json:"body"`
	Receipts string `json:"receipts"`
}

// SetGenesisResponse represents the response from setting genesis
type SetGenesisResponse struct {
	Success        bool   `json:"success"`
	OldGenesisHash string `json:"oldGenesisHash"`
	NewGenesisHash string `json:"newGenesisHash"`
	Error          string `json:"error,omitempty"`
}

// SetGenesis sets the genesis block from imported data and reloads the blockchain
// This should be called BEFORE importing other blocks to ensure the chain is consistent
// This is the migrate_setGenesis RPC method
func (api *MigrateAPI) SetGenesis(req SetGenesisRequest) (*SetGenesisResponse, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	if api.vm.ethDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	if req.Height != 0 {
		return nil, fmt.Errorf("genesis block must have height 0, got %d", req.Height)
	}

	api.vm.ensureLogger()
	api.vm.log.Info("Setting genesis from imported block 0")

	response := &SetGenesisResponse{}

	// Get the old genesis hash for reference
	if api.vm.blockChain != nil {
		genesis := api.vm.blockChain.GetBlockByNumber(0)
		if genesis != nil {
			response.OldGenesisHash = genesis.Hash().Hex()
		}
	}

	// Decode the new genesis data
	headerBytes, err := decodeHex(req.Header)
	if err != nil {
		return nil, fmt.Errorf("failed to decode header hex: %v", err)
	}

	bodyBytes, err := decodeHex(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode body hex: %v", err)
	}

	receiptsBytes, err := decodeHex(req.Receipts)
	if err != nil {
		return nil, fmt.Errorf("failed to decode receipts hex: %v", err)
	}

	hashBytes, err := decodeHex(req.Hash)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hash: %v", err)
	}

	hash := common.BytesToHash(hashBytes)
	number := uint64(0)

	api.vm.log.Info("Writing new genesis block",
		"hash", hash.Hex(),
		"headerSize", len(headerBytes),
		"bodySize", len(bodyBytes))

	// Write the genesis block to the database
	rawdb.WriteHeaderNumber(api.vm.ethDB, hash, number)

	// Write header RLP
	key := headerKey(number, hash)
	if err := api.vm.ethDB.Put(key, headerBytes); err != nil {
		return nil, fmt.Errorf("failed to write header: %v", err)
	}

	// Write body
	rawdb.WriteBodyRLP(api.vm.ethDB, hash, number, bodyBytes)

	// Write receipts
	if len(receiptsBytes) > 0 {
		rawdb.WriteRawReceipts(api.vm.ethDB, hash, number, receiptsBytes)
	}

	// Write canonical hash for block 0
	rawdb.WriteCanonicalHash(api.vm.ethDB, hash, number)

	// Update head pointers to genesis
	rawdb.WriteHeadBlockHash(api.vm.ethDB, hash)
	rawdb.WriteHeadHeaderHash(api.vm.ethDB, hash)

	response.NewGenesisHash = hash.Hex()
	response.Success = true

	api.vm.log.Info("Genesis block set successfully",
		"oldHash", response.OldGenesisHash,
		"newHash", response.NewGenesisHash)

	return response, nil
}

// JSONBlockTransaction represents a transaction in JSON-RPC format
type JSONBlockTransaction struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	From             string `json:"from"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	MaxFeePerGas     string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFee   string `json:"maxPriorityFeePerGas,omitempty"`
	Hash             string `json:"hash"`
	Input            string `json:"input"`
	Nonce            string `json:"nonce"`
	To               string `json:"to"`
	TransactionIndex string `json:"transactionIndex"`
	Value            string `json:"value"`
	Type             string `json:"type,omitempty"`
	ChainId          string `json:"chainId,omitempty"`
	V                string `json:"v"`
	R                string `json:"r"`
	S                string `json:"s"`
	// Access list for EIP-2930/EIP-1559 transactions
	AccessList json.RawMessage `json:"accessList,omitempty"`
}

// JSONBlock represents a block in JSON-RPC format (eth_getBlockByNumber response)
type JSONBlock struct {
	BaseFeePerGas    string                 `json:"baseFeePerGas,omitempty"`
	BlockGasCost     string                 `json:"blockGasCost,omitempty"`
	Difficulty       string                 `json:"difficulty"`
	ExtraData        string                 `json:"extraData"`
	GasLimit         string                 `json:"gasLimit"`
	GasUsed          string                 `json:"gasUsed"`
	Hash             string                 `json:"hash"`
	LogsBloom        string                 `json:"logsBloom"`
	Miner            string                 `json:"miner"`
	MixHash          string                 `json:"mixHash"`
	Nonce            string                 `json:"nonce"`
	Number           string                 `json:"number"`
	ParentHash       string                 `json:"parentHash"`
	ReceiptsRoot     string                 `json:"receiptsRoot"`
	Sha3Uncles       string                 `json:"sha3Uncles"`
	StateRoot        string                 `json:"stateRoot"`
	Timestamp        string                 `json:"timestamp"`
	TransactionsRoot string                 `json:"transactionsRoot"`
	Transactions     []JSONBlockTransaction `json:"transactions"`
	Uncles           []string               `json:"uncles"`
}

// ImportJSONBlocksResponse represents the response from importing JSON blocks
type ImportJSONBlocksResponse struct {
	Imported      int      `json:"imported"`
	Failed        int      `json:"failed"`
	FirstHeight   uint64   `json:"firstHeight"`
	LastHeight    uint64   `json:"lastHeight"`
	Errors        []string `json:"errors,omitempty"`
	HeadBlockHash string   `json:"headBlockHash"`
}

// ImportJSONBlocks imports an array of blocks in JSON-RPC format into the C-Chain database
// This accepts the raw output from eth_getBlockByNumber and converts to RLP internally
// This is the migrate_importJSONBlocks RPC method
func (api *MigrateAPI) ImportJSONBlocks(blocks []json.RawMessage) (*ImportJSONBlocksResponse, error) {
	if api.vm == nil {
		return nil, fmt.Errorf("VM not initialized")
	}

	if api.vm.ethDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("no blocks provided")
	}

	api.vm.ensureLogger()
	api.vm.log.Info("Starting JSON block import via RPC", "count", len(blocks))

	response := &ImportJSONBlocksResponse{
		Errors: []string{},
	}

	var firstHeight, lastHeight uint64
	firstSet := false

	for i, rawBlock := range blocks {
		var block JSONBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			errMsg := fmt.Sprintf("block %d: failed to parse JSON: %v", i, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Parse block number
		blockNum, err := hexutil.DecodeUint64(block.Number)
		if err != nil {
			errMsg := fmt.Sprintf("block %d: failed to parse number: %v", i, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Track height range
		if !firstSet {
			firstHeight = blockNum
			firstSet = true
		}
		lastHeight = blockNum

		// Convert JSON block to types.Header
		header, err := api.jsonToHeader(&block)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to convert header: %v", blockNum, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Encode header to RLP
		headerRLP, err := rlp.EncodeToBytes(header)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to encode header: %v", blockNum, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Convert transactions to types.Transactions for body
		txs, err := api.jsonToTransactions(block.Transactions)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to convert transactions: %v", blockNum, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Create body (transactions + uncles)
		body := &types.Body{
			Transactions: txs,
			Uncles:       []*types.Header{}, // Uncles are typically empty in Lux
		}

		// Encode body to RLP
		bodyRLP, err := rlp.EncodeToBytes(body)
		if err != nil {
			errMsg := fmt.Sprintf("height %d: failed to encode body: %v", blockNum, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Get hash from the block
		hash := common.HexToHash(block.Hash)

		// Write to database
		rawdb.WriteHeaderNumber(api.vm.ethDB, hash, blockNum)

		// Write header RLP
		key := headerKey(blockNum, hash)
		if err := api.vm.ethDB.Put(key, headerRLP); err != nil {
			errMsg := fmt.Sprintf("height %d: failed to write header: %v", blockNum, err)
			response.Errors = append(response.Errors, errMsg)
			response.Failed++
			continue
		}

		// Write body RLP
		rawdb.WriteBodyRLP(api.vm.ethDB, hash, blockNum, bodyRLP)

		// Write canonical hash
		rawdb.WriteCanonicalHash(api.vm.ethDB, hash, blockNum)

		response.Imported++

		// Log progress periodically
		if response.Imported%1000 == 0 {
			api.vm.log.Info("JSON import progress", "imported", response.Imported, "current", blockNum)
		}
	}

	// Update head pointers to the last successfully imported block
	if response.Imported > 0 && lastHeight > 0 {
		// Find the last block's hash
		for i := len(blocks) - 1; i >= 0; i-- {
			var block JSONBlock
			if err := json.Unmarshal(blocks[i], &block); err == nil {
				hash := common.HexToHash(block.Hash)
				rawdb.WriteHeadBlockHash(api.vm.ethDB, hash)
				rawdb.WriteHeadHeaderHash(api.vm.ethDB, hash)
				response.HeadBlockHash = hash.Hex()
				break
			}
		}
	}

	response.FirstHeight = firstHeight
	response.LastHeight = lastHeight

	api.vm.log.Info("JSON block import complete",
		"imported", response.Imported,
		"failed", response.Failed,
		"firstHeight", firstHeight,
		"lastHeight", lastHeight,
	)

	// Auto-reload blockchain state so eth_blockNumber reflects imported blocks
	if response.Imported > 0 {
		api.vm.log.Info("Reloading blockchain state after import...")
		if err := api.vm.ReloadBlockchainState(); err != nil {
			api.vm.log.Warn("Failed to reload blockchain state", "error", err)
		} else {
			// Get the new block number after reload
			if api.vm.blockChain != nil {
				if currentBlock := api.vm.blockChain.CurrentBlock(); currentBlock != nil {
					api.vm.log.Info("Blockchain reloaded", "newHeight", currentBlock.Number.Uint64())
				}
			}
		}
	}

	return response, nil
}

// jsonToHeader converts a JSONBlock to a types.Header
func (api *MigrateAPI) jsonToHeader(block *JSONBlock) (*types.Header, error) {
	header := &types.Header{}

	// Parse all fields
	if block.ParentHash != "" {
		header.ParentHash = common.HexToHash(block.ParentHash)
	}
	if block.Sha3Uncles != "" {
		header.UncleHash = common.HexToHash(block.Sha3Uncles)
	}
	if block.Miner != "" {
		header.Coinbase = common.HexToAddress(block.Miner)
	}
	if block.StateRoot != "" {
		header.Root = common.HexToHash(block.StateRoot)
	}
	if block.TransactionsRoot != "" {
		header.TxHash = common.HexToHash(block.TransactionsRoot)
	}
	if block.ReceiptsRoot != "" {
		header.ReceiptHash = common.HexToHash(block.ReceiptsRoot)
	}
	if block.LogsBloom != "" {
		bloomBytes, err := decodeHex(block.LogsBloom)
		if err != nil {
			return nil, fmt.Errorf("failed to decode logsBloom: %v", err)
		}
		copy(header.Bloom[:], bloomBytes)
	}
	if block.Difficulty != "" {
		difficulty, err := hexutil.DecodeBig(block.Difficulty)
		if err != nil {
			return nil, fmt.Errorf("failed to decode difficulty: %v", err)
		}
		header.Difficulty = difficulty
	}
	if block.Number != "" {
		number, err := hexutil.DecodeBig(block.Number)
		if err != nil {
			return nil, fmt.Errorf("failed to decode number: %v", err)
		}
		header.Number = number
	}
	if block.GasLimit != "" {
		gasLimit, err := hexutil.DecodeUint64(block.GasLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to decode gasLimit: %v", err)
		}
		header.GasLimit = gasLimit
	}
	if block.GasUsed != "" {
		gasUsed, err := hexutil.DecodeUint64(block.GasUsed)
		if err != nil {
			return nil, fmt.Errorf("failed to decode gasUsed: %v", err)
		}
		header.GasUsed = gasUsed
	}
	if block.Timestamp != "" {
		timestamp, err := hexutil.DecodeUint64(block.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to decode timestamp: %v", err)
		}
		header.Time = timestamp
	}
	if block.ExtraData != "" {
		extraData, err := decodeHex(block.ExtraData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode extraData: %v", err)
		}
		header.Extra = extraData
	}
	if block.MixHash != "" {
		header.MixDigest = common.HexToHash(block.MixHash)
	}
	if block.Nonce != "" {
		nonceBytes, err := decodeHex(block.Nonce)
		if err != nil {
			return nil, fmt.Errorf("failed to decode nonce: %v", err)
		}
		if len(nonceBytes) == 8 {
			copy(header.Nonce[:], nonceBytes)
		}
	}
	if block.BaseFeePerGas != "" {
		baseFee, err := hexutil.DecodeBig(block.BaseFeePerGas)
		if err != nil {
			return nil, fmt.Errorf("failed to decode baseFeePerGas: %v", err)
		}
		header.BaseFee = baseFee
	}

	return header, nil
}

// jsonToTransactions converts JSON transactions to types.Transactions
func (api *MigrateAPI) jsonToTransactions(jsonTxs []JSONBlockTransaction) (types.Transactions, error) {
	txs := make(types.Transactions, 0, len(jsonTxs))

	for i, jtx := range jsonTxs {
		tx, err := api.jsonToTransaction(&jtx)
		if err != nil {
			return nil, fmt.Errorf("tx %d: %v", i, err)
		}
		txs = append(txs, tx)
	}

	return txs, nil
}

// jsonToTransaction converts a single JSON transaction to types.Transaction
func (api *MigrateAPI) jsonToTransaction(jtx *JSONBlockTransaction) (*types.Transaction, error) {
	// Parse common fields
	nonce, err := hexutil.DecodeUint64(jtx.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %v", err)
	}

	gas, err := hexutil.DecodeUint64(jtx.Gas)
	if err != nil {
		return nil, fmt.Errorf("failed to decode gas: %v", err)
	}

	value, err := hexutil.DecodeBig(jtx.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value: %v", err)
	}

	data, err := decodeHex(jtx.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode input: %v", err)
	}

	var to *common.Address
	if jtx.To != "" {
		addr := common.HexToAddress(jtx.To)
		to = &addr
	}

	// Parse signature
	v, err := hexutil.DecodeBig(jtx.V)
	if err != nil {
		return nil, fmt.Errorf("failed to decode v: %v", err)
	}
	r, err := hexutil.DecodeBig(jtx.R)
	if err != nil {
		return nil, fmt.Errorf("failed to decode r: %v", err)
	}
	s, err := hexutil.DecodeBig(jtx.S)
	if err != nil {
		return nil, fmt.Errorf("failed to decode s: %v", err)
	}

	// Determine transaction type
	var txType uint8
	if jtx.Type != "" {
		typeVal, err := hexutil.DecodeUint64(jtx.Type)
		if err == nil {
			txType = uint8(typeVal)
		}
	}

	var tx *types.Transaction

	switch txType {
	case types.LegacyTxType:
		// Legacy transaction
		gasPrice, err := hexutil.DecodeBig(jtx.GasPrice)
		if err != nil {
			return nil, fmt.Errorf("failed to decode gasPrice: %v", err)
		}

		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gas,
			To:       to,
			Value:    value,
			Data:     data,
			V:        v,
			R:        r,
			S:        s,
		})

	case types.DynamicFeeTxType:
		// EIP-1559 transaction
		maxFeePerGas, err := hexutil.DecodeBig(jtx.MaxFeePerGas)
		if err != nil {
			return nil, fmt.Errorf("failed to decode maxFeePerGas: %v", err)
		}
		maxPriorityFee, err := hexutil.DecodeBig(jtx.MaxPriorityFee)
		if err != nil {
			return nil, fmt.Errorf("failed to decode maxPriorityFeePerGas: %v", err)
		}
		chainID, err := hexutil.DecodeBig(jtx.ChainId)
		if err != nil {
			return nil, fmt.Errorf("failed to decode chainId: %v", err)
		}

		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: maxPriorityFee,
			GasFeeCap: maxFeePerGas,
			Gas:       gas,
			To:        to,
			Value:     value,
			Data:      data,
			V:         v,
			R:         r,
			S:         s,
		})

	default:
		// Default to legacy with gasPrice
		gasPrice, err := hexutil.DecodeBig(jtx.GasPrice)
		if err != nil {
			gasPrice = big.NewInt(0)
		}

		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gas,
			To:       to,
			Value:    value,
			Data:     data,
			V:        v,
			R:        r,
			S:        s,
		})
	}

	return tx, nil
}
