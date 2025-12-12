// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/trie"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <path-to-migrated-db>\n", os.Args[0])
		os.Exit(1)
	}

	migratedPath := os.Args[1]
	
	fmt.Printf("Opening migrated database: %s\n", migratedPath)

	// Open BadgerDB
	opts := badger.DefaultOptions(migratedPath)
	opts.Logger = nil // Disable badger logging
	bdb, err := badger.Open(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open BadgerDB: %v\n", err)
		os.Exit(1)
	}
	defer bdb.Close()

	// Wrap in our database interface
	db := badgerdb.NewDatabase(bdb, make(chan struct{}))

	// Read genesis block header to get state root
	genesisHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
	
	var header *types.Header
	err = bdb.View(func(txn *badger.Txn) error {
		// Read header: 'h' + number + hash
		headerKey := make([]byte, 41)
		headerKey[0] = 'h'
		// number = 0 (8 bytes, big-endian)
		copy(headerKey[9:41], genesisHash.Bytes())

		item, err := txn.Get(headerKey)
		if err != nil {
			return fmt.Errorf("genesis header not found: %w", err)
		}

		headerRLP, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		header = new(types.Header)
		return rlp.DecodeBytes(headerRLP, header)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read genesis header: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Genesis block found:\n")
	fmt.Printf("  Hash: %s\n", genesisHash.Hex())
	fmt.Printf("  State root: %s\n", header.Root.Hex())
	fmt.Printf("  Number: %d\n", header.Number.Uint64())

	// Create state database and trie
	stateDB, err := state.New(header.Root, state.NewDatabase(db), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open state: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nDumping all accounts...\n")

	// Dump accounts
	accounts := make(map[common.Address]types.Account)
	
	// Iterate through state trie
	tr, err := trie.New(trie.TrieID(header.Root), stateDB.Database().TrieDB())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open trie: %v\n", err)
		os.Exit(1)
	}

	iter, err := tr.NodeIterator(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create iterator: %v\n", err)
		os.Exit(1)
	}

	count := 0
	for iter.Next(true) {
		if iter.Leaf() {
			// Decode account address from key
			var addr common.Address
			copy(addr[:], iter.LeafKey())

			// Decode account data
			var acc types.StateAccount
			if err := rlp.DecodeBytes(iter.LeafBlob(), &acc); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to decode account %s: %v\n", addr.Hex(), err)
				continue
			}

			accounts[addr] = types.Account{
				Nonce:    acc.Nonce,
				Balance:  acc.Balance,
				CodeHash: acc.CodeHash,
				Root:     acc.Root,
			}
			count++

			if count%1000 == 0 {
				fmt.Printf("  Processed %d accounts...\n", count)
			}
		}
	}

	if iter.Error() != nil {
		fmt.Fprintf(os.Stderr, "Iterator error: %v\n", iter.Error())
		os.Exit(1)
	}

	fmt.Printf("\nTotal accounts: %d\n", count)

	// Create genesis alloc
	alloc := make(map[common.Address]types.Account)
	for addr, acc := range accounts {
		alloc[addr] = acc
		
		// Print accounts with balance
		if acc.Balance != nil && acc.Balance.Sign() > 0 {
			fmt.Printf("  %s: %s wei\n", addr.Hex(), acc.Balance.String())
		}
	}

	// Output JSON
	output := map[string]interface{}{
		"accounts": alloc,
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal JSON: %v\n", err)
		os.Exit(1)
	}

	outputFile := "/tmp/genesis-state-96369.json"
	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ State dumped to: %s\n", outputFile)
}
