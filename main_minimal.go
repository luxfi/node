package main

import (
    "fmt"
    "os"
    "time"
)

func main() {
    fmt.Println("Lux Node (Minimal Build)")
    fmt.Println("========================")
    fmt.Println("WARNING: This is a stub build for testing")
    fmt.Println("")
    
    // Parse basic flags
    for _, arg := range os.Args {
        if arg == "--version" {
            fmt.Println("luxd/1.13.4-minimal")
            os.Exit(0)
        }
        if arg == "--help" {
            fmt.Println("Usage: luxd [options]")
            fmt.Println("Options:")
            fmt.Println("  --data-dir PATH      Data directory")
            fmt.Println("  --network-id ID      Network ID (96369 for mainnet)")
            fmt.Println("  --http-port PORT     HTTP API port (default: 9650)")
            fmt.Println("  --staking-port PORT  Staking port (default: 9651)")
            fmt.Println("  --version           Show version")
            os.Exit(0)
        }
    }
    
    fmt.Println("Starting node...")
    fmt.Println("Data directory: ~/.luxd")
    fmt.Println("Network ID: 96369")
    fmt.Println("HTTP API: http://localhost:9650")
    fmt.Println("Staking port: 9651")
    fmt.Println("")
    fmt.Println("Node is running (stub mode)")
    fmt.Println("Press Ctrl+C to stop")
    
    // Keep running
    for {
        time.Sleep(10 * time.Second)
    }
}
