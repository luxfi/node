// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"fmt"

	"github.com/luxfi/evm/ethclient"
	"github.com/luxfi/evm/plugin/evm/client"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/info"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/rpc"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/xvm"
	"github.com/luxfi/node/wallet"
	"github.com/luxfi/node/wallet/chain/c"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/x"

	ethcommon "github.com/luxfi/geth/common"
	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
)

const (
	MainnetAPIURI = "https://api.lux.network"
	TestnetAPIURI    = "https://api.lux-test.network"
	LocalAPIURI   = "http://localhost:9650"

	fetchLimit = 1024
)

var (
	_ UTXOClient = (*platformvm.Client)(nil)
	_ UTXOClient = (*xvm.Client)(nil)
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

type LUXState struct {
	PClient *platformvm.Client
	PCTX    *pbuilder.Context
	XClient *xvm.Client
	XCTX    *xbuilder.Context
	CClient client.Client
	CCTX    *c.Context
	UTXOs   wallet.UTXOs
}

func FetchState(
	ctx context.Context,
	uri string,
	addrs set.Set[ids.ShortID],
) (
	*LUXState,
	error,
) {
	infoClient := info.NewClient(uri)
	pClient := platformvm.NewClient(uri)
	xClient := xvm.NewClient(uri, "X")
	cClient := client.NewClient(uri, "C")

	pCTX, err := p.NewContextFromClients(ctx, infoClient, pClient)
	if err != nil {
		return nil, err
	}

	xCTX, err := x.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, err
	}

	cCTX, err := c.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, err
	}

	utxos := wallet.NewUTXOs()
	addrList := addrs.List()
	// Only P-chain and X-chain support atomic UTXOs
	chains := []struct {
		id     ids.ID
		client UTXOClient
		codec  codec.Manager
	}{
		{
			id:     constants.PlatformChainID,
			client: pClient,
			codec:  txs.Codec,
		},
		{
			id:     xCTX.BlockchainID,
			client: xClient,
			codec:  xbuilder.Parser.Codec(),
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
				return nil, err
			}
		}
	}
	return &LUXState{
		PClient: pClient,
		PCTX:    pCTX,
		XClient: xClient,
		XCTX:    xCTX,
		CClient: cClient,
		CCTX:    cCTX,
		UTXOs:   utxos,
	}, nil
}

func FetchPState(
	ctx context.Context,
	uri string,
	addrs set.Set[ids.ShortID],
) (
	*platformvm.Client,
	*pbuilder.Context,
	wallet.UTXOs,
	error,
) {
	infoClient := info.NewClient(uri)
	chainClient := platformvm.NewClient(uri)

	context, err := p.NewContextFromClients(ctx, infoClient, chainClient)
	if err != nil {
		return nil, nil, nil, err
	}

	utxos := wallet.NewUTXOs()
	addrList := addrs.List()
	err = AddAllUTXOs(
		ctx,
		utxos,
		chainClient,
		txs.Codec,
		constants.PlatformChainID,
		constants.PlatformChainID,
		addrList,
	)
	return chainClient, context, utxos, err
}

type EthState struct {
	Client   ethclient.Client
	Accounts map[ethcommon.Address]*c.Account
}

func FetchEthState(
	ctx context.Context,
	uri string,
	addrs set.Set[ethcommon.Address],
) (*EthState, error) {
	path := fmt.Sprintf(
		"%s/ext/%s/C/rpc",
		uri,
		constants.ChainAliasPrefix,
	)
	client, err := ethclient.Dial(path)
	if err != nil {
		return nil, err
	}

	accounts := make(map[ethcommon.Address]*c.Account, addrs.Len())
	for addr := range addrs {
		balance, err := client.BalanceAt(ctx, addr, nil)
		if err != nil {
			return nil, err
		}
		nonce, err := client.NonceAt(ctx, addr, nil)
		if err != nil {
			return nil, err
		}
		accounts[addr] = &c.Account{
			Balance: balance,
			Nonce:   nonce,
		}
	}
	return &EthState{
		Client:   client,
		Accounts: accounts,
	}, nil
}

// AddAllUTXOs fetches all the UTXOs referenced by [addresses] that were sent
// from [sourceChainID] to [destinationChainID] from the [client]. It then uses
// [codec] to parse the returned UTXOs and it adds them into [utxos]. If [ctx]
// expires, then the returned error will be immediately reported.
func AddAllUTXOs(
	ctx context.Context,
	utxos wallet.UTXOs,
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
