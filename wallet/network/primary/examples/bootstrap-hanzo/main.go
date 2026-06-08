// bootstrap-hanzo creates the hanzo chain on testnet/devnet
// using a BIP-44 m/44'/9000'/0'/0/<idx> key derived from MNEMONIC.
//
// Usage:
//
//	MNEMONIC="..." go run . --uri=http://localhost:19640 --network=testnet \
//	  --genesis=/Users/z/work/lux/genesis/configs/hanzo-testnet/genesis.json \
//	  --hrp=test --bip44-idx=5
//
// Deprecated post-chain-create flow: after CreateChainTx, this binary
// adds validators to the created network via the legacy
// IssueAddChainValidatorTx. Under LP-018 sovereign-L1, validators join
// a network via AddValidatorTx — chains live on networks, validators
// do not register per-chain. The legacy call is preserved for one
// release cycle for wire/codec compat with pre-LP-018 binaries.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/go-json-experiment/json"
	"log"
	"os"
	"strings"
	"time"

	luxbip32 "github.com/luxfi/go-bip32"
	luxbip39 "github.com/luxfi/go-bip39"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/sdk/info"
	"github.com/luxfi/sdk/platformvm"
	"github.com/luxfi/utxo/secp256k1fx"
)

// Default hanzo EVM VM ID (matches mainnet hanzo deployment).
const defaultHanzoVMID = "nyGCobireNhxFB7iM5bxV74hAY6j9nQX6wizxfWomnMMtztkr"

func deriveLuxKey(mnemonic string, idx uint32) (*secp256k1.PrivateKey, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	if !luxbip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid BIP39 mnemonic")
	}
	seed := luxbip39.NewSeed(mnemonic, "")
	master, err := luxbip32.NewMasterKey(seed)
	if err != nil {
		return nil, err
	}
	purpose, err := master.NewChildKey(luxbip32.FirstHardenedChild + 44)
	if err != nil {
		return nil, err
	}
	coinType, err := purpose.NewChildKey(luxbip32.FirstHardenedChild + 9000)
	if err != nil {
		return nil, err
	}
	account, err := coinType.NewChildKey(luxbip32.FirstHardenedChild + 0)
	if err != nil {
		return nil, err
	}
	change, err := account.NewChildKey(0)
	if err != nil {
		return nil, err
	}
	child, err := change.NewChildKey(idx)
	if err != nil {
		return nil, err
	}
	return secp256k1.ToPrivateKey(child.Key)
}

func formatPAddr(hrp string, a ids.ShortID) string {
	b32, err := address.FormatBech32(hrp, a[:])
	if err != nil {
		return ""
	}
	return "P-" + b32
}

