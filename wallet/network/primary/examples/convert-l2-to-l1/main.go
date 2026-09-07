// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	apiinfo "github.com/luxfi/api/info"
	"github.com/luxfi/constants"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/wallet/network/primary"
	"github.com/luxfi/node/wallet/network/primary/examples/keyutil"
	"github.com/luxfi/sdk/info"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func main() {
	key := keyutil.MustLoadKey()
	uri := primary.LocalAPIURI
	kc := primary.NewKeychainAdapter(secp256k1fx.NewKeychain(key))

	// Create adapter for the keychain
	netID := ids.FromStringOrPanic("2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof")
	chainID := ids.FromStringOrPanic("E8nTR9TtRwfkS7XFjTYUYHENQ91mkPMtDUwwCeu7rNgBBtkqu")
	addressHex := ""
	weight := constants.Schmeckle

	address, err := hex.DecodeString(addressHex)
	if err != nil {
		log.Fatalf("failed to decode address %q: %s\n", addressHex, err)
	}

	ctx := context.Background()
	infoClient := info.NewClient(uri)

	nodeInfoStartTime := time.Now()
	nodeID, nodePoP, err := infoClient.GetNodeID(ctx)
	if err != nil {
		log.Fatalf("failed to fetch node IDs: %s\n", err)
	}
	pop, err := parseProofOfPossession(nodePoP)
	if err != nil {
		log.Fatalf("failed to parse node proof of possession: %s\n", err)
	}
	log.Printf("fetched node ID %s in %s\n", nodeID, time.Since(nodeInfoStartTime))

	validationID := netID.Append(0)
	conversionID, err := message.ChainToL1ConversionID(message.ChainToL1ConversionData{
		ChainID:        netID,
		ManagerChainID: chainID,
		ManagerAddress: address,
		Validators: []message.ChainToL1ConversionValidatorData{
			{
				NodeID:       nodeID.Bytes(),
				BLSPublicKey: pop.PublicKey,
				Weight:       weight,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to calculate conversionID: %s\n", err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting and registers [netID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(
		ctx,
		&primary.WalletConfig{
			URI:              uri,
			LUXKeychain:      kc,
			EVMKeychain:      kc,
			PChainTxsToFetch: set.Of(netID),
		},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	convertNetToL1StartTime := time.Now()
	// NOTE: The P-Chain wallet builder does not expose a ConvertNetwork issue
	// path — the legacy ConvertNetworkToL1Tx (and its wallet Issue method) was
	// removed in the native-ZAP migration. This example now constructs the
	// unsigned promote tx directly via the native txs constructor to document
	// the new shape; a runnable flow needs a wallet builder that sources a
	// funded *lux.BaseTx (UTXO selection) and signs it.
	_ = wallet
	sec := security.Mode{Admission: security.Gated, Manager: security.PChain}
	validators := []*txs.NetworkValidator{
		{
			NodeID:  nodeID[:],
			Weight:  weight,
			Balance: constants.Lux,
			Signer:  *pop,
		},
	}
	convertNetTx, err := txs.NewConvertNetworkTx(
		&lux.BaseTx{},
		netID,                      // network being promoted
		constants.PrimaryNetworkID, // new parent (L1 ⇐ Primary)
		chainID,                    // chain hosting the manager
		sec,
		address,
		validators,
		&secp256k1fx.Input{}, // existing-owner authorization (unsigned here)
	)
	if err != nil {
		log.Fatalf("failed to build convert network tx: %s\n", err)
	}
	log.Printf("built unsigned convert-network tx (%d bytes) promoting net %s, validationID %s, conversionID %s in %s\n",
		len(convertNetTx.Bytes()),
		netID,
		validationID,
		conversionID,
		time.Since(convertNetToL1StartTime),
	)
}

func parseProofOfPossession(pop *apiinfo.ProofOfPossession) (*signer.ProofOfPossession, error) {
	if pop == nil {
		return nil, fmt.Errorf("missing proof of possession")
	}
	pkBytes, err := formatting.Decode(formatting.HexNC, pop.PublicKey)
	if err != nil {
		return nil, err
	}
	sigBytes, err := formatting.Decode(formatting.HexNC, pop.ProofOfPossession)
	if err != nil {
		return nil, err
	}
	var out signer.ProofOfPossession
	if len(pkBytes) != len(out.PublicKey) || len(sigBytes) != len(out.ProofOfPossession) {
		return nil, fmt.Errorf("unexpected proof of possession sizes")
	}
	copy(out.PublicKey[:], pkBytes)
	copy(out.ProofOfPossession[:], sigBytes)
	return &out, nil
}
