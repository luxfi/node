// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	gethcommon "github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethclient"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	_ "github.com/luxfi/node/vms/components/lux" // registers utxo.ParseUTXO ZAP dispatcher
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/x"
	"github.com/luxfi/rpc"
	"github.com/luxfi/sdk/info"
	lux "github.com/luxfi/utxo"

	ethcommon "github.com/luxfi/geth/common"
	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
	walletcommon "github.com/luxfi/node/wallet/network/primary/common"
)

// EthAccount represents an Ethereum account's state
type EthAccount struct {
	Balance *big.Int
	Nonce   uint64
}

const (
	MainnetAPIURI = "https://api.lux.network"
	TestnetAPIURI = "https://api.lux-test.network"
	LocalAPIURI   = "http://localhost:9630"

	fetchLimit = 1024
)

// perform their own assertions.
var (
	_ UTXOClient = (*platformvm.Client)(nil)
	_ UTXOClient = (*XClient)(nil)
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

// XClient is a client for interacting with the X-Chain
type XClient struct {
	requester rpc.EndpointRequester
}

// NewXClient returns a new X-Chain client
func NewXClient(uri, chainAlias string) *XClient {
	return &XClient{
		requester: rpc.NewEndpointRequester(
			fmt.Sprintf("%s/v1/bc/%s", uri, chainAlias),
		),
	}
}

// GetAtomicUTXOs implements UTXOClient.
// For local/dev networks where X-chain atomic operations aren't needed,
// this returns empty to allow P-chain operations to proceed.
func (c *XClient) GetAtomicUTXOs(
	ctx context.Context,
	addrs []ids.ShortID,
	sourceChain string,
	limit uint32,
	startAddress ids.ShortID,
	startUTXOID ids.ID,
	options ...rpc.Option,
) ([][]byte, ids.ShortID, ids.ID, error) {
	// Return empty for X-chain since atomic UTXOs are rarely needed
	return nil, ids.ShortEmpty, ids.Empty, nil
}

type LUXState struct {
	PClient *platformvm.Client
	PCTX    *pbuilder.Context
	XClient *XClient
	XCTX    *xbuilder.Context
	// CClient evm.Client // Implementation note
	// CCTX    *c.Context
	UTXOs walletcommon.UTXOs
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
	// cClient := evm.NewCChainClient(uri) // Implementation note

	pCTX, err := p.NewContextFromClients(ctx, infoClient, pClient)
	if err != nil {
		return nil, err
	}

	// Set network ID on pClient for proper bech32 address formatting
	pClient.SetNetworkID(pCTX.NetworkID)

	// X-chain fees are derived from the P-chain context. The fee values
	// here match the primary network defaults and are overridden at the
	// chain level when dynamic fees are enabled.
	utxoAssetID := pCTX.UTXOAssetID
	baseTxFee := uint64(1000000)         // 0.001 LUX
	createAssetTxFee := uint64(10000000) // 0.01 LUX

	// X-Chain is opt-in: post-coreth networks (e.g. test+dev primary that
	// bake only P + EVM genesis chains) run in P-only mode where the X
	// alias is not registered. We fail-soft to an empty XCTX in that case
	// — XCTX.BlockchainID == ids.Empty becomes a stable sentinel, and the
	// UTXO scan below treats it as "no X-chain UTXOs to fetch" because the
	// XClient.GetAtomicUTXOs returns empty. This lets MakeWallet succeed
	// in P-only mode without leaking X-chain queries into wallet callers.
	xCTX, err := x.NewContextFromClients(ctx, infoClient, utxoAssetID, baseTxFee, createAssetTxFee)
	if err != nil {
		// One specific class of failure is expected: "no ID with alias X".
		// Any other error (network, codec, etc.) is fatal and must surface.
		if !isXChainNotEnabled(err) {
			return nil, err
		}
		xCTX = &xbuilder.Context{
			NetworkID:        pCTX.NetworkID,
			BlockchainID:     ids.Empty, // sentinel: X-Chain disabled in this network
			UTXOAssetID:      utxoAssetID,
			BaseTxFee:        baseTxFee,
			CreateAssetTxFee: createAssetTxFee,
		}
	}

	// Create X-chain client using standard "X" alias
	xClient := NewXClient(uri, "X")

	// cCTX, err := c.NewContextFromClients(ctx, infoClient, xClient)
	// if err != nil {
	// 	return nil, err
	// }

	utxos := walletcommon.NewUTXOs()
	addrList := addrs.List()
	chains := []struct {
		id     ids.ID
		client UTXOClient
	}{
		{
			id:     constants.PlatformChainID,
			client: pClient,
		},
	}
	// Only include the X-chain entry when the network actually serves an
	// X-Chain. In P-only mode (xCTX.BlockchainID == ids.Empty) skipping
	// the pair avoids spurious `platform.getUTXOs` calls with sourceChain
	// = zero ID, which the P-chain API rejects.
	if xCTX.BlockchainID != ids.Empty {
		chains = append(chains, struct {
			id     ids.ID
			client UTXOClient
		}{
			id:     xCTX.BlockchainID,
			client: xClient,
		})
	}
	for _, destinationChain := range chains {
		for _, sourceChain := range chains {
			err = AddAllUTXOs(
				ctx,
				utxos,
				destinationChain.client,
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
		// CClient: cClient,
		// CCTX:    cCTX,
		UTXOs: utxos,
	}, nil
}

type EthState struct {
	Client   *ethclient.Client
	Accounts map[ethcommon.Address]*EthAccount
}

func FetchEthState(
	ctx context.Context,
	uri string,
	addrs set.Set[ethcommon.Address],
) (*EthState, error) {
	path := fmt.Sprintf(
		"%s/v1/%s/C/rpc",
		uri,
		constants.ChainAliasPrefix,
	)
	client, err := ethclient.Dial(path)
	if err != nil {
		return nil, err
	}

	accounts := make(map[ethcommon.Address]*EthAccount, addrs.Len())
	for addr := range addrs {
		// Convert ethereum address to geth address
		gethAddr := gethcommon.Address(addr)
		balance, err := client.BalanceAt(ctx, gethAddr, nil)
		if err != nil {
			return nil, err
		}
		nonce, err := client.NonceAt(ctx, gethAddr, nil)
		if err != nil {
			return nil, err
		}
		accounts[addr] = &EthAccount{
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
// from [sourceChainID] to [destinationChainID] from the [client] and adds
// them into [utxos]. Each UTXO is decoded by lux.ParseUTXO (ZAP wire). If
// [ctx] expires, the returned error is reported immediately.
func AddAllUTXOs(
	ctx context.Context,
	utxos walletcommon.UTXOs,
	client UTXOClient,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	addrs []ids.ShortID,
) error {
	var (
		// When source == destination, use empty string to fetch native UTXOs
		// Otherwise use the chain ID to fetch atomic UTXOs
		sourceChainIDStr string
		startAddr        ids.ShortID
		startUTXO        ids.ID
	)
	if sourceChainID != destinationChainID {
		sourceChainIDStr = sourceChainID.String()
	}
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
			u, err := lux.ParseUTXO(utxoBytes)
			if err != nil {
				return err
			}
			if err := utxos.AddUTXO(ctx, sourceChainID, destinationChainID, u); err != nil {
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

// isXChainNotEnabled detects the canonical "info.getBlockchainID returns
// no-such-alias for X" error that surfaces when running against a P-only
// network (one whose platform genesis does not include an XVM chain).
// We match by substring of the JSON-RPC error message rather than a typed
// sentinel because the upstream info-service emits a free-form string;
// re-evaluate this matcher if/when info.getBlockchainID gains a typed
// error path.
func isXChainNotEnabled(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "there is no id with alias: x") ||
		strings.Contains(msg, "no chain with alias")
}
