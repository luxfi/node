// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/formatting/address"
	"github.com/luxfi/node/v2/utils/set"
	"github.com/luxfi/node/v2/wallet/chain/p/builder"
	"github.com/luxfi/node/v2/wallet/chain/p/wallet"
	"github.com/luxfi/node/v2/wallet/subnet/primary"
)

func main() {
	uri := primary.LocalAPIURI
	addrStr := "P-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	addr, err := address.ParseToID(addrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	addresses := set.Of(addr)

	ctx := context.Background()

	fetchStartTime := time.Now()
	state, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}
	log.Printf("fetched state of %s in %s\n", addrStr, time.Since(fetchStartTime))

	pUTXOs := primary.NewChainUTXOs(constants.PlatformChainID, state.UTXOs)
	pBackend := wallet.NewBackend(pUTXOs, nil)
	pBuilder := builder.New(addresses, state.PCTX, pBackend)

	currentBalances, err := pBuilder.GetBalance()
	if err != nil {
		log.Fatalf("failed to get the balance: %s\n", err)
	}

	luxID := state.PCTX.LUXAssetID
	luxBalance := currentBalances[luxID]
	log.Printf("current LUX balance of %s is %d nLUX\n", addrStr, luxBalance)
}
