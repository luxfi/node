// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// RPC Replay - Fetch historic blocks via RPC and execute into C-Chain

package cchainvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/common/hexutil"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/trie"
)

// ReplayConfig for RPC-based block replay
type ReplayConfig struct {
	URL                string        // Source subnet RPC endpoint
	Start              uint64        // Start block (0 = auto-detect)
	End                uint64        // End block (0 = fetch tip)
	Batch              int           // Blocks per batch (default: 500)
	FetchConcurrency   int           // Parallel fetchers (default: 8)
	MaxRetries         int           // Retry attempts (default: 8)
	BackoffMin         time.Duration // Min backoff (default: 300ms)
	BackoffMax         time.Duration // Max backoff (default: 5s)
	ResumePath         string        // Progress checkpoint file
	VerifyRoots        bool          // Verify tx/receipt roots (default: true)
	VerifySigs         bool          // Verify signatures (default: true)
	HardStopOnMismatch bool          // Stop on verification failure (default: true)
}

// DefaultReplayConfig returns safe defaults
func DefaultReplayConfig(url string) ReplayConfig {
	return ReplayConfig{
		URL:                url,
		Start:              0,
		End:                0,
		Batch:              500,
		FetchConcurrency:   8,
		MaxRetries:         8,
		BackoffMin:         300 * time.Millisecond,
		BackoffMax:         5 * time.Second,
		ResumePath:         "/var/lib/lux/replay.progress.json",
		VerifyRoots:        true,
		VerifySigs:         true,
		HardStopOnMismatch: true,
	}
}

// RPCReplayer fetches blocks via RPC and executes into blockchain
type RPCReplayer struct {
	cfg    ReplayConfig
	vm     *VM
	mu     sync.RWMutex
	height uint64 // Current replay height
}

// FetchedBlock contains block data from RPC
type FetchedBlock struct {
	Num      uint64
	Header   *types.Header
	Txs      types.Transactions
	Receipts types.Receipts
}

// RPCBlock is the JSON-RPC block structure
type RPCBlock struct {
	Number       *hexutil.Big     `json:"number"`
	Hash         common.Hash      `json:"hash"`
	ParentHash   common.Hash      `json:"parentHash"`
	Nonce        types.BlockNonce `json:"nonce"`
	MixHash      common.Hash      `json:"mixHash"`
	Miner        common.Address   `json:"miner"`
	Difficulty   *hexutil.Big     `json:"difficulty"`
	ExtraData    hexutil.Bytes    `json:"extraData"`
	Size         hexutil.Uint64   `json:"size"`
	GasLimit     hexutil.Uint64   `json:"gasLimit"`
	GasUsed      hexutil.Uint64   `json:"gasUsed"`
	Timestamp    hexutil.Uint64   `json:"timestamp"`
	Transactions []RPCTx          `json:"transactions"`
	Uncles       []common.Hash    `json:"uncles"`
	Root         common.Hash      `json:"stateRoot"`
	TxRoot       common.Hash      `json:"transactionsRoot"`
	ReceiptRoot  common.Hash      `json:"receiptsRoot"`
	Bloom        types.Bloom      `json:"logsBloom"`
	BaseFee      *hexutil.Big     `json:"baseFeePerGas,omitempty"`
}

// RPCTx is the JSON-RPC transaction structure
type RPCTx struct {
	Hash             common.Hash     `json:"hash"`
	Nonce            hexutil.Uint64  `json:"nonce"`
	From             common.Address  `json:"from"`
	To               *common.Address `json:"to"`
	Value            *hexutil.Big    `json:"value"`
	GasPrice         *hexutil.Big    `json:"gasPrice"`
	MaxFeePerGas     *hexutil.Big    `json:"maxFeePerGas,omitempty"`
	MaxPriorityFee   *hexutil.Big    `json:"maxPriorityFeePerGas,omitempty"`
	Gas              hexutil.Uint64  `json:"gas"`
	Input            hexutil.Bytes   `json:"input"`
	V                *hexutil.Big    `json:"v"`
	R                *hexutil.Big    `json:"r"`
	S                *hexutil.Big    `json:"s"`
	Type             hexutil.Uint64  `json:"type,omitempty"`
	AccessList       []interface{}   `json:"accessList,omitempty"`
	ChainID          *hexutil.Big    `json:"chainId,omitempty"`
	TransactionIndex hexutil.Uint64  `json:"transactionIndex"`
}

