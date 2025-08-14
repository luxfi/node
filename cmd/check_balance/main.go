package main

import (
    "encoding/hex"
    "fmt"
    "math/big"
    "path/filepath"
    
    "github.com/cockroachdb/pebble"
    "github.com/luxfi/geth/rlp"
)

type Account struct {
    Nonce    uint64
    Balance  *big.Int
    Root     [32]byte
    CodeHash []byte
}

func main() {
    dbPath := "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
    
    // Address to check: 0x8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84
    addr, _ := hex.DecodeString("8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84")
    
    db, err := pebble.Open(filepath.Clean(dbPath), &pebble.Options{ReadOnly: true})
    if err \!= nil {
        panic(err)
    }
    defer db.Close()
    
    // Check account state (prefix 0x00)
    accKey := append([]byte{0x00}, addr...)
    val, closer, err := db.Get(accKey)
    if err \!= nil {
        fmt.Printf("No account state for address 0x8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84\n")
        fmt.Printf("Error: %v\n", err)
    } else {
        defer closer.Close()
        
        // Decode the account
        var acc Account
        if err := rlp.DecodeBytes(val, &acc); err \!= nil {
            fmt.Printf("Failed to decode account: %v\n", err)
        } else {
            // Convert balance to LUX (divide by 10^18)
            balanceEth := new(big.Float).Quo(new(big.Float).SetInt(acc.Balance), big.NewFloat(1e18))
            fmt.Printf("Address: 0x8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84\n")
            fmt.Printf("Balance: %s wei\n", acc.Balance.String())
            fmt.Printf("Balance: %.18f LUX\n", balanceEth)
            fmt.Printf("Nonce: %d\n", acc.Nonce)
            fmt.Printf("Storage Root: %x\n", acc.Root)
            fmt.Printf("Code Hash: %x\n", acc.CodeHash)
        }
    }
}
