// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func main() {
	opts := badger.DefaultOptions("/Users/z/work/lux/genesis/migrated-ethdb")
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Find header key for block 1
	err = db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		blockNum := uint64(1)
		prefix := make([]byte, 9)
		prefix[0] = 'h'
		binary.BigEndian.PutUint64(prefix[1:9], blockNum)

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key()

			if len(key) == 41 && key[0] == 'h' {
				fmt.Printf("Found header key for block 1: %x\n", key)

				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				fmt.Printf("\nValue length: %d bytes\n", len(val))
				fmt.Printf("First 200 bytes (hex):\n%s\n\n", hex.EncodeToString(val[:min(200, len(val))]))

				// Decode RLP structure field by field
				fmt.Println("=== RLP Structure Analysis ===")
				s := rlp.NewStream(bytes.NewReader(val), 0)

				// Start list
				listSize, err := s.List()
				if err != nil {
					fmt.Printf("ERROR: Not an RLP list: %v\n", err)
					return err
				}
				fmt.Printf("RLP List size: %d bytes\n\n", listSize)

				// Field 0: ParentHash
				fmt.Println("Field 0 (ParentHash):")
				kind, size, err := s.Kind()
				if err != nil {
					fmt.Printf("  ERROR getting kind: %v\n", err)
					return err
				}
				fmt.Printf("  Kind: %v, Size: %d\n", kind, size)

				var parentHash common.Hash
				if err := s.Decode(&parentHash); err != nil {
					fmt.Printf("  ERROR decoding as Hash: %v\n", err)
					// Try reading raw bytes
					raw := make([]byte, size)
					if _, err := s.Raw(); err == nil {
						fmt.Printf("  Raw bytes: %x\n", raw)
					}
				} else {
					fmt.Printf("  Decoded Hash: %s\n", parentHash.Hex())
				}

				// Field 1: UncleHash
				fmt.Println("\nField 1 (UncleHash):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var uncleHash common.Hash
					if err := s.Decode(&uncleHash); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", uncleHash.Hex())
					}
				}

				// Field 2: Coinbase
				fmt.Println("\nField 2 (Coinbase):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var coinbase common.Address
					if err := s.Decode(&coinbase); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", coinbase.Hex())
					}
				}

				// Field 3: Root
				fmt.Println("\nField 3 (Root):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var root common.Hash
					if err := s.Decode(&root); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", root.Hex())
					}
				}

				// Field 4: TxHash
				fmt.Println("\nField 4 (TxHash):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var txHash common.Hash
					if err := s.Decode(&txHash); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", txHash.Hex())
					}
				}

				// Field 5: ReceiptHash
				fmt.Println("\nField 5 (ReceiptHash):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var receiptHash common.Hash
					if err := s.Decode(&receiptHash); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", receiptHash.Hex())
					}
				}

				// Field 6: Bloom
				fmt.Println("\nField 6 (Bloom):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d bytes\n", kind, size)
					var bloom [256]byte
					if err := s.Decode(&bloom); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %x... (256 bytes)\n", bloom[:8])
					}
				}

				// Field 7: Difficulty
				fmt.Println("\nField 7 (Difficulty):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var difficulty *big.Int
					if err := s.Decode(&difficulty); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", difficulty.String())
					}
				}

				// Field 8: Number
				fmt.Println("\nField 8 (Number):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var number *big.Int
					if err := s.Decode(&number); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", number.String())
					}
				}

				// Field 9: GasLimit
				fmt.Println("\nField 9 (GasLimit):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var gasLimit uint64
					if err := s.Decode(&gasLimit); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %d\n", gasLimit)
					}
				}

				// Field 10: GasUsed
				fmt.Println("\nField 10 (GasUsed):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var gasUsed uint64
					if err := s.Decode(&gasUsed); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %d\n", gasUsed)
					}
				}

				// Field 11: Time
				fmt.Println("\nField 11 (Time):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var timestamp uint64
					if err := s.Decode(&timestamp); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %d\n", timestamp)
					}
				}

				// Field 12: Extra
				fmt.Println("\nField 12 (Extra):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var extra []byte
					if err := s.Decode(&extra); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %x (%d bytes)\n", extra, len(extra))
					}
				}

				// Field 13: MixDigest
				fmt.Println("\nField 13 (MixDigest):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var mixDigest common.Hash
					if err := s.Decode(&mixDigest); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %s\n", mixDigest.Hex())
					}
				}

				// Field 14: Nonce
				fmt.Println("\nField 14 (Nonce):")
				kind, size, err = s.Kind()
				if err != nil {
					fmt.Printf("  ERROR: %v\n", err)
				} else {
					fmt.Printf("  Kind: %v, Size: %d\n", kind, size)
					var nonce [8]byte
					if err := s.Decode(&nonce); err != nil {
						fmt.Printf("  ERROR decoding: %v\n", err)
					} else {
						fmt.Printf("  Decoded: %x\n", nonce)
					}
				}

				// Check for more fields (BaseFee, ExtDataHash)
				fmt.Println("\nChecking for additional fields...")
				fieldNum := 15
				for {
					kind, size, err = s.Kind()
					if err == rlp.EOL {
						fmt.Println("End of list reached")
						break
					}
					if err != nil {
						fmt.Printf("ERROR at field %d: %v\n", fieldNum, err)
						break
					}
					fmt.Printf("\nField %d: Kind=%v, Size=%d\n", fieldNum, kind, size)
					
					// Try to read as raw bytes
					raw, err := s.Raw()
					if err != nil {
						fmt.Printf("  ERROR reading raw: %v\n", err)
						break
					}
					fmt.Printf("  Raw bytes: %x\n", raw)
					fieldNum++
				}

				return nil
			}
		}

		fmt.Println("No header key found for block 1")
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
