// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/log"

	"github.com/luxfi/database"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/antithesis"
	"github.com/luxfi/node/tests/fixture/e2e"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/exchangevm"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	timerpkg "github.com/luxfi/node/utils/timer"
	xtxs "github.com/luxfi/node/vms/exchangevm/txs"
	ptxs "github.com/luxfi/node/vms/platformvm/txs"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
)

const NumKeys = 5

func main() {
	// TODO(marun) Support choosing the log format
	tc := tests.NewTestContext(tests.NewDefaultLogger(""))
	defer tc.Cleanup()
	require := require.New(tc)

	c := antithesis.NewConfig(
		tc,
		&tmpnet.Network{
			Owner: "antithesis-node",
		},
	)
	ctx := tests.DefaultNotifyContext(c.Duration, tc.DeferCleanup)

	kc := secp256k1fx.NewKeychain(genesis.EWOQKey)
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakeWallet(
		ctx,
		c.URIs[0],
		kc,
		kc,
		primary.WalletConfig{},
	)
	require.NoError(err, "failed to initialize wallet")
	tc.Log().Info("synced wallet",
		log.Duration("duration", time.Since(walletSyncStartTime)),
	)

	genesisWorkload := &workload{
		id:     0,
		log:    tests.NewDefaultLogger(fmt.Sprintf("worker %d", 0)),
		wallet: wallet,
		addrs:  set.Of(genesis.EWOQKey.Address()),
		uris:   c.URIs,
	}

	workloads := make([]*workload, NumKeys)
	workloads[0] = genesisWorkload

	var (
		genesisXWallet  = wallet.X()
		genesisXBuilder = genesisXWallet.Builder()
		genesisXContext = genesisXBuilder.Context()
		luxAssetID     = genesisXContext.XAssetID
	)
	for i := 1; i < NumKeys; i++ {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(err, "failed to generate key")

		var (
			addr          = key.Address()
			baseStartTime = time.Now()
		)
		baseTx, err := genesisXWallet.IssueBaseTx([]*lux.TransferableOutput{{
			Asset: lux.Asset{
				ID: luxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100 * units.KiloLux,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						addr,
					},
				},
			},
		}})
		require.NoError(err, "failed to issue initial funding X-chain baseTx")
		tc.Log().Info("issued initial funding X-chain baseTx",
			log.Stringer("txID", baseTx.ID()),
			log.Duration("duration", time.Since(baseStartTime)),
		)

		require.NoError(genesisWorkload.confirmXChainTx(ctx, baseTx), "failed to confirm initial funding X-chain baseTx")

		uri := c.URIs[i%len(c.URIs)]
		kc := secp256k1fx.NewKeychain(key)
		walletSyncStartTime := time.Now()
		wallet, err := primary.MakeWallet(
			ctx,
			uri,
			kc,
			kc,
			primary.WalletConfig{},
		)
		require.NoError(err, "failed to initialize wallet")
		tc.Log().Info("synced wallet",
			log.Duration("duration", time.Since(walletSyncStartTime)),
		)

		workloads[i] = &workload{
			id:     i,
			log:    tests.NewDefaultLogger(fmt.Sprintf("worker %d", i)),
			wallet: wallet,
			addrs:  set.Of(addr),
			uris:   c.URIs,
		}
	}

	lifecycle.SetupComplete(map[string]any{
		"msg":        "initialized workers",
		"numWorkers": NumKeys,
	})

	for _, w := range workloads[1:] {
		go w.run(ctx)
	}
	genesisWorkload.run(ctx)
}

type workload struct {
	id     int
	log    log.Logger
	wallet *primary.Wallet
	addrs  set.Set[ids.ShortID]
	uris   []string
}