// RPCReceipt is the JSON-RPC receipt structure
type RPCReceipt struct {
	TransactionHash   common.Hash    `json:"transactionHash"`
	TransactionIndex  hexutil.Uint64 `json:"transactionIndex"`
	BlockHash         common.Hash    `json:"blockHash"`
	BlockNumber       *hexutil.Big   `json:"blockNumber"`
	From              common.Address `json:"from"`
	To                *common.Address `json:"to"`
	CumulativeGasUsed hexutil.Uint64 `json:"cumulativeGasUsed"`
	GasUsed           hexutil.Uint64 `json:"gasUsed"`
	ContractAddress   *common.Address `json:"contractAddress,omitempty"`
	Logs              []RPCLog       `json:"logs"`
	LogsBloom         types.Bloom    `json:"logsBloom"`
	Type              hexutil.Uint64 `json:"type"`
	Status            hexutil.Uint64 `json:"status"`
}

// RPCLog is the JSON-RPC log structure
type RPCLog struct {
	Address          common.Address `json:"address"`
	Topics           []common.Hash  `json:"topics"`
	Data             hexutil.Bytes  `json:"data"`
	BlockNumber      hexutil.Uint64 `json:"blockNumber"`
	TransactionHash  common.Hash    `json:"transactionHash"`
	TransactionIndex hexutil.Uint64 `json:"transactionIndex"`
	BlockHash        common.Hash    `json:"blockHash"`
	LogIndex         hexutil.Uint64 `json:"logIndex"`
	Removed          bool           `json:"removed"`
}

// StartRPCReplay begins RPC-based block replay
func (vm *VM) StartRPCReplay(ctx context.Context, cfg ReplayConfig) error {
	// Determine start/end
	start := cfg.Start
	if start == 0 {
		local := vm.blockChain.CurrentBlock().Number.Uint64()
		if local > 0 {
			start = local + 1
		}
	}

	end := cfg.End
	if end == 0 {
		tip, err := rpcGetBlockNumber(ctx, cfg.URL)
		if err != nil {
			return fmt.Errorf("failed to get remote tip: %w", err)
		}
		end = tip
	}

	vm.log.Info("Starting RPC replay",
		"source", cfg.URL,
		"start", start,
		"end", end,
		"total", end-start+1,
	)

	replayer := &RPCReplayer{
		cfg:    cfg,
		vm:     vm,
		height: start - 1,
	}

	// Load resume point if exists
	if err := replayer.loadProgress(); err != nil {
		vm.log.Warn("Could not load progress", "error", err)
	}

	// Start replay pipeline
	return replayer.run(ctx, start, end)
}

