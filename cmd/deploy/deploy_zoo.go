package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/net/primary"
	"github.com/luxfi/node/wallet/net/primary/common"
	"github.com/luxfi/node/wallet/net/primary/examples/keyutil"
	"github.com/luxfi/vm/secp256k1fx"
)

func main() {
	key := keyutil.MustLoadKey()
	uri := primary.LocalAPIURI
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))
	ownerAddr := key.Address()

	ctx := context.Background()

	// Fetch state
	luxAddrs := kc.Addresses()
	fmt.Printf("Wallet address: %v\n", luxAddrs.List())

	luxState, err := primary.FetchState(ctx, uri, luxAddrs)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}
	fmt.Printf("Network ID: %d\n", luxState.PCTX.NetworkID)
	fmt.Printf("LUX Asset ID: %s\n", luxState.PCTX.XAssetID)

	// Create wallet components
	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	pBackend := p.NewBackend(luxState.PCTX, pUTXOs, nil)
	pBuilder := builder.New(luxAddrs, luxState.PCTX, pBackend)
	pSigner := signer.New(kc, pBackend)

	// Step 1: Create a Net (chain)
	fmt.Println("\n=== Step 1: Creating Net (chain) ===")
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ownerAddr},
	}

	unsignedCreateNetTx, err := pBuilder.NewCreateChainTx(owner)
	if err != nil {
		log.Fatalf("failed to build CreateNet tx: %s\n", err)
	}
	fmt.Printf("Built CreateNet tx with %d inputs\n", len(unsignedCreateNetTx.Ins))

	// Use SignUnsigned to sign and wrap into *txs.Tx
	createNetTx, err := signer.SignUnsigned(ctx, pSigner, unsignedCreateNetTx)
	if err != nil {
		log.Fatalf("failed to sign CreateNet tx: %s\n", err)
	}
	netID := createNetTx.ID()
	fmt.Printf("Signed CreateNet tx: %s\n", netID)

	// Issue the CreateNet transaction
	issuedID, err := luxState.PClient.IssueTx(ctx, createNetTx.Bytes())
	if err != nil {
		log.Fatalf("failed to issue CreateNet tx: %s\n", err)
	}
	fmt.Printf("Issued CreateNet tx: %s\n", issuedID)

	// Wait for transaction to be accepted
	fmt.Println("Waiting for CreateNet tx to be accepted...")
	err = platformvm.AwaitTxAccepted(luxState.PClient, ctx, issuedID, 100*time.Millisecond)
	if err != nil {
		log.Fatalf("CreateNet tx failed: %s\n", err)
	}
	fmt.Printf("CreateNet tx accepted! Net ID: %s\n", netID)

	// Accept the tx locally to register the chain owner
	err = pBackend.AcceptTx(ctx, createNetTx)
	if err != nil {
		log.Fatalf("failed to accept CreateNet tx locally: %s\n", err)
	}
	fmt.Println("Registered Net owner locally")

	// Step 2: Create a blockchain on the Net
	fmt.Println("\n=== Step 2: Creating Zoo blockchain ===")

	// XSVM VM ID
	xsvmID := ids.ID{'x', 's', 'v', 'm'}

	// Genesis data for XSVM (minimal)
	genesisData := []byte(`{"timestamp": 0}`)

	// Recreate builder with updated state (after AcceptTx)
	pBuilder = builder.New(luxAddrs, luxState.PCTX, pBackend)
	pSigner = signer.New(kc, pBackend)

	unsignedCreateChainTx, err := pBuilder.NewCreateChainTx(
		netID,       // Net ID to create blockchain on
		genesisData, // Genesis data
		xsvmID,      // VM ID
		nil,         // No FX IDs
		"Zoo",       // Chain name
	)
	if err != nil {
		log.Fatalf("failed to build CreateChain tx: %s\n", err)
	}
	fmt.Printf("Built CreateChain tx with %d inputs\n", len(unsignedCreateChainTx.Ins))

	createChainTx, err := signer.SignUnsigned(ctx, pSigner, unsignedCreateChainTx)
	if err != nil {
		log.Fatalf("failed to sign CreateChain tx: %s\n", err)
	}
	chainID := createChainTx.ID()
	fmt.Printf("Signed CreateChain tx: %s\n", chainID)

	// Issue the CreateChain transaction
	issuedChainID, err := luxState.PClient.IssueTx(ctx, createChainTx.Bytes())
	if err != nil {
		log.Fatalf("failed to issue CreateChain tx: %s\n", err)
	}
	fmt.Printf("Issued CreateChain tx: %s\n", issuedChainID)

	// Wait for transaction to be accepted
	fmt.Println("Waiting for CreateChain tx to be accepted...")
	err = platformvm.AwaitTxAccepted(luxState.PClient, ctx, issuedChainID, 100*time.Millisecond)
	if err != nil {
		log.Fatalf("CreateChain tx failed: %s\n", err)
	}

	fmt.Printf("\n=== SUCCESS ===\n")
	fmt.Printf("Net ID: %s\n", netID)
	fmt.Printf("Zoo Chain ID: %s\n", chainID)
	fmt.Printf("\nZoo blockchain has been deployed!\n")
}
