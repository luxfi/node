package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	opts := badger.DefaultOptions("/Users/z/work/lux/genesis/migrated-ethdb")
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Test block 1 and a few others
	testBlocks := []uint64{1, 2, 10, 100, 1000}

	err = db.View(func(txn *badger.Txn) error {
		for _, blockNum := range testBlocks {
			prefix := make([]byte, 9)
			prefix[0] = 'h'
			binary.BigEndian.PutUint64(prefix[1:9], blockNum)

			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()

			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				item := it.Item()
				key := item.Key()

				if len(key) == 41 && key[0] == 'h' {
					// Extract hash from key (last 32 bytes)
					hashInKey := key[9:41]

					// Read the RLP-encoded header value
					value, err := item.ValueCopy(nil)
					if err != nil {
						return err
					}

					// Compute hash of the RLP-encoded header
					computedHash := crypto.Keccak256Hash(value)

					fmt.Printf("Block %d:\n", blockNum)
					fmt.Printf("  Hash in key:     %s\n", hex.EncodeToString(hashInKey))
					fmt.Printf("  Computed hash:   %s\n", computedHash.Hex())
					fmt.Printf("  Match: %v\n\n", hex.EncodeToString(hashInKey) == computedHash.Hex()[2:])
					break
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}
