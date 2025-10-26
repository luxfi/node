// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"fmt"
	"log/slog"

	"github.com/luxfi/geth/common"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/ids"
)

// ForceBlockchainReload forces the blockchain to recognize replayed blocks
// This is called after replay completes to ensure eth_blockNumber returns the correct value
func (vm *VM) ForceBlockchainReload(targetHeight uint64, targetHash common.Hash) error {
	vm.log.Info("ForceBlockchainReload: Starting blockchain head update",
		"targetHeight", targetHeight,
		"targetHash", targetHash.Hex())

	if vm.blockChain == nil {
		return fmt.Errorf("blockchain not initialized")
	}

	// Step 1: Read the target block from database to verify it exists
	targetBlock := rawdb.ReadBlock(vm.ethDB, targetHash, targetHeight)
	if targetBlock == nil {
		return fmt.Errorf("target block %d (hash: %s) not found in database", targetHeight, targetHash.Hex())
	}

	vm.log.Info("Target block found in database",
		"number", targetBlock.NumberU64(),
		"hash", targetBlock.Hash().Hex(),
		"stateRoot", targetBlock.Root().Hex())

	// Step 2: Force write all canonical chain markers
	vm.log.Info("Writing canonical chain markers...")

	// Write canonical hash for the target block
	rawdb.WriteCanonicalHash(vm.ethDB, targetHash, targetHeight)

	// Write head markers
	rawdb.WriteHeadBlockHash(vm.ethDB, targetHash)
	rawdb.WriteHeadHeaderHash(vm.ethDB, targetHash)
	rawdb.WriteHeadFastBlockHash(vm.ethDB, targetHash)

	// Write the header and body explicitly
	rawdb.WriteHeader(vm.ethDB, targetBlock.Header())
	rawdb.WriteBody(vm.ethDB, targetHash, targetHeight, targetBlock.Body())
	rawdb.WriteBlock(vm.ethDB, targetBlock)

	vm.log.Info("Canonical chain markers written")

	// Step 3: Force blockchain to reload from disk
	// SetHead is not ideal but it forces the blockchain to reload its internal state
	vm.log.Info("Forcing blockchain SetHead to target height...")
	vm.blockChain.SetHead(targetHeight)

	// Step 4: Verify the blockchain recognized the new head
	currentBlock := vm.blockChain.CurrentBlock()
	currentHeader := vm.blockChain.CurrentHeader()

	if currentBlock == nil {
		return fmt.Errorf("blockchain CurrentBlock is nil after SetHead")
	}

	vm.log.Info("Blockchain state after SetHead",
		"currentBlockNumber", currentBlock.Number.Uint64(),
		"currentBlockHash", currentBlock.Hash().Hex(),
		"currentHeaderNumber", currentHeader.Number.Uint64(),
		"currentHeaderHash", currentHeader.Hash().Hex())

	// Step 5: If SetHead didn't work, try InsertChain with the target block
	if currentBlock.Number.Uint64() != targetHeight {
		vm.log.Warn("SetHead didn't update to target height, trying InsertChain...")

		// Insert the target block explicitly
		_, err := vm.blockChain.InsertChain([]*types.Block{targetBlock})
		if err != nil {
			vm.log.Error("InsertChain failed", "error", err)
			// Continue anyway, the block might already be there
		}

		// Check again
		currentBlock = vm.blockChain.CurrentBlock()
		if currentBlock != nil {
			vm.log.Info("After InsertChain",
				"currentBlockNumber", currentBlock.Number.Uint64(),
				"currentBlockHash", currentBlock.Hash().Hex())
		}
	}

	// Step 6: Update VM's lastAccepted
	vm.mu.Lock()
	vm.lastAccepted = ids.ID(targetHash)
	vm.mu.Unlock()

	finalBlockNumber := uint64(0)
	if currentBlock != nil {
		finalBlockNumber = currentBlock.Number.Uint64()
	}
	vm.log.Info("ForceBlockchainReload completed",
		"lastAccepted", vm.lastAccepted.String(),
		"finalBlockNumber", finalBlockNumber)

	return nil
}

