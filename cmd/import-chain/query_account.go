// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	
	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type Account struct {
	Nonce    uint64
	Balance  []byte
	Root     common.Hash
	CodeHash []byte
}

func main() {
	dbPath := "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	
	// Open PebbleDB
	opts := &pebble.Options{
		ReadOnly: true,
	}
	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	// Treasury address
	treasury := common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714")
	
	// Construct account key (prefix + address)
	// In go-ethereum: append([]byte("secure-key-"), crypto.Keccak256(address.Bytes())...)
	accountKey := append([]byte{0x33, 0x7f, 0xb7, 0x3f}, treasury.Bytes()...)
	
	val, closer, err := db.Get(accountKey)
	if err != nil {
		fmt.Printf("Account not found with key %x: %v\n", accountKey, err)
		return
	}
	defer closer.Close()
	
	var acc Account
	if err := rlp.DecodeBytes(val, &acc); err != nil {
		fmt.Printf("Error decoding account: %v\n", err)
		return
	}
	
	fmt.Printf("Treasury Account:\n")
	fmt.Printf("  Address: %s\n", treasury.Hex())
	fmt.Printf("  Nonce: %d\n", acc.Nonce)
	fmt.Printf("  Balance: %s wei\n", hex.EncodeToString(acc.Balance))
	fmt.Printf("  Root: %s\n", acc.Root.Hex())
	fmt.Printf("  CodeHash: %x\n", acc.CodeHash)
}
