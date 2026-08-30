// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"errors"
	"fmt"
	"net/http"

	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/container/linked"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/node/vms/xvm/txs"
	lux "github.com/luxfi/utxo"
)

type WalletService struct {
	vm         *VM
	pendingTxs *linked.Hashmap[ids.ID, *txs.Tx]
}

// update refreshes the UTXO set, removing spent UTXOs from pending transactions
func (w *WalletService) update(utxos []*lux.UTXO) ([]*lux.UTXO, error) {
	// Pending transaction filtering is handled at the mempool level;
	// UTXOs returned here may include those referenced by pending txs.
	return utxos, nil
}

func (w *WalletService) decided(txID ids.ID) {
	if !w.pendingTxs.Delete(txID) {
		return
	}

	w.vm.log.Info("tx decided over wallet API",
		log.Stringer("txID", txID),
	)
	for {
		txID, tx, ok := w.pendingTxs.Oldest()
		if !ok {
			return
		}

		err := w.vm.network.IssueTxFromRPCWithoutVerification(tx)
		if err == nil {
			w.vm.log.Info("issued tx to mempool over wallet API",
				log.Stringer("txID", txID),
			)
			return
		}
		if errors.Is(err, mempool.ErrDuplicateTx) {
			return
		}

		w.pendingTxs.Delete(txID)
		w.vm.log.Warn("dropping tx issued over wallet API",
			log.Stringer("txID", txID),
			log.String("error", err.Error()),
		)
	}
}

func (w *WalletService) issue(tx *txs.Tx) (ids.ID, error) {
	txID := tx.ID()
	w.vm.log.Info("issuing tx over wallet API",
		log.Stringer("txID", txID),
	)

	if _, ok := w.pendingTxs.Get(txID); ok {
		w.vm.log.Warn("issuing duplicate tx over wallet API",
			log.Stringer("txID", txID),
		)
		return txID, nil
	}

	if w.pendingTxs.Len() == 0 {
		if err := w.vm.network.IssueTxFromRPCWithoutVerification(tx); err == nil {
			w.vm.log.Info("issued tx to mempool over wallet API",
				log.Stringer("txID", txID),
			)
		} else if !errors.Is(err, mempool.ErrDuplicateTx) {
			w.vm.log.Warn("failed to issue tx over wallet API",
				log.Stringer("txID", txID),
				log.String("error", err.Error()),
			)
			return ids.Empty, err
		}
	} else {
		w.vm.log.Info("enqueueing tx over wallet API",
			log.Stringer("txID", txID),
		)
	}

	w.pendingTxs.Put(txID, tx)
	return txID, nil
}

// IssueTx attempts to issue a transaction into consensus
func (w *WalletService) IssueTx(_ *http.Request, args *apitypes.FormattedTx, reply *apitypes.JSONTxID) error {
	w.vm.log.Warn("deprecated API called",
		log.String("service", "wallet"),
		log.String("method", "issueTx"),
		log.String("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx, err := w.vm.parser.ParseTx(txBytes)
	if err != nil {
		return err
	}

	w.vm.Lock.Lock()
	defer w.vm.Lock.Unlock()

	txID, err := w.issue(tx)
	reply.TxID = txID
	return err
}