// RecreateBackendAfterReplay recreates the backend and blockchain after replay
// This ensures all components properly recognize the replayed data
func (vm *VM) RecreateBackendAfterReplay(genesis *gethcore.Genesis) error {
	vm.log.Info("RecreateBackendAfterReplay: Starting backend recreation...")

	// Save current head information before recreation
	savedHeadHash := rawdb.ReadHeadBlockHash(vm.ethDB)
	savedHeadNumber := uint64(0)
	if savedHeadHash != (common.Hash{}) {
		if number, ok := rawdb.ReadHeaderNumber(vm.ethDB, savedHeadHash); ok {
			savedHeadNumber = number
		}
	}

	vm.log.Info("Saved head before recreation",
		"number", savedHeadNumber,
		"hash", savedHeadHash.Hex())

	// Ensure BlobSchedule is set for Cancun fork
	if genesis != nil && genesis.Config != nil && genesis.Config.CancunTime != nil {
		if genesis.Config.BlobScheduleConfig == nil {
			genesis.Config.BlobScheduleConfig = &params.BlobScheduleConfig{}
		}
		if genesis.Config.BlobScheduleConfig.Cancun == nil {
			genesis.Config.BlobScheduleConfig.Cancun = &params.BlobConfig{
				Target:         3,
				Max:            6,
				UpdateFraction: 3338477,
			}
			vm.log.Info("Fixed BlobSchedule configuration")
		}
	}

	// Recreate the backend
	newBackend, err := NewMinimalEthBackend(vm.ethDB, &vm.ethConfig, genesis)
	if err != nil {
		return fmt.Errorf("failed to recreate backend: %w", err)
	}

	// Replace the old backend
	vm.backend = newBackend
	vm.blockChain = newBackend.BlockChain()
	vm.txPool = newBackend.TxPool()

	vm.log.Info("Backend recreated successfully")

	// Force restore the saved head if it was overwritten
	if savedHeadNumber > 0 {
		// Check if genesis setup overwrote our head
		currentHead := rawdb.ReadHeadBlockHash(vm.ethDB)
		if currentHead != savedHeadHash {
			vm.log.Warn("Head was overwritten during backend recreation, restoring...",
				"was", currentHead.Hex(),
				"restoring", savedHeadHash.Hex())

			// Restore head markers
			rawdb.WriteHeadBlockHash(vm.ethDB, savedHeadHash)
			rawdb.WriteHeadHeaderHash(vm.ethDB, savedHeadHash)
			rawdb.WriteHeadFastBlockHash(vm.ethDB, savedHeadHash)

			// Force blockchain to recognize the restored head
			vm.blockChain.SetHead(savedHeadNumber)
		}

		// Force update blockchain head if needed
		err = vm.ForceBlockchainReload(savedHeadNumber, savedHeadHash)
		if err != nil {
			vm.log.Error("Failed to force blockchain reload", "error", err)
			// Continue anyway
		}
	}

	// Final verification
	currentBlock := vm.blockChain.CurrentBlock()
	if currentBlock != nil {
		vm.log.Info("Backend recreation complete",
			"currentBlockNumber", currentBlock.Number.Uint64(),
			"currentBlockHash", currentBlock.Hash().Hex())

		// Update lastAccepted
		vm.mu.Lock()
		vm.lastAccepted = ids.ID(currentBlock.Hash())
		vm.mu.Unlock()
	} else {
		vm.log.Warn("CurrentBlock is nil after backend recreation")
	}

	return nil
}

