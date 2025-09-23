package main

import (
	"fmt"
	"log"
	"math/big"

	"github.com/dgraph-io/badger/v3"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/crypto"
)

const (
	targetBlock = 1000000 // 0xF4240
	dbPath      = "/home/z/.luxd/chainData/C/db/badgerdb/ethdb"
	
	// luxdefi.eth address: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714
	targetAddress = "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
	
	// State root extracted from block 1,000,000
	stateRootHex = "0x6c7090b26bb10ee0e9ef197ccc67c5ea7b4feccbe04d68eec9871c7edee5c897"
)

// Ethereum account structure
type Account struct {
	Nonce    uint64      `json:"nonce"    gencodec:"required"`
	Balance  *big.Int    `json:"balance"  gencodec:"required"`
	Root     common.Hash `json:"root"     gencodec:"required"` // merkle root of the storage trie
	CodeHash []byte      `json:"codeHash" gencodec:"required"`
}

func main() {
	log.Printf("=== FINAL BALANCE QUERY ===")
	log.Printf("Target Block: %d", targetBlock)
	log.Printf("Target Address: %s (luxdefi.eth)", targetAddress)
	log.Printf("State Root: %s", stateRootHex)
	log.Printf("===========================")
	
	// Open BadgerDB
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open BadgerDB: %v", err)
	}
	defer db.Close()
	
	// Convert target address to common.Address
	addr := common.HexToAddress(targetAddress)
	stateRoot := common.HexToHash(stateRootHex)
	
	// Calculate the account key: keccak256(address)
	accountKey := crypto.Keccak256(addr.Bytes())
	log.Printf("Account key (hash): %x", accountKey)
	
	// Try different approaches to find the account data
	found := false
	var accountData []byte
	
	// Method 1: Direct key lookup (legacy hash-based storage)
	log.Printf("Trying direct key lookup...")
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(accountKey)
		if err != nil {
			return err
		}
		accountData, err = item.ValueCopy(nil)
		return err
	})
	if err == nil && len(accountData) > 0 {
		log.Printf("Found account data via direct lookup!")
		found = true
	} else {
		log.Printf("Direct lookup failed: %v", err)
	}
	
	// Method 2: Path-based storage (newer geth format)
	if !found {
		log.Printf("Trying path-based storage...")
		pathKey := append([]byte("A"), accountKey...) // "A" prefix for account trie nodes
		err = db.View(func(txn *badger.Txn) error {
			item, err := txn.Get(pathKey)
			if err != nil {
				return err
			}
			accountData, err = item.ValueCopy(nil)
			return err
		})
		if err == nil && len(accountData) > 0 {
			log.Printf("Found account data via path-based storage!")
			found = true
		} else {
			log.Printf("Path-based lookup failed: %v", err)
		}
	}
	
	// Method 3: Search for keys containing the account hash
	if !found {
		log.Printf("Searching for keys containing account hash...")
		err = db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			it := txn.NewIterator(opts)
			defer it.Close()
			
			count := 0
			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				key := item.Key()
				
				// Look for keys that contain our account hash
				if contains(key, accountKey) {
					log.Printf("Found potential key: %x", key)
					value, err := item.ValueCopy(nil)
					if err == nil && len(value) > 0 {
						// Try to decode as account
						var testAccount Account
						if rlp.DecodeBytes(value, &testAccount) == nil {
							accountData = value
							found = true
							log.Printf("Found account data in key: %x", key)
							return nil
						}
					}
				}
				
				count++
				if count > 100000 { // Limit search to avoid timeout
					break
				}
			}
			return nil
		})
	}
	
	if !found {
		// Try searching with different key patterns
		log.Printf("Trying alternative key patterns...")
		
		// Look for snapshot data (if available)
		snapshotKey := append([]byte("a"), crypto.Keccak256(addr.Bytes())...) // "a" prefix for snapshot accounts
		err = db.View(func(txn *badger.Txn) error {
			item, err := txn.Get(snapshotKey)
			if err != nil {
				return err
			}
			accountData, err = item.ValueCopy(nil)
			return err
		})
		if err == nil && len(accountData) > 0 {
			log.Printf("Found account data in snapshot!")
			found = true
		}
	}
	
	if !found {
		fmt.Printf("\n=== ACCOUNT NOT FOUND ===\n")
		fmt.Printf("The account %s was not found in the state at block %d.\n", addr.Hex(), targetBlock)
		fmt.Printf("This could mean:\n")
		fmt.Printf("1. The account has zero balance and no transactions at this block\n")
		fmt.Printf("2. The account was created after block %d\n", targetBlock)
		fmt.Printf("3. The state trie requires traversal (more complex implementation needed)\n")
		fmt.Printf("========================\n")
		return
	}
	
	// Decode the account data
	var account Account
	if err := rlp.DecodeBytes(accountData, &account); err != nil {
		log.Fatalf("Failed to decode account data: %v", err)
	}
	
	// Convert balance to human-readable format
	balanceInWei := account.Balance
	balanceInEther := new(big.Float).Quo(new(big.Float).SetInt(balanceInWei), big.NewFloat(1e18))
	
	fmt.Printf("\n=== BALANCE QUERY RESULT ===\n")
	fmt.Printf("Block Number: %d (0x%X)\n", targetBlock, targetBlock)
	fmt.Printf("State Root: %s\n", stateRoot.Hex())
	fmt.Printf("Address: %s (luxdefi.eth)\n", addr.Hex())
	fmt.Printf("Balance (Wei): %s\n", balanceInWei.String())
	fmt.Printf("Balance (ETH): %s\n", balanceInEther.Text('f', 18))
	fmt.Printf("Nonce: %d\n", account.Nonce)
	fmt.Printf("Storage Root: %s\n", common.BytesToHash(account.Root[:]).Hex())
	fmt.Printf("Code Hash: %x\n", account.CodeHash)
	fmt.Printf("============================\n")
}

// Helper function to check if a byte slice contains another byte slice
func contains(haystack, needle []byte) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		found := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}