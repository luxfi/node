package main

import (
	"fmt"
	"log"
	"math/big"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/trie"
)

const (
	targetBlock = 1000000 // 0xF4240
	dbPath      = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	// luxdefi.eth address: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714
	targetAddress = "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
)

// BadgerDB wrapper to implement ethdb.Database
type BadgerWrapper struct {
	db *badger.DB
}

func NewBadgerWrapper(path string) (*BadgerWrapper, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil // Disable logging for cleaner output
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerWrapper{db: db}, nil
}

func (b *BadgerWrapper) Get(key []byte) ([]byte, error) {
	var value []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		value, err = item.ValueCopy(nil)
		return err
	})
	return value, err
}

func (b *BadgerWrapper) Has(key []byte) (bool, error) {
	err := b.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return false, nil
	}
	return err == nil, err
}

func (b *BadgerWrapper) Put(key []byte, value []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

func (b *BadgerWrapper) Delete(key []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (b *BadgerWrapper) Close() error {
	return b.db.Close()
}

func (b *BadgerWrapper) NewBatch() ethdb.Batch {
	// Not needed for read operations
	return nil
}

func (b *BadgerWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	// Not needed for this specific query
	return nil
}

func (b *BadgerWrapper) Stat(property string) (string, error) {
	return "", nil
}

func (b *BadgerWrapper) Compact(start []byte, limit []byte) error {
	return nil
}

func main() {
	log.Printf("Querying balance for address %s at block %d", targetAddress, targetBlock)
	
	// Open BadgerDB
	db, err := NewBadgerWrapper(dbPath)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Convert target address to common.Address
	addr := common.HexToAddress(targetAddress)
	log.Printf("Converted address: %s", addr.Hex())
	
	// Read canonical hash for block 1,000,000
	canonicalHash := rawdb.ReadCanonicalHash(db, targetBlock)
	if canonicalHash == (common.Hash{}) {
		log.Fatalf("No canonical hash found for block %d", targetBlock)
	}
	log.Printf("Canonical hash for block %d: %s", targetBlock, canonicalHash.Hex())
	
	// Read header for the block
	header := rawdb.ReadHeader(db, canonicalHash, targetBlock)
	if header == nil {
		log.Fatalf("No header found for block %d with hash %s", targetBlock, canonicalHash.Hex())
	}
	
	stateRoot := header.Root
	log.Printf("State root for block %d: %s", targetBlock, stateRoot.Hex())
	
	// Create state database
	stateDB := state.NewDatabase(db)
	
	// Open state trie at the specific root
	stateTrie, err := trie.New(trie.StateTrieID(stateRoot), stateDB)
	if err != nil {
		log.Fatalf("Failed to open state trie: %v", err)
	}
	
	// Get account state - use the address hash as the key
	accountKey := common.Keccak256(addr.Bytes())
	accountData, err := stateTrie.Get(accountKey)
	if err != nil {
		log.Fatalf("Failed to get account data: %v", err)
	}
	
	if len(accountData) == 0 {
		fmt.Printf("Account %s has zero balance at block %d\n", addr.Hex(), targetBlock)
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
	// Account data is RLP encoded
	if err := rlp.DecodeBytes(accountData, &account); err != nil {
		log.Fatalf("Failed to decode account data: %v", err)
	}
	
	// Convert balance to human-readable format
	balanceInWei := account.Balance
	balanceInEther := new(big.Float).Quo(new(big.Float).SetInt(balanceInWei), big.NewFloat(1e18))
	
	fmt.Printf("\n=== BALANCE QUERY RESULT ===\n")
	fmt.Printf("Block Number: %d (0x%X)\n", targetBlock, targetBlock)
	fmt.Printf("Block Hash: %s\n", canonicalHash.Hex())
	fmt.Printf("State Root: %s\n", stateRoot.Hex())
	fmt.Printf("Address: %s (luxdefi.eth)\n", addr.Hex())
	fmt.Printf("Balance (Wei): %s\n", balanceInWei.String())
	fmt.Printf("Balance (ETH): %s\n", balanceInEther.Text('f', 18))
	fmt.Printf("Nonce: %d\n", account.Nonce)
	fmt.Printf("============================\n")
}