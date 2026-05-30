// deploy-chains creates chains on the Lux network using the SDK wallet.
//
// Usage:
//
//	MNEMONIC="word1 word2 ..." go run . --network=mainnet
//	go run . --network=mainnet mainnet-key-01
//	MNEMONIC="word1 word2 ..." go run . --network=mainnet --chain=zoo
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/constants"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/node/wallet/network/primary/examples/keyutil"
	"github.com/luxfi/sdk/info"
	"github.com/luxfi/sdk/platformvm"
	"github.com/luxfi/utxo/secp256k1fx"
)

var evmVMID = ids.FromStringOrPanic("ag3GReYPNuSR17rUP8acMdZipQBikdXNRKDyFszAysmy3vDXE")

type networkConfig struct {
	URI    string
	Suffix string
}

func main() {
	network := flag.String("network", "", "network: mainnet, testnet, devnet")
	chainFilter := flag.String("chain", "", "deploy only this chain (zoo, hanzo, spc, pars)")
	chainNameOverride := flag.String("chain-name", "", "override blockchain name on P-chain (for redeployment)")
	uri := flag.String("uri", "", "override node URI")
	skipValidators := flag.Bool("skip-validators", false, "skip adding chain validators")
	stateDir := flag.String("state-dir", "", "path to state/chains directory")
	flag.Parse()

	if *network == "" {
		log.Fatal("--network required (mainnet, testnet, devnet)")
	}

	// Resolve state dir
	if *stateDir == "" {
		home, _ := os.UserHomeDir()
		*stateDir = filepath.Join(home, "work/lux/state/chains")
	}

	nets := map[string]networkConfig{
		"mainnet": {URI: "https://api.lux.network", Suffix: "mainnet"},
		"testnet": {URI: "https://api.lux-test.network", Suffix: "testnet"},
		"devnet":  {URI: "https://api.lux-dev.network", Suffix: "devnet"},
	}

	nc, ok := nets[*network]
	if !ok {
		log.Fatalf("unknown network: %s", *network)
	}
	if *uri != "" {
		nc.URI = *uri
	}

	chains := []string{"zoo", "hanzo", "spc", "pars"}
	if *chainFilter != "" {
		chains = []string{*chainFilter}
	}

	// Load key
	key := keyutil.MustLoadKey()
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	addr := key.PublicKey().Address()
	log.Printf("Deploying to %s via %s (addr: %s, hex: %x)", *network, nc.URI, addr, addr[:])

	ctx := context.Background()

	// Fetch ALL validator node IDs from P-chain
	var nodeIDs []ids.NodeID
	var minPrimaryEnd uint64
	if !*skipValidators {
		pClient := platformvm.NewClient(nc.URI)
		validators, err := pClient.GetCurrentValidators(ctx, ids.Empty, nil)
		if err != nil {
			log.Printf("WARNING: could not fetch validators: %v (skipping validators)", err)
			*skipValidators = true
		} else {
			for _, v := range validators {
				nodeIDs = append(nodeIDs, v.NodeID)
				log.Printf("Validator: %s (end: %d)", v.NodeID, v.EndTime)
				if minPrimaryEnd == 0 || v.EndTime < minPrimaryEnd {
					minPrimaryEnd = v.EndTime
				}
			}
			log.Printf("Found %d validators (min primary end: %d)", len(nodeIDs), minPrimaryEnd)
		}
	}

	// Also get our connected node ID for info
	infoClient := info.NewClient(nc.URI)
	myNodeID, _, err := infoClient.GetNodeID(ctx)
	if err == nil {
		log.Printf("Connected to: %s", myNodeID)
	}

	// Check P-chain balance via direct RPC first
	log.Println("Checking P-chain balance...")
	{
		pClient := platformvm.NewClient(nc.URI)
		balResp, err := pClient.GetBalance(ctx, []ids.ShortID{addr})
		if err != nil {
			log.Printf("WARNING: direct getBalance failed: %v", err)
		} else {
			log.Printf("Direct P-chain balance (via RPC): %+v", balResp)
		}
	}

	fundWallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         nc.URI,
		LUXKeychain: kc,
		EVMKeychain: kc,
	})
	if err != nil {
		log.Fatalf("wallet sync failed: %v", err)
	}

	luxAssetID := fundWallet.X().Builder().Context().UTXOAssetID
	pBalanceMap, err := fundWallet.P().Builder().GetBalance()
	if err != nil {
		log.Fatalf("P-chain balance check failed: %v", err)
	}
	pBalance := pBalanceMap[luxAssetID]
	log.Printf("Wallet P-chain balance: %d nLUX (%.2f LUX)", pBalance, float64(pBalance)/1e9)

	if pBalance < 1_000_000_000 { // Need at least 1 LUX for tx fees
		log.Printf("P-chain balance low (%d nLUX). Checking X-chain...", pBalance)
		xBalanceMap, _ := fundWallet.X().Builder().GetFTBalance()
		xBalance := xBalanceMap[luxAssetID]
		log.Printf("X-chain balance: %d nLUX (%.2f LUX)", xBalance, float64(xBalance)/1e9)

		if xBalance > 0 {
			exportAmount := xBalance
			if exportAmount > 10_000_000_000 {
				exportAmount = 10_000_000_000 // Cap at 10 LUX
			}
			log.Printf("Exporting %d nLUX from X→P...", exportAmount)

			xChainID := fundWallet.X().Builder().Context().BlockchainID
			exportTx, err := fundWallet.X().IssueExportTx(
				constants.PlatformChainID,
				[]*lux.TransferableOutput{{
					Asset: lux.Asset{ID: luxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: exportAmount,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
				}},
			)
			if err != nil {
				log.Printf("X→P export failed: %v", err)
			} else {
				log.Printf("X→P export tx: %s", exportTx.ID())
				time.Sleep(3 * time.Second)

				importTx, err := fundWallet.P().IssueImportTx(
					xChainID,
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				)
				if err != nil {
					log.Printf("P-chain import failed: %v", err)
				} else {
					log.Printf("P-chain import tx: %s", importTx.ID())
					time.Sleep(3 * time.Second)

					// Re-sync wallet
					fundWallet, _ = primary.MakeWallet(ctx, &primary.WalletConfig{
						URI:         nc.URI,
						LUXKeychain: kc,
						EVMKeychain: kc,
					})
					pBalanceMap, _ = fundWallet.P().Builder().GetBalance()
					pBalance = pBalanceMap[luxAssetID]
					log.Printf("P-Chain balance after import: %d nLUX (%.2f LUX)", pBalance, float64(pBalance)/1e9)
				}
			}
		}
	}

	// Deploy each chain
	for _, chainName := range chains {
		genesisPath := filepath.Join(*stateDir, chainName+"-"+nc.Suffix, "genesis.json")
		genesisBytes, err := os.ReadFile(genesisPath)
		if err != nil {
			log.Printf("SKIP %s: %v", chainName, err)
			continue
		}

		var g map[string]interface{}
		if err := json.Unmarshal(genesisBytes, &g); err != nil {
			log.Fatalf("invalid genesis for %s: %v", chainName, err)
		}
		evmChainID := g["config"].(map[string]interface{})["chainId"]
		log.Printf("\n=== %s (EVM chainId: %v) ===", chainName, evmChainID)

		// Sync wallet
		log.Println("Syncing wallet...")
		wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
			URI:         nc.URI,
			LUXKeychain: kc,
			EVMKeychain: kc,
		})
		if err != nil {
			log.Fatalf("wallet sync failed: %v", err)
		}
		pWallet := wallet.P()

		// Create chain (network)
		log.Println("Creating chain...")
		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		}
		createNetTx, err := pWallet.IssueCreateNetworkTx(owner)
		if err != nil {
			log.Fatalf("create chain failed: %v", err)
		}
		networkID := createNetTx.ID()
		log.Printf("Network: %s", networkID)

		// Wait for tx acceptance (mainnet needs longer)
		log.Println("Waiting 10s for network tx acceptance...")
		time.Sleep(10 * time.Second)

		// Re-sync wallet with network tx (retry up to 5 times)
		var wallet2 primary.Wallet
		for attempt := 0; attempt < 5; attempt++ {
			wallet2, err = primary.MakeWallet(ctx, &primary.WalletConfig{
				URI:              nc.URI,
				LUXKeychain:      kc,
				EVMKeychain:      kc,
				PChainTxsToFetch: set.Of(networkID),
			})
			if err == nil {
				break
			}
			log.Printf("Wallet re-sync attempt %d failed: %v, retrying in 5s...", attempt+1, err)
			time.Sleep(5 * time.Second)
		}
		if err != nil {
			log.Fatalf("wallet re-sync failed after retries: %v", err)
		}

		// Create blockchain
		deployName := chainName
		if *chainNameOverride != "" {
			deployName = *chainNameOverride
		}
		log.Printf("Creating blockchain %s (P-chain name: %s)...", chainName, deployName)
		createChainTx, err := wallet2.P().IssueCreateChainTx(
			networkID,
			genesisBytes,
			evmVMID,
			nil,
			deployName,
		)
		if err != nil {
			log.Fatalf("create chain failed: %v", err)
		}
		blockchainID := createChainTx.ID()
		log.Printf("Blockchain: %s", blockchainID)

		// Add ALL validators to the chain network
		if !*skipValidators && len(nodeIDs) > 0 {
			time.Sleep(2 * time.Second)

			wallet3, err := primary.MakeWallet(ctx, &primary.WalletConfig{
				URI:              nc.URI,
				LUXKeychain:      kc,
				EVMKeychain:      kc,
				PChainTxsToFetch: set.Of(networkID),
			})
			if err != nil {
				log.Printf("WARNING: validator wallet sync failed: %v", err)
			} else {
				startTime := time.Now().Add(60 * time.Second)
				// Use primary validator end time minus 1 hour buffer, or 300 days
				endTime := startTime.Add(300 * 24 * time.Hour)
				if minPrimaryEnd > 0 {
					primaryEnd := time.Unix(int64(minPrimaryEnd), 0)
					safeEnd := primaryEnd.Add(-1 * time.Hour) // 1 hour buffer
					if safeEnd.Before(endTime) {
						endTime = safeEnd
					}
					log.Printf("Network validator: start=%s end=%s (primary ends %s)", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), primaryEnd.Format(time.RFC3339))
				}

				for _, nodeID := range nodeIDs {
					log.Printf("Adding validator %s to network %s...", nodeID, networkID)
					_, err := wallet3.P().IssueAddChainValidatorTx(&txs.ChainValidator{
						Validator: txs.Validator{
							NodeID: nodeID,
							Start:  uint64(startTime.Unix()),
							End:    uint64(endTime.Unix()),
							Wght:   20,
						},
						Chain: networkID,
					})
					if err != nil {
						log.Printf("WARNING: add validator %s failed: %v", nodeID, err)
					} else {
						log.Printf("Validator added: %s", nodeID)
					}
					time.Sleep(1 * time.Second) // Space out validator txs
				}
			}
		}

		fmt.Printf("\n--- %s %s ---\n", chainName, nc.Suffix)
		fmt.Printf("Network ID:    %s\n", networkID)
		fmt.Printf("Blockchain ID: %s\n", blockchainID)
		fmt.Printf("VM ID:         %s\n", evmVMID)
		fmt.Printf("EVM Chain ID:  %v\n", evmChainID)
		fmt.Println()
	}

	log.Println("Done!")
	log.Println("Next steps:")
	log.Println("  1. Update Helm values with new chain/blockchain IDs")
	log.Println("  2. Restart nodes to track new chains")
	log.Println("  3. Import RLP blocks if available")
}
