// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/big"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/rlp"
)

const (
	targetBlock = 1000000 // 0xF4240
	dbPath      = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
)

// Database key prefixes from geth rawdb schema
var (
	headerPrefix = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
)

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// More comprehensive header struct that might include extra fields
type ExtendedHeader struct {
	ParentHash       common.Hash      `json:"parentHash"       gencodec:"required"`
	UncleHash        common.Hash      `json:"sha3Uncles"       gencodec:"required"`
	Coinbase         common.Address   `json:"miner"            gencodec:"required"`
	Root             common.Hash      `json:"stateRoot"        gencodec:"required"`
	TxHash           common.Hash      `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash      common.Hash      `json:"receiptsRoot"     gencodec:"required"`
	Bloom            types.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty       *big.Int         `json:"difficulty"       gencodec:"required"`
	Number           *big.Int         `json:"number"           gencodec:"required"`
	GasLimit         uint64           `json:"gasLimit"         gencodec:"required"`
	GasUsed          uint64           `json:"gasUsed"          gencodec:"required"`
	Time             uint64           `json:"timestamp"        gencodec:"required"`
	Extra            []byte           `json:"extraData"        gencodec:"required"`
	MixDigest        common.Hash      `json:"mixHash"`
	Nonce            types.BlockNonce `json:"nonce"`
	BaseFee          *big.Int         `json:"baseFeePerGas"    rlp:"optional"`
	WithdrawalsHash  *common.Hash     `json:"withdrawalsRoot"  rlp:"optional"`
	BlobGasUsed      *uint64          `json:"blobGasUsed"      rlp:"optional"`
	ExcessBlobGas    *uint64          `json:"excessBlobGas"    rlp:"optional"`
	ParentBeaconRoot *common.Hash     `json:"parentBeaconBlockRoot" rlp:"optional"`
}

func main() {
	log.Printf("Decoding header for block %d with extended structure", targetBlock)
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Find the actual block hash by looking at available keys
	blockPrefix := append(headerPrefix, encodeBlockNumber(targetBlock)...)
	var headerKey []byte
	
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		
		for it.Seek(blockPrefix); it.ValidForPrefix(blockPrefix); it.Next() {
			key := it.Item().Key()
			// Look for the header key (has hash suffix, not "n" suffix)
			if len(key) == len(blockPrefix) + 32 { // hash is 32 bytes
				headerKey = make([]byte, len(key))
				copy(headerKey, key)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Error finding block hash: %v", err)
	}
	
	// Read header data
	var headerData []byte
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(headerKey)
		if err != nil {
			return err
		}
		headerData, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		log.Fatalf("Failed to read header data: %v", err)
	}
	
	// Try decoding with extended header
	var extHeader ExtendedHeader
	if err := rlp.DecodeBytes(headerData, &extHeader); err != nil {
		log.Printf("Failed to decode as extended header: %v", err)
		
		// Let's try to decode raw RLP and inspect structure
		var raw []interface{}
		if err := rlp.DecodeBytes(headerData, &raw); err != nil {
			log.Fatalf("Failed to decode as raw RLP: %v", err)
		}
		
		log.Printf("RLP structure has %d elements:", len(raw))
		for i, field := range raw {
			switch v := field.(type) {
			case []byte:
				if len(v) == 32 {
					log.Printf("  [%d]: Hash %s", i, common.BytesToHash(v).Hex())
				} else if len(v) == 20 {
					log.Printf("  [%d]: Address %s", i, common.BytesToAddress(v).Hex())
				} else {
					log.Printf("  [%d]: Bytes[%d] %x", i, len(v), v)
				}
			case *big.Int:
				log.Printf("  [%d]: BigInt %s", i, v.String())
			case uint64:
				log.Printf("  [%d]: Uint64 %d", i, v)
			default:
				log.Printf("  [%d]: %T %v", i, v, v)
			}
		}
		
		// Extract state root manually (should be at index 3)
		if len(raw) > 3 {
			if stateRootBytes, ok := raw[3].([]byte); ok && len(stateRootBytes) == 32 {
				stateRoot := common.BytesToHash(stateRootBytes)
				log.Printf("\nExtracted State Root: %s", stateRoot.Hex())
				
				// Use this state root for the balance query
				fmt.Printf("\n=== STATE ROOT FOUND ===\n")
				fmt.Printf("Block Number: %d\n", targetBlock)
				fmt.Printf("State Root: %s\n", stateRoot.Hex())
				fmt.Printf("========================\n")
			}
		}
		
	} else {
		log.Printf("Successfully decoded as extended header!")
		log.Printf("State Root: %s", extHeader.Root.Hex())
		log.Printf("Block Number: %s", extHeader.Number.String())
		log.Printf("Gas Used: %d", extHeader.GasUsed)
		
		fmt.Printf("\n=== STATE ROOT FOUND ===\n")
		fmt.Printf("Block Number: %d\n", targetBlock)
		fmt.Printf("State Root: %s\n", extHeader.Root.Hex())
		fmt.Printf("========================\n")
	}
}