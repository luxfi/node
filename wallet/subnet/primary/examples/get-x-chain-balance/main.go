// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/utils/formatting/address"
	"github.com/luxdefi/node/utils/set"
	"github.com/luxdefi/node/wallet/chain/x"
	"github.com/luxdefi/node/wallet/subnet/primary"
)

func main() {
	uri := primary.LocalAPIURI
	addrStr := "X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	addr, err := address.ParseToID(addrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	addresses := set.Set[ids.ShortID]{}
	addresses.Add(addr)

	ctx := context.Background()

	fetchStartTime := time.Now()
	_, xCtx, utxos, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}
	log.Printf("fetched state of %s in %s\n", addrStr, time.Since(fetchStartTime))

	xChainID := xCtx.BlockchainID()

	xUTXOs := primary.NewChainUTXOs(xChainID, utxos)
	xBackend := x.NewBackend(xCtx, xUTXOs)
	xBuilder := x.NewBuilder(addresses, xBackend)

	currentBalances, err := xBuilder.GetFTBalance()
	if err != nil {
		log.Fatalf("failed to get the balance: %s\n", err)
	}

	luxID := xCtx.LUXAssetID()
	luxBalance := currentBalances[luxID]
	log.Printf("current LUX balance of %s is %d nLUX\n", addrStr, luxBalance)
}
