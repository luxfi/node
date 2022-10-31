// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/luxdefi/luxd/api/info"
	"github.com/luxdefi/luxd/codec"
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/utils/constants"
	"github.com/luxdefi/luxd/utils/rpc"
	"github.com/luxdefi/luxd/vms/avm"
	"github.com/luxdefi/luxd/vms/components/lux"
	"github.com/luxdefi/luxd/vms/platformvm"
	"github.com/luxdefi/luxd/vms/platformvm/txs"
	"github.com/luxdefi/luxd/wallet/chain/p"
	"github.com/luxdefi/luxd/wallet/chain/x"
)

const (
	MainnetAPIURI = "https://api.lux.network"
	FujiAPIURI    = "https://api.lux-test.network"
	LocalAPIURI   = "http://localhost:9650"

	fetchLimit = 1024
)

// TODO: refactor UTXOClient definition to allow the client implementations to
//       perform their own assertions.
var (
	_ UTXOClient = platformvm.Client(nil)
	_ UTXOClient = avm.Client(nil)
)

type UTXOClient interface {
	GetAtomicUTXOs(
		ctx context.Context,
		addrs []ids.ShortID,
		sourceChain string,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
}

func FetchState(ctx context.Context, uri string, addrs ids.ShortSet) (p.Context, x.Context, UTXOs, error) {
	infoClient := info.NewClient(uri)
	xClient := avm.NewClient(uri, "X")

	pCTX, err := p.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}

	xCTX, err := x.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}

	utxos := NewUTXOs()
	addrList := addrs.List()
	chains := []struct {
		id     ids.ID
		client UTXOClient
		codec  codec.Manager
	}{
		{
			id:     constants.PlatformChainID,
			client: platformvm.NewClient(uri),
			codec:  txs.Codec,
		},
		{
			id:     xCTX.BlockchainID(),
			client: xClient,
			codec:  x.Parser.Codec(),
		},
	}
	for _, destinationChain := range chains {
		for _, sourceChain := range chains {
			err = AddAllUTXOs(
				ctx,
				utxos,
				destinationChain.client,
				destinationChain.codec,
				sourceChain.id,
				destinationChain.id,
				addrList,
			)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}
	return pCTX, xCTX, utxos, nil
}

// AddAllUTXOs fetches all the UTXOs referenced by [addresses] that were sent
// from [sourceChainID] to [destinationChainID] from the [client]. It then uses
// [codec] to parse the returned UTXOs and it adds them into [utxos]. If [ctx]
// expires, then the returned error will be immediately reported.
func AddAllUTXOs(
	ctx context.Context,
	utxos UTXOs,
	client UTXOClient,
	codec codec.Manager,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	addrs []ids.ShortID,
) error {
	var (
		sourceChainIDStr = sourceChainID.String()
		startAddr        ids.ShortID
		startUTXO        ids.ID
	)
	for {
		utxosBytes, endAddr, endUTXO, err := client.GetAtomicUTXOs(
			ctx,
			addrs,
			sourceChainIDStr,
			fetchLimit,
			startAddr,
			startUTXO,
		)
		if err != nil {
			return err
		}

		for _, utxoBytes := range utxosBytes {
			var utxo lux.UTXO
			_, err := codec.Unmarshal(utxoBytes, &utxo)
			if err != nil {
				return err
			}

			if err := utxos.AddUTXO(ctx, sourceChainID, destinationChainID, &utxo); err != nil {
				return err
			}
		}

		if len(utxosBytes) < fetchLimit {
			break
		}

		// Update the vars to query the next page of UTXOs.
		startAddr = endAddr
		startUTXO = endUTXO
	}
	return nil
}
