// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package create

import (
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/keychain"
	"github.com/luxfi/node/wallet/subnet/primary"
	"github.com/luxfi/node/wallet/subnet/primary/common"
)

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "create",
		Short: "Creates a new chain",
		RunE:  createFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func createFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()
	kc := secp256k1fx.NewKeychain(config.PrivateKey)
	walletKC := keychain.NewWalletKeychain(kc)

	// NewWalletFromURI fetches the available UTXOs owned by [kc] on the network
	// that [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:              config.URI,
		LUXKeychain:      walletKC,
		EthKeychain:      walletKC,
		PChainTxsToFetch: set.Of(config.SubnetID),
	})
	if err != nil {
		return err
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
		Timestamp: 0,
		Allocations: []genesis.Allocation{
			{
				Address: config.Address,
				Balance: config.Balance,
			},
		},
	})
	if err != nil {
		return err
	}

	createChainStartTime := time.Now()
	createChainTxID, err := pWallet.IssueCreateChainTx(
		config.SubnetID,
		genesisBytes,
		constants.XSVMID,
		nil,
		config.Name,
		common.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	log.Printf("created chain %s in %s\n", createChainTxID, time.Since(createChainStartTime))
	return nil
}