func main() {
	uri := flag.String("uri", "", "luxd API URI (e.g. http://localhost:19640)")
	network := flag.String("network", "", "human label: testnet|devnet (for logs)")
	genesisPath := flag.String("genesis", "", "EVM genesis JSON for the hanzo chain")
	hrp := flag.String("hrp", "", "P-chain bech32 HRP: test|dev")
	bipIdx := flag.Uint("bip44-idx", 5, "BIP44 derivation index at m/44'/9000'/0'/0/<idx>")
	chainName := flag.String("chain-name", "hanzo", "chain name (don't change unless intentional)")
	skipValidators := flag.Bool("skip-validators", false, "skip adding primary validators to the new chain")
	vmIDStr := flag.String("vm-id", defaultHanzoVMID, "EVM VM ID present in luxd's --plugin-dir")
	existingNetID := flag.String("network-id", "", "if set, skip CreateNetworkTx and reuse this network ID for the CreateChainTx")
	flag.Parse()

	if *uri == "" || *genesisPath == "" || *hrp == "" {
		log.Fatal("--uri, --genesis, --hrp are required")
	}
	hanzoVMID, err := ids.FromString(*vmIDStr)
	if err != nil {
		log.Fatalf("invalid --vm-id: %v", err)
	}
	mnemonic := os.Getenv("MNEMONIC")
	if mnemonic == "" {
		log.Fatal("MNEMONIC env var required")
	}

	key, err2 := deriveLuxKey(mnemonic, uint32(*bipIdx))
	err = err2
	if err != nil {
		log.Fatalf("derive key: %v", err)
	}
	addr := key.PublicKey().Address()
	log.Printf("[%s] derived key at m/44'/9000'/0'/0/%d -> %s", *network, *bipIdx, formatPAddr(*hrp, addr))

	genesisBytes, err := os.ReadFile(*genesisPath)
	if err != nil {
		log.Fatalf("read genesis: %v", err)
	}
	var g map[string]any
	if err := json.Unmarshal(genesisBytes, &g); err != nil {
		log.Fatalf("invalid genesis json: %v", err)
	}
	cfg, _ := g["config"].(map[string]any)
	if cfg == nil {
		log.Fatalf("genesis has no .config")
	}
	evmChainID, _ := cfg["chainId"]
	log.Printf("[%s] genesis EVM chainId = %v", *network, evmChainID)

	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	ctx := context.Background()

	// Sanity: print connected node ID and primary validators
	infoClient := info.NewClient(*uri)
	myNodeID, _, err := infoClient.GetNodeID(ctx)
	if err == nil {
		log.Printf("[%s] connected to node %s", *network, myNodeID)
	}

	pClient := platformvm.NewClient(*uri)
	balResp, err := pClient.GetBalance(ctx, []ids.ShortID{addr})
	if err != nil {
		log.Fatalf("getBalance: %v", err)
	}
	log.Printf("[%s] P-chain balance of control key: %+v", *network, balResp)

	var nodeIDs []ids.NodeID
	var minPrimaryEnd uint64
	if !*skipValidators {
		validators, err := pClient.GetCurrentValidators(ctx, ids.Empty, nil)
		if err != nil {
			log.Printf("WARN: GetCurrentValidators failed: %v (skipping validators)", err)
			*skipValidators = true
		} else {
			for _, v := range validators {
				nodeIDs = append(nodeIDs, v.NodeID)
				if minPrimaryEnd == 0 || v.EndTime < minPrimaryEnd {
					minPrimaryEnd = v.EndTime
				}
			}
			log.Printf("[%s] discovered %d primary validators (min end = %d)", *network, len(nodeIDs), minPrimaryEnd)
		}
	}

	w, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         *uri,
		LUXKeychain: kc,
		EVMKeychain: kc,
	})
	if err != nil {
		log.Fatalf("wallet sync: %v", err)
	}

	utxoAssetID := w.X().Builder().Context().UTXOAssetID
	pBal, err := w.P().Builder().GetBalance()
	if err != nil {
		log.Fatalf("P balance: %v", err)
	}
	log.Printf("[%s] wallet P-chain LUX = %d nLUX", *network, pBal[utxoAssetID])
	if pBal[utxoAssetID] < 1_500_000_000 { // 1.5 LUX min: createNetwork ~1 LUX + createChain ~0.5 LUX
		log.Fatalf("insufficient P-chain balance, need >= 1.5 LUX, have %d nLUX", pBal[utxoAssetID])
	}

	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}

	var netID ids.ID
	if *existingNetID != "" {
		netID, err = ids.FromString(*existingNetID)
		if err != nil {
			log.Fatalf("invalid --network-id: %v", err)
		}
		log.Printf("[%s] reusing existing network ID = %s (skipping CreateNetworkTx)", *network, netID)
	} else {
		log.Printf("[%s] IssueCreateNetworkTx ...", *network)
		createNetTx, err := w.P().IssueCreateNetworkTx(owner)
		if err != nil {
			log.Fatalf("create network: %v", err)
		}
		netID = createNetTx.ID()
		log.Printf("[%s] network ID = %s", *network, netID)
		time.Sleep(10 * time.Second)
	}

	var w2 primary.Wallet
	for i := 0; i < 5; i++ {
		w2, err = primary.MakeWallet(ctx, &primary.WalletConfig{
			URI:              *uri,
			LUXKeychain:      kc,
			EVMKeychain:      kc,
			PChainTxsToFetch: set.Of(netID),
		})
		if err == nil {
			break
		}
		log.Printf("re-sync wallet attempt %d: %v", i+1, err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Fatalf("wallet re-sync failed: %v", err)
	}

	log.Printf("[%s] IssueCreateChainTx (vmID=%s, name=%q) ...", *network, hanzoVMID, *chainName)
	createChainTx, err := w2.P().IssueCreateChainTx(netID, genesisBytes, hanzoVMID, nil, *chainName)
	if err != nil {
		log.Fatalf("create chain: %v", err)
	}
	bcID := createChainTx.ID()
	log.Printf("[%s] blockchain ID = %s", *network, bcID)

	if !*skipValidators && len(nodeIDs) > 0 {
		time.Sleep(3 * time.Second)
		w3, err := primary.MakeWallet(ctx, &primary.WalletConfig{
			URI:              *uri,
			LUXKeychain:      kc,
			EVMKeychain:      kc,
			PChainTxsToFetch: set.Of(netID),
		})
		if err != nil {
			log.Printf("WARN: validator wallet sync failed: %v", err)
		} else {
			startTime := time.Now().Add(60 * time.Second)
			endTime := startTime.Add(300 * 24 * time.Hour)
			if minPrimaryEnd > 0 {
				primaryEnd := time.Unix(int64(minPrimaryEnd), 0)
				safe := primaryEnd.Add(-1 * time.Hour)
				if safe.Before(endTime) {
					endTime = safe
				}
			}
			for _, nid := range nodeIDs {
				_, err := w3.P().IssueAddChainValidatorTx(&txs.ChainValidator{
					Validator: txs.Validator{
						NodeID: nid,
						Start:  uint64(startTime.Unix()),
						End:    uint64(endTime.Unix()),
						Wght:   20,
					},
					Chain: netID,
				})
				if err != nil {
					log.Printf("WARN add validator %s: %v", nid, err)
				} else {
					log.Printf("[%s] added chain validator: %s", *network, nid)
				}
				time.Sleep(1 * time.Second)
			}
		}
	}

	fmt.Println()
	fmt.Printf("---- %s hanzo chain bootstrap COMPLETE ----\n", *network)
	fmt.Printf("NETWORK_ID=%s\n", netID)
	fmt.Printf("BLOCKCHAIN_ID=%s\n", bcID)
	fmt.Printf("VM_ID=%s\n", hanzoVMID)
	fmt.Printf("EVM_CHAIN_ID=%v\n", evmChainID)
	fmt.Printf("CONTROL_KEY=%s\n", formatPAddr(*hrp, addr))
	fmt.Println()
}
