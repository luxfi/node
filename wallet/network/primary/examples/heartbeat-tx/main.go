// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// heartbeat-tx submits one zero-value self-transfer EVM transaction per
// configured chain. Etna's "empty-block-production-off" feature only seals
// EVM blocks when a transaction arrives, so a single tx is enough to advance
// `eth_blockNumber` past 0x0 and prove the chain is alive.
//
// Designed to run from a Kubernetes CronJob:
//
//	heartbeat-tx \
//	  --chains=C,hanzo,zoo,pars,spc \
//	  --rpc-base=https://api.lux-test.network \
//	  --mnemonic-env=LUX_MNEMONIC \
//	  --bip44-idx=10
//
// EVM keys are derived at BIP44 m/44'/60'/0'/0/<idx> (Ethereum coin type 60).
// Index 10 is reserved for heartbeats — validators occupy 0-4 and subnet
// control keys occupy 5-7.
//
// One failed chain does not abort the run; each chain reports its own error.
// Exit code 0 if every chain accepted its tx, 1 otherwise.
package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethclient"

	luxbip32 "github.com/luxfi/go-bip32"
	luxbip39 "github.com/luxfi/go-bip39"
)

// deriveEVMKey returns the ECDSA key for the BIP44 path m/44'/60'/0'/0/idx
// (Ethereum coin type 60).
func deriveEVMKey(mnemonic string, idx uint32) (*ecdsa.PrivateKey, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	if !luxbip39.IsMnemonicValid(mnemonic) {
		return nil, errors.New("invalid BIP39 mnemonic")
	}
	seed := luxbip39.NewSeed(mnemonic, "")
	master, err := luxbip32.NewMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("master key: %w", err)
	}
	purpose, err := master.NewChildKey(luxbip32.FirstHardenedChild + 44)
	if err != nil {
		return nil, fmt.Errorf("purpose: %w", err)
	}
	coinType, err := purpose.NewChildKey(luxbip32.FirstHardenedChild + 60)
	if err != nil {
		return nil, fmt.Errorf("coin type: %w", err)
	}
	account, err := coinType.NewChildKey(luxbip32.FirstHardenedChild + 0)
	if err != nil {
		return nil, fmt.Errorf("account: %w", err)
	}
	change, err := account.NewChildKey(0)
	if err != nil {
		return nil, fmt.Errorf("change: %w", err)
	}
	child, err := change.NewChildKey(idx)
	if err != nil {
		return nil, fmt.Errorf("child idx %d: %w", idx, err)
	}
	return crypto.ToECDSA(child.Key)
}

// sendSignedTx builds, signs, and submits one EVM tx, returning the receipt
// block (or the latest block as a best-effort) and gasUsed.
func sendSignedTx(ctx context.Context, client *ethclient.Client, chainID *big.Int, from *ecdsa.PrivateKey, to common.Address, value *big.Int, waitFor time.Duration) (blockNum uint64, txHash common.Hash, gasUsed uint64, err error) {
	fromAddr := common.Address(crypto.PubkeyToAddress(from.PublicKey))
	nonce, err := client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return 0, common.Hash{}, 0, fmt.Errorf("pendingNonceAt: %w", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return 0, common.Hash{}, 0, fmt.Errorf("suggestGasPrice: %w", err)
	}
	// 25% headroom above suggested.
	gasPrice = new(big.Int).Div(new(big.Int).Mul(gasPrice, big.NewInt(125)), big.NewInt(100))

	const gasLimit uint64 = 21000
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    value,
		Gas:      gasLimit,
		GasPrice: gasPrice,
	})
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), from)
	if err != nil {
		return 0, common.Hash{}, 0, fmt.Errorf("signTx: %w", err)
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return 0, signed.Hash(), 0, fmt.Errorf("sendTransaction: %w", err)
	}
	txHash = signed.Hash()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		receipt, rErr := client.TransactionReceipt(ctx, txHash)
		if rErr == nil && receipt != nil {
			return receipt.BlockNumber.Uint64(), txHash, receipt.GasUsed, nil
		}
		time.Sleep(2 * time.Second)
	}
	blockNum, _ = client.BlockNumber(ctx)
	return blockNum, txHash, 0, fmt.Errorf("receipt not found within %s (tx accepted)", waitFor)
}