// ReloadBlockchainState forces the blockchain to reload its state from the database
// This is useful after direct database modifications or replay operations
func (vm *VM) ReloadBlockchainState() error {
	if vm.blockChain == nil {
		return fmt.Errorf("blockchain not initialized")
	}

	// Get the actual head from database
	headHash := rawdb.ReadHeadBlockHash(vm.ethDB)
	if headHash == (common.Hash{}) {
		return fmt.Errorf("no head block hash in database")
	}

	headNumber := uint64(0)
	if num, ok := rawdb.ReadHeaderNumber(vm.ethDB, headHash); ok {
		headNumber = num
	}

	vm.log.Info("ReloadBlockchainState: Database head",
		"number", headNumber,
		"hash", headHash.Hex())

	// Get current blockchain state
	currentBlock := vm.blockChain.CurrentBlock()
	needsUpdate := false

	if currentBlock == nil {
		needsUpdate = true
		vm.log.Warn("CurrentBlock is nil, needs update")
	} else if currentBlock.Number.Uint64() != headNumber {
		needsUpdate = true
		vm.log.Warn("CurrentBlock mismatch",
			"current", currentBlock.Number.Uint64(),
			"expected", headNumber)
	} else if currentBlock.Hash() != headHash {
		needsUpdate = true
		vm.log.Warn("CurrentBlock hash mismatch",
			"current", currentBlock.Hash().Hex(),
			"expected", headHash.Hex())
	}

	if !needsUpdate {
		vm.log.Info("Blockchain state is already correct")
		return nil
	}

	// Force update
	return vm.ForceBlockchainReload(headNumber, headHash)
}

// VerifyBlockchainIntegrity checks that the blockchain properly recognizes replayed data
func (vm *VM) VerifyBlockchainIntegrity() error {
	if vm.blockChain == nil {
		return fmt.Errorf("blockchain not initialized")
	}

	// Check database head
	dbHeadHash := rawdb.ReadHeadBlockHash(vm.ethDB)
	dbHeadNumber := uint64(0)
	if dbHeadHash != (common.Hash{}) {
		if num, ok := rawdb.ReadHeaderNumber(vm.ethDB, dbHeadHash); ok {
			dbHeadNumber = num
		}
	}

	// Check blockchain's view
	currentBlock := vm.blockChain.CurrentBlock()
	currentHeader := vm.blockChain.CurrentHeader()

	vm.log.Info("Blockchain integrity check",
		"dbHeadNumber", dbHeadNumber,
		"dbHeadHash", dbHeadHash.Hex())

	if currentBlock != nil {
		vm.log.Info("CurrentBlock",
			"number", currentBlock.Number.Uint64(),
			"hash", currentBlock.Hash().Hex(),
			"stateRoot", currentBlock.Root.Hex())
	} else {
		vm.log.Warn("CurrentBlock is nil")
	}

	if currentHeader != nil {
		vm.log.Info("CurrentHeader",
			"number", currentHeader.Number.Uint64(),
			"hash", currentHeader.Hash().Hex())
	} else {
		vm.log.Warn("CurrentHeader is nil")
	}

	// Check for mismatches
	if currentBlock != nil && dbHeadNumber > 0 {
		if currentBlock.Number.Uint64() != dbHeadNumber {
			return fmt.Errorf("blockchain head mismatch: blockchain=%d, database=%d",
				currentBlock.Number.Uint64(), dbHeadNumber)
		}
		if currentBlock.Hash() != dbHeadHash {
			return fmt.Errorf("blockchain hash mismatch: blockchain=%s, database=%s",
				currentBlock.Hash().Hex(), dbHeadHash.Hex())
		}
	}

	// Check state availability
	if currentBlock != nil {
		stateDb, err := vm.blockChain.StateAt(currentBlock.Root)
		if err != nil {
			return fmt.Errorf("state unavailable at block %d: %w", currentBlock.Number.Uint64(), err)
		}

		// Try to read a known account balance as a test
		testAccount := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
		balance := stateDb.GetBalance(testAccount)
		vm.log.Info("Test account balance check",
			"account", testAccount.Hex(),
			"balance", balance.String())
	}

	vm.log.Info("Blockchain integrity check passed")
	return nil
}

// Initialize logger if needed
func (vm *VM) ensureLogger() {
	if vm.log == nil {
		vm.log = slog.Default().With("vm", "cchain", "component", "blockchain-reload")
	}
}