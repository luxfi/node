package main

import (
    "fmt"
    "github.com/luxfi/consensus/config"
    "github.com/luxfi/consensus/sampling"
)

func main() {
    fmt.Println("=== Consensus Parameter Verification ===")
    fmt.Println()
    
    // Check MainnetParameters
    mainnet := config.MainnetParameters
    fmt.Printf("Mainnet Parameters:\n")
    fmt.Printf("  K: %d\n", mainnet.K)
    fmt.Printf("  MaxItemProcessingTime: %v (%.3f seconds)\n", 
        mainnet.MaxItemProcessingTime, mainnet.MaxItemProcessingTime.Seconds())
    fmt.Println()
    
    // Check TestnetParameters
    testnet := config.TestnetParameters
    fmt.Printf("Testnet Parameters:\n")
    fmt.Printf("  K: %d\n", testnet.K)
    fmt.Printf("  MaxItemProcessingTime: %v (%.3f seconds)\n",
        testnet.MaxItemProcessingTime, testnet.MaxItemProcessingTime.Seconds())
    fmt.Println()
    
    // Check LocalParameters
    local := config.LocalParameters
    fmt.Printf("Local Parameters:\n")
    fmt.Printf("  K: %d\n", local.K)
    fmt.Printf("  MaxItemProcessingTime: %v (%.3f seconds)\n",
        local.MaxItemProcessingTime, local.MaxItemProcessingTime.Seconds())
    fmt.Println()
    
    // Check sampling.DefaultParameters (backward compatibility)
    defaults := sampling.DefaultParameters
    fmt.Printf("Sampling DefaultParameters (backward compatibility):\n")
    fmt.Printf("  K: %d\n", defaults.K)
    fmt.Printf("  MaxItemProcessingTime: %v (%.3f seconds)\n",
        defaults.MaxItemProcessingTime, defaults.MaxItemProcessingTime.Seconds())
    fmt.Println()
    
    // Verify correct timings
    fmt.Println("=== Verification ===")
    if mainnet.MaxItemProcessingTime.Seconds() == 0.963 {
        fmt.Println("✓ Mainnet consensus time: 0.963s")
    } else {
        fmt.Printf("✗ Mainnet consensus time incorrect: %.3fs\n", mainnet.MaxItemProcessingTime.Seconds())
    }
    
    if testnet.MaxItemProcessingTime.Seconds() == 0.63 {
        fmt.Println("✓ Testnet consensus time: 0.63s")
    } else {
        fmt.Printf("✗ Testnet consensus time incorrect: %.3fs\n", testnet.MaxItemProcessingTime.Seconds())
    }
    
    if local.MaxItemProcessingTime.Seconds() == 0.369 {
        fmt.Println("✓ Local consensus time: 0.369s")
    } else {
        fmt.Printf("✗ Local consensus time incorrect: %.3fs\n", local.MaxItemProcessingTime.Seconds())
    }
}