// heartbeatChain submits one zero-value self-transfer on the given chain.
// If seedKey is non-nil and the heartbeat key's balance is below seedFloor,
// it first sends `seedAmount` from seedKey to the heartbeat key and waits
// for that to confirm before issuing the heartbeat tx.
func heartbeatChain(ctx context.Context, chain, rpcBase string, key, seedKey *ecdsa.PrivateKey, seedAmount, seedFloor *big.Int, waitFor time.Duration) (prev, next uint64, txHash common.Hash, gasUsed uint64, err error) {
	url := fmt.Sprintf("%s/ext/bc/%s/rpc", strings.TrimRight(rpcBase, "/"), chain)
	client, err := ethclient.DialContext(ctx, url)
	if err != nil {
		return 0, 0, common.Hash{}, 0, fmt.Errorf("dial %s: %w", url, err)
	}
	defer client.Close()

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return 0, 0, common.Hash{}, 0, fmt.Errorf("chainId: %w", err)
	}

	// luxfi/crypto returns its own common.Address; geth/ethclient takes
	// geth/common.Address, which is `type Address luxfi/crypto/common.Address`,
	// so an explicit named-type conversion is all that's needed.
	addr := common.Address(crypto.PubkeyToAddress(key.PublicKey))

	prev, err = client.BlockNumber(ctx)
	if err != nil {
		return 0, 0, common.Hash{}, 0, fmt.Errorf("blockNumber: %w", err)
	}

	balance, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return prev, prev, common.Hash{}, 0, fmt.Errorf("balanceAt: %w", err)
	}

	if balance.Cmp(seedFloor) < 0 {
		if seedKey == nil {
			return prev, prev, common.Hash{}, 0, fmt.Errorf("account %s balance %s below floor %s on chain %s (chainId %s) and no seeder configured", addr.Hex(), balance, seedFloor, chain, chainID)
		}
		seedAddr := common.Address(crypto.PubkeyToAddress(seedKey.PublicKey))
		seedBal, err := client.BalanceAt(ctx, seedAddr, nil)
		if err != nil {
			return prev, prev, common.Hash{}, 0, fmt.Errorf("seed balanceAt: %w", err)
		}
		if seedBal.Cmp(seedAmount) < 0 {
			return prev, prev, common.Hash{}, 0, fmt.Errorf("seed account %s balance %s < seedAmount %s on chain %s", seedAddr.Hex(), seedBal, seedAmount, chain)
		}
		seedBlock, seedHash, _, err := sendSignedTx(ctx, client, chainID, seedKey, addr, seedAmount, waitFor)
		if err != nil {
			return prev, prev, common.Hash{}, 0, fmt.Errorf("seed tx: %w", err)
		}
		log.Printf("[%s] SEED %s -> %s amount=%s tx=%s block=0x%x", chain, seedAddr.Hex(), addr.Hex(), seedAmount, seedHash.Hex(), seedBlock)
	}

	next, txHash, gasUsed, err = sendSignedTx(ctx, client, chainID, key, addr, big.NewInt(0), waitFor)
	if err != nil {
		return prev, next, txHash, gasUsed, err
	}
	return prev, next, txHash, gasUsed, nil
}

func main() {
	var (
		chains         string
		rpcBase        string
		mnemonicEnv    string
		bipIdx         uint
		waitSecs       uint
		seedFromIdx    int
		seedAmountWei  string
		seedFloorWei   string
	)
	flag.StringVar(&chains, "chains", "C,hanzo,zoo,pars,spc", "comma-separated chain aliases under /ext/bc/<chain>/rpc")
	flag.StringVar(&rpcBase, "rpc-base", "", "RPC base URL, e.g. https://api.lux-test.network")
	flag.StringVar(&mnemonicEnv, "mnemonic-env", "LUX_MNEMONIC", "env var holding the BIP39 mnemonic")
	flag.UintVar(&bipIdx, "bip44-idx", 10, "BIP44 child index at m/44'/60'/0'/0/<idx>")
	flag.UintVar(&waitSecs, "wait-secs", 12, "seconds to wait for receipt per chain")
	flag.IntVar(&seedFromIdx, "seed-from-idx", -1, "if >= 0, top up heartbeat key from this BIP44 idx when balance falls below --seed-floor-wei")
	flag.StringVar(&seedAmountWei, "seed-amount-wei", "1000000000000000", "wei to send when seeding heartbeat key (default 0.001 LUX = ~4700 heartbeats at 25gwei*21000)")
	flag.StringVar(&seedFloorWei, "seed-floor-wei", "100000000000000", "if heartbeat key balance is below this, top up from seed key (default 0.0001 LUX)")
	flag.Parse()

	if rpcBase == "" {
		log.Fatal("--rpc-base is required")
	}
	mnemonic := strings.TrimSpace(os.Getenv(mnemonicEnv))
	if mnemonic == "" {
		log.Fatalf("env %s is empty", mnemonicEnv)
	}

	key, err := deriveEVMKey(mnemonic, uint32(bipIdx))
	if err != nil {
		log.Fatalf("derive key idx=%d: %v", bipIdx, err)
	}
	addr := common.Address(crypto.PubkeyToAddress(key.PublicKey))

	var seedKey *ecdsa.PrivateKey
	if seedFromIdx >= 0 {
		seedKey, err = deriveEVMKey(mnemonic, uint32(seedFromIdx))
		if err != nil {
			log.Fatalf("derive seed key idx=%d: %v", seedFromIdx, err)
		}
		seedAddr := common.Address(crypto.PubkeyToAddress(seedKey.PublicKey))
		log.Printf("[heartbeat-tx] seed_key_idx=%d seed_addr=%s", seedFromIdx, seedAddr.Hex())
	}

	seedAmount, ok := new(big.Int).SetString(seedAmountWei, 10)
	if !ok {
		log.Fatalf("invalid --seed-amount-wei: %q", seedAmountWei)
	}
	seedFloor, ok := new(big.Int).SetString(seedFloorWei, 10)
	if !ok {
		log.Fatalf("invalid --seed-floor-wei: %q", seedFloorWei)
	}

	log.Printf("[heartbeat-tx] rpc=%s key_idx=%d addr=%s", rpcBase, bipIdx, addr.Hex())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var failures int
	for _, c := range strings.Split(chains, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		prev, next, txHash, gasUsed, err := heartbeatChain(ctx, c, rpcBase, key, seedKey, seedAmount, seedFloor, time.Duration(waitSecs)*time.Second)
		if err != nil {
			failures++
			log.Printf("[%s] FAIL prev=0x%x tx=%s: %v", c, prev, txHash.Hex(), err)
			continue
		}
		log.Printf("[%s] OK 0x%x -> 0x%x tx=%s gasUsed=%d", c, prev, next, txHash.Hex(), gasUsed)
	}

	if failures > 0 {
		log.Printf("[heartbeat-tx] DONE with %d failures", failures)
		os.Exit(1)
	}
	log.Printf("[heartbeat-tx] DONE all chains OK")
}
