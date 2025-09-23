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
	"github.com/luxfi/crypto"
)

const (
	targetBlock = 1000000 // 0xF4240
	dbPath      = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	// luxdefi.eth address: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714
	targetAddress = "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
)

// Database key prefixes from geth rawdb schema
var (
	headerPrefix       = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
	headerHashSuffix   = []byte("n") // headerPrefix + num (uint64 big endian) + headerHashSuffix -> hash
)

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerHashKey = headerPrefix + num (uint64 big endian) + headerHashSuffix
func headerHashKey(number uint64) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), headerHashSuffix...)
}

// headerKey = headerPrefix + num (uint64 big endian) + hash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// Simple trie walker to find account data
type TrieWalker struct {
	db *badger.DB
}

func (tw *TrieWalker) Get(key []byte) ([]byte, error) {
	var value []byte
	err := tw.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		value, err = item.ValueCopy(nil)
		return err
	})
	return value, err
}

func (tw *TrieWalker) getAccountFromTrie(stateRoot common.Hash, address common.Address) ([]byte, error) {
	// In Ethereum state trie, account key is keccak256(address)
	accountKey := Keccak256(address.Bytes())
	
	// Try to get the account data directly using the state root and account hash
	// This is a simplified approach - in reality we'd need to traverse the trie
	
	// First try the legacy trie node format (hash -> data)
	accountData, err := tw.Get(accountKey)
	if err == nil && len(accountData) > 0 {
		return accountData, nil
	}
	
	// Try path-based storage scheme (newer format)
	// Account trie nodes use prefix "A" + path
	pathKey := append([]byte("A"), accountKey...)
	accountData, err = tw.Get(pathKey)
	if err == nil && len(accountData) > 0 {
		return accountData, nil
	}
	
	return nil, fmt.Errorf("account not found")
}

func Keccak256(data []byte) []byte {
	return crypto.Keccak256(data)
}

func main() {
	log.Printf("Querying balance for address %s at block %d", targetAddress, targetBlock)
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable logging for cleaner output
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Convert target address to common.Address
	addr := common.HexToAddress(targetAddress)
	log.Printf("Converted address: %s", addr.Hex())
	
	// Find the actual block hash by looking at available keys
	blockPrefix := append(headerPrefix, encodeBlockNumber(targetBlock)...)
	var actualBlockHash common.Hash
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
				actualBlockHash = common.BytesToHash(hashBytes)
				headerKey = make([]byte, len(key))
				copy(headerKey, key)
				log.Printf("Found actual block hash for block %d: %s", targetBlock, actualBlockHash.Hex())
				return nil
			}
		}
		return fmt.Errorf("no header found for block %d", targetBlock)
	})
	if err != nil {
		log.Fatalf("Error finding block hash: %v", err)
	}
	
	// Read header for the block using the found key
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
		log.Fatalf("No header found for block %d with hash %s: %v", targetBlock, actualBlockHash.Hex(), err)
	}
	
	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		log.Fatalf("Failed to decode header: %v", err)
	}
	
	stateRoot := header.Root
	log.Printf("State root for block %d: %s", targetBlock, stateRoot.Hex())
	
	// Create trie walker
	walker := &TrieWalker{db: db}
	
	// Try to get account data
	accountData, err := walker.getAccountFromTrie(stateRoot, addr)
	if err != nil {
		log.Printf("Account not found in simple lookup, trying trie traversal...")
		
		// If direct lookup fails, we need to traverse the trie
		// For now, let's check if account exists with zero balance
		fmt.Printf("Account %s not found in state at block %d\n", addr.Hex(), targetBlock)
		fmt.Printf("This might indicate:\n")
		fmt.Printf("1. Account has zero balance and no transactions\n")
		fmt.Printf("2. Account doesn't exist at this block height\n")
		fmt.Printf("3. Need full trie traversal (more complex implementation)\n")
		return
	}
	
	// Decode account state - Ethereum account format
	type Account struct {
		Nonce    uint64
		Balance  *big.Int
		Root     common.Hash // Storage root
		CodeHash []byte     // Code hash
	}
	
	var account Account
	if err := rlp.DecodeBytes(accountData, &account); err != nil {
		log.Fatalf("Failed to decode account data: %v", err)
	}
	
	// Convert balance to human-readable format
	balanceInWei := account.Balance
	balanceInEther := new(big.Float).Quo(new(big.Float).SetInt(balanceInWei), big.NewFloat(1e18))
	
	fmt.Printf("\n=== BALANCE QUERY RESULT ===\n")
	fmt.Printf("Block Number: %d (0x%X)\n", targetBlock, targetBlock)
	fmt.Printf("Block Hash: %s\n", actualBlockHash.Hex())
	fmt.Printf("State Root: %s\n", stateRoot.Hex())
	fmt.Printf("Address: %s (luxdefi.eth)\n", addr.Hex())
	fmt.Printf("Balance (Wei): %s\n", balanceInWei.String())
	fmt.Printf("Balance (ETH): %s\n", balanceInEther.Text('f', 18))
	fmt.Printf("Nonce: %d\n", account.Nonce)
	fmt.Printf("============================\n")
}