func (w *workload) run(ctx context.Context) {
	timer := timerpkg.StoppedTimer()

	tc := tests.NewTestContext(w.log)
	defer tc.Cleanup()
	require := require.New(tc)

	xLUX, pLUX := e2e.GetWalletBalances(tc, w.wallet)
	assert.Reachable("wallet starting", map[string]any{
		"worker":   w.id,
		"xBalance": xLUX,
		"pBalance": pLUX,
	})

	for {
		val, err := rand.Int(rand.Reader, big.NewInt(5))
		require.NoError(err, "failed to read randomness")

		flowID := val.Int64()
		w.log.Info("executing test",
			log.Int("workerID", w.id),
			log.Int64("flowID", flowID),
		)
		switch flowID {
		case 0:
			w.issueXChainBaseTx(ctx)
		case 1:
			w.issueXChainCreateAssetTx(ctx)
		case 2:
			w.issueXChainOperationTx(ctx)
		case 3:
			w.issueXToPTransfer(ctx)
		case 4:
			w.issuePToXTransfer(ctx)
		}

		val, err = rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
		require.NoError(err, "failed to read randomness")

		timer.Reset(time.Duration(val.Int64()))
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// executeTest executes a test at random.
func (w *workload) executeTest(ctx context.Context) {
	tc := w.newTestContext(ctx)
	// Panics will be recovered without being rethrown, ensuring that test failures are not fatal.
	defer tc.Recover()
	require := require.New(tc)

	// Ensure this value matches the number of tests + 1 to offset
	// 0-based + 1 for sleep case in the switch statement for flowID
	testCount := int64(6)

	val, err := rand.Int(rand.Reader, big.NewInt(testCount))
	require.NoError(err, "failed to read randomness")

	flowID := val.Int64()
	switch flowID {
	case 0:
		// TODO(marun) Create abstraction for a test that supports a name e.g. `aTest{name: "foo", mytestfunc}`
		w.log.Info("executing issueXChainBaseTx")
		w.issueXChainBaseTx(ctx)
	case 1:
		w.log.Info("executing issueXChainCreateAssetTx")
		w.issueXChainCreateAssetTx(ctx)
	case 2:
		w.log.Info("executing issueXChainOperationTx")
		w.issueXChainOperationTx(ctx)
	case 3:
		w.log.Info("executing issueXToPTransfer")
		w.issueXToPTransfer(ctx)
	case 4:
		w.log.Info("executing issuePToXTransfer")
		w.issuePToXTransfer(ctx)
	case 5:
		w.log.Info("sleeping")
	}

	// TODO(marun) Enable execution of the banff e2e test as part of https://github.com/luxfi/node/issues/4049
	// w.log.Info("executing banff.TestCustomAssetTransfer")
	// addr, _ := w.addrs.Peek()
	// banff.TestCustomAssetTransfer(tc, *w.wallet, addr)
}

func (w *workload) issueXChainBaseTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			log.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		luxAssetID   = xContext.XAssetID
		luxBalance   = balances[luxAssetID]
		baseTxFee     = xContext.BaseTxFee
		neededBalance = baseTxFee + units.Schmeckle
	)
	if luxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			log.Uint64("balance", luxBalance),
			log.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner         = w.makeOwner()
		baseStartTime = time.Now()
	)
	baseTx, err := xWallet.IssueBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{
					ID: luxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt:          units.Schmeckle,
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain baseTx",
			log.Error(err),
		)
		return
	}
	w.log.Info("issued new X-chain baseTx",
		log.Stringer("txID", baseTx.ID()),
		log.Duration("duration", time.Since(baseStartTime)),
	)

	if err := w.confirmXChainTx(ctx, baseTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "base"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, baseTx)
}

func (w *workload) issueXChainCreateAssetTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			log.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		luxAssetID   = xContext.XAssetID
		luxBalance   = balances[luxAssetID]
		neededBalance = xContext.CreateAssetTxFee
	)
	if luxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			log.Uint64("balance", luxBalance),
			log.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner                = w.makeOwner()
		createAssetStartTime = time.Now()
	)
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"HI",
		"HI",
		1,
		map[uint32][]verify.State{
			0: {
				&secp256k1fx.TransferOutput{
					Amt:          units.Schmeckle,
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain create asset transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created new X-chain asset",
		log.Stringer("txID", createAssetTx.ID()),
		log.Duration("duration", time.Since(createAssetStartTime)),
	)

	if err := w.confirmXChainTx(ctx, createAssetTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "createAsset"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, createAssetTx)
}

func (w *workload) issueXChainOperationTx(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			log.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext         = xBuilder.Context()
		luxAssetID      = xContext.XAssetID
		luxBalance      = balances[luxAssetID]
		createAssetTxFee = xContext.CreateAssetTxFee
		baseTxFee        = xContext.BaseTxFee
		neededBalance    = createAssetTxFee + baseTxFee
	)
	if luxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			log.Uint64("balance", luxBalance),
			log.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner                = w.makeOwner()
		createAssetStartTime = time.Now()
	)
	createAssetTx, err := xWallet.IssueCreateAssetTx(
		"HI",
		"HI",
		1,
		map[uint32][]verify.State{
			2: {
				&propertyfx.MintOutput{
					OutputOwners: owner,
				},
			},
		},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain create asset transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created new X-chain asset",
		log.Stringer("txID", createAssetTx.ID()),
		log.Duration("duration", time.Since(createAssetStartTime)),
	)

	operationStartTime := time.Now()
	operationTx, err := xWallet.IssueOperationTxMintProperty(
		createAssetTx.ID(),
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain operation transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("issued X-chain operation transaction",
		log.Stringer("txID", operationTx.ID()),
		log.Duration("duration", time.Since(operationStartTime)),
	)

	if err := w.confirmXChainTx(ctx, createAssetTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "createAsset"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, createAssetTx)

	if err := w.confirmXChainTx(ctx, operationTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "operation"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, operationTx)
}