// run executes the replay pipeline
func (rr *RPCReplayer) run(ctx context.Context, start, end uint64) error {
	// Pipeline: Fetcher -> Verifier -> Inserter
	fetchCh := make(chan *FetchedBlock, rr.cfg.Batch)
	verifyCh := make(chan *FetchedBlock, rr.cfg.Batch)
	errCh := make(chan error, 1)

	// Start fetchers (concurrent)
	var fetchWg sync.WaitGroup
	for i := 0; i < rr.cfg.FetchConcurrency; i++ {
		fetchWg.Add(1)
		go func(workerID int) {
			defer fetchWg.Done()
			rr.fetcher(ctx, workerID, start, end, fetchCh, errCh)
		}(i)
	}

	// Close fetch channel when all fetchers done
	go func() {
		fetchWg.Wait()
		close(fetchCh)
	}()

	// Start verifier (single threaded, ordered)
	go rr.verifier(ctx, fetchCh, verifyCh, errCh)

	// Start inserter (single threaded, strict order)
	go rr.inserter(ctx, verifyCh, errCh)

	// Wait for completion or error
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fetcher fetches blocks from RPC
func (rr *RPCReplayer) fetcher(ctx context.Context, id int, start, end uint64, out chan<- *FetchedBlock, errCh chan<- error) {
	for num := start + uint64(id); num <= end; num += uint64(rr.cfg.FetchConcurrency) {
		// Fetch with retries
		block, err := rr.fetchBlockWithRetry(ctx, num)
		if err != nil {
			errCh <- fmt.Errorf("fetch block %d: %w", num, err)
			return
		}

		select {
		case out <- block:
		case <-ctx.Done():
			return
		}
	}
}

// fetchBlockWithRetry fetches a single block with exponential backoff
func (rr *RPCReplayer) fetchBlockWithRetry(ctx context.Context, num uint64) (*FetchedBlock, error) {
	backoff := rr.cfg.BackoffMin

	for attempt := 0; attempt < rr.cfg.MaxRetries; attempt++ {
		block, err := rr.fetchBlock(ctx, num)
		if err == nil {
			return block, nil
		}

		if attempt < rr.cfg.MaxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > rr.cfg.BackoffMax {
				backoff = rr.cfg.BackoffMax
			}
		}
	}

	return nil, fmt.Errorf("max retries exceeded for block %d", num)
}

// fetchBlock fetches a single block via RPC
func (rr *RPCReplayer) fetchBlock(ctx context.Context, num uint64) (*FetchedBlock, error) {
	// Fetch block with transactions
	var rpcBlock RPCBlock
	if err := rpcCall(ctx, rr.cfg.URL, "eth_getBlockByNumber", &rpcBlock, hexutil.EncodeUint64(num), true); err != nil {
		return nil, err
	}

	// Convert header
	header := convertHeaderLegacy(&rpcBlock)

	// Convert transactions
	txs := make(types.Transactions, len(rpcBlock.Transactions))
	for i, rpcTx := range rpcBlock.Transactions {
		tx, err := convertTx(&rpcTx, rr.cfg.VerifySigs)
		if err != nil {
			return nil, fmt.Errorf("convert tx %d: %w", i, err)
		}
		txs[i] = tx
	}

	// Fetch receipts
	receipts, err := rr.fetchReceipts(ctx, rpcBlock.Hash, len(txs))
	if err != nil {
		return nil, fmt.Errorf("fetch receipts: %w", err)
	}

	return &FetchedBlock{
		Num:      num,
		Header:   header,
		Txs:      txs,
		Receipts: receipts,
	}, nil
}

// fetchReceipts gets all receipts for a block
func (rr *RPCReplayer) fetchReceipts(ctx context.Context, blockHash common.Hash, txCount int) (types.Receipts, error) {
	// Try eth_getBlockReceipts first
	var rpcReceipts []RPCReceipt
	if err := rpcCall(ctx, rr.cfg.URL, "eth_getBlockReceipts", &rpcReceipts, blockHash); err == nil {
		return convertReceipts(rpcReceipts)
	}

	// Fallback: fetch per-tx receipts (slower)
	receipts := make(types.Receipts, txCount)
	// TODO: implement per-tx receipt fetching with worker pool
	return receipts, nil
}

// verifier validates block roots and continuity
func (rr *RPCReplayer) verifier(ctx context.Context, in <-chan *FetchedBlock, out chan<- *FetchedBlock, errCh chan<- error) {
	defer close(out)

	for block := range in {
		if rr.cfg.VerifyRoots {
			// Verify transaction root
			txRoot := types.DeriveSha(block.Txs, trie.NewStackTrie(nil))
			if txRoot != block.Header.TxHash {
				errCh <- fmt.Errorf("block %d: tx root mismatch", block.Num)
				return
			}

			// Verify receipt root
			receiptRoot := types.DeriveSha(block.Receipts, trie.NewStackTrie(nil))
			if receiptRoot != block.Header.ReceiptHash {
				errCh <- fmt.Errorf("block %d: receipt root mismatch", block.Num)
				return
			}

			// Verify bloom (compute from all receipts)
			bloom := types.Bloom{}
			for _, receipt := range block.Receipts {
				bloom.Add(types.CreateBloom(receipt).Bytes())
			}
			if bloom != block.Header.Bloom {
				errCh <- fmt.Errorf("block %d: bloom mismatch", block.Num)
				return
			}
		}

		select {
		case out <- block:
		case <-ctx.Done():
			return
		}
	}
}

