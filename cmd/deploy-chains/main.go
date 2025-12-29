// Deploy testnet chains: Zootest (L2), Hanzotest (L1), SPCtest (L2)
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
)

func main() {
	// Load node0 EC key (validator 1, has P-chain balance from genesis)
	keyHex, err := os.ReadFile(os.ExpandEnv("$HOME/.lux/keys/node0/ec/private.key"))
	if err != nil {
		log.Fatalf("Failed to read key: %v", err)
	}
	keyBytes, err := hex.DecodeString(strings.TrimSpace(string(keyHex)))
	if err != nil {
		log.Fatalf("Failed to decode key: %v", err)
	}
	privKey, err := secp256k1.ToPrivateKey(keyBytes)
	if err != nil {
		log.Fatalf("Failed to create private key: %v", err)
	}

	ctx := context.Background()
	uri := "http://127.0.0.1:9630"

	// Create keychain
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(privKey))

	fmt.Println("=== Creating Testnet Chains ===")
	fmt.Printf("URI: %s\n", uri)
	fmt.Printf("Address: %s\n", privKey.Address())

	// Initialize wallet
	fmt.Println("\nInitializing wallet...")
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         uri,
		LUXKeychain: kc,
		EthKeychain: kc,
	})
	if err != nil {
		log.Fatalf("Failed to initialize wallet: %v", err)
	}

	pWallet := wallet.P()

	// Owner for subnets
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{privKey.Address()},
	}

	// EVM VM ID
	evmVMID := constants.EVMID

	// Load genesis files from genesis repo
	genesisDir := os.ExpandEnv("$HOME/work/lux/genesis/configs")

	// Chain configurations with file paths
	chainConfigs := []struct {
		Name        string
		GenesisFile string
	}{
		{Name: "zootest", GenesisFile: genesisDir + "/zoo-testnet/genesis.json"},
		{Name: "hanzotest", GenesisFile: genesisDir + "/hanzo-testnet/genesis.json"},
		{Name: "spctest", GenesisFile: genesisDir + "/spc-testnet/genesis.json"},
	}

	type chainInfo struct {
		Name    string
		Genesis string
	}
	var chains []chainInfo

	for _, cfg := range chainConfigs {
		genesisBytes, err := os.ReadFile(cfg.GenesisFile)
		if err != nil {
			log.Printf("Warning: Failed to read genesis for %s: %v, using default", cfg.Name, err)
			continue
		}
		chains = append(chains, chainInfo{
			Name:    cfg.Name,
			Genesis: string(genesisBytes),
		})
		fmt.Printf("Loaded genesis for %s (%d bytes)\n", cfg.Name, len(genesisBytes))
	}

	for _, chain := range chains {
		fmt.Printf("\n=== Creating %s ===\n", chain.Name)

		// Step 1: Create subnet
		fmt.Printf("Creating subnet for %s...\n", chain.Name)
		createSubnetTx, err := pWallet.IssueCreateSubnetTx(owner)
		if err != nil {
			log.Printf("Failed to create subnet for %s: %v", chain.Name, err)
			continue
		}
		subnetID := createSubnetTx.ID()
		fmt.Printf("  Subnet ID: %s\n", subnetID)

		// Wait for acceptance
		fmt.Println("  Waiting for subnet acceptance...")
		time.Sleep(3 * time.Second)

		// Step 2: Create blockchain in the subnet
		fmt.Printf("Creating blockchain for %s...\n", chain.Name)
		createChainTx, err := pWallet.IssueCreateChainTx(
			subnetID,
			[]byte(chain.Genesis),
			evmVMID,
			nil, // fxIDs
			chain.Name,
		)
		if err != nil {
			log.Printf("Failed to create chain for %s: %v", chain.Name, err)
			continue
		}
		chainID := createChainTx.ID()
		fmt.Printf("  Chain ID: %s\n", chainID)

		// Wait for acceptance
		fmt.Println("  Waiting for chain acceptance...")
		time.Sleep(3 * time.Second)

		fmt.Printf("  ✅ %s deployed successfully!\n", chain.Name)
	}

	fmt.Println("\n=== Deployment Complete ===")
}