func (w *workload) issueXToPTransfer(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		pWallet  = w.wallet.P()
		xBuilder = xWallet.Builder()
	)
	balances, err := xBuilder.GetFTBalance()
	if err != nil {
		w.log.Error("failed to fetch X-chain balances",
			log.Error(err),
		)
		assert.Unreachable("failed to fetch X-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		luxAssetID   = xContext.XAssetID
		luxBalance   = balances[luxAssetID]
		xBaseTxFee    = xContext.BaseTxFee
		neededBalance = xBaseTxFee + units.Lux
	)
	if luxBalance < neededBalance {
		w.log.Info("skipping X-chain tx issuance due to insufficient balance",
			log.Uint64("balance", luxBalance),
			log.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		owner           = w.makeOwner()
		exportStartTime = time.Now()
	)
	exportTx, err := xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*lux.TransferableOutput{{
			Asset: lux.Asset{
				ID: luxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Lux,
			},
		}},
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain export transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created X-chain export transaction",
		log.Stringer("txID", exportTx.ID()),
		log.Duration("duration", time.Since(exportStartTime)),
	)

	var (
		xChainID        = xContext.BlockchainID
		importStartTime = time.Now()
	)
	importTx, err := pWallet.IssueImportTx(
		xChainID,
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue P-chain import transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created P-chain import transaction",
		log.Stringer("txID", importTx.ID()),
		log.Duration("duration", time.Since(importStartTime)),
	)

	if err := w.confirmXChainTx(ctx, exportTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "export"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, exportTx)

	if err := w.confirmPChainTx(ctx, importTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "P"),
			log.String("txType", "import"),
			log.Error(err),
		)
		return
	}

	w.verifyPChainTxConsumedUTXOs(ctx, importTx)
}

func (w *workload) issuePToXTransfer(ctx context.Context) {
	var (
		xWallet  = w.wallet.X()
		pWallet  = w.wallet.P()
		xBuilder = xWallet.Builder()
		pBuilder = pWallet.Builder()
	)
	balances, err := pBuilder.GetBalance()
	if err != nil {
		w.log.Error("failed to fetch P-chain balances",
			log.Error(err),
		)
		assert.Unreachable("failed to fetch P-chain balances", map[string]any{
			"worker": w.id,
			"err":    err,
		})
		return
	}

	var (
		xContext      = xBuilder.Context()
		pContext      = pBuilder.Context()
		luxAssetID   = pContext.XAssetID
		luxBalance   = balances[luxAssetID]
		txFees        = xContext.BaseTxFee
		neededBalance = txFees + units.Schmeckle
	)
	if luxBalance < neededBalance {
		w.log.Info("skipping P-chain tx issuance due to insufficient balance",
			log.Uint64("balance", luxBalance),
			log.Uint64("neededBalance", neededBalance),
		)
		return
	}

	var (
		xChainID        = xContext.BlockchainID
		owner           = w.makeOwner()
		exportStartTime = time.Now()
	)
	exportTx, err := pWallet.IssueExportTx(
		xChainID,
		[]*lux.TransferableOutput{{
			Asset: lux.Asset{
				ID: luxAssetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Schmeckle,
			},
		}},
	)
	if err != nil {
		w.log.Warn("failed to issue P-chain export transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created P-chain export transaction",
		log.Stringer("txID", exportTx.ID()),
		log.Duration("duration", time.Since(exportStartTime)),
	)

	importStartTime := time.Now()
	importTx, err := xWallet.IssueImportTx(
		constants.PlatformChainID,
		&owner,
	)
	if err != nil {
		w.log.Warn("failed to issue X-chain import transaction",
			log.Error(err),
		)
		return
	}
	w.log.Info("created X-chain import transaction",
		log.Stringer("txID", importTx.ID()),
		log.Duration("duration", time.Since(importStartTime)),
	)

	if err := w.confirmPChainTx(ctx, exportTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "P"),
			log.String("txType", "export"),
			log.Error(err),
		)
		return
	}

	w.verifyPChainTxConsumedUTXOs(ctx, exportTx)

	if err := w.confirmXChainTx(ctx, importTx); err != nil {
		w.log.Warn("failed to confirm transaction",
			log.String("chain", "X"),
			log.String("txType", "import"),
			log.Error(err),
		)
		return
	}

	w.verifyXChainTxConsumedUTXOs(ctx, importTx)
}