// inserter executes blocks sequentially into blockchain
func (rr *RPCReplayer) inserter(ctx context.Context, in <-chan *FetchedBlock, errCh chan<- error) {
	expected := rr.height + 1
	window := make(map[uint64]*FetchedBlock)
	batchCount := 0

	for {
		select {
		case block, ok := <-in:
			if !ok {
				// Channel closed, we're done
				rr.vm.log.Info("Replay complete", "finalHeight", rr.height)
				return
			}

			// Buffer block
			window[block.Num] = block

			// Process contiguous sequence
			for {
				next, exists := window[expected]
				if !exists {
					break
				}

				// Verify parent continuity
				if expected > 0 {
					current := rr.vm.blockChain.CurrentBlock()
					if next.Header.ParentHash != current.Hash() {
						errCh <- fmt.Errorf("non-contiguous: block %d parent %s != current %s",
							expected, next.Header.ParentHash.Hex(), current.Hash().Hex())
						return
					}
				}

				// Create block
				coreBlock := types.NewBlockWithHeader(next.Header).WithBody(types.Body{Transactions: next.Txs})

				// Insert (executes state transitions)
				_, err := rr.vm.blockChain.InsertChain(types.Blocks{coreBlock})
				if err != nil {
					errCh <- fmt.Errorf("insert block %d: %w", expected, err)
					return
				}

				delete(window, expected)
				rr.height = expected
				expected++
				batchCount++

				// Progress logging
				if batchCount%1000 == 0 {
					rr.vm.log.Info("Replay progress", "height", rr.height, "queued", len(window))
				}

				// Persist progress every 256 blocks
				if batchCount%256 == 0 {
					if err := rr.saveProgress(); err != nil {
						rr.vm.log.Warn("Failed to save progress", "error", err)
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// Helper functions

func rpcCall(ctx context.Context, url, method string, result interface{}, params ...interface{}) error {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return json.Unmarshal(rpcResp.Result, result)
}

func rpcGetBlockNumber(ctx context.Context, url string) (uint64, error) {
	var result hexutil.Uint64
	if err := rpcCall(ctx, url, "eth_blockNumber", &result); err != nil {
		return 0, err
	}
	return uint64(result), nil
}

func convertHeaderLegacy(rpc *RPCBlock) *types.Header {
	header := &types.Header{
		ParentHash:  rpc.ParentHash,
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    rpc.Miner,
		Root:        rpc.Root,
		TxHash:      rpc.TxRoot,
		ReceiptHash: rpc.ReceiptRoot,
		Bloom:       rpc.Bloom,
		Difficulty:  (*big.Int)(rpc.Difficulty),
		Number:      (*big.Int)(rpc.Number),
		GasLimit:    uint64(rpc.GasLimit),
		GasUsed:     uint64(rpc.GasUsed),
		Time:        uint64(rpc.Timestamp),
		Extra:       rpc.ExtraData,
		MixDigest:   rpc.MixHash,
		Nonce:       rpc.Nonce,
	}

	if rpc.BaseFee != nil {
		header.BaseFee = (*big.Int)(rpc.BaseFee)
	}

	return header
}

func convertTx(rpc *RPCTx, verifySig bool) (*types.Transaction, error) {
	var tx *types.Transaction

	// Build transaction based on type
	switch uint8(rpc.Type) {
	case types.LegacyTxType:
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    uint64(rpc.Nonce),
			GasPrice: (*big.Int)(rpc.GasPrice),
			Gas:      uint64(rpc.Gas),
			To:       rpc.To,
			Value:    (*big.Int)(rpc.Value),
			Data:     rpc.Input,
			V:        (*big.Int)(rpc.V),
			R:        (*big.Int)(rpc.R),
			S:        (*big.Int)(rpc.S),
		})

	case types.AccessListTxType:
		tx = types.NewTx(&types.AccessListTx{
			ChainID:  (*big.Int)(rpc.ChainID),
			Nonce:    uint64(rpc.Nonce),
			GasPrice: (*big.Int)(rpc.GasPrice),
			Gas:      uint64(rpc.Gas),
			To:       rpc.To,
			Value:    (*big.Int)(rpc.Value),
			Data:     rpc.Input,
			V:        (*big.Int)(rpc.V),
			R:        (*big.Int)(rpc.R),
			S:        (*big.Int)(rpc.S),
		})

	case types.DynamicFeeTxType:
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   (*big.Int)(rpc.ChainID),
			Nonce:     uint64(rpc.Nonce),
			GasTipCap: (*big.Int)(rpc.MaxPriorityFee),
			GasFeeCap: (*big.Int)(rpc.MaxFeePerGas),
			Gas:       uint64(rpc.Gas),
			To:        rpc.To,
			Value:     (*big.Int)(rpc.Value),
			Data:      rpc.Input,
			V:        (*big.Int)(rpc.V),
			R:        (*big.Int)(rpc.R),
			S:        (*big.Int)(rpc.S),
		})

	default:
		return nil, fmt.Errorf("unknown tx type: %d", rpc.Type)
	}

	// Verify signature if required
	if verifySig {
		signer := types.LatestSignerForChainID((*big.Int)(rpc.ChainID))
		from, err := types.Sender(signer, tx)
		if err != nil {
			return nil, fmt.Errorf("sig recovery failed: %w", err)
		}
		if from != rpc.From {
			return nil, fmt.Errorf("from mismatch: got %s want %s", from.Hex(), rpc.From.Hex())
		}
	}

	return tx, nil
}

func convertReceipts(rpcReceipts []RPCReceipt) (types.Receipts, error) {
	receipts := make(types.Receipts, len(rpcReceipts))

	for i, rpc := range rpcReceipts {
		logs := make([]*types.Log, len(rpc.Logs))
		for j, rpcLog := range rpc.Logs {
			logs[j] = &types.Log{
				Address: rpcLog.Address,
				Topics:  rpcLog.Topics,
				Data:    rpcLog.Data,
			}
		}

		receipts[i] = &types.Receipt{
			Type:              uint8(rpc.Type),
			Status:            uint64(rpc.Status),
			CumulativeGasUsed: uint64(rpc.CumulativeGasUsed),
			Logs:              logs,
			Bloom:             rpc.LogsBloom,
		}

		if rpc.ContractAddress != nil {
			receipts[i].ContractAddress = *rpc.ContractAddress
		}
	}

	return receipts, nil
}

// Progress management

type RPCReplayProgress struct {
	LastImported uint64      `json:"lastImported"`
	LastHash     common.Hash `json:"lastHash"`
	Timestamp    time.Time   `json:"timestamp"`
}

func (rr *RPCReplayer) saveProgress() error {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	progress := RPCReplayProgress{
		LastImported: rr.height,
		LastHash:     rr.vm.blockChain.CurrentBlock().Hash(),
		Timestamp:    time.Now(),
	}

	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: tmp file + rename
	tmpPath := rr.cfg.ResumePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, rr.cfg.ResumePath)
}

func (rr *RPCReplayer) loadProgress() error {
	data, err := os.ReadFile(rr.cfg.ResumePath)
	if err != nil {
		return err
	}

	var progress RPCReplayProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return err
	}

	// Verify DB matches progress
	current := rr.vm.blockChain.CurrentBlock()
	if current.Number.Uint64() == progress.LastImported && current.Hash() == progress.LastHash {
		rr.height = progress.LastImported
		rr.vm.log.Info("Resumed from checkpoint", "height", rr.height)
		return nil
	}

	rr.vm.log.Warn("Progress file doesn't match DB, starting from DB head",
		"fileHeight", progress.LastImported,
		"dbHeight", current.Number.Uint64())

	return nil
}
