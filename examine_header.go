package main

import (
	"encoding/binary"
	"encoding/hex"
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

func main() {
	log.Printf("Examining header data for block %d", targetBlock)
	
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
				hashBytes := key[len(blockPrefix):]
				actualBlockHash := common.BytesToHash(hashBytes)
				headerKey = make([]byte, len(key))
				copy(headerKey, key)
				log.Printf("Found actual block hash for block %d: %s", targetBlock, actualBlockHash.Hex())
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
	
	log.Printf("Header data length: %d bytes", len(headerData))
	log.Printf("Header data hex: %s", hex.EncodeToString(headerData[:min(100, len(headerData))]))
	
	// Try to decode with different header structures
	log.Printf("Attempting to decode header...")
	
	// Try standard geth header
	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		log.Printf("Failed to decode as standard header: %v", err)
		
		// Try with a simpler header structure (without withdrawals)
		type SimpleHeader struct {
			ParentHash  common.Hash    `json:"parentHash"       gencodec:"required"`
			UncleHash   common.Hash    `json:"sha3Uncles"       gencodec:"required"`
			Coinbase    common.Address `json:"miner"            gencodec:"required"`
			Root        common.Hash    `json:"stateRoot"        gencodec:"required"`
			TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
			ReceiptHash common.Hash    `json:"receiptsRoot"     gencodec:"required"`
			Bloom       types.Bloom    `json:"logsBloom"        gencodec:"required"`
			Difficulty  *big.Int       `json:"difficulty"       gencodec:"required"`
			Number      *big.Int       `json:"number"           gencodec:"required"`
			GasLimit    uint64         `json:"gasLimit"         gencodec:"required"`
			GasUsed     uint64         `json:"gasUsed"          gencodec:"required"`
			Time        uint64         `json:"timestamp"        gencodec:"required"`
			Extra       []byte         `json:"extraData"        gencodec:"required"`
			MixDigest   common.Hash    `json:"mixHash"`
			Nonce       types.BlockNonce `json:"nonce"`
			BaseFee     *big.Int       `json:"baseFeePerGas" rlp:"optional"`
		}
		
		var simpleHeader SimpleHeader
		if err := rlp.DecodeBytes(headerData, &simpleHeader); err != nil {
			log.Printf("Failed to decode as simple header: %v", err)
		} else {
			log.Printf("Successfully decoded as simple header!")
			log.Printf("State Root: %s", simpleHeader.Root.Hex())
			log.Printf("Block Number: %s", simpleHeader.Number.String())
			log.Printf("Gas Used: %d", simpleHeader.GasUsed)
		}
	} else {
		log.Printf("Successfully decoded as standard header!")
		log.Printf("State Root: %s", header.Root.Hex())
		log.Printf("Block Number: %s", header.Number.String())
		log.Printf("Gas Used: %d", header.GasUsed)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}