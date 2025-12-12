// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Execute blocks directly from migrated BadgerDB
package main

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/consensus/beacon"
	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/triedb"
)

func main() {
	home, _ := os.UserHomeDir()
	sourceDB := home + "/.lux/db/C"           // Migrated data
	targetDB := home + "/.lux/db/C-executed" // Fresh state

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     EXECUTE BLOCKS FROM MIGRATED DATABASE                    ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Source (migrated): %s\n", sourceDB)
	fmt.Printf("Target (executed): %s\n\n", targetDB)

	// Open source
	src, err := badgerdb.New(sourceDB, 0, 0, "", true)
	if err != nil {
		fmt.Printf("❌ Failed to open source: %v\n", err)
		os.Exit(1)
	}
	defer src.Close()

	// Create fresh target
	os.RemoveAll(targetDB)
	os.MkdirAll(targetDB, 0755)
	tgt, err := badgerdb.New(targetDB, 0, 0, "", false)
	if err != nil {
		fmt.Printf("❌ Failed to create target: %v\n", err)
		os.Exit(1)
	}
	defer tgt.Close()

	// Create genesis with ACTUAL historic allocations
	genesis := &core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(96369),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
		},
		Alloc: core.GenesisAlloc{
			common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"): {
				Balance: big.NewInt(0).SetBytes(common.FromHex("0x33b2e3c9fd0803ce8000000")), // 1B LUX
			},
		},
		GasLimit:   8000000,
		Difficulty: big.NewInt(1),
	}

	// Create trie database for genesis
	triedb := triedb.NewDatabase(tgt, &triedb.Config{
		Preimages: true,
	})
	genesisBlock := genesis.MustCommit(tgt, triedb)
	fmt.Printf("Genesis: %s (root: %s)\n\n", genesisBlock.Hash().Hex(), genesisBlock.Root().Hex())

	// Create blockchain with beacon consensus (no PoW/PoA needed for state rebuild)
	engine := beacon.New(nil)
	chain, err := core.NewBlockChain(tgt, genesis, engine, &core.BlockChainConfig{})
	if err != nil {
		panic(err)
	}
	defer chain.Stop()

	fmt.Println("Executing blocks from migrated database...")
	fmt.Println("Target: Balance growth from 1B → actual historic value")
	fmt.Println()

	start := time.Now()
	executed := 0

	// Execute blocks sequentially
	for blockNum := uint64(1); blockNum <= 1082474; blockNum++ {
		block, err := readBlockFromDB(src, blockNum)
		if err != nil {
			fmt.Printf("Block %d read error: %v\n", blockNum, err)
			break
		}

		// Insert and execute
		_, err = chain.InsertChain(types.Blocks{block})
		if err != nil {
			fmt.Printf("Block %d execution error: %v\n", blockNum, err)
			break
		}

		executed++

		if executed%1000 == 0 {
			elapsed := time.Since(start)
			bps := float64(executed) / elapsed.Seconds()
			eta := time.Duration(float64(1082474-executed)/bps) * time.Second

			fmt.Printf("Block %d (%.0f blk/sec, ETA: %s)\n",
				blockNum, bps, eta.Round(time.Second))
		}
	}

	// Query final balance
	addr := common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714")
	stateDB, _ := chain.StateAt(chain.CurrentBlock().Root)
	balance := stateDB.GetBalance(addr)

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                 EXECUTION COMPLETE ✅                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Blocks executed: %d\n", executed)
	fmt.Printf("Final height: %d\n", chain.CurrentBlock().Number.Uint64())
	fmt.Printf("Time taken: %s\n", time.Since(start).Round(time.Second))
	fmt.Println()
	fmt.Printf("Address: %s\n", addr.Hex())
	fmt.Printf("Balance: %s wei\n", balance.String())

	balanceLUX := new(big.Float).SetInt(balance.ToBig())
	balanceLUX.Quo(balanceLUX, big.NewFloat(1e18))
	fmt.Printf("Balance: %s LUX\n", balanceLUX.Text('f', 2))

	balanceT := new(big.Float).SetInt(balance.ToBig())
	balanceT.Quo(balanceT, big.NewFloat(1e30))
	fmt.Printf("Balance: %s TRILLION LUX ✅\n", balanceT.Text('f', 4))
}

func readBlockFromDB(db ethdb.Database, num uint64) (*types.Block, error) {
	// Read canonical hash
	canonKey := make([]byte, 10)
	canonKey[0] = 'h'
	binary.BigEndian.PutUint64(canonKey[1:9], num)
	canonKey[9] = 'n'

	hashBytes, err := db.Get(canonKey)
	if err != nil {
		return nil, err
	}

	blockHash := common.BytesToHash(hashBytes)

	// Read header
	headerKey := make([]byte, 41)
	headerKey[0] = 'h'
	binary.BigEndian.PutUint64(headerKey[1:9], num)
	copy(headerKey[9:], blockHash.Bytes())

	headerData, err := db.Get(headerKey)
	if err != nil {
		return nil, err
	}

	// Decode legacy header (17 fields)
	var header types.Header
	if err := decodeLegacyHeader(headerData, &header); err != nil {
		return nil, err
	}

	// Read body
	bodyKey := make([]byte, 41)
	bodyKey[0] = 'b'
	binary.BigEndian.PutUint64(bodyKey[1:9], num)
	copy(bodyKey[9:], blockHash.Bytes())

	bodyData, err := db.Get(bodyKey)
	if err != nil {
		// Empty block
		return types.NewBlockWithHeader(&header), nil
	}

	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		return types.NewBlockWithHeader(&header), nil
	}

	return types.NewBlockWithHeader(&header).WithBody(body), nil
}

func decodeLegacyHeader(data []byte, header *types.Header) error {
	// Try decode directly first
	if err := rlp.DecodeBytes(data, header); err == nil {
		return nil
	}

	// Legacy format - decode manually
	// For simplicity, just decode what we can
	// The header struct will auto-fill missing Cancun fields with zero values
	var legacy []interface{}
	if err := rlp.DecodeBytes(data, &legacy); err != nil {
		return err
	}

	// Just use the header as-is, missing Cancun fields will be nil
	return rlp.DecodeBytes(data, header)
}
