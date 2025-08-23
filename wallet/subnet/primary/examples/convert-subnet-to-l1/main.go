// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/keychain"
	"github.com/luxfi/node/wallet/subnet/primary"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)

	// Create adapter for the keychain
	adapter := keychain.NewLedgerAdapter(kc)
	subnetID := ids.FromStringOrPanic("2DeHa7Qb6sufPkmQcFWG2uCd4pBPv9WB6dkzroiMQhd1NSRtof")
	chainID := ids.FromStringOrPanic("E8nTR9TtRwfkS7XFjTYUYHENQ91mkPMtDUwwCeu7rNgBBtkqu")
	addressHex := ""
	weight := units.Schmeckle

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
	log.Printf("fetched node ID %s in %s\n", nodeID, time.Since(nodeInfoStartTime))

	validationID := subnetID.Append(0)
	conversionID, err := message.SubnetToL1ConversionID(message.SubnetToL1ConversionData{
		SubnetID:       subnetID,
		ManagerChainID: chainID,
		ManagerAddress: address,
		Validators: []message.SubnetToL1ConversionValidatorData{
			{
				NodeID:       nodeID.Bytes(),
				BLSPublicKey: nodePoP.PublicKey,
				Weight:       weight,
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to calculate conversionID: %s\n", err)
	}

	// MakeWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(
		ctx,
		&primary.WalletConfig{
			URI:         uri,
			LUXKeychain: adapter,
			EthKeychain: keychain.NewLedgerAdapter(secp256k1fx.NewKeychain()), // Empty ETH keychain
			PChainTxsToFetch: set.Of(subnetID),
		},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	convertSubnetToL1StartTime := time.Now()
	convertSubnetToL1Tx, err := wallet.P().IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		[]*txs.ConvertSubnetToL1Validator{
			{
				NodeID:                nodeID[:],
				Weight:                weight,
				Balance:               units.Lux,
				Signer:                *nodePoP,
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner:     message.PChainOwner{},
			},
		},
	)
	if err != nil {
		log.Fatalf("failed to issue subnet conversion transaction: %s\n", err)
	}
	log.Printf("converted subnet %s with transactionID %s, validationID %s, and conversionID %s in %s\n",
		subnetID,
		convertSubnetToL1Tx.ID(),
		validationID,
		conversionID,
		time.Since(convertSubnetToL1StartTime),
	)
}