func (w *workload) makeOwner() secp256k1fx.OutputOwners {
	addr, _ := w.addrs.Peek()
	return secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			addr,
		},
	}
}

func (w *workload) confirmXChainTx(ctx context.Context, tx *xtxs.Tx) error {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := avm.NewClient(uri, "X")
		if err := avm.AwaitTxAccepted(client, ctx, txID, 100*time.Millisecond); err != nil {
			return fmt.Errorf("failed to confirm X-chain transaction %s on %s: %w", txID, uri, err)
		}
		w.log.Info("confirmed X-chain transaction",
			log.Stringer("txID", txID),
			log.String("uri", uri),
		)
	}
	w.log.Info("confirmed X-chain transaction",
		log.Stringer("txID", txID),
	)
	return nil
}

func (w *workload) confirmPChainTx(ctx context.Context, tx *ptxs.Tx) error {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := platformvm.NewClient(uri)
		if err := platformvm.AwaitTxAccepted(client, ctx, txID, 100*time.Millisecond); err != nil {
			return fmt.Errorf("failed to confirm P-chain transaction %s on %s: %w", txID, uri, err)
		}
		w.log.Info("confirmed P-chain transaction",
			log.Stringer("txID", txID),
			log.String("uri", uri),
		)
	}
	w.log.Info("confirmed P-chain transaction",
		log.Stringer("txID", txID),
	)
	return nil
}

func (w *workload) verifyXChainTxConsumedUTXOs(ctx context.Context, tx *xtxs.Tx) {
	txID := tx.ID()
	chainID := w.wallet.X().Builder().Context().BlockchainID
	for _, uri := range w.uris {
		client := avm.NewClient(uri, "X")

		utxos := common.NewUTXOs()
		err := primary.AddAllUTXOs(
			ctx,
			utxos,
			client,
			xbuilder.Parser.Codec(),
			chainID,
			chainID,
			w.addrs.List(),
		)
		if err != nil {
			w.log.Warn("failed to fetch X-chain UTXOs",
				log.String("uri", uri),
				log.Error(err),
			)
			return
		}

		inputs := tx.Unsigned.InputIDs()
		for input := range inputs {
			_, err := utxos.GetUTXO(ctx, chainID, chainID, input)
			if err != database.ErrNotFound {
				w.log.Error("failed to verify that X-chain UTXO was deleted",
					log.String("uri", uri),
					log.Stringer("txID", txID),
					log.Stringer("utxoID", input),
					log.Error(err),
				)
				assert.Unreachable("failed to verify that X-chain UTXO was deleted", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txID,
					"utxoID": input,
					"err":    err,
				})
				return
			}
		}
		w.log.Info("confirmed all X-chain UTXOs consumed by tx are not present on node",
			log.Stringer("txID", txID),
			log.String("uri", uri),
		)
	}
	w.log.Info("confirmed all X-chain UTXOs consumed by tx are not present on all nodes",
		log.Stringer("txID", txID),
	)
}

func (w *workload) verifyPChainTxConsumedUTXOs(ctx context.Context, tx *ptxs.Tx) {
	txID := tx.ID()
	for _, uri := range w.uris {
		client := platformvm.NewClient(uri)

		utxos := common.NewUTXOs()
		err := primary.AddAllUTXOs(
			ctx,
			utxos,
			client,
			ptxs.Codec,
			constants.PlatformChainID,
			constants.PlatformChainID,
			w.addrs.List(),
		)
		if err != nil {
			w.log.Warn("failed to fetch P-chain UTXOs",
				log.String("uri", uri),
				log.Error(err),
			)
			return
		}

		inputs := tx.Unsigned.InputIDs()
		for input := range inputs {
			_, err := utxos.GetUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, input)
			if err != database.ErrNotFound {
				w.log.Error("failed to verify that P-chain UTXO was deleted",
					log.String("uri", uri),
					log.Stringer("txID", txID),
					log.Stringer("utxoID", input),
					log.Error(err),
				)
				assert.Unreachable("failed to verify that P-chain UTXO was deleted", map[string]any{
					"worker": w.id,
					"uri":    uri,
					"txID":   txID,
					"utxoID": input,
					"err":    err,
				})
				return
			}
		}
		w.log.Info("confirmed all P-chain UTXOs consumed by tx are not present on node",
			log.Stringer("txID", txID),
			log.String("uri", uri),
		)
	}
	w.log.Info("confirmed all P-chain UTXOs consumed by tx are not present on all nodes",
		log.Stringer("txID", txID),
	)
}
