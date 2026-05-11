// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"sync"
	"time"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/txs/auth"
	txmempool "github.com/luxfi/node/vms/txs/mempool"
)

var (
	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool struct {
	txmempool.Mempool[*txs.Tx]

	// authMu guards the auth-policy fields. The policy is optional —
	// callers that haven't migrated their construction path get the
	// classical-compat default (admit everything). Once a chain pins a
	// non-nil profile via SetAuthPolicy, Add enforces it.
	authMu     sync.RWMutex
	authPolicy *config.ChainSecurityProfile
	authRegistry auth.ClassicalCompatRegistry
}

func New(namespace string, registerer metric.Registerer) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &Mempool{Mempool: pool}, nil
}

// SetAuthPolicy installs the chain's credential-admission policy. The
// profile pins whether classical secp256k1 credentials are admissible;
// the registry names the allow-list of legacy operators that may still
// post classical credentials under strict-PQ. Passing (nil, nil)
// disables the gate (the classical-compat path); the strict-PQ chain
// builder MUST install a non-nil profile.
//
// Safe to call once at chain construction; concurrent Add calls observe
// the new policy on their next entry.
func (m *Mempool) SetAuthPolicy(profile *config.ChainSecurityProfile, registry auth.ClassicalCompatRegistry) {
	m.authMu.Lock()
	m.authPolicy = profile
	m.authRegistry = registry
	m.authMu.Unlock()
}

// authPolicySnapshot returns the current (profile, registry) pair under
// a read lock. The result is a snapshot — concurrent SetAuthPolicy
// callers do not mutate it.
func (m *Mempool) authPolicySnapshot() (*config.ChainSecurityProfile, auth.ClassicalCompatRegistry) {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	return m.authPolicy, m.authRegistry
}

func (m *Mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	}

	if profile, registry := m.authPolicySnapshot(); profile != nil {
		// Originator is left zero here because P-chain txs do not bind
		// a single canonical "from" address — the input owners can name
		// many keys. Strict-PQ refusal of a classical Credential does
		// not depend on the originator (the registry path is the
		// allow-list); the chain builder MUST install a registry only
		// if the chain has a meaningful concept of legacy originator.
		if err := auth.EnforceCredentialPolicy(tx.Creds, profile, registry, ids.ShortEmpty); err != nil {
			return err
		}
	}
	return m.Mempool.Add(tx)
}

func (m *Mempool) HasTxs() bool {
	return m.Len() > 0
}

func (m *Mempool) Has(txID ids.ID) bool {
	_, exists := m.Get(txID)
	return exists
}

func (m *Mempool) PeekTxs(n int) []*txs.Tx {
	var result []*txs.Tx
	count := 0
	m.Iterate(func(tx *txs.Tx) bool {
		if count >= n {
			return false
		}
		result = append(result, tx)
		count++
		return true
	})
	return result
}

func (m *Mempool) DropExpiredStakerTxs(minStartTime time.Time) []ids.ID {
	var droppedTxIDs []ids.ID
	var txsToRemove []*txs.Tx

	m.Iterate(func(tx *txs.Tx) bool {
		// Check if this is a staker transaction
		switch stakerTx := tx.Unsigned.(type) {
		case *txs.AddValidatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddDelegatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddPermissionlessValidatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		case *txs.AddPermissionlessDelegatorTx:
			if stakerTx.StartTime().Before(minStartTime) {
				droppedTxIDs = append(droppedTxIDs, tx.ID())
				txsToRemove = append(txsToRemove, tx)
			}
		}
		return true
	})

	if len(txsToRemove) > 0 {
		m.Remove(txsToRemove...)
	}

	return droppedTxIDs
}

func (m *Mempool) Remove(txs ...*txs.Tx) {
	m.Mempool.Remove(txs...)
}
