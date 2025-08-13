package main

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    
    "github.com/luxfi/database"
    "github.com/luxfi/database/prefixdb"
    "github.com/luxfi/ids"
    "github.com/luxfi/node/vms/cchainvm"
    consensusNode "github.com/luxfi/node/consensus"
    "github.com/luxfi/log"
)

func main() {
    fmt.Println("=== Testing C-Chain Boot with Migrated Data ===")
    
    // Set environment to signal we have migrated data
    os.Setenv("LUX_IMPORTED_HEIGHT", "1082780")
    os.Setenv("LUX_IMPORTED_BLOCK_ID", "32dede1fc8e0f11ecde12fb42aef7933fc6c5fcf863bc277b5eac08ae4d461f0")
    
    // Create VM
    vm := &cchainvm.VM{}
    
    // Create context
    chainID := ids.ID{}
    copy(chainID[:], []byte("cchain"))
    
    ctx := &consensusNode.Context{
        NetworkID:    96369,
        ChainID:      chainID,
        ChainDataDir: "/Users/z/.luxd/network-96369/chains/EWi9aPkGe6EfJ3SobCAmSUXRPLa4brF3cThwPwmHTrD1y13jy",
        Log:          log.NoOp{},
    }
    
    // Open the main database
    dbPath := filepath.Join(ctx.ChainDataDir, "db")
    db, err := database.New(dbPath, nil)
    if err != nil {
        panic(fmt.Sprintf("Failed to open database: %v", err))
    }
    defer db.Close()
    
    // Create prefixed database for C-Chain
    cchainDB := prefixdb.New([]byte("cchain"), db)
    
    // Initialize VM with nil genesis (should use existing data)
    fmt.Println("Initializing VM with nil genesis...")
    err = vm.Initialize(
        context.Background(),
        ctx,
        cchainDB,
        nil,  // nil genesis - should use existing
        nil,  // upgrade bytes
        nil,  // config bytes
        nil,  // fxs
        nil,  // app sender
    )
    
    if err != nil {
        fmt.Printf("VM initialization failed: %v\n", err)
        fmt.Println("Attempting to diagnose issue...")
        
        // Try to check what's in the database
        ethdbPath := filepath.Join(ctx.ChainDataDir, "ethdb")
        fmt.Printf("Checking ethdb at: %s\n", ethdbPath)
        if _, err := os.Stat(ethdbPath); err == nil {
            fmt.Println("✓ ethdb directory exists")
        } else {
            fmt.Printf("✗ ethdb not found: %v\n", err)
        }
        
        vmPath := filepath.Join(ctx.ChainDataDir, "vm")
        fmt.Printf("Checking vm at: %s\n", vmPath)
        if _, err := os.Stat(vmPath); err == nil {
            fmt.Println("✓ vm directory exists")
        } else {
            fmt.Printf("✗ vm not found: %v\n", err)
        }
    } else {
        fmt.Println("✓ VM initialized successfully!")
        
        // Try to get the last accepted block
        lastAccepted, err := vm.LastAccepted(context.Background())
        if err != nil {
            fmt.Printf("Failed to get last accepted: %v\n", err)
        } else {
            fmt.Printf("✓ Last accepted block: %s\n", lastAccepted)
        }
    }
    
    fmt.Println("\n=== Test Complete ===")
